package httpapi

import (
	"database/sql"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"family-location/backend/internal/auth"
	"family-location/backend/internal/location"
	"family-location/backend/internal/webui"
)

type WebConfig struct {
	AMapJSKey          string
	AMapSecurityJSCode string
}

func (c WebConfig) mapEnabled() bool { return c.AMapJSKey != "" && c.AMapSecurityJSCode != "" }

type webAPI struct {
	auth   *authAPI
	config WebConfig
	client *http.Client
}

func registerWeb(mux *http.ServeMux, db *sql.DB, cfg WebConfig, client *http.Client) *webAPI {
	a := &webAPI{auth: &authAPI{service: auth.New(db)}, config: cfg, client: client}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		target := "/admin"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusFound)
	})
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) { serveWebAsset(w, r, "index.html") })
	mux.HandleFunc("GET /family", func(w http.ResponseWriter, r *http.Request) { serveWebAsset(w, r, "family.html") })
	a.registerFamilyAccess(mux, location.New(db))
	mux.HandleFunc("GET /assets/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "index.html" || name == "family.html" {
			http.NotFound(w, r)
			return
		}
		serveWebAsset(w, r, name)
	})
	mux.HandleFunc("GET /api/v1/web-config", a.configuration)
	return a
}

func serveWebAsset(w http.ResponseWriter, r *http.Request, name string) {
	mime, ok := map[string]string{
		"index.html": "text/html; charset=utf-8", "styles.css": "text/css; charset=utf-8",
		"app.mjs": "text/javascript; charset=utf-8", "state.mjs": "text/javascript; charset=utf-8",
		"map.mjs": "text/javascript; charset=utf-8", "icon.svg": "image/svg+xml",
		"management.mjs": "text/javascript; charset=utf-8",
		"family.html":    "text/html; charset=utf-8", "family.mjs": "text/javascript; charset=utf-8",
	}[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	body, err := webui.Files.ReadFile("assets/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func (a *webAPI) configuration(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.auth.admin(w, r, false); !ok {
		return
	}
	a.writeConfiguration(w, r)
}
func (a *webAPI) writeConfiguration(w http.ResponseWriter, r *http.Request) {
	key := ""
	if a.config.mapEnabled() {
		key = a.config.AMapJSKey
	}
	writeJSON(w, r, http.StatusOK, map[string]any{
		"map":          map[string]any{"enabled": a.config.mapEnabled(), "key": key, "service_host": "/_AMapService"},
		"poll_seconds": 10, "stale_after_seconds": int(location.StaleAfter / time.Second),
	})
}

const maxMapResponseBytes = 4 << 20

func newMapClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// proxy handles its own routing before ServeMux can clean or redirect unsafe paths.
func (a *webAPI) proxy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		apiError(w, r, 405, "method_not_allowed")
		return
	}
	if _, err := a.auth.service.AuthenticateSession(r.Context(), cookieToken(r)); err != nil {
		if err = a.auth.service.AnyFamilyAccess(r.Context(), namedCookieToken(r, familyBrowserCookie)); err != nil {
			authError(w, r, err)
			return
		}
	}
	if !a.config.mapEnabled() {
		apiError(w, r, 503, "map_not_configured")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/_AMapService")
	if !safeMapPath(path) || r.URL.EscapedPath() != r.URL.Path {
		apiError(w, r, 400, "invalid_map_request")
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		apiError(w, r, 400, "invalid_map_request")
		return
	}
	for key := range query {
		if strings.EqualFold(key, "key") || strings.EqualFold(key, "jscode") {
			delete(query, key)
		}
	}
	query.Set("key", a.config.AMapJSKey)
	query.Set("jscode", a.config.AMapSecurityJSCode)
	host := "restapi.amap.com"
	if path == "/v4/map/styles" || strings.HasPrefix(path, "/v4/map/styles/") {
		host = "webapi.amap.com"
	}
	target := url.URL{Scheme: "https", Host: host, Path: path, RawQuery: query.Encode()}
	upstream, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), nil)
	if err != nil {
		apiError(w, r, 502, "map_unavailable")
		return
	}
	// Deliberately use a fresh request: no cookies, authorization, host overrides or client headers.
	response, err := a.client.Do(upstream)
	if err != nil {
		apiError(w, r, 502, "map_unavailable")
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 || response.ContentLength > maxMapResponseBytes {
		apiError(w, r, 502, "map_unavailable")
		return
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxMapResponseBytes+1))
	if err != nil || len(body) > maxMapResponseBytes {
		apiError(w, r, 502, "map_unavailable")
		return
	}
	contentType := response.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(response.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func safeMapPath(path string) bool {
	if !strings.HasPrefix(path, "/v3/") && !strings.HasPrefix(path, "/v4/") {
		return false
	}
	for _, segment := range strings.Split(path[1:], "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for _, c := range segment {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
				return false
			}
		}
	}
	return true
}
