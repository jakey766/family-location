package httpapi

import (
	"family-location/backend/internal/auth"
	"family-location/backend/internal/location"
	"net/http"
	"time"
)

const familyBrowserCookie = "family_viewer"

func namedCookieToken(r *http.Request, name string) string {
	token := ""
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == name {
			token = cookie.Value
			count++
		}
	}
	if count != 1 {
		return ""
	}
	return token
}
func (a *webAPI) registerFamilyAccess(mux *http.ServeMux, locations *location.Service) {
	mux.HandleFunc("POST /api/v1/family/{family}/session", a.familyLogin)
	mux.HandleFunc("DELETE /api/v1/family/{family}/session", a.familyLogout)
	mux.HandleFunc("GET /api/v1/family/{family}/snapshot", func(w http.ResponseWriter, r *http.Request) {
		result, err := locations.FamilySnapshot(r.Context(), namedCookieToken(r, familyBrowserCookie), r.PathValue("family"))
		if err != nil {
			authError(w, r, err)
			return
		}
		writeJSON(w, r, 200, result)
	})
	mux.HandleFunc("GET /api/v1/family/{family}/web-config", func(w http.ResponseWriter, r *http.Request) {
		if _, err := a.auth.service.FamilyAccess(r.Context(), namedCookieToken(r, familyBrowserCookie), r.PathValue("family")); err != nil {
			authError(w, r, err)
			return
		}
		a.writeConfiguration(w, r)
	})
}
func (a *webAPI) familyLogin(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		apiError(w, r, 403, "forbidden")
		return
	}
	var input struct {
		Passcode string `json:"passcode"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	session, err := a.auth.service.LoginFamily(r.Context(), r.PathValue("family"), input.Passcode, namedCookieToken(r, familyBrowserCookie), clientIP(r))
	if err != nil {
		authError(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: familyBrowserCookie, Value: session.Token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: int(time.Until(session.CookieExpiresAt).Seconds()), Expires: session.CookieExpiresAt})
	writeJSON(w, r, 200, session)
}
func (a *webAPI) familyLogout(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		apiError(w, r, 403, "forbidden")
		return
	}
	if err := a.auth.service.LogoutFamily(r.Context(), namedCookieToken(r, familyBrowserCookie), r.PathValue("family")); err != nil {
		authError(w, r, err)
		return
	}
	w.WriteHeader(204)
}
func (a *authAPI) updateFamily(w http.ResponseWriter, r *http.Request) {
	session, ok := a.admin(w, r, true)
	if !ok {
		return
	}
	key := r.PathValue("family")
	if r.Method == http.MethodDelete {
		if err := a.service.DeleteFamily(r.Context(), session.User, key); err != nil {
			authError(w, r, err)
			return
		}
		w.WriteHeader(204)
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	result, err := a.service.RenameFamily(r.Context(), session.User, key, input.Name)
	if err != nil {
		authError(w, r, err)
		return
	}
	writeJSON(w, r, 200, result)
}
func (a *authAPI) familyPasscode(w http.ResponseWriter, r *http.Request) {
	session, ok := a.admin(w, r, true)
	if !ok {
		return
	}
	// Raw fields distinguish omitted (generate) from explicit null (invalid).
	var input map[string]any
	if !decodeJSON(w, r, &input) {
		return
	}
	var passcode *string
	for key, value := range input {
		if key != "passcode" {
			authError(w, r, auth.ErrInvalidInput)
			return
		}
		v, ok := value.(string)
		if !ok {
			authError(w, r, auth.ErrInvalidInput)
			return
		}
		passcode = &v
	}
	if input == nil {
		authError(w, r, auth.ErrInvalidInput)
		return
	}
	result, err := a.service.SetFamilyPasscode(r.Context(), session.User, r.PathValue("family"), passcode)
	if err != nil {
		authError(w, r, err)
		return
	}
	writeJSON(w, r, 200, map[string]string{"passcode": result})
}
func (a *authAPI) purgeDevice(w http.ResponseWriter, r *http.Request) {
	session, ok := a.admin(w, r, true)
	if !ok {
		return
	}
	if err := a.service.PurgeDevice(r.Context(), session.User, r.PathValue("id")); err != nil {
		authError(w, r, err)
		return
	}
	w.WriteHeader(204)
}
