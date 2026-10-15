package location

import (
	"context"
	"crypto/rand"
	"encoding/base64"

	"family-location/backend/internal/auth"
)

func (s *Service) Start(ctx context.Context, d auth.Device) (Sharing, error) {
	var bytes [32]byte
	if _, e := rand.Read(bytes[:]); e != nil {
		return Sharing{}, e
	}
	result := Sharing{SharingSessionID: base64.RawURLEncoding.EncodeToString(bytes[:])}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Sharing{}, e
	}
	defer tx.Rollback()
	if _, e = loadDevice(ctx, tx, d); e != nil {
		return Sharing{}, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE devices SET sharing_session_id=?,last_sequence=0,last_captured_at=NULL WHERE id=?`, result.SharingSessionID, d.ID); e != nil {
		return Sharing{}, e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM latest_locations WHERE device_id=?`, d.ID); e != nil {
		return Sharing{}, e
	}
	if e = tx.Commit(); e != nil {
		return Sharing{}, e
	}
	return result, nil
}
func (s *Service) Stop(ctx context.Context, d auth.Device, id string) error {
	if id == "" || len(id) > 128 {
		return auth.ErrInvalidInput
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	v, e := loadDevice(ctx, tx, d)
	if e != nil {
		return e
	}
	if v.session.Valid && v.session.String != id {
		return ErrSession
	}
	if _, e = tx.ExecContext(ctx, `UPDATE devices SET sharing_session_id=NULL,last_sequence=0,last_captured_at=NULL WHERE id=?`, d.ID); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM latest_locations WHERE device_id=?`, d.ID); e != nil {
		return e
	}
	return tx.Commit()
}
