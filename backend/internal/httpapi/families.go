package httpapi

import (
	"encoding/json"
	"family-location/backend/internal/auth"
	"net/http"
	"net/url"
)

func (a *authAPI) registerFamilies(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/families", a.families)
	mux.HandleFunc("POST /api/v1/families", a.createFamily)
	mux.HandleFunc("PATCH /api/v1/families/{family}", a.updateFamily)
	mux.HandleFunc("DELETE /api/v1/families/{family}", a.updateFamily)
	mux.HandleFunc("PUT /api/v1/families/{family}/passcode", a.familyPasscode)
	mux.HandleFunc("DELETE /api/v1/devices/{id}/purge", a.purgeDevice)
	mux.HandleFunc("PUT /api/v1/families/{family}/devices/{id}", a.membership)
	mux.HandleFunc("DELETE /api/v1/families/{family}/devices/{id}", a.membership)
	mux.HandleFunc("GET /api/v1/devices/{id}/config", a.deviceConfig)
	mux.HandleFunc("PUT /api/v1/devices/{id}/config", a.deviceConfig)
}
func (a *authAPI) selectedAdmin(w http.ResponseWriter, r *http.Request, user auth.Admin) (auth.Admin, bool) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		authError(w, r, auth.ErrNotFound)
		return auth.Admin{}, false
	}
	values, present := query["family"]
	if !present {
		return user, true
	}
	if len(values) != 1 || values[0] == "" {
		authError(w, r, auth.ErrNotFound)
		return auth.Admin{}, false
	}
	selected, err := a.service.SelectFamily(r.Context(), user, values[0])
	if err != nil {
		authError(w, r, err)
		return auth.Admin{}, false
	}
	return selected, true
}
func (a *authAPI) families(w http.ResponseWriter, r *http.Request) {
	session, ok := a.admin(w, r, false)
	if !ok {
		return
	}
	result, err := a.service.Families(r.Context(), session.User)
	if err != nil {
		authError(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"families": result})
}
func (a *authAPI) createFamily(w http.ResponseWriter, r *http.Request) {
	session, ok := a.admin(w, r, true)
	if !ok {
		return
	}
	var input struct {
		Name     string          `json:"name"`
		Passcode json.RawMessage `json:"passcode"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	passcode := ""
	if input.Passcode != nil {
		if err := json.Unmarshal(input.Passcode, &passcode); err != nil {
			authError(w, r, auth.ErrInvalidInput)
			return
		}
		if len(passcode) != 7 {
			authError(w, r, auth.ErrInvalidInput)
			return
		}
	}
	result, err := a.service.CreateFamily(r.Context(), session.User, input.Name, passcode)
	if err != nil {
		authError(w, r, err)
		return
	}
	writeJSON(w, r, 201, result)
}
func (a *authAPI) membership(w http.ResponseWriter, r *http.Request) {
	session, ok := a.admin(w, r, true)
	if !ok {
		return
	}
	add := r.Method == http.MethodPut
	if add && !decodeJSON(w, r, &struct{}{}) {
		return
	}
	if err := a.service.SetMembership(r.Context(), session.User, r.PathValue("family"), r.PathValue("id"), add); err != nil {
		authError(w, r, err)
		return
	}
	w.WriteHeader(204)
}
func (a *authAPI) deviceConfig(w http.ResponseWriter, r *http.Request) {
	session, ok := a.admin(w, r, r.Method == http.MethodPut)
	if !ok {
		return
	}
	var result auth.DeviceConfig
	var err error
	if r.Method == http.MethodGet {
		result, err = a.service.DeviceConfig(r.Context(), session.User, r.PathValue("id"))
	} else {
		var input struct {
			Moving     *int    `json:"moving_interval_seconds"`
			Stationary *int    `json:"stationary_interval_seconds"`
			Accuracy   *bool   `json:"moving_high_accuracy"`
			Start      *string `json:"quiet_hours_start"`
			End        *string `json:"quiet_hours_end"`
		}
		if !decodeJSON(w, r, &input) {
			return
		}
		if input.Moving == nil || input.Stationary == nil || input.Accuracy == nil || input.Start == nil || input.End == nil {
			authError(w, r, auth.ErrInvalidInput)
			return
		}
		result, err = a.service.UpdateDeviceConfig(r.Context(), session.User, r.PathValue("id"), auth.DeviceConfig{MovingIntervalSeconds: *input.Moving, StationaryIntervalSeconds: *input.Stationary, MovingHighAccuracy: *input.Accuracy, QuietHoursStart: *input.Start, QuietHoursEnd: *input.End})
	}
	if err != nil {
		authError(w, r, err)
		return
	}
	writeJSON(w, r, 200, result)
}

// Parse selection once and reject ambiguous filters before resolving a family.
func ownedScope(w http.ResponseWriter, r *http.Request) (bool, bool) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		authError(w, r, auth.ErrNotFound)
		return false, false
	}
	values, exists := q["scope"]
	if !exists {
		return false, true
	}
	if len(values) != 1 || values[0] != "owned" || q.Has("family") {
		authError(w, r, auth.ErrInvalidInput)
		return false, false
	}
	return true, true
}
