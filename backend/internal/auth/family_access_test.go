package auth

import (
	"context"
	"errors"
	"family-location/backend/internal/storage"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFamilyGrantIndependentDeadlinesPersistenceAndRotation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "persistent.sqlite")
	db, err := storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	s := New(db)
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	a := admin(t, s, "owner")
	first, err := s.CreateFamily(ctx, a, "first", "0000001")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateFamily(ctx, a, "second", "0000002")
	if err != nil {
		t.Fatal(err)
	}
	one, err := s.LoginFamily(ctx, first.PublicID, "0000001", "", "ip-one")
	if err != nil {
		t.Fatal(err)
	}
	if one.ExpiresAt != now.Add(7*24*time.Hour) {
		t.Fatal("wrong fixed deadline")
	}
	now = now.Add(24 * time.Hour)
	two, err := s.LoginFamily(ctx, second.PublicID, "0000002", one.Token, "ip-two")
	if err != nil {
		t.Fatal(err)
	}
	if one.Token != two.Token || two.CookieExpiresAt != two.ExpiresAt {
		t.Fatal("browser token not reused")
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = storage.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	s = New(db)
	s.now = func() time.Time { return now }
	access, err := s.FamilyAccess(ctx, two.Token, first.PublicID)
	if err != nil || access.ExpiresAt != one.ExpiresAt {
		t.Fatal("restart/other family extended first grant", err)
	}
	if _, err = s.RenameFamily(ctx, a, first.PublicID, "renamed"); err != nil {
		t.Fatal(err)
	}
	access, err = s.FamilyAccess(ctx, two.Token, first.PublicID)
	if err != nil || access.Name != "renamed" {
		t.Fatal("rename revoked grant", err)
	}
	now = one.ExpiresAt.Add(-time.Second)
	if _, err = s.FamilyAccess(ctx, two.Token, first.PublicID); err != nil {
		t.Fatal(err)
	}
	now = one.ExpiresAt
	if _, err = s.FamilyAccess(ctx, two.Token, first.PublicID); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("7-day boundary allowed", err)
	}
	if _, err = s.FamilyAccess(ctx, two.Token, second.PublicID); err != nil {
		t.Fatal("other deadline affected", err)
	}
	pass := "0123456"
	if _, err = s.SetFamilyPasscode(ctx, a, second.PublicID, &pass); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FamilyAccess(ctx, two.Token, second.PublicID); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("rotation left old grant", err)
	}
	two, err = s.LoginFamily(ctx, second.PublicID, pass, two.Token, "ip-three")
	if err != nil {
		t.Fatal(err)
	}
	one, err = s.LoginFamily(ctx, first.PublicID, "0000001", two.Token, "ip-four")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.LogoutFamily(ctx, one.Token, first.PublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FamilyAccess(ctx, one.Token, first.PublicID); !errors.Is(err, ErrUnauthorized) {
		t.Fatal(err)
	}
	if _, err = s.FamilyAccess(ctx, one.Token, second.PublicID); err != nil {
		t.Fatal("single family logout revoked another", err)
	}
	var hash string
	db.QueryRow(`SELECT passcode_hash FROM families WHERE id=?`, second.ID).Scan(&hash)
	if !strings.HasPrefix(hash, "$argon2id$") || strings.Contains(hash, pass) {
		t.Fatal("bad stored passcode")
	}
	if _, err = s.SetFamilyPasscode(ctx, a, second.PublicID, &pass); err != nil {
		t.Fatal(err)
	}
	var rotated string
	db.QueryRow(`SELECT passcode_hash FROM families WHERE id=?`, second.ID).Scan(&rotated)
	if hash == rotated {
		t.Fatal("salt reused")
	}
}

