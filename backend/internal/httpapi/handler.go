package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"family-location/backend/internal/storage"
)

func New(db *sql.DB) http.Handler {
	return NewWithWebConfig(db, WebConfig{})
}

func NewWithWebConfig(db *sql.DB, cfg WebConfig) http.Handler {
	return newWithWebClient(db, cfg, newMapClient())
}

func newWithWebClient(db *sql.DB, cfg WebConfig, client *http.Client) http.Handler {
	mux := http.NewServeMux()
	web := registerWeb(mux, db, cfg, client)
	registerAuth(mux, db)
	registerLocations(mux, db)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		var version int
		err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version)
		if err != nil || version != storage.SchemaVersion {
			writeJSON(w, r, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "database": "unavailable"})
			return
		}
		writeJSON(w, r, http.StatusOK, map[string]string{"status": "ok", "database": "ok"})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodHead {
			w = headResponseWriter{w}
		}
		if strings.HasPrefix(r.URL.Path, "/_AMapService/") {
			web.proxy(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

type headResponseWriter struct{ http.ResponseWriter }

func (w headResponseWriter) Write(body []byte) (int, error) { return len(body), nil }

func writeJSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_ = json.NewEncoder(w).Encode(value)
	}
}
