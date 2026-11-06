package location

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"family-location/backend/internal/auth"
)

func (s *Service) Upload(ctx context.Context, d auth.Device, p Input) (UploadResult, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return UploadResult{}, e
	}
	defer tx.Rollback()
	v, e := loadDevice(ctx, tx, d)
	if e != nil {
		return UploadResult{}, e
	}
	// Sample after acquiring the connection so waiting cannot extend a stale lease.
	now := s.now().UTC()
	if !validInput(p, now) {
		return UploadResult{}, auth.ErrInvalidInput
	}
	if !v.session.Valid || v.session.String != p.SharingSessionID {
		return UploadResult{}, ErrSession
	}
	if p.Sequence < v.sequence {
		return UploadResult{}, ErrSequence
	}
	config, e := auth.ReadDeviceConfig(ctx, tx, d.ID)
	if e != nil {
		return UploadResult{}, e
	}
	if p.Sequence == v.sequence {
		return UploadResult{Accepted: false, Config: config}, nil
	}
	captured := p.CapturedAt.UnixMilli()
	if v.captured.Valid && captured < v.captured.Int64 {
		return UploadResult{}, ErrSequence
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO latest_locations(device_id,latitude,longitude,accuracy_m,battery_percent,captured_at,received_at,lease_expires_at)
 VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(device_id) DO UPDATE SET latitude=excluded.latitude,longitude=excluded.longitude,accuracy_m=excluded.accuracy_m,battery_percent=excluded.battery_percent,captured_at=excluded.captured_at,received_at=excluded.received_at,lease_expires_at=excluded.lease_expires_at`, d.ID, p.Latitude, p.Longitude, p.AccuracyM, p.BatteryPercent, captured, now.UnixMilli(), now.Add(time.Duration(config.StaleAfterSeconds())*time.Second).UnixMilli()); e != nil {
		return UploadResult{}, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE devices SET last_sequence=?,last_captured_at=? WHERE id=?`, p.Sequence, captured, d.ID); e != nil {
		return UploadResult{}, e
	}
	if e = tx.Commit(); e != nil {
		return UploadResult{}, e
	}
	return UploadResult{Accepted: true, Config: config}, nil
}
func (s *Service) List(ctx context.Context, a auth.Admin) (Snapshot, error) {
	return s.listAdmin(ctx, a, false)
}
func (s *Service) ListOwned(ctx context.Context, a auth.Admin) (Snapshot, error) {
	return s.listAdmin(ctx, a, true)
}
func (s *Service) listAdmin(ctx context.Context, a auth.Admin, owned bool) (Snapshot, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Snapshot{}, e
	}
	defer tx.Rollback()
	var found int
	if owned {
		e = tx.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id=?`, a.ID).Scan(&found)
	} else {
		e = tx.QueryRowContext(ctx, `SELECT 1 FROM family_admins fa JOIN families f ON f.id=fa.family_id WHERE fa.user_id=? AND fa.family_id=? AND f.deleted_at IS NULL`, a.ID, a.FamilyID).Scan(&found)
	}
	if errors.Is(e, sql.ErrNoRows) {
		if owned {
			return Snapshot{}, auth.ErrUnauthorized
		}
		var deleted sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT deleted_at FROM families WHERE id=?`, a.FamilyID).Scan(&deleted); err == nil && deleted.Valid {
			return Snapshot{}, auth.ErrNotFound
		}
		return Snapshot{}, auth.ErrUnauthorized
	}
	if e != nil {
		return Snapshot{}, e
	}
	filter := `EXISTS(SELECT 1 FROM family_devices fd WHERE fd.device_id=d.id AND fd.family_id=?)`
	id := a.FamilyID
	if owned {
		filter = `EXISTS(SELECT 1 FROM device_admins da WHERE da.device_id=d.id AND da.user_id=?)`
		id = a.ID
	}
	return readLocations(ctx, tx, s.now().UTC(), filter, id)
}