func TestFamilyLimitsPersistAndHashConcurrencyIsBounded(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	a := admin(t, s, "owner")
	f, err := s.CreateFamily(ctx, a, "limited", "1234567")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err = s.LoginFamily(ctx, f.PublicID, "7654321", "", "blocked-ip"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatal(err)
		}
	}
	restarted := New(db)
	if _, err = restarted.LoginFamily(ctx, f.PublicID, "1234567", "", "blocked-ip"); !errors.Is(err, ErrRateLimited) {
		t.Fatal("restart reset limit", err)
	}
	if _, err = restarted.LoginFamily(ctx, f.PublicID, "1234567", "", "different-ip"); err != nil {
		t.Fatal("one source locked entire family", err)
	}
	restarted.hashSlots = make(chan struct{}, 1)
	restarted.hashSlots <- struct{}{}
	if _, err = restarted.LoginFamily(ctx, f.PublicID, "1234567", "", "third-ip"); !errors.Is(err, ErrRateLimited) {
		t.Fatal("unbounded hashes", err)
	}
}

func TestDeletedFamilyScrubOwnershipAndPurge(t *testing.T) {
	s, db := setup(t)
	ctx := context.Background()
	a := admin(t, s, "owner")
	other := admin(t, s, "other")
	fs, _ := s.Families(ctx, a)
	f := fs[0]
	pass := "0123456"
	s.SetFamilyPasscode(ctx, a, f.PublicID, &pass)
	p, err := s.CreatePairing(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Pair(ctx, p.ID, p.Code, "phone", "ip")
	if err != nil {
		t.Fatal(err)
	}
	shared, err := s.CreateFamily(ctx, a, "shared")
	if err != nil {
		t.Fatal(err)
	}
	// Giving a second administrator access to a view must not grant ownership.
	if _, err = db.Exec(`INSERT INTO family_admins(family_id,user_id) VALUES(?,?)`, shared.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.SetMembership(ctx, a, shared.PublicID, d.Device.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DeviceConfig(ctx, other, d.Device.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("membership escalated owner", err)
	}
	if err = s.RevokeDevice(ctx, other, d.Device.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err = s.DeleteFamily(ctx, other, f.PublicID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	oldPair, err := s.CreatePairing(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := s.LoginFamily(ctx, f.PublicID, pass, "", "viewer")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteFamily(ctx, a, f.PublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pair(ctx, oldPair.ID, oldPair.Code, "phone 2", "ip"); !errors.Is(err, ErrPairingInvalid) {
		t.Fatal("deleted pairing survived", err)
	}
	if _, err = s.FamilyAccess(ctx, grant.Token, f.PublicID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	var n int
	for _, q := range []string{`SELECT COUNT(*) FROM families WHERE id=? AND name='' AND passcode_hash IS NULL AND deleted_at IS NOT NULL`, `SELECT COUNT(*) FROM devices WHERE family_id=?`, `SELECT COUNT(*) FROM users WHERE family_id=?`} {
		if err = db.QueryRow(q, f.ID).Scan(&n); err != nil || n != 1 {
			t.Fatal("historical id or scrub failed", q, n, err)
		}
	}
	for _, table := range []string{"family_admins", "family_devices", "pairing_codes", "family_grants"} {
		if err = db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE family_id=?`, f.ID).Scan(&n); err != nil || n != 0 {
			t.Fatal("deleted relations survived", table, n, err)
		}
	}
	if _, err = s.AuthenticateDevice(ctx, d.Token); err != nil {
		t.Fatal("family deletion revoked device", err)
	}
	if _, err = s.DeviceConfig(ctx, a, d.Device.ID); err != nil {
		t.Fatal("family deletion lost owner", err)
	}
	if err = s.PurgeDevice(ctx, a, d.Device.ID); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if err = s.RevokeDevice(ctx, a, d.Device.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.PurgeDevice(ctx, other, d.Device.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err = s.PurgeDevice(ctx, a, d.Device.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"device_admins", "family_devices", "device_configs", "latest_locations"} {
		if err = db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE device_id=?`, d.Device.ID).Scan(&n); err != nil || n != 0 {
			t.Fatal("purge relations survived", table, n, err)
		}
	}
}
