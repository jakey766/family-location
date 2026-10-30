package storage

import (
	"context"
	"database/sql"
	"testing"
)

func TestV4MigrationPreservesDeviceAndAddsMembershipConfig(t *testing.T) {
	path := createV2Database(t)
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Exec(locationSchema + `INSERT INTO schema_migrations(version) VALUES(3);
 UPDATE devices SET sharing_session_id='kept-session',last_sequence=4,last_captured_at=1000;
 INSERT INTO latest_locations VALUES('device',31,121,10,NULL,1000,2000,422000);`)
	old.Close()
	if err != nil {
		t.Fatal(err)
	}
	var publicID string
	for i := 0; i < 2; i++ {
		db, err := Open(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		var id string
		if err = db.QueryRow(`SELECT public_id FROM families WHERE id=7`).Scan(&id); err != nil {
			db.Close()
			t.Fatal(err)
		}
		if len(id) < 12 || (i == 1 && id != publicID) {
			db.Close()
			t.Fatal("identifier not stable")
		}
		publicID = id
		for _, query := range []string{
			`SELECT COUNT(*) FROM family_admins WHERE family_id=7 AND user_id=8`,
			`SELECT COUNT(*) FROM family_devices WHERE family_id=7 AND device_id='device'`,
			`SELECT COUNT(*) FROM device_configs WHERE device_id='device' AND version=1 AND moving_interval_seconds=120 AND stationary_interval_seconds=300 AND moving_high_accuracy=0 AND quiet_hours_start='22:00' AND quiet_hours_end='08:00'`,
			`SELECT COUNT(*) FROM latest_locations l JOIN devices d ON d.id=l.device_id WHERE d.token_hash=X'5566' AND d.sharing_session_id='kept-session' AND d.last_sequence=4 AND l.captured_at=1000`,
		} {
			var n int
			if err = db.QueryRow(query).Scan(&n); err != nil || n != 1 {
				db.Close()
				t.Fatalf("migration query failed: %s n=%d err=%v", query, n, err)
			}
		}
		db.Close()
	}
}

func TestV4MigrationFailureRollsBackNewRelations(t *testing.T) {
	path := createV2Database(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(locationSchema + `INSERT INTO schema_migrations(version) VALUES(3); CREATE TABLE device_configs(legacy TEXT); INSERT INTO device_configs VALUES('kept');`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if upgraded, err := Open(context.Background(), path); err == nil {
		upgraded.Close()
		t.Fatal("expected schema conflict")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, tc := range []struct {
		query string
		want  int
	}{
		{`SELECT MAX(version) FROM schema_migrations`, 3},
		{`SELECT COUNT(*) FROM pragma_table_info('families') WHERE name='public_id'`, 0},
		{`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('family_admins','family_devices')`, 0},
		{`SELECT COUNT(*) FROM device_configs WHERE legacy='kept'`, 1},
		{`SELECT COUNT(*) FROM devices WHERE id='device' AND token_hash=X'5566'`, 1},
	} {
		var n int
		if err = db.QueryRow(tc.query).Scan(&n); err != nil || n != tc.want {
			t.Fatalf("rollback count=%d err=%v for %s", n, err, tc.query)
		}
	}
}
