package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"time"
)

func (s *Service) authorizeAdmin(ctx context.Context, a Admin) error {
	var found int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM family_admins WHERE user_id=? AND family_id=?", a.ID, a.FamilyID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnauthorized
	}
	if err != nil {
		return fmt.Errorf("authorize admin: %w", err)
	}
	return nil
}

// Pairing IDs have 60 random bits; device and session tokens retain 256 bits.
func randomPairingID() (string, error) {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate pairing id: %w", err)
	}
	for i := range b {
		b[i] = alphabet[b[i]&31]
	}
	return string(b), nil
}

func (s *Service) CreatePairing(ctx context.Context, a Admin) (Pairing, error) {
	if err := s.authorizeAdmin(ctx, a); err != nil {
		return Pairing{}, err
	}
	number, err := rand.Int(rand.Reader, big.NewInt(100000000))
	if err != nil {
		return Pairing{}, fmt.Errorf("generate code: %w", err)
	}
	p := Pairing{Code: fmt.Sprintf("%08d", number.Int64()), ExpiresAt: s.now().Add(10 * time.Minute).Truncate(time.Second)}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Pairing{}, fmt.Errorf("begin pairing: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM pairing_codes WHERE expires_at<=? OR family_id=?", s.now().Unix(), a.FamilyID); err != nil {
		return Pairing{}, fmt.Errorf("clean pairings: %w", err)
	}
	for attempt := 0; attempt < 5; attempt++ {
		id, err := randomPairingID()
		if err != nil {
			return Pairing{}, err
		}
		result, err := tx.ExecContext(ctx, "INSERT INTO pairing_codes(id,family_id,code_hash,expires_at) VALUES (?,?,?,?) ON CONFLICT(id) DO NOTHING", id, a.FamilyID, digest(id+":"+p.Code), p.ExpiresAt.Unix())
		if err != nil {
			return Pairing{}, fmt.Errorf("create pairing: %w", err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return Pairing{}, fmt.Errorf("check pairing creation: %w", err)
		}
		if n == 1 {
			p.ID = id
			break
		}
	}
	if p.ID == "" {
		return Pairing{}, fmt.Errorf("generate unique pairing id: retries exhausted")
	}
	if err := tx.Commit(); err != nil {
		return Pairing{}, fmt.Errorf("commit pairing: %w", err)
	}
	return p, nil
}
func (s *Service) Pair(ctx context.Context, pairingID, code, name, ip string) (PairedDevice, error) {
	if !validName(name) {
		return PairedDevice{}, ErrInvalidInput
	}
	if err := s.limit(ctx, limitRule{"pair-ip:" + ip, 20}); err != nil {
		return PairedDevice{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PairedDevice{}, fmt.Errorf("begin device pairing: %w", err)
	}
	defer tx.Rollback()
	var familyID, expires int64
	var attempts int
	var stored []byte
	var consumed sql.NullInt64
	err = tx.QueryRowContext(ctx, "SELECT family_id,code_hash,expires_at,attempts,consumed_at FROM pairing_codes WHERE id=?", pairingID).Scan(&familyID, &stored, &expires, &attempts, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return PairedDevice{}, ErrPairingInvalid
	}
	if err != nil {
		return PairedDevice{}, fmt.Errorf("load pairing: %w", err)
	}
	now := s.now().Unix()
	if expires <= now || attempts >= 5 || consumed.Valid {
		return PairedDevice{}, ErrPairingInvalid
	}
	if subtle.ConstantTimeCompare(stored, digest(pairingID+":"+code)) != 1 {
		if _, err := tx.ExecContext(ctx, "UPDATE pairing_codes SET attempts=attempts+1 WHERE id=? AND attempts<5", pairingID); err != nil {
			return PairedDevice{}, fmt.Errorf("record failed pairing: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return PairedDevice{}, fmt.Errorf("commit failed pairing: %w", err)
		}
		return PairedDevice{}, ErrPairingInvalid
	}
	result, err := tx.ExecContext(ctx, "UPDATE pairing_codes SET consumed_at=? WHERE id=? AND consumed_at IS NULL AND attempts<5 AND expires_at>?", now, pairingID, now)
	if err != nil {
		return PairedDevice{}, fmt.Errorf("consume pairing: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return PairedDevice{}, fmt.Errorf("pairing update result: %w", err)
	}
	if n != 1 {
		return PairedDevice{}, ErrPairingInvalid
	}
	id, err := randomToken()
	if err != nil {
		return PairedDevice{}, err
	}
	token, err := randomToken()
	if err != nil {
		return PairedDevice{}, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO devices(id,family_id,name,token_hash,created_at) VALUES (?,?,?,?,?)", id, familyID, name, digest(token), now); err != nil {
		return PairedDevice{}, fmt.Errorf("create device: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO family_devices(family_id,device_id) VALUES (?,?)", familyID, id); err != nil {
		return PairedDevice{}, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO device_configs(device_id) VALUES (?)", id); err != nil {
		return PairedDevice{}, err
	}
	if err := tx.Commit(); err != nil {
		return PairedDevice{}, fmt.Errorf("commit device: %w", err)
	}
	return PairedDevice{Device: Device{ID: id, FamilyID: familyID, Name: name, CreatedAt: time.Unix(now, 0).UTC()}, Token: token}, nil
}
func (s *Service) Devices(ctx context.Context, a Admin) ([]Device, error) {
	if err := s.authorizeAdmin(ctx, a); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT d.id,d.family_id,d.name,d.created_at,d.revoked_at FROM devices d JOIN family_devices fd ON fd.device_id=d.id WHERE fd.family_id=? ORDER BY d.created_at,d.id", a.FamilyID)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()
	result := make([]Device, 0)
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, fmt.Errorf("read device: %w", err)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate devices: %w", err)
	}
	return result, nil
}
func (s *Service) RevokeDevice(ctx context.Context, a Admin, id string) error {
	if err := s.authorizeAdmin(ctx, a); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin device revocation: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE devices SET revoked_at=COALESCE(revoked_at,?),sharing_session_id=NULL,last_sequence=0,last_captured_at=NULL WHERE id=? AND family_id IN (SELECT family_id FROM family_admins WHERE user_id=?)", s.now().Unix(), id, a.ID)
	if err != nil {
		return fmt.Errorf("revoke device: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("revocation result: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM latest_locations WHERE device_id=?", id); err != nil {
		return fmt.Errorf("delete revoked location: %w", err)
	}
	return tx.Commit()
}
func (s *Service) AuthenticateDevice(ctx context.Context, token string) (Device, error) {
	if len(token) != 43 {
		return Device{}, ErrUnauthorized
	}
	d, err := scanDevice(s.db.QueryRowContext(ctx, "SELECT id,family_id,name,created_at,revoked_at FROM devices WHERE token_hash=? AND revoked_at IS NULL", digest(token)))
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrUnauthorized
	}
	if err != nil {
		return Device{}, fmt.Errorf("authenticate device: %w", err)
	}
	return d, nil
}

type scanner interface{ Scan(...any) error }

func scanDevice(row scanner) (Device, error) {
	var d Device
	var created int64
	var revoked sql.NullInt64
	if err := row.Scan(&d.ID, &d.FamilyID, &d.Name, &created, &revoked); err != nil {
		return Device{}, err
	}
	d.CreatedAt = time.Unix(created, 0).UTC()
	if revoked.Valid {
		v := time.Unix(revoked.Int64, 0).UTC()
		d.RevokedAt = &v
	}
	return d, nil
}
