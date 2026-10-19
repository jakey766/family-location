package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"family-location/backend/internal/storage"
)

const testPassword = "a secure test password"

func setup(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "auth.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db), db
}
func admin(t *testing.T, s *Service, username string) Admin {
	t.Helper()
	a, err := s.CreateAdmin(context.Background(), username, testPassword, "家庭")
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func TestShortPairingIDAndLegacyCompatibility(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	a := admin(t, s, "short-pair")
	p, err := s.CreatePairing(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.ID) != 12 || strings.Trim(p.ID, "23456789ABCDEFGHJKLMNPQRSTUVWXYZ") != "" {
		t.Fatalf("expected 12 unambiguous characters, got length %d", len(p.ID))
	}
	paired, err := s.Pair(ctx, p.ID, p.Code, "short", "short-ip")
	if err != nil {
		t.Fatal(err)
	}
	if len(paired.Token) != 43 || len(paired.Device.ID) != 43 {
		t.Fatal("device credentials must keep their original length")
	}
	p, err = s.CreatePairing(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	legacy := strings.Repeat("x", 43)
	if _, err := db.Exec("UPDATE pairing_codes SET id=?,code_hash=? WHERE id=?", legacy, digest(legacy+":"+p.Code), p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pair(ctx, legacy, p.Code, "legacy", "legacy-ip"); err != nil {
		t.Fatal(err)
	}
}
func TestAdminAndSessionLifecycle(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	a := admin(t, s, "Alice")
	if a.Username != "alice" {
		t.Fatal(a)
	}
	if _, err := s.CreateAdmin(ctx, "ALICE", testPassword, "other"); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	var hash string
	if err := db.QueryRow("SELECT password_hash FROM users WHERE id=?", a.ID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=1,p=4$") || strings.Contains(hash, testPassword) {
		t.Fatal("invalid password storage")
	}
	for _, creds := range [][2]string{{"missing", testPassword}, {"alice", "wrong password"}} {
		if _, err := s.Login(ctx, creds[0], creds[1], "127.0.0.1"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal(err)
		}
	}
	session, err := s.Login(ctx, "ALICE", testPassword, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(session.Token))
	var stored []byte
	if err := db.QueryRow("SELECT token_hash FROM web_sessions").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if string(stored) != string(digest[:]) || len(session.Token) < 40 || session.CSRFToken == "" {
		t.Fatal("invalid session storage")
	}
	got, err := s.AuthenticateSession(ctx, session.Token)
	if err != nil || got.User != a || got.CSRFToken != session.CSRFToken {
		t.Fatalf("session: %+v %v", got, err)
	}
	if _, err := s.AuthenticateDevice(ctx, session.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal(err)
	}
	if err := s.Logout(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateSession(ctx, session.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal(err)
	}
	session, err = s.Login(ctx, "alice", testPassword, "127.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return session.ExpiresAt }
	if _, err := s.AuthenticateSession(ctx, session.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal(err)
	}
}
func TestValidation(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	for _, c := range [][3]string{{"ab", testPassword, "family"}, {"中文名", testPassword, "family"}, {"a b", testPassword, "family"}, {"valid", "short", "family"}, {"valid", strings.Repeat("p", 129), "family"}, {"valid", testPassword, " "}, {"valid", testPassword, strings.Repeat("家", 81)}} {
		if _, err := s.CreateAdmin(ctx, c[0], c[1], c[2]); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("expected invalid input: %v", err)
		}
	}
}
func TestPairingIsolationRevocation(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	a := admin(t, s, "alice")
	b := admin(t, s, "bobby")
	p, err := s.CreatePairing(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Code) != 8 {
		t.Fatal("wrong code length")
	}
	pd, err := s.Pair(ctx, p.ID, p.Code, "手机", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if pd.Device.FamilyID != a.FamilyID {
		t.Fatal(pd.Device)
	}
	digest := sha256.Sum256([]byte(pd.Token))
	var stored []byte
	if err := db.QueryRow("SELECT token_hash FROM devices WHERE id=?", pd.Device.ID).Scan(&stored); err != nil || string(stored) != string(digest[:]) {
		t.Fatal("device token not hashed", err)
	}
	if _, err := s.Pair(ctx, p.ID, p.Code, "another", "127.0.0.1"); !errors.Is(err, ErrPairingInvalid) {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateSession(ctx, pd.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateDevice(ctx, pd.Token); err != nil {
		t.Fatal(err)
	}
	list, err := s.Devices(ctx, a)
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	list, err = s.Devices(ctx, b)
	if err != nil || len(list) != 0 {
		t.Fatal(list, err)
	}
	if err := s.RevokeDevice(ctx, b, pd.Device.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.RevokeDevice(ctx, a, pd.Device.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateDevice(ctx, pd.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatal(err)
	}
	list, err = s.Devices(ctx, a)
	if err != nil || list[0].RevokedAt == nil {
		t.Fatal(list, err)
	}
}
func TestPairingExpiryAttemptsReplacementAndConcurrency(t *testing.T) {
	s, _ := setup(t)
	ctx := context.Background()
	a := admin(t, s, "alice")
	p, err := s.CreatePairing(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := s.Pair(ctx, p.ID, "wrong", "device", "ip"); !errors.Is(err, ErrPairingInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := s.Pair(ctx, p.ID, p.Code, "device", "ip"); !errors.Is(err, ErrPairingInvalid) {
		t.Fatal(err)
	}
	old, _ := s.CreatePairing(ctx, a)
	p, _ = s.CreatePairing(ctx, a)
	if _, err := s.Pair(ctx, old.ID, old.Code, "device", "ip"); !errors.Is(err, ErrPairingInvalid) {
		t.Fatal(err)
	}
	s.now = func() time.Time { return p.ExpiresAt }
	if _, err := s.Pair(ctx, p.ID, p.Code, "device", "ip"); !errors.Is(err, ErrPairingInvalid) {
		t.Fatal(err)
	}
	s.now = time.Now
	p, err = s.CreatePairing(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.Pair(ctx, p.ID, p.Code, "device", fmt.Sprint(i))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrPairingInvalid) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("successes=%d", success)
	}
}
func TestPersistentLimitsAndHashConcurrency(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	admin(t, s, "alice")
	for i := 0; i < 5; i++ {
		if _, err := s.Login(ctx, "alice", "incorrect", fmt.Sprint(i)); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal(err)
		}
	}
	restarted := New(db)
	if _, err := restarted.Login(ctx, "alice", testPassword, "new"); !errors.Is(err, ErrRateLimited) {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := s.Login(ctx, fmt.Sprintf("user%d", i), "incorrect", "fixed"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal(err)
		}
	}
	if _, err := s.Login(ctx, "fresh", testPassword, "fixed"); !errors.Is(err, ErrRateLimited) {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if _, err := s.Pair(ctx, "unknown", "wrong", "device", "pair-ip"); !errors.Is(err, ErrPairingInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := restarted.Pair(ctx, "unknown", "wrong", "device", "pair-ip"); !errors.Is(err, ErrRateLimited) {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Now().Add(11 * time.Minute) }
	if _, err := s.Login(ctx, "alice", testPassword, "new"); err != nil {
		t.Fatal(err)
	}
	s.hashSlots <- struct{}{}
	s.hashSlots <- struct{}{}
	if _, err := s.Login(ctx, "alice", testPassword, "concurrency"); !errors.Is(err, ErrRateLimited) {
		t.Fatal(err)
	}
	<-s.hashSlots
	<-s.hashSlots
}

func TestRejectUnicodeUsernameCaseFold(t *testing.T) {
	s, _ := setup(t)
	if _, err := s.CreateAdmin(context.Background(), "Kevin", testPassword, "family"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("non-ASCII username accepted: %v", err)
	}
}
func TestLimitCapacityCleanupAndDatabaseReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "persistent.sqlite")
	db, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(2000000000, 0)
	s := New(db)
	s.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		if err := s.limit(ctx, limitRule{"persisted", 5}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s = New(db)
	s.now = func() time.Time { return now }
	if err := s.limit(ctx, limitRule{"persisted", 5}); !errors.Is(err, ErrRateLimited) {
		t.Fatal(err)
	}
	_, err = db.Exec(`WITH RECURSIVE numbers(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM numbers WHERE n<9999) INSERT INTO auth_limits(key,window_start,count) SELECT 'seed-'||n,?,1 FROM numbers`, now.Unix())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.limit(ctx, limitRule{"new", 5}); !errors.Is(err, ErrRateLimited) {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM auth_limits").Scan(&count); err != nil || count != 10000 {
		t.Fatal(count, err)
	}
	now = now.Add(10 * time.Minute)
	if err := s.limit(ctx, limitRule{"new", 5}); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM auth_limits").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
func TestPairingRollbackAndSecretStorage(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	a := admin(t, s, "alice")
	p, err := s.CreatePairing(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err := db.QueryRow("SELECT code_hash FROM pairing_codes WHERE id=?", p.ID).Scan(&stored); err != nil || string(stored) == p.Code {
		t.Fatal("plaintext pairing code", err)
	}
	if _, err := db.Exec("CREATE TRIGGER fail_device BEFORE INSERT ON devices BEGIN SELECT RAISE(ABORT,'forced failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pair(ctx, p.ID, p.Code, "device", "ip"); err == nil {
		t.Fatal("expected database error")
	}
	if _, err := db.Exec("DROP TRIGGER fail_device"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pair(ctx, p.ID, p.Code, "device", "ip"); err != nil {
		t.Fatalf("pairing consumed after rollback: %v", err)
	}
}
func TestCreationCleansExpiredSessionsAndPairings(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	a := admin(t, s, "alice")
	session, err := s.Login(ctx, "alice", testPassword, "ip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePairing(ctx, a); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return session.ExpiresAt }
	if _, err := s.Login(ctx, "alice", testPassword, "ip"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePairing(ctx, a); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"web_sessions", "pairing_codes"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s=%d %v", table, count, err)
		}
	}
}
