package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"family-location/backend/internal/auth"
)

func TestWebStaticResources(t *testing.T) {
	h := New(testDB(t))
	for _, tc := range []struct{ path, mime string }{
		{"/admin", "text/html; charset=utf-8"}, {"/assets/styles.css", "text/css; charset=utf-8"},
		{"/assets/app.mjs", "text/javascript; charset=utf-8"}, {"/assets/state.mjs", "text/javascript; charset=utf-8"},
		{"/assets/map.mjs", "text/javascript; charset=utf-8"}, {"/assets/icon.svg", "image/svg+xml"},
	} {
		for _, method := range []string{"GET", "HEAD"} {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(method, tc.path, nil))
			if w.Code != 200 || w.Header().Get("Content-Type") != tc.mime || w.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s %s status=%d headers=%v", method, tc.path, w.Code, w.Header())
			}
			if method == "HEAD" && w.Body.Len() != 0 {
				t.Fatal("HEAD has body")
			}
			if method == "GET" && w.Body.Len() == 0 {
				t.Fatal("empty asset")
			}
		}
	}
	for _, path := range []string{"/assets/", "/assets/missing.js", "/assets/index.html", "/assets/deep/app.mjs", "/api/v1/unknown", "/missing"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		expectStatus(t, w, 404)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	expectStatus(t, w, 405)
}

type webTransport func(*http.Request) (*http.Response, error)

