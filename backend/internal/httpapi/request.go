package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

const maxJSONBody = 8 << 10

func apiError(w http.ResponseWriter, r *http.Request, status int, code string) {
	writeJSON(w, r, status, map[string]string{"error": code})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	kind, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" {
		apiError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type")
		return false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxJSONBody))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			apiError(w, r, http.StatusRequestEntityTooLarge, "body_too_large")
		} else {
			apiError(w, r, http.StatusBadRequest, "invalid_json")
		}
		return false
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 || body[0] != '{' {
		apiError(w, r, http.StatusBadRequest, "invalid_json")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		apiError(w, r, http.StatusBadRequest, "invalid_json")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		apiError(w, r, http.StatusBadRequest, "invalid_json")
		return false
	}
	return true
}

// This release serves clients directly. Forwarded headers are intentionally not
// used for either CSRF origin checks or rate-limit identities.
func sameOrigin(r *http.Request) bool {
	values := r.Header.Values("Origin")
	if len(values) != 1 {
		return false
	}
	if strings.ContainsAny(values[0], "?#") {
		return false
	}
	u, err := url.Parse(values[0])
	if err != nil || u.User != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return u.Scheme == scheme && strings.EqualFold(u.Host, r.Host)
}
