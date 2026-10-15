package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// Build a real v2 database without calling Open, which always upgrades to the
// newest version. authSchema is the unchanged schema shipped with version 2.
func createV2Database(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "v2.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT 'original'
	); INSERT INTO schema_migrations(version) VALUES (1), (2);` + authSchema + `
		INSERT INTO families VALUES (7, 'family', 100);
		INSERT INTO users VALUES (8, 7, 'admin', 'password-hash', 101);
		INSERT INTO web_sessions VALUES (X'1122', 8, 999);
		INSERT INTO pairing_codes VALUES ('pair', 7, X'3344', 998, 2, NULL);
		INSERT INTO devices VALUES ('device', 7, 'phone', X'5566', 102, NULL);
		INSERT INTO auth_limits VALUES ('limit', 103, 3);
	`); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUpgradeV2PreservesAuthDataAndSharingDefaults(t *testing.T) {
	path := createV2Database(t)
	for attempt := 0; attempt < 2; attempt++ {
		db, err := Open(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version IN (1,2,3)`).Scan(&count); err != nil || count != 3 {
			db.Close()
			t.Fatalf("want migrations 1,2,3: count=%d err=%v", count, err)
		}
		for _, tc := range []struct {
			query string
			want  int
		}{
			{`SELECT COUNT(*) FROM schema_migrations WHERE version IN (1,2) AND applied_at='original'`, 2},
			{`SELECT COUNT(*) FROM families WHERE id=7 AND name='family' AND created_at=100`, 1},
			{`SELECT COUNT(*) FROM users WHERE id=8 AND family_id=7 AND username='admin' AND password_hash='password-hash' AND created_at=101`, 1},
			{`SELECT COUNT(*) FROM web_sessions WHERE token_hash=X'1122' AND user_id=8 AND expires_at=999`, 1},
			{`SELECT COUNT(*) FROM pairing_codes WHERE id='pair' AND family_id=7 AND code_hash=X'3344' AND expires_at=998 AND attempts=2 AND consumed_at IS NULL`, 1},
			{`SELECT COUNT(*) FROM devices WHERE id='device' AND family_id=7 AND name='phone' AND token_hash=X'5566' AND created_at=102 AND revoked_at IS NULL AND sharing_session_id IS NULL AND last_sequence=0 AND last_captured_at IS NULL`, 1},
			{`SELECT COUNT(*) FROM auth_limits WHERE key='limit' AND window_start=103 AND count=3`, 1},
		} {
			if err := db.QueryRow(tc.query).Scan(&count); err != nil || count != tc.want {
				db.Close()
				t.Fatalf("data changed on open %d: %s: count=%d err=%v", attempt, tc.query, count, err)
			}
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestV3MigrationFailureRollsBackColumnsAndVersion(t *testing.T) {
	path := createV2Database(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE latest_locations (legacy_value TEXT); INSERT INTO latest_locations VALUES ('kept')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if migrated, err := Open(context.Background(), path); err == nil {
		migrated.Close()
		t.Fatal("expected v3 migration conflict")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, columns int
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != 2 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('devices') WHERE name IN ('sharing_session_id','last_sequence','last_captured_at')`).Scan(&columns); err != nil || columns != 0 {
		t.Fatalf("partial columns=%d err=%v", columns, err)
	}
	var value string
	if err := db.QueryRow(`SELECT legacy_value FROM latest_locations`).Scan(&value); err != nil || value != "kept" {
		t.Fatalf("legacy value=%q err=%v", value, err)
	}
	if _, err := db.Exec(`DROP TABLE latest_locations`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(context.Background(), path)
	if err != nil {
		t.Fatalf("retry after conflict resolution: %v", err)
	}
	defer db.Close()
}

func TestLatestLocationStorageConstraints(t *testing.T) {
	path := createV2Database(t)
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if db != nil {
			db.Close()
		}
	}()
	const insert = `INSERT INTO latest_locations(device_id,latitude,longitude,accuracy_m,battery_percent,captured_at,received_at,lease_expires_at) VALUES ('device',90,-180,0,NULL,1720000000001,1720000000002,1720000420002)`
	if _, err := db.Exec(insert); err != nil {
		t.Fatal(err)
	}
	var capturedAt, receivedAt, leaseExpiresAt int64
	var battery sql.NullInt64
	if err := db.QueryRow(`SELECT captured_at,received_at,lease_expires_at,battery_percent FROM latest_locations`).Scan(&capturedAt, &receivedAt, &leaseExpiresAt, &battery); err != nil || capturedAt != 1720000000001 || receivedAt != 1720000000002 || leaseExpiresAt != 1720000420002 || battery.Valid {
		t.Fatalf("millisecond times or nullable battery changed: %d %d %d %v err=%v", capturedAt, receivedAt, leaseExpiresAt, battery, err)
	}
	for _, tc := range []struct{ name, statement string }{
		{"duplicate device", insert},
		{"unknown device", `UPDATE latest_locations SET device_id='unknown'`},
		{"latitude low", `UPDATE latest_locations SET latitude=-90.001`},
		{"latitude high", `UPDATE latest_locations SET latitude=90.001`},
		{"longitude low", `UPDATE latest_locations SET longitude=-180.001`},
		{"longitude high", `UPDATE latest_locations SET longitude=180.001`},
		{"accuracy negative", `UPDATE latest_locations SET accuracy_m=-0.001`},
		{"battery low", `UPDATE latest_locations SET battery_percent=-1`},
		{"battery high", `UPDATE latest_locations SET battery_percent=101`},
		{"negative sequence", `UPDATE devices SET last_sequence=-1`},
		{"null sequence", `UPDATE devices SET last_sequence=NULL`},
		{"null latitude", `UPDATE latest_locations SET latitude=NULL`},
		{"null longitude", `UPDATE latest_locations SET longitude=NULL`},
		{"null accuracy", `UPDATE latest_locations SET accuracy_m=NULL`},
		{"null captured time", `UPDATE latest_locations SET captured_at=NULL`},
		{"null received time", `UPDATE latest_locations SET received_at=NULL`},
		{"null lease", `UPDATE latest_locations SET lease_expires_at=NULL`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := db.Exec(tc.statement); err == nil {
				t.Fatalf("invalid statement accepted: %s", tc.statement)
			}
		})
	}
	for _, statement := range []string{
		`UPDATE latest_locations SET latitude=-90,longitude=180,battery_percent=0`,
		`UPDATE latest_locations SET battery_percent=100`,
		`UPDATE devices SET sharing_session_id='session',last_sequence=2,last_captured_at=1720000000001`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_index_list('latest_locations') AS indexes JOIN pragma_index_info(indexes.name) AS columns ON columns.name='lease_expires_at' AND columns.seqno=0`).Scan(&count); err != nil || count < 1 {
		t.Fatalf("missing lease expiry index: count=%d err=%v", count, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM latest_locations AS l JOIN devices AS d ON l.device_id=d.id WHERE d.sharing_session_id='session' AND d.last_sequence=2 AND d.last_captured_at=1720000000001 AND l.latitude=-90 AND l.longitude=180 AND l.battery_percent=100 AND l.captured_at=1720000000001 AND l.received_at=1720000000002 AND l.lease_expires_at=1720000420002`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("location or watermark changed on reopen: count=%d err=%v", count, err)
	}
	if _, err := db.Exec(`DELETE FROM devices WHERE id='device'`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM latest_locations`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("location not deleted with device: count=%d err=%v", count, err)
	}
}
