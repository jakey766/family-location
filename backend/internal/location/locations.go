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
	if p.Sequence == v.sequence {
		return UploadResult{Accepted: false}, nil
	}
	captured := p.CapturedAt.UnixMilli()
	if v.captured.Valid && captured < v.captured.Int64 {
		return UploadResult{}, ErrSequence
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO latest_locations(device_id,latitude,longitude,accuracy_m,battery_percent,captured_at,received_at,lease_expires_at)
 VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(device_id) DO UPDATE SET latitude=excluded.latitude,longitude=excluded.longitude,accuracy_m=excluded.accuracy_m,battery_percent=excluded.battery_percent,captured_at=excluded.captured_at,received_at=excluded.received_at,lease_expires_at=excluded.lease_expires_at`, d.ID, p.Latitude, p.Longitude, p.AccuracyM, p.BatteryPercent, captured, now.UnixMilli(), now.Add(LeaseDuration).UnixMilli()); e != nil {
		return UploadResult{}, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE devices SET last_sequence=?,last_captured_at=? WHERE id=?`, p.Sequence, captured, d.ID); e != nil {
		return UploadResult{}, e
	}
	if e = tx.Commit(); e != nil {
		return UploadResult{}, e
	}
	return UploadResult{Accepted: true}, nil
}
func (s *Service) List(ctx context.Context, a auth.Admin) (Snapshot, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Snapshot{}, e
	}
	defer tx.Rollback()
	var found int
	e = tx.QueryRowContext(ctx, `SELECT 1 FROM users WHERE id=? AND family_id=?`, a.ID, a.FamilyID).Scan(&found)
	if errors.Is(e, sql.ErrNoRows) {
		return Snapshot{}, auth.ErrUnauthorized
	}
	if e != nil {
		return Snapshot{}, e
	}
	now := s.now().UTC()
	result := Snapshot{Locations: make([]Location, 0), ServerTime: now}
	rows, e := tx.QueryContext(ctx, `SELECT d.id,d.name,d.last_sequence,l.latitude,l.longitude,l.accuracy_m,l.battery_percent,l.captured_at,l.received_at,l.lease_expires_at
 FROM latest_locations l JOIN devices d ON d.id=l.device_id
 WHERE d.family_id=? AND d.revoked_at IS NULL AND d.sharing_session_id IS NOT NULL ORDER BY d.id`, a.FamilyID)
	if e != nil {
		return Snapshot{}, e
	}
	defer rows.Close()
	for rows.Next() {
		var v Location
		var battery sql.NullInt64
		var captured, received, expires int64
		if e = rows.Scan(&v.DeviceID, &v.Name, &v.Sequence, &v.Latitude, &v.Longitude, &v.AccuracyM, &battery, &captured, &received, &expires); e != nil {
			return Snapshot{}, e
		}
		if battery.Valid {
			b := int(battery.Int64)
			v.BatteryPercent = &b
		}
		v.CoordinateSystem = "GCJ-02"
		v.CapturedAt = time.UnixMilli(captured).UTC()
		v.ReceivedAt = time.UnixMilli(received).UTC()
		v.LeaseExpiresAt = time.UnixMilli(expires).UTC()
		v.Stale = !v.CapturedAt.After(now.Add(-StaleAfter)) || !v.LeaseExpiresAt.After(now)
		result.Locations = append(result.Locations, v)
	}
	if e = rows.Err(); e != nil {
		return Snapshot{}, e
	}
	return result, nil
}
