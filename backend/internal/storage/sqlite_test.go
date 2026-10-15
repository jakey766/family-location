package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestPersistenceAndMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "family ?#.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	var version, count int
	if err := db.QueryRow("SELECT MAX(version), COUNT(*) FROM schema_migrations").Scan(&version, &count); err != nil || version != SchemaVersion || count != SchemaVersion {
		t.Fatalf("migration version=%d count=%d err=%v", version, count, err)
	}
	if _, err := db.Exec("CREATE TABLE persistence_test (value TEXT); INSERT INTO persistence_test VALUES ('kept')"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	var value string
	if err := db.QueryRow("SELECT value FROM persistence_test").Scan(&value); err != nil || value != "kept" {
		t.Fatalf("persistent value=%q err=%v", value, err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil || count != SchemaVersion {
		t.Fatalf("repeated migration count=%d err=%v", count, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("database permissions: %o", info.Mode().Perm())
	}
}

func TestUpgradeV1PreservesData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))); INSERT INTO schema_migrations VALUES (1,'original'); CREATE TABLE preserved (value TEXT); INSERT INTO preserved VALUES ('kept')`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRow("SELECT value FROM preserved").Scan(&value); err != nil || value != "kept" {
		t.Fatalf("value=%q error=%v", value, err)
	}
	if err := db.QueryRow("SELECT applied_at FROM schema_migrations WHERE version=1").Scan(&value); err != nil || value != "original" {
		t.Fatalf("migration overwritten: %q %v", value, err)
	}
	for _, table := range []string{"families", "users", "web_sessions", "pairing_codes", "devices", "auth_limits"} {
		if _, err := db.Exec("SELECT * FROM " + table); err != nil {
			t.Fatalf("missing %s: %v", table, err)
		}
	}
}

func TestPragmasSurviveConnectionReplacement(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// No idle connections forces the driver to configure every replacement.
	db.SetMaxIdleConns(0)
	for _, tc := range []struct{ pragma, want string }{
		{"journal_mode", "wal"}, {"foreign_keys", "1"}, {"busy_timeout", "5000"},
	} {
		var got string
		if err := db.QueryRow("PRAGMA " + tc.pragma).Scan(&got); err != nil || got != tc.want {
			t.Fatalf("%s=%q err=%v", tc.pragma, got, err)
		}
	}
	if _, err := db.Exec("CREATE TABLE parent (id INTEGER PRIMARY KEY); CREATE TABLE child (parent_id INTEGER REFERENCES parent(id))"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO child VALUES (99)"); err == nil {
		t.Fatal("foreign key violation accepted")
	}
}

func TestOpenRejectsInvalidDatabase(t *testing.T) {
	dir := t.TempDir()
	badFile := filepath.Join(dir, "not-a-database")
	if err := os.WriteFile(badFile, []byte("this is not SQLite"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", ":memory:", dir, filepath.Join(badFile, "db.sqlite"), badFile} {
		t.Run(path, func(t *testing.T) {
			db, err := Open(context.Background(), path)
			if err == nil {
				db.Close()
				t.Fatal("expected error")
			}
		})
	}
}

func TestOpenCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "db.sqlite"))
	if err == nil {
		db.Close()
		t.Fatal("canceled open succeeded")
	}
}

func TestRejectNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO schema_migrations (version) VALUES (999)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(context.Background(), path)
	if err == nil {
		db.Close()
		t.Fatal("newer schema accepted")
	}
}

func TestMigrationFailureRollsBackVersionAndTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT 'original'); INSERT INTO schema_migrations(version) VALUES (1); CREATE TABLE users (legacy_value TEXT); INSERT INTO users VALUES ('kept')`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	if migrated, err := Open(context.Background(), path); err == nil {
		migrated.Close()
		t.Fatal("expected migration conflict")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, count int
	var value string
	if err := db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil || version != 1 {
		t.Fatal(version, err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name='families'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial migration remains: count=%d error=%v", count, err)
	}
	if err := db.QueryRow("SELECT legacy_value FROM users").Scan(&value); err != nil || value != "kept" {
		t.Fatal(value, err)
	}
}