func (f webTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func newWebClient(t *testing.T, cfg WebConfig, transport http.RoundTripper) *apiTestClient {
	t.Helper()
	db := testDB(t)
	svc := auth.New(db)
	if _, err := svc.CreateAdmin(context.Background(), "owner", "test-password-only!", "家庭"); err != nil {
		t.Fatal(err)
	}
	client := newMapClient()
	client.Transport = transport
	client.Timeout = 100 * time.Millisecond
	h := newWithWebClient(db, cfg, client)
	c := &apiTestClient{t: t, h: h, ip: "192.0.2.10"}
	c.login("owner", "test-password-only!")
	return c
}
func TestWebConfigurationRequiresAdminAndNeverExposesSecurityCode(t *testing.T) {
	for _, cfg := range []WebConfig{{}, {AMapJSKey: "public-key", AMapSecurityJSCode: "private-secret"}} {
		c := newWebClient(t, cfg, nil)
		w := c.request("GET", "/api/v1/web-config", "", nil)
		expectStatus(t, w, 200)
		var got struct {
			Map struct {
				Enabled     bool   `json:"enabled"`
				Key         string `json:"key"`
				ServiceHost string `json:"service_host"`
			} `json:"map"`
			Poll  int `json:"poll_seconds"`
			Stale int `json:"stale_after_seconds"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.Map.Enabled != (cfg.AMapJSKey != "") || got.Map.Key != cfg.AMapJSKey || got.Map.ServiceHost != "/_AMapService" || got.Poll != 10 || got.Stale != 420 {
			t.Fatalf("bad config: %s", w.Body.String())
		}
		if strings.Contains(w.Body.String(), "private-secret") || strings.Contains(w.Body.String(), "jscode") {
			t.Fatal("security code exposed")
		}
		c.cookie = nil
		expectStatus(t, c.request("GET", "/api/v1/web-config", "", map[string]string{"Authorization": "Bearer device-token"}), 401)
	}
}
func TestAMapProxyFixedDestinationCredentialsAndHeaders(t *testing.T) {
	cfg := WebConfig{AMapJSKey: "configured-key", AMapSecurityJSCode: "configured-secret"}
	calls := 0
	transport := webTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		wantHost := "restapi.amap.com"
		if strings.HasPrefix(r.URL.Path, "/v4/map/styles") {
			wantHost = "webapi.amap.com"
		}
		if r.URL.Scheme != "https" || r.URL.Host != wantHost || r.Host != wantHost {
			t.Errorf("wrong destination: %s %s", r.URL, r.Host)
		}
		q := r.URL.Query()
		if len(q) != 3 || len(q["key"]) != 1 || q.Get("key") != cfg.AMapJSKey || len(q["jscode"]) != 1 || q.Get("jscode") != cfg.AMapSecurityJSCode || q.Get("callback") != "mapCallback" {
			t.Errorf("wrong upstream query")
		}
		if len(r.Header) != 0 {
			t.Errorf("forwarded client headers: %v", r.Header)
		}
		if r.Body != nil {
			t.Error("forwarded body")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/javascript"}, "Set-Cookie": {"secret=upstream"}, "Location": {"https://evil.test"}}, Body: io.NopCloser(strings.NewReader("mapCallback({})"))}, nil
	})
	c := newWebClient(t, cfg, transport)
	for _, path := range []string{"/v3/ip", "/v4/place/text", "/v4/map/styles", "/v4/map/styles/custom"} {
		w := c.request("GET", "/_AMapService"+path+"?key=bad&key=second&KEY=bad&jscode=bad&jscode=second&JSCODE=bad&callback=mapCallback", "", map[string]string{"Authorization": "Bearer secret", "X-Forwarded-Host": "evil.test", "X-Custom": "private"})
		expectStatus(t, w, 200)
		if w.Header().Get("Content-Type") != "application/javascript" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Set-Cookie") != "" || w.Header().Get("Location") != "" {
			t.Fatalf("unsafe headers %v", w.Header())
		}
		if w.Body.String() != "mapCallback({})" {
			t.Fatal("body changed")
		}
	}
	w := c.request("HEAD", "/_AMapService/v3/ip?callback=mapCallback", "", nil)
	expectStatus(t, w, 200)
	if w.Body.Len() != 0 {
		t.Fatal("HEAD body")
	}
	if calls != 5 {
		t.Fatalf("calls %d", calls)
	}
	c.cookie = nil
	expectStatus(t, c.request("GET", "/_AMapService/v3/ip", "", nil), 401)
	if calls != 5 {
		t.Fatal("anonymous request reached upstream")
	}
}
func TestAMapProxyRejectsUnsafePathsAndMethods(t *testing.T) {
	c := newWebClient(t, WebConfig{AMapJSKey: "key", AMapSecurityJSCode: "secret"}, webTransport(func(*http.Request) (*http.Response, error) {
		t.Fatal("unsafe request reached upstream")
		return nil, nil
	}))
	for _, path := range []string{"/", "/v3/", "/v2/ip", "/v3/../ip", "/v3/%2e%2e/ip", "/v3/%252e%252e/ip", "/v3/a%2fb", "/v3/a%5cb", "/v3//ip", "/v3/https://evil.test", "/v3/a%00b", "/v3/ip?key=%zz"} {
		w := c.request("GET", "/_AMapService"+path, "", nil)
		if w.Code != 400 {
			t.Errorf("path %s status %d", path, w.Code)
		}
	}
	expectStatus(t, c.request("POST", "/_AMapService/v3/ip", "", nil), 405)
	c = newWebClient(t, WebConfig{}, nil)
	expectStatus(t, c.request("GET", "/_AMapService/v3/ip", "", nil), 503)
}
func TestAMapProxyUpstreamFailuresAreBoundedAndRedacted(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		err    error
	}{
		{name: "redirect", status: 302, body: "private-secret"}, {name: "failure", status: 500, body: "private-secret"},
		{name: "network", err: errors.New("https://restapi.amap.com?jscode=private-secret")},
		{name: "oversized", status: 200, body: strings.Repeat("x", (4<<20)+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := newWebClient(t, WebConfig{AMapJSKey: "key", AMapSecurityJSCode: "private-secret"}, webTransport(func(*http.Request) (*http.Response, error) {
				calls++
				if calls > 1 {
					t.Fatal("upstream redirect was followed")
				}
				if tc.err != nil {
					return nil, tc.err
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": {"https://evil.test"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}))
			w := c.request("GET", "/_AMapService/v3/ip", "", nil)
			expectStatus(t, w, 502)
			if w.Body.String() != "{\"error\":\"map_unavailable\"}\n" || w.Header().Get("Location") != "" {
				t.Fatalf("unsafe error %s", w.Body.String())
			}
		})
	}
}

func TestAMapProxyTimeoutAndCancellation(t *testing.T) {
	if timeout := newMapClient().Timeout; timeout <= 0 || timeout > 5*time.Second {
		t.Fatalf("unbounded proxy timeout %s", timeout)
	}
	c := newWebClient(t, WebConfig{AMapJSKey: "key", AMapSecurityJSCode: "secret"}, webTransport(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	}))
	start := time.Now()
	w := c.request("GET", "/_AMapService/v3/ip", "", nil)
	expectStatus(t, w, 502)
	if time.Since(start) > time.Second {
		t.Fatal("upstream timeout was not bounded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("GET", "/_AMapService/v3/ip", nil).WithContext(ctx)
	r.AddCookie(c.cookie)
	w = httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	if w.Code == 200 {
		t.Fatal("canceled request succeeded")
	}
}

func TestManagementAssetIsServed(t *testing.T) {
	h := New(testDB(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/assets/management.mjs", nil))
	if w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("management module status=%d type=%s", w.Code, w.Header().Get("Content-Type"))
	}
}

func TestFamilyMapGrantIsReadOnlyAndProxyKeepsWhitelist(t *testing.T) {
	calls := 0
	cfg := WebConfig{AMapJSKey: "public-map-key", AMapSecurityJSCode: "private-map-secret"}
	c := newWebClient(t, cfg, webTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Scheme != "https" || r.URL.Host != "restapi.amap.com" || len(r.Header) != 0 {
			t.Fatal("viewer proxy forwarded credentials or changed upstream")
		}
		if r.URL.Query().Get("key") != cfg.AMapJSKey || r.URL.Query().Get("jscode") != cfg.AMapSecurityJSCode {
			t.Fatal("proxy keys overridden")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	}))
	w := c.request("POST", "/api/v1/families", `{"name":"map family","passcode":"0123456"}`, nil)
	expectStatus(t, w, 201)
	key := decodeMap(t, w.Body.Bytes())["public_id"].(string)
	base := "/api/v1/family/" + key
	viewer := &apiTestClient{t: t, h: c.h, ip: "192.0.2.220"}
	expectStatus(t, viewer.request("GET", base+"/web-config", "", nil), 401)
	expectStatus(t, viewer.request("POST", base+"/session", `{"passcode":"0123456"}`, map[string]string{"Origin": ""}), 403)
	w = viewer.request("POST", base+"/session", `{"passcode":"0123456"}`, nil)
	expectStatus(t, w, 200)
	viewer.cookie = w.Result().Cookies()[0]
	w = viewer.request("GET", base+"/web-config", "", nil)
	expectStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), cfg.AMapJSKey) || strings.Contains(w.Body.String(), cfg.AMapSecurityJSCode) {
		t.Fatal("map config projection")
	}
	for _, path := range []string{"/api/v1/web-config", "/api/v1/families", "/api/v1/devices/device/config", "/api/v1/locations?scope=owned"} {
		expectStatus(t, viewer.request("GET", path, "", nil), 401)
	}
	expectStatus(t, viewer.request("POST", "/api/v1/pairing-codes", `{}`, nil), 401)
	expectStatus(t, viewer.request("PUT", "/api/v1/device/location", `{}`, nil), 401)
	expectStatus(t, viewer.request("GET", "/api/v1/family/missing/web-config", "", nil), 404)
	expectStatus(t, viewer.request("GET", "/_AMapService/v3/ip?key=evil&jscode=evil", "", map[string]string{"Authorization": "Bearer private"}), 200)
	for _, path := range []string{"/v3/../ip", "/v3/a%2fb", "/v3/https://evil.test"} {
		expectStatus(t, viewer.request("GET", "/_AMapService"+path, "", nil), 400)
	}
	expectStatus(t, c.request("PUT", "/api/v1/families/"+key+"/passcode", `{}`, nil), 200)
	expectStatus(t, viewer.request("GET", base+"/web-config", "", nil), 401)
	expectStatus(t, viewer.request("GET", "/_AMapService/v3/ip", "", nil), 401)
	if calls != 1 {
		t.Fatal("unauthorized proxy reached upstream", calls)
	}
}
