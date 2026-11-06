package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type DeviceConfig struct {
	Version                   int64  `json:"version"`
	MovingIntervalSeconds     int    `json:"moving_interval_seconds"`
	StationaryIntervalSeconds int    `json:"stationary_interval_seconds"`
	MovingHighAccuracy        bool   `json:"moving_high_accuracy"`
	QuietHoursStart           string `json:"quiet_hours_start"`
	QuietHoursEnd             string `json:"quiet_hours_end"`
}

func (c DeviceConfig) Valid() bool {
	validTime := func(s string) bool { t, err := time.Parse("15:04", s); return err == nil && t.Format("15:04") == s }
	return c.MovingIntervalSeconds >= 60 && c.MovingIntervalSeconds <= 3600 && c.MovingIntervalSeconds%60 == 0 && c.StationaryIntervalSeconds >= c.MovingIntervalSeconds && c.StationaryIntervalSeconds <= 3600 && c.StationaryIntervalSeconds%60 == 0 && validTime(c.QuietHoursStart) && validTime(c.QuietHoursEnd) && c.QuietHoursStart != c.QuietHoursEnd
}
func (c DeviceConfig) StaleAfterSeconds() int { return max(420, c.StationaryIntervalSeconds+120) }

type ConfigReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func ReadDeviceConfig(ctx context.Context, q ConfigReader, id string) (DeviceConfig, error) {
	var c DeviceConfig
	err := q.QueryRowContext(ctx, `SELECT version,moving_interval_seconds,stationary_interval_seconds,moving_high_accuracy,quiet_hours_start,quiet_hours_end FROM device_configs WHERE device_id=?`, id).Scan(&c.Version, &c.MovingIntervalSeconds, &c.StationaryIntervalSeconds, &c.MovingHighAccuracy, &c.QuietHoursStart, &c.QuietHoursEnd)
	return c, err
}
func authorizeDeviceOwner(ctx context.Context, q ConfigReader, a Admin, id string) error {
	var found int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM devices d JOIN device_admins fa ON fa.device_id=d.id WHERE d.id=? AND fa.user_id=? AND d.revoked_at IS NULL`, id, a.ID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func (s *Service) DeviceConfig(ctx context.Context, a Admin, id string) (DeviceConfig, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DeviceConfig{}, err
	}
	defer tx.Rollback()
	if err = authorizeDeviceOwner(ctx, tx, a, id); err != nil {
		return DeviceConfig{}, err
	}
	return ReadDeviceConfig(ctx, tx, id)
}
func (s *Service) UpdateDeviceConfig(ctx context.Context, a Admin, id string, c DeviceConfig) (DeviceConfig, error) {
	if !c.Valid() {
		return DeviceConfig{}, ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DeviceConfig{}, err
	}
	defer tx.Rollback()
	if err = authorizeDeviceOwner(ctx, tx, a, id); err != nil {
		return DeviceConfig{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE device_configs SET version=version+1,moving_interval_seconds=?,stationary_interval_seconds=?,moving_high_accuracy=?,quiet_hours_start=?,quiet_hours_end=? WHERE device_id=?`, c.MovingIntervalSeconds, c.StationaryIntervalSeconds, c.MovingHighAccuracy, c.QuietHoursStart, c.QuietHoursEnd, id); err != nil {
		return DeviceConfig{}, err
	}
	c, err = ReadDeviceConfig(ctx, tx, id)
	if err != nil {
		return DeviceConfig{}, err
	}
	if err = tx.Commit(); err != nil {
		return DeviceConfig{}, err
	}
	return c, nil
}
