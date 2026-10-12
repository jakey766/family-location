package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"family-location/backend/internal/auth"
)

func TestAuthRoutesRejectAnonymousAndCrossSite(t *testing.T) {
	h := New(testDB(t))
	for _, tc := range []struct {
		method, path string
		code         int
	}{
		{"GET", "/api/v1/auth/me", 401},
		{"GET", "/api/v1/devices", 401},
		{"GET", "/api/v1/device/me", 401},
		{"POST", "/api/v1/auth/login", 403},
	} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{}`)))
		if r.Code != tc.code {
			t.Errorf("%s %s: got %d want %d", tc.method, tc.path, r.Code, tc.code)
		}
	}
}

type apiTestClient struct {
	t      *testing.T
	h      http.Handler
	cookie *http.Cookie
	csrf   string
	ip     string
}

func (c *apiTestClient) request(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	c.t.Helper()
	r := httptest.NewRequest(method, "http://family.test"+path, strings.NewReader(body))
	r.RemoteAddr = c.ip + ":1234"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://family.test")
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	if c.csrf != "" {
		r.Header.Set("X-CSRF-Token", c.csrf)
	}
	for key, value := range headers {
		if value == "" {
			r.Header.Del(key)
		} else {
			r.Header.Set(key, value)
		}
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	if w.Header().Get("Cache-Control") != "no-store" {
		c.t.Fatal("missing no-store")
	}
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		c.t.Fatal("CORS unexpectedly enabled")
	}
	return w
}

func expectStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status=%d want=%d", w.Code, want)
	}
}

func (c *apiTestClient) login(username, password string) {
	c.t.Helper()
	b, _ := json.Marshal(map[string]string{"username": username, "password": password})
	w := c.request("POST", "/api/v1/auth/login", string(b), nil)
	expectStatus(c.t, w, 200)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		c.t.Fatal("missing session cookie")
	}
	c.cookie = cookies[0]
	if !c.cookie.HttpOnly || c.cookie.Secure || c.cookie.SameSite != http.SameSiteStrictMode || c.cookie.Path != "/" || c.cookie.MaxAge != 86400 {
		c.t.Fatal("incorrect HTTP cookie attributes")
	}
	var session auth.Session
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		c.t.Fatal(err)
	}
	if session.CSRFToken == "" || strings.Contains(w.Body.String(), c.cookie.Value) {
		c.t.Fatal("missing CSRF or exposed session token")
	}
	c.csrf = session.CSRFToken
}

func newAuthClient(t *testing.T) (*apiTestClient, *auth.Service) {
	t.Helper()
	db := testDB(t)
	svc := auth.New(db)
	if _, err := svc.CreateAdmin(context.Background(), "owner", "test-password-only!", "家庭"); err != nil {
		t.Fatal(err)
	}
	return &apiTestClient{t: t, h: New(db), ip: "192.0.2.10"}, svc
}

func TestAdminDeviceLifecycleAndFamilyIsolation(t *testing.T) {
	c, svc := newAuthClient(t)
	c.login("owner", "test-password-only!")
	w := c.request("GET", "/api/v1/auth/me", "", nil)
	expectStatus(t, w, 200)
	var session auth.Session
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	if session.CSRFToken != c.csrf {
		t.Fatal("CSRF recovery changed token")
	}
	w = c.request("GET", "/api/v1/devices", "", nil)
	expectStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"devices":[]`) {
		t.Fatal("empty devices must be array")
	}
	w = c.request("POST", "/api/v1/pairing-codes", `{}`, nil)
	expectStatus(t, w, 201)
	var pairing auth.Pairing
	if err := json.Unmarshal(w.Body.Bytes(), &pairing); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"pairing_id": pairing.ID, "code": pairing.Code, "name": "Mi 11 Lite"})
	deviceClient := &apiTestClient{t: t, h: c.h, ip: "192.0.2.11"}
	w = deviceClient.request("POST", "/api/v1/devices/pair", string(body), map[string]string{"Origin": ""})
	expectStatus(t, w, 201)
	var paired auth.PairedDevice
	if err := json.Unmarshal(w.Body.Bytes(), &paired); err != nil {
		t.Fatal(err)
	}
	if paired.Token == "" || paired.Device.FamilyID != session.User.FamilyID {
		t.Fatal("invalid device credentials")
	}
	w = deviceClient.request("POST", "/api/v1/devices/pair", string(body), nil)
	expectStatus(t, w, 400)
	headers := map[string]string{"Authorization": "Bearer " + paired.Token}
	w = deviceClient.request("GET", "/api/v1/device/me", "", headers)
	expectStatus(t, w, 200)
	if strings.Contains(w.Body.String(), paired.Token) {
		t.Fatal("token leaked in device lookup")
	}
	expectStatus(t, deviceClient.request("GET", "/api/v1/devices", "", headers), 401)
	deviceClient.cookie = &http.Cookie{Name: sessionCookie, Value: paired.Token}
	expectStatus(t, deviceClient.request("GET", "/api/v1/auth/me", "", nil), 401)
	deviceClient.cookie = nil
	expectStatus(t, c.request("GET", "/api/v1/device/me", "", map[string]string{"Authorization": "Bearer " + c.cookie.Value}), 401)

	if _, err := svc.CreateAdmin(context.Background(), "other", "test-password-only!", "另一家"); err != nil {
		t.Fatal(err)
	}
	other := &apiTestClient{t: t, h: c.h, ip: "192.0.2.12"}
	other.login("other", "test-password-only!")
	w = other.request("GET", "/api/v1/devices", "", nil)
	expectStatus(t, w, 200)
	if strings.Contains(w.Body.String(), paired.Device.ID) {
		t.Fatal("cross-family device leaked")
	}
	expectStatus(t, other.request("DELETE", "/api/v1/devices/"+paired.Device.ID, "", nil), 404)
	expectStatus(t, deviceClient.request("GET", "/api/v1/device/me", "", headers), 200)
	w = c.request("GET", "/api/v1/devices", "", nil)
	expectStatus(t, w, 200)
	if strings.Contains(w.Body.String(), paired.Token) || !strings.Contains(w.Body.String(), paired.Device.ID) {
		t.Fatal("device list invalid")
	}
	expectStatus(t, c.request("DELETE", "/api/v1/devices/"+paired.Device.ID, "", nil), 204)
	expectStatus(t, deviceClient.request("GET", "/api/v1/device/me", "", headers), 401)
	w = c.request("POST", "/api/v1/auth/logout", "", nil)
	expectStatus(t, w, 204)
	if cookies := w.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatal("logout did not clear cookie")
	}
	expectStatus(t, c.request("GET", "/api/v1/auth/me", "", nil), 401)
}

