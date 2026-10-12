package httpapi

import (
	"crypto/subtle"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"family-location/backend/internal/auth"
)

const sessionCookie = "family_session"

type authAPI struct{ service *auth.Service }

func registerAuth(mux *http.ServeMux, db *sql.DB) {
	a := &authAPI{service: auth.New(db)}
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("GET /api/v1/auth/me", a.me)
	mux.HandleFunc("POST /api/v1/auth/logout", a.logout)
	mux.HandleFunc("POST /api/v1/pairing-codes", a.createPairing)
	mux.HandleFunc("POST /api/v1/devices/pair", a.pair)
	mux.HandleFunc("GET /api/v1/devices", a.devices)
	mux.HandleFunc("DELETE /api/v1/devices/{id}", a.revokeDevice)
	mux.HandleFunc("GET /api/v1/device/me", a.deviceMe)
}

func (a *authAPI) login(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		apiError(w, r, 403, "forbidden")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	session, err := a.service.Login(r.Context(), input.Username, input.Password, clientIP(r))
	if err != nil {
		authError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: session.Token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 24 * 60 * 60, Expires: session.ExpiresAt})
	writeJSON(w, r, 200, session)
}

func (a *authAPI) me(w http.ResponseWriter, r *http.Request) {
	session, ok := a.admin(w, r, false)
	if ok {
		writeJSON(w, r, 200, session)
	}
}

func (a *authAPI) logout(w http.ResponseWriter, r *http.Request) {
	_, ok := a.admin(w, r, true)
	if !ok {
		return
	}
	if err := a.service.Logout(r.Context(), cookieToken(r)); err != nil {
		authError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	w.WriteHeader(http.StatusNoContent)
}

func (a *authAPI) createPairing(w http.ResponseWriter, r *http.Request) {
	session, ok := a.admin(w, r, true)
	if !ok {
		return
	}
	if !decodeJSON(w, r, &struct{}{}) {
		return
	}
	pairing, err := a.service.CreatePairing(r.Context(), session.User)
	if err != nil {
		authError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, pairing)
}

func (a *authAPI) pair(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID   string `json:"pairing_id"`
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	device, err := a.service.Pair(r.Context(), input.ID, input.Code, input.Name, clientIP(r))
	if err != nil {
		authError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusCreated, device)
}

func (a *authAPI) devices(w http.ResponseWriter, r *http.Request) {
	session, ok := a.admin(w, r, false)
	if !ok {
		return
	}
	devices, err := a.service.Devices(r.Context(), session.User)
	if err != nil {
		authError(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"devices": devices})
}

func (a *authAPI) revokeDevice(w http.ResponseWriter, r *http.Request) {
	session, ok := a.admin(w, r, true)
	if !ok {
		return
	}
	if err := a.service.RevokeDevice(r.Context(), session.User, r.PathValue("id")); err != nil {
		authError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *authAPI) deviceMe(w http.ResponseWriter, r *http.Request) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		apiError(w, r, 401, "unauthorized")
		return
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		apiError(w, r, 401, "unauthorized")
		return
	}
	device, err := a.service.AuthenticateDevice(r.Context(), parts[1])
	if err != nil {
		authError(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]any{"device": device})
}

func (a *authAPI) admin(w http.ResponseWriter, r *http.Request, mutation bool) (auth.Session, bool) {
	session, err := a.service.AuthenticateSession(r.Context(), cookieToken(r))
	if err != nil {
		authError(w, r, err)
		return auth.Session{}, false
	}
	if mutation && (!sameOrigin(r) || len(r.Header.Values("X-CSRF-Token")) != 1 || subtle.ConstantTimeCompare([]byte(session.CSRFToken), []byte(r.Header.Get("X-CSRF-Token"))) != 1) {
		apiError(w, r, 403, "forbidden")
		return auth.Session{}, false
	}
	return session, true
}

func cookieToken(r *http.Request) string {
	var token string
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == sessionCookie {
			token = cookie.Value
			count++
		}
	}
	if count != 1 {
		return ""
	}
	return token
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func authError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		apiError(w, r, 401, "invalid_credentials")
	case errors.Is(err, auth.ErrUnauthorized):
		apiError(w, r, 401, "unauthorized")
	case errors.Is(err, auth.ErrInvalidInput):
		apiError(w, r, 400, "invalid_input")
	case errors.Is(err, auth.ErrPairingInvalid):
		apiError(w, r, 400, "invalid_pairing")
	case errors.Is(err, auth.ErrNotFound):
		apiError(w, r, 404, "not_found")
	case errors.Is(err, auth.ErrConflict):
		apiError(w, r, 409, "conflict")
	case errors.Is(err, auth.ErrRateLimited):
		w.Header().Set("Retry-After", "600")
		apiError(w, r, 429, "rate_limited")
	default:
		apiError(w, r, 500, "internal_error")
	}
}
