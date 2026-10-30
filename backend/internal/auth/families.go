package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type Family struct {
	ID       int64  `json:"id"`
	PublicID string `json:"public_id"`
	Name     string `json:"name"`
}

func (s *Service) Families(ctx context.Context, a Admin) ([]Family, error) {
	if err := s.authorizeAdmin(ctx, a); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT f.id,f.public_id,f.name FROM families f JOIN family_admins fa ON fa.family_id=f.id WHERE fa.user_id=? ORDER BY f.id`, a.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Family, 0)
	for rows.Next() {
		var f Family
		if err := rows.Scan(&f.ID, &f.PublicID, &f.Name); err != nil {
			return nil, err
		}
		result = append(result, f)
	}
	return result, rows.Err()
}

func (s *Service) CreateFamily(ctx context.Context, a Admin, name string) (Family, error) {
	if err := s.authorizeAdmin(ctx, a); err != nil {
		return Family{}, err
	}
	name = strings.TrimSpace(name)
	if !validName(name) {
		return Family{}, ErrInvalidInput
	}
	publicID, err := randomToken()
	if err != nil {
		return Family{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Family{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO families(name,created_at,public_id) VALUES(?,?,?)`, name, s.now().Unix(), publicID)
	if err != nil {
		return Family{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Family{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO family_admins(family_id,user_id) VALUES(?,?)`, id, a.ID); err != nil {
		return Family{}, err
	}
	if err = tx.Commit(); err != nil {
		return Family{}, err
	}
	return Family{ID: id, PublicID: publicID, Name: name}, nil
}

// SelectFamily resolves only an explicitly authorized public identifier. It
// never falls back when an identifier is absent, invalid or inaccessible.
func (s *Service) SelectFamily(ctx context.Context, a Admin, publicID string) (Admin, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT f.id FROM families f JOIN family_admins fa ON fa.family_id=f.id WHERE f.public_id=? AND fa.user_id=?`, publicID, a.ID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Admin{}, ErrNotFound
	}
	if err != nil {
		return Admin{}, err
	}
	a.FamilyID = id
	return a, nil
}

type ManagedDevice struct {
	Device
	Families []Family `json:"families"`
}

func (s *Service) OwnedDevices(ctx context.Context, a Admin) ([]ManagedDevice, error) {
	if err := s.authorizeAdmin(ctx, a); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT d.id,d.family_id,d.name,d.created_at,d.revoked_at FROM devices d WHERE EXISTS(SELECT 1 FROM family_admins fa WHERE fa.family_id=d.family_id AND fa.user_id=?) ORDER BY d.created_at,d.id`, a.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ManagedDevice, 0)
	index := make(map[string]int)
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		index[d.ID] = len(result)
		result = append(result, ManagedDevice{Device: d, Families: make([]Family, 0)})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	memberships, err := tx.QueryContext(ctx, `SELECT fd.device_id,f.id,f.public_id,f.name FROM family_devices fd JOIN families f ON f.id=fd.family_id JOIN family_admins fa ON fa.family_id=f.id WHERE fa.user_id=? ORDER BY f.id`, a.ID)
	if err != nil {
		return nil, err
	}
	defer memberships.Close()
	for memberships.Next() {
		var id string
		var f Family
		if err = memberships.Scan(&id, &f.ID, &f.PublicID, &f.Name); err != nil {
			return nil, err
		}
		if i, ok := index[id]; ok {
			result[i].Families = append(result[i].Families, f)
		}
	}
	return result, memberships.Err()
}

func (s *Service) SetMembership(ctx context.Context, a Admin, publicID, deviceID string, add bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var familyID int64
	err = tx.QueryRowContext(ctx, `SELECT f.id FROM families f JOIN family_admins fa ON fa.family_id=f.id WHERE f.public_id=? AND fa.user_id=?`, publicID, a.ID).Scan(&familyID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var found int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM devices d JOIN family_admins fa ON fa.family_id=d.family_id WHERE d.id=? AND fa.user_id=? AND d.revoked_at IS NULL`, deviceID, a.ID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if add {
		_, err = tx.ExecContext(ctx, `INSERT INTO family_devices(family_id,device_id) VALUES(?,?) ON CONFLICT DO NOTHING`, familyID, deviceID)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM family_devices WHERE family_id=? AND device_id=?`, familyID, deviceID)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