func TestOriginCSRFAndInputValidation(t *testing.T) {
	c, _ := newAuthClient(t)
	for _, origin := range []string{"", "null", "http://evil.test", "https://family.test", "http://family.test/", "http://family.test?x=1"} {
		expectStatus(t, c.request("POST", "/api/v1/auth/login", `{}`, map[string]string{"Origin": origin}), 403)
	}
	for _, tc := range []struct {
		body, contentType string
		status            int
	}{
		{`{}`, "text/plain", 415}, {`null`, "application/json", 400}, {`{} {}`, "application/json", 400},
		{`{"family_id":1}`, "application/json", 400}, {`{`, "application/json", 400},
		{`{"username":"` + strings.Repeat("x", 8192) + `"}`, "application/json", 413},
	} {
		expectStatus(t, c.request("POST", "/api/v1/auth/login", tc.body, map[string]string{"Content-Type": tc.contentType}), tc.status)
	}
	c.login("owner", "test-password-only!")
	for _, headers := range []map[string]string{
		{"Origin": ""}, {"Origin": "http://evil.test"}, {"X-CSRF-Token": ""}, {"X-CSRF-Token": "wrong"},
	} {
		expectStatus(t, c.request("POST", "/api/v1/pairing-codes", `{}`, headers), 403)
		expectStatus(t, c.request("POST", "/api/v1/auth/logout", "", headers), 403)
		expectStatus(t, c.request("DELETE", "/api/v1/devices/example", "", headers), 403)
	}
	expectStatus(t, c.request("POST", "/api/v1/pairing-codes", `{"family_id":99}`, nil), 400)
	expectStatus(t, c.request("POST", "/api/v1/pairing-codes", `{}`, nil), 201)
}

func TestLoginRateLimitIgnoresForwardedIP(t *testing.T) {
	c, _ := newAuthClient(t)
	for i := 0; i < 5; i++ {
		expectStatus(t, c.request("POST", "/api/v1/auth/login", `{"username":"owner","password":"wrong-password"}`, map[string]string{"X-Forwarded-For": "203.0.113.1"}), 401)
	}
	w := c.request("POST", "/api/v1/auth/login", `{"username":"owner","password":"wrong-password"}`, map[string]string{"X-Forwarded-For": "203.0.113.2"})
	expectStatus(t, w, 429)
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("missing retry hint")
	}
}

func TestSameOriginAndDuplicateHeaders(t *testing.T) {
	r := httptest.NewRequest("POST", "http://family.test/", nil)
	r.Header.Add("Origin", "http://family.test")
	r.Header.Add("Origin", "http://family.test")
	if sameOrigin(r) {
		t.Fatal("duplicate origin accepted")
	}
	r.Header.Set("Origin", "http://family.test#")
	if sameOrigin(r) {
		t.Fatal("malformed origin accepted")
	}
}

func TestTLSLoginAndLogoutUseSecureCookie(t *testing.T) {
	c, _ := newAuthClient(t)
	r := httptest.NewRequest("POST", "https://family.test/api/v1/auth/login", strings.NewReader(`{"username":"owner","password":"test-password-only!"}`))
	r.Header.Set("Origin", "https://family.test")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	expectStatus(t, w, 200)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Fatal("TLS session cookie must be Secure")
	}
	var session auth.Session
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("POST", "https://family.test/api/v1/auth/logout", nil)
	r.AddCookie(cookies[0])
	r.Header.Set("Origin", "https://family.test")
	r.Header.Set("X-CSRF-Token", session.CSRFToken)
	w = httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	expectStatus(t, w, 204)
	cookies = w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || cookies[0].MaxAge != -1 {
		t.Fatal("TLS logout must clear the Secure cookie")
	}
}
