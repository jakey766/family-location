package storage

import (
	"context"
	"database/sql"
	"testing"
)

func createV4Database(t *testing.T) string {
	t.Helper()
	path := createV2Database(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(locationSchema + familySchema + `INSERT INTO schema_migrations(version) VALUES(3),(4);
 INSERT INTO families(id,name,created_at,public_id) VALUES(9,'view only',0,'second-family');
 INSERT INTO users VALUES(10,9,'other','hash',0);
 INSERT INTO family_admins VALUES(9,10),(7,10);
 INSERT INTO family_devices VALUES(9,'device');
 UPDATE devices SET sharing_session_id='kept-session',last_sequence=9,last_captured_at=1234;
 UPDATE device_configs SET version=4,moving_interval_seconds=180,stationary_interval_seconds=600;
 INSERT INTO latest_locations VALUES('device',31,121,10,50,1234,2345,422345);`)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
func TestV5MigrationPreservesAllDataAndOriginalManagementAuthorization(t *testing.T) {
	path := createV4Database(t)
	for i := 0; i < 2; i++ {
		db, err := Open(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			q string
			n int
		}{
			{`SELECT COUNT(*) FROM device_admins WHERE device_id='device' AND user_id IN (8,10)`, 2},
			{`SELECT COUNT(*) FROM families WHERE passcode_hash IS NULL AND deleted_at IS NULL AND passcode_version=0`, 2},
			{`SELECT COUNT(*) FROM devices WHERE token_hash=X'5566' AND sharing_session_id='kept-session' AND last_sequence=9 AND last_captured_at=1234`, 1},
			{`SELECT COUNT(*) FROM device_configs WHERE device_id='device' AND version=4 AND moving_interval_seconds=180 AND stationary_interval_seconds=600`, 1},
			{`SELECT COUNT(*) FROM latest_locations WHERE device_id='device' AND captured_at=1234 AND received_at=2345`, 1},
			{`SELECT COUNT(*) FROM family_devices WHERE device_id='device'`, 2},
			{`SELECT COUNT(*) FROM pairing_codes WHERE id='pair' AND attempts=2 AND code_hash=X'3344'`, 1},
			{`SELECT COUNT(*) FROM web_sessions WHERE token_hash=X'1122'`, 1},
		} {
			var n int
			if err = db.QueryRow(tc.q).Scan(&n); err != nil || n != tc.n {
				db.Close()
				t.Fatal(tc.q, n, err)
			}
		}
		db.Close()
	}
}
func TestV5MigrationDoesNotInferOwnersFromVisibility(t *testing.T) {
	path := createV4Database(t)
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = old.Exec(`DELETE FROM family_admins WHERE family_id=7 AND user_id=10`); err != nil {
		t.Fatal(err)
	}
	old.Close()
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var n int
	if err = db.QueryRow(`SELECT COUNT(*) FROM device_admins WHERE user_id=10`).Scan(&n); err != nil || n != 0 {
		t.Fatal("view-only became owner", n, err)
	}
}
func TestV5MigrationFailureRollsBack(t *testing.T) {
	path := createV4Database(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE family_grants(legacy TEXT);INSERT INTO family_grants VALUES('kept');`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if upgraded, err := Open(context.Background(), path); err == nil {
		upgraded.Close()
		t.Fatal("expected conflict")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, tc := range []struct {
		q string
		n int
	}{
		{`SELECT MAX(version) FROM schema_migrations`, 4},
		{`SELECT COUNT(*) FROM pragma_table_info('families') WHERE name IN ('deleted_at','passcode_hash','passcode_version')`, 0},
		{`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('device_admins','family_browsers')`, 0},
		{`SELECT COUNT(*) FROM family_grants WHERE legacy='kept'`, 1},
		{`SELECT COUNT(*) FROM devices WHERE id='device' AND sharing_session_id='kept-session'`, 1},
	} {
		var n int
		if err = db.QueryRow(tc.q).Scan(&n); err != nil || n != tc.n {
			t.Fatal(tc.q, n, err)
		}
	}
}