type FamilyDevice struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	RevokedAt *time.Time `json:"revoked_at"`
}
type FamilySnapshot struct {
	Family     auth.FamilyAccess `json:"family"`
	Devices    []FamilyDevice    `json:"devices"`
	Locations  []Location        `json:"locations"`
	ServerTime time.Time         `json:"server_time"`
	ExpiresAt  time.Time         `json:"expires_at"`
}

func (s *Service) FamilySnapshot(ctx context.Context, token, key string) (FamilySnapshot, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return FamilySnapshot{}, err
	}
	defer tx.Rollback()
	now := s.now().UTC()
	family, err := auth.ReadFamilyAccess(ctx, tx, token, key, now)
	if err != nil {
		return FamilySnapshot{}, err
	}
	result := FamilySnapshot{Family: family, Devices: make([]FamilyDevice, 0), ServerTime: now, ExpiresAt: family.ExpiresAt}
	rows, err := tx.QueryContext(ctx, `SELECT d.id,d.name FROM devices d JOIN family_devices fd ON fd.device_id=d.id WHERE fd.family_id=? AND d.revoked_at IS NULL ORDER BY d.created_at,d.id`, family.FamilyID)
	if err != nil {
		return FamilySnapshot{}, err
	}
	for rows.Next() {
		var d FamilyDevice
		if err = rows.Scan(&d.ID, &d.Name); err != nil {
			rows.Close()
			return FamilySnapshot{}, err
		}
		result.Devices = append(result.Devices, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return FamilySnapshot{}, err
	}
	locations, err := readLocations(ctx, tx, now, `EXISTS(SELECT 1 FROM family_devices fd WHERE fd.device_id=d.id AND fd.family_id=?)`, family.FamilyID)
	if err != nil {
		return FamilySnapshot{}, err
	}
	result.Locations = locations.Locations
	return result, nil
}
func readLocations(ctx context.Context, tx *sql.Tx, now time.Time, filter string, id int64) (Snapshot, error) {
	result := Snapshot{Locations: make([]Location, 0), ServerTime: now}
	rows, e := tx.QueryContext(ctx, `SELECT d.id,d.name,d.last_sequence,l.latitude,l.longitude,l.accuracy_m,l.battery_percent,l.captured_at,l.received_at,l.lease_expires_at,c.stationary_interval_seconds
 FROM latest_locations l JOIN devices d ON d.id=l.device_id JOIN device_configs c ON c.device_id=d.id
 WHERE `+filter+` AND d.revoked_at IS NULL AND d.sharing_session_id IS NOT NULL ORDER BY d.id`, id)
	if e != nil {
		return Snapshot{}, e
	}
	defer rows.Close()
	for rows.Next() {
		var v Location
		var battery sql.NullInt64
		var captured, received, expires int64
		var stationary int
		if e = rows.Scan(&v.DeviceID, &v.Name, &v.Sequence, &v.Latitude, &v.Longitude, &v.AccuracyM, &battery, &captured, &received, &expires, &stationary); e != nil {
			return Snapshot{}, e
		}
		if battery.Valid {
			b := int(battery.Int64)
			v.BatteryPercent = &b
		}
		v.CoordinateSystem = "GCJ-02"
		v.CapturedAt = time.UnixMilli(captured).UTC()
		v.ReceivedAt = time.UnixMilli(received).UTC()
		v.StaleAfterSeconds = max(420, stationary+120)
		// Recompute from the last accepted report so changing settings takes effect
		// without counting duplicate uploads as new reports.
		v.LeaseExpiresAt = v.ReceivedAt.Add(time.Duration(v.StaleAfterSeconds) * time.Second)
		v.Stale = !v.CapturedAt.After(now.Add(-time.Duration(v.StaleAfterSeconds)*time.Second)) || !v.LeaseExpiresAt.After(now)
		result.Locations = append(result.Locations, v)
	}
	if e = rows.Err(); e != nil {
		return Snapshot{}, e
	}
	return result, nil
}
