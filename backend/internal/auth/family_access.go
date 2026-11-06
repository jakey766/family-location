package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

const FamilyAccessDuration = 7 * 24 * time.Hour

func validPasscode(value string) bool {
	if len(value) != 7 {
		return false
	}
	for _, c := range []byte(value) {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func randomPasscode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(10000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%07d", n.Int64()), nil
}
func (s *Service) hashPasscode(passcode string) (string, error) {
	if !validPasscode(passcode) {
		return "", ErrInvalidInput
	}
	if err := s.acquireHash(); err != nil {
		return "", err
	}
	defer func() { <-s.hashSlots }()
	return passwordHash(passcode)
}
func (s *Service) RenameFamily(ctx context.Context, a Admin, key, name string) (Family, error) {
	name = strings.TrimSpace(name)
	if !validName(name) {
		return Family{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Family{}, err
	}
	defer tx.Rollback()
	var f Family
	err = tx.QueryRowContext(ctx, `SELECT f.id,f.public_id,f.name,f.passcode_hash IS NOT NULL FROM families f JOIN family_admins fa ON fa.family_id=f.id WHERE f.public_id=? AND fa.user_id=? AND f.deleted_at IS NULL`, key, a.ID).Scan(&f.ID, &f.PublicID, &f.Name, &f.PasscodeSet)
	if errors.Is(err, sql.ErrNoRows) {
		return Family{}, ErrNotFound
	}
	if err != nil {
		return Family{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE families SET name=? WHERE id=?`, name, f.ID); err != nil {
		return Family{}, err
	}
	f.Name = name
	return f, tx.Commit()
}
func (s *Service) SetFamilyPasscode(ctx context.Context, a Admin, key string, passcode *string) (string, error) {
	// Resolve access before expensive hashing and recheck inside the write transaction.
	if _, err := s.SelectFamily(ctx, a, key); err != nil {
		return "", err
	}
	value := ""
	var err error
	if passcode == nil {
		value, err = randomPasscode()
	} else {
		value = *passcode
	}
	if err != nil {
		return "", err
	}
	hash, err := s.hashPasscode(value)
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE families SET passcode_hash=?,passcode_version=passcode_version+1 WHERE public_id=? AND deleted_at IS NULL AND id IN (SELECT family_id FROM family_admins WHERE user_id=?)`, hash, key, a.ID)
	if err != nil {
		return "", err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", err
	}
	if n != 1 {
		return "", ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM family_grants WHERE family_id=(SELECT id FROM families WHERE public_id=?)`, key); err != nil {
		return "", err
	}
	return value, tx.Commit()
}
func (s *Service) DeleteFamily(ctx context.Context, a Admin, key string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT f.id FROM families f JOIN family_admins fa ON fa.family_id=f.id WHERE f.public_id=? AND fa.user_id=? AND f.deleted_at IS NULL`, key, a.ID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE families SET deleted_at=?,name='',passcode_hash=NULL,passcode_version=passcode_version+1 WHERE id=?`, s.now().Unix(), id); err != nil {
		return err
	}
	for _, table := range []string{"family_devices", "pairing_codes", "family_grants", "family_admins"} {
		if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE family_id=?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Service) PurgeDevice(ctx context.Context, a Admin, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var revoked sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT d.revoked_at FROM devices d JOIN device_admins da ON da.device_id=d.id WHERE d.id=? AND da.user_id=?`, id, a.ID).Scan(&revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !revoked.Valid {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM devices WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

type FamilyAccess struct {
	FamilyID  int64     `json:"-"`
	PublicID  string    `json:"public_id"`
	Name      string    `json:"name"`
	ExpiresAt time.Time `json:"-"`
}

type FamilySession struct {
	Token           string    `json:"-"`
	ExpiresAt       time.Time `json:"expires_at"`
	CookieExpiresAt time.Time `json:"-"`
}

func (s *Service) LoginFamily(ctx context.Context, key, passcode, token, ip string) (FamilySession, error) {
	if err := s.limit(ctx, limitRule{"family-ip:" + ip, 30}, limitRule{"family-key-ip:" + key + ":" + ip, 5}); err != nil {
		return FamilySession{}, err
	}
	if !validPasscode(passcode) {
		return FamilySession{}, ErrInvalidInput
	}
	if err := s.acquireHash(); err != nil {
		return FamilySession{}, err
	}
	var id, version int64
	var hash sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id,passcode_hash,passcode_version FROM families WHERE public_id=? AND deleted_at IS NULL`, key).Scan(&id, &hash, &version)
	if err != nil {
		<-s.hashSlots
		if errors.Is(err, sql.ErrNoRows) {
			return FamilySession{}, ErrNotFound
		}
		return FamilySession{}, err
	}
	valid := checkPassword(passcode, hash.String)
	<-s.hashSlots
	if !valid {
		return FamilySession{}, ErrInvalidCredentials
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return FamilySession{}, err
	}
	defer tx.Rollback()
	// Recheck the exact version/hash after hashing, so a concurrent rotation or
	// deletion cannot grant access using the previous secret.
	var found int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM families WHERE id=? AND deleted_at IS NULL AND passcode_version=? AND passcode_hash=?`, id, version, hash.String).Scan(&found); errors.Is(err, sql.ErrNoRows) {
		return FamilySession{}, ErrUnauthorized
	} else if err != nil {
		return FamilySession{}, err
	}
	now := s.now().Unix()
	expires := now + int64(FamilyAccessDuration/time.Second)
	if _, err = tx.ExecContext(ctx, `DELETE FROM family_browsers WHERE expires_at<=?`, now); err != nil {
		return FamilySession{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM family_grants WHERE expires_at<=?`, now); err != nil {
		return FamilySession{}, err
	}
	var cookieExpires int64
	if len(token) == 43 {
		err = tx.QueryRowContext(ctx, `SELECT expires_at FROM family_browsers WHERE token_hash=? AND expires_at>?`, digest(token), now).Scan(&cookieExpires)
	} else {
		err = sql.ErrNoRows
	}
	if errors.Is(err, sql.ErrNoRows) {
		token, err = randomToken()
		if err != nil {
			return FamilySession{}, err
		}
		cookieExpires = expires
	} else if err != nil {
		return FamilySession{}, err
	}
	cookieExpires = max(cookieExpires, expires)
	if _, err = tx.ExecContext(ctx, `INSERT INTO family_browsers(token_hash,expires_at) VALUES(?,?) ON CONFLICT(token_hash) DO UPDATE SET expires_at=excluded.expires_at`, digest(token), cookieExpires); err != nil {
		return FamilySession{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO family_grants(token_hash,family_id,passcode_version,expires_at) VALUES(?,?,?,?) ON CONFLICT(token_hash,family_id) DO UPDATE SET passcode_version=excluded.passcode_version,expires_at=excluded.expires_at`, digest(token), id, version, expires); err != nil {
		return FamilySession{}, err
	}
	if err = tx.Commit(); err != nil {
		return FamilySession{}, err
	}
	return FamilySession{Token: token, ExpiresAt: time.Unix(expires, 0).UTC(), CookieExpiresAt: time.Unix(cookieExpires, 0).UTC()}, nil
}

// ReadFamilyAccess accepts the caller's transaction so authorization and data
// reads observe the same family/version/membership snapshot.
func ReadFamilyAccess(ctx context.Context, q ConfigReader, token, key string, now time.Time) (FamilyAccess, error) {
	var f FamilyAccess
	err := q.QueryRowContext(ctx, `SELECT id,public_id,name FROM families WHERE public_id=? AND deleted_at IS NULL`, key).Scan(&f.FamilyID, &f.PublicID, &f.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return FamilyAccess{}, ErrNotFound
	}
	if err != nil {
		return FamilyAccess{}, err
	}
	if len(token) != 43 {
		return FamilyAccess{}, ErrUnauthorized
	}
	var expires int64
	err = q.QueryRowContext(ctx, `SELECT g.expires_at FROM family_grants g JOIN family_browsers b ON b.token_hash=g.token_hash JOIN families f ON f.id=g.family_id WHERE g.token_hash=? AND g.family_id=? AND g.expires_at>? AND b.expires_at>? AND f.deleted_at IS NULL AND f.passcode_hash IS NOT NULL AND g.passcode_version=f.passcode_version`, digest(token), f.FamilyID, now.Unix(), now.Unix()).Scan(&expires)
	if errors.Is(err, sql.ErrNoRows) {
		return FamilyAccess{}, ErrUnauthorized
	}
	if err != nil {
		return FamilyAccess{}, err
	}
	f.ExpiresAt = time.Unix(expires, 0).UTC()
	return f, nil
}
func (s *Service) FamilyAccess(ctx context.Context, token, key string) (FamilyAccess, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return FamilyAccess{}, err
	}
	defer tx.Rollback()
	return ReadFamilyAccess(ctx, tx, token, key, s.now())
}
func (s *Service) AnyFamilyAccess(ctx context.Context, token string) error {
	if len(token) != 43 {
		return ErrUnauthorized
	}
	var found int
	now := s.now().Unix()
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM family_grants g JOIN family_browsers b ON b.token_hash=g.token_hash JOIN families f ON f.id=g.family_id WHERE g.token_hash=? AND g.expires_at>? AND b.expires_at>? AND f.deleted_at IS NULL AND f.passcode_hash IS NOT NULL AND g.passcode_version=f.passcode_version LIMIT 1`, digest(token), now, now).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnauthorized
	}
	return err
}
func (s *Service) LogoutFamily(ctx context.Context, token, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM family_grants WHERE token_hash=? AND family_id=(SELECT id FROM families WHERE public_id=?)`, digest(token), key)
	return err
}
