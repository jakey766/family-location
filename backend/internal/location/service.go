// Package location stores one current point per device, never a trajectory.
package location

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"family-location/backend/internal/auth"
)

const (
	LeaseDuration         = 7 * time.Minute
	StaleAfter            = 5 * time.Minute
	MaxCaptureAge         = 24 * time.Hour
	FutureTolerance       = 2 * time.Minute
	MaxSequence     int64 = 1<<53 - 1
)

var (
	ErrSession  = errors.New("sharing session mismatch or stopped")
	ErrSequence = errors.New("out of order location")
)

type Service struct {
	db  *sql.DB
	now func() time.Time
}

func New(db *sql.DB) *Service { return &Service{db: db, now: time.Now} }

type Sharing struct {
	SharingSessionID string `json:"sharing_session_id"`
}
type Input struct {
	SharingSessionID string    `json:"sharing_session_id"`
	Sequence         int64     `json:"sequence"`
	Latitude         float64   `json:"latitude"`
	Longitude        float64   `json:"longitude"`
	CoordinateSystem string    `json:"coordinate_system"`
	AccuracyM        float64   `json:"accuracy_m"`
	BatteryPercent   *int      `json:"battery_percent"`
	CapturedAt       time.Time `json:"captured_at"`
}
type UploadResult struct {
	Accepted bool `json:"accepted"`
}
type Location struct {
	DeviceID         string    `json:"device_id"`
	Name             string    `json:"name"`
	Sequence         int64     `json:"sequence"`
	Latitude         float64   `json:"latitude"`
	Longitude        float64   `json:"longitude"`
	CoordinateSystem string    `json:"coordinate_system"`
	AccuracyM        float64   `json:"accuracy_m"`
	BatteryPercent   *int      `json:"battery_percent"`
	CapturedAt       time.Time `json:"captured_at"`
	ReceivedAt       time.Time `json:"received_at"`
	LeaseExpiresAt   time.Time `json:"lease_expires_at"`
	Stale            bool      `json:"stale"`
}
type Snapshot struct {
	Locations  []Location `json:"locations"`
	ServerTime time.Time  `json:"server_time"`
}
type state struct {
	session  sql.NullString
	sequence int64
	captured sql.NullInt64
}

// Recheck identity inside the write transaction: HTTP authentication may have
// completed before a concurrent revocation acquired the database connection.
func loadDevice(ctx context.Context, tx *sql.Tx, d auth.Device) (state, error) {
	var v state
	e := tx.QueryRowContext(ctx, `SELECT sharing_session_id,last_sequence,last_captured_at FROM devices WHERE id=? AND family_id=? AND revoked_at IS NULL`, d.ID, d.FamilyID).Scan(&v.session, &v.sequence, &v.captured)
	if errors.Is(e, sql.ErrNoRows) {
		return v, auth.ErrUnauthorized
	}
	return v, e
}
func validInput(p Input, now time.Time) bool {
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	return p.SharingSessionID != "" && len(p.SharingSessionID) <= 128 && p.Sequence > 0 && p.Sequence <= MaxSequence &&
		finite(p.Latitude) && p.Latitude >= -90 && p.Latitude <= 90 && finite(p.Longitude) && p.Longitude >= -180 && p.Longitude <= 180 &&
		finite(p.AccuracyM) && p.AccuracyM >= 0 && p.CoordinateSystem == "GCJ-02" &&
		(p.BatteryPercent == nil || (*p.BatteryPercent >= 0 && *p.BatteryPercent <= 100)) &&
		!p.CapturedAt.Before(now.Add(-MaxCaptureAge)) && !p.CapturedAt.After(now.Add(FutureTolerance))
}
