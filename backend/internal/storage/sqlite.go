package storage

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const SchemaVersion = 2

// Open initializes a persistent database. Only one process should own this file.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(path) == "" || path == ":memory:" {
		return nil, fmt.Errorf("database path must name a persistent file")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	if info, err := os.Stat(absPath); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("database path must be a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("inspect database file: %w", err)
	}
	file, err := os.OpenFile(absPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("create database file: %w", err)
	}
	chmodErr := file.Chmod(0600)
	closeErr := file.Close()
	if chmodErr != nil {
		return nil, fmt.Errorf("restrict database permissions: %w", chmodErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close database file: %w", closeErr)
	}

	// URL encoding keeps spaces, '?' and '#' in filenames out of DSN options.
	dsn := url.URL{Scheme: "file", Path: absPath}
	params := url.Values{}
	for _, pragma := range []string{"busy_timeout(5000)", "journal_mode(WAL)", "foreign_keys(1)"} {
		params.Add("_pragma", pragma)
	}
	dsn.RawQuery = params.Encode()
	db, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize sqlite: %w", err)
	}
	return db, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY CHECK (version > 0),
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`); err != nil {
		return err
	}
	var version int
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return err
	}
	if version > SchemaVersion {
		return fmt.Errorf("database schema %d is newer than supported schema %d", version, SchemaVersion)
	}
	for next := version + 1; next <= SchemaVersion; next++ {
		if next == 2 {
			if _, err := tx.ExecContext(ctx, authSchema); err != nil {
				return fmt.Errorf("migration %d: %w", next, err)
			}
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations (version) VALUES (?)", next); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const authSchema = `
CREATE TABLE families (
 id INTEGER PRIMARY KEY, name TEXT NOT NULL, created_at INTEGER NOT NULL
);
CREATE TABLE users (
 id INTEGER PRIMARY KEY, family_id INTEGER NOT NULL REFERENCES families(id),
 username TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, created_at INTEGER NOT NULL
);
CREATE TABLE web_sessions (
 token_hash BLOB PRIMARY KEY, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 expires_at INTEGER NOT NULL
);
CREATE INDEX web_sessions_expiry ON web_sessions(expires_at);
CREATE TABLE pairing_codes (
 id TEXT PRIMARY KEY, family_id INTEGER NOT NULL REFERENCES families(id),
 code_hash BLOB NOT NULL, expires_at INTEGER NOT NULL,
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 5), consumed_at INTEGER
);
CREATE INDEX pairing_codes_family ON pairing_codes(family_id);
CREATE INDEX pairing_codes_expiry ON pairing_codes(expires_at);
CREATE TABLE devices (
 id TEXT PRIMARY KEY, family_id INTEGER NOT NULL REFERENCES families(id),
 name TEXT NOT NULL, token_hash BLOB NOT NULL UNIQUE,
 created_at INTEGER NOT NULL, revoked_at INTEGER
);
CREATE INDEX devices_family ON devices(family_id);
CREATE TABLE auth_limits (
 key TEXT PRIMARY KEY, window_start INTEGER NOT NULL, count INTEGER NOT NULL CHECK(count > 0)
);
CREATE INDEX auth_limits_window ON auth_limits(window_start);
`
