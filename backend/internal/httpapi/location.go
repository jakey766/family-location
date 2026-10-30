package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"family-location/backend/internal/auth"
	"family-location/backend/internal/location"
)

type locationAPI struct {
	auth    *authAPI
	service *location.Service
}

func registerLocations(mux *http.ServeMux, db *sql.DB) {
	a := &locationAPI{auth: &authAPI{service: auth.New(db)}, service: location.New(db)}
	mux.HandleFunc("POST /api/v1/device/sharing/start", a.start)
	mux.HandleFunc("POST /api/v1/device/sharing/stop", a.stop)
	mux.HandleFunc("PUT /api/v1/device/location", a.upload)
	mux.HandleFunc("GET /api/v1/locations", a.list)
}
func (a *locationAPI) start(w http.ResponseWriter, r *http.Request) {
	d, ok := a.auth.device(w, r)
	if !ok {
		return
	}
	if !decodeJSON(w, r, &struct{}{}) {
		return
	}
	result, e := a.service.Start(r.Context(), d)
	if e != nil {
		locationError(w, r, e)
		return
	}
	writeJSON(w, r, 201, result)
}
func (a *locationAPI) stop(w http.ResponseWriter, r *http.Request) {
	d, ok := a.auth.device(w, r)
	if !ok {
		return
	}
	var input location.Sharing
	if !decodeJSON(w, r, &input) {
		return
	}
	if e := a.service.Stop(r.Context(), d, input.SharingSessionID); e != nil {
		locationError(w, r, e)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (a *locationAPI) upload(w http.ResponseWriter, r *http.Request) {
	d, ok := a.auth.device(w, r)
	if !ok {
		return
	}
	// Pointers distinguish a missing/null number from a valid zero value.
	var input struct {
		SharingSessionID string     `json:"sharing_session_id"`
		Sequence         *int64     `json:"sequence"`
		Latitude         *float64   `json:"latitude"`
		Longitude        *float64   `json:"longitude"`
		CoordinateSystem string     `json:"coordinate_system"`
		AccuracyM        *float64   `json:"accuracy_m"`
		BatteryPercent   *int       `json:"battery_percent"`
		CapturedAt       *time.Time `json:"captured_at"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if input.Sequence == nil || input.Latitude == nil || input.Longitude == nil || input.AccuracyM == nil || input.CapturedAt == nil {
		apiError(w, r, 400, "invalid_input")
		return
	}
	result, e := a.service.Upload(r.Context(), d, location.Input{SharingSessionID: input.SharingSessionID, Sequence: *input.Sequence, Latitude: *input.Latitude, Longitude: *input.Longitude, CoordinateSystem: input.CoordinateSystem, AccuracyM: *input.AccuracyM, BatteryPercent: input.BatteryPercent, CapturedAt: *input.CapturedAt})
	if e != nil {
		locationError(w, r, e)
		return
	}
	writeJSON(w, r, 200, result)
}
func (a *locationAPI) list(w http.ResponseWriter, r *http.Request) {
	session, ok := a.auth.admin(w, r, false)
	if !ok {
		return
	}
	selected, ok := a.auth.selectedAdmin(w, r, session.User)
	if !ok {
		return
	}
	result, e := a.service.List(r.Context(), selected)
	if e != nil {
		locationError(w, r, e)
		return
	}
	writeJSON(w, r, 200, result)
}
func locationError(w http.ResponseWriter, r *http.Request, e error) {
	switch {
	case errors.Is(e, location.ErrSession):
		apiError(w, r, 409, "sharing_session_conflict")
	case errors.Is(e, location.ErrSequence):
		apiError(w, r, 409, "out_of_order")
	default:
		authError(w, r, e)
	}
}
