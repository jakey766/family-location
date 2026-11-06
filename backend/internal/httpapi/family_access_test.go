package httpapi

import (
	"context"
	"encoding/json"
	"family-location/backend/internal/auth"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFamilyAccessAndLastFamilyDeletion(t *testing.T) {
	c, svc := newAuthClient(t)
	c.login("owner", "test-password-only!")
	w := c.request("GET", "/api/v1/families", "", nil)
	first := decodeMap(t, w.Body.Bytes())["families"].([]any)[0].(map[string]any)
	if first["passcode_set"] != false {
		t.Fatal("legacy family must require passcode setup")
	}
	key := first["public_id"].(string)
	base := "/api/v1/family/" + key
	viewer := &apiTestClient{t: t, h: c.h, ip: "192.0.2.132"}
	expectStatus(t, viewer.request("GET", base+"/snapshot", "", nil), 401)
	expectStatus(t, c.request("GET", base+"/snapshot", "", nil), 401)
	expectStatus(t, c.request("PUT", "/api/v1/families/"+key+"/passcode", `{"passcode":"0123456"}`, nil), 200)
	w = viewer.request("POST", base+"/session", `{"passcode":"0123456"}`, nil)
	expectStatus(t, w, 200)
	if len(w.Result().Cookies()) != 1 {
		t.Fatal("missing independent cookie")
	}
	viewer.cookie = w.Result().Cookies()[0]
	if !viewer.cookie.HttpOnly || viewer.cookie.SameSite != http.SameSiteStrictMode || viewer.cookie.Name == sessionCookie {
		t.Fatal("unsafe viewer cookie")
	}
	expectStatus(t, viewer.request("GET", base+"/snapshot", "", nil), 200)
	expectStatus(t, viewer.request("GET", "/api/v1/auth/me", "", nil), 401)
	expectStatus(t, viewer.request("GET", "/api/v1/devices?scope=owned", "", nil), 401)
	expectStatus(t, viewer.request("DELETE", base+"/session", "", map[string]string{"Origin": "https://evil.test"}), 403)
	w = c.request("POST", "/api/v1/pairing-codes", `{}`, nil)
	var p auth.Pairing
	json.Unmarshal(w.Body.Bytes(), &p)
	d, err := svc.Pair(context.Background(), p.ID, p.Code, "test device", "192.0.2.155")
	if err != nil {
		t.Fatal(err)
	}
	headers := map[string]string{"Authorization": "Bearer " + d.Token}
	w = c.request("POST", "/api/v1/device/sharing/start", `{}`, headers)
	sharing := decodeMap(t, w.Body.Bytes())["sharing_session_id"].(string)
	upload := fmt.Sprintf(`{"sharing_session_id":%q,"sequence":1,"latitude":31,"longitude":121,"coordinate_system":"GCJ-02","accuracy_m":10,"captured_at":%q}`, sharing, time.Now().UTC().Format(time.RFC3339))
	expectStatus(t, c.request("PUT", "/api/v1/device/location", upload, headers), 200)
	w = viewer.request("GET", base+"/snapshot", "", nil)
	expectStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), d.Device.ID) || strings.Contains(w.Body.String(), "family_id") || strings.Contains(w.Body.String(), "config") {
		t.Fatal("invalid viewer projection", w.Body.String())
	}
	expectStatus(t, c.request("DELETE", "/api/v1/devices/"+d.Device.ID+"/purge", "", nil), 409)
	expectStatus(t, c.request("DELETE", "/api/v1/families/"+key, "", nil), 204)
	expectStatus(t, viewer.request("GET", base+"/snapshot", "", nil), 404)
	c.login("owner", "test-password-only!")
	expectStatus(t, c.request("GET", "/api/v1/devices", "", nil), 404)
	expectStatus(t, c.request("GET", "/api/v1/devices/"+d.Device.ID+"/config", "", nil), 200)
	for _, path := range []string{"/api/v1/devices?scope=owned", "/api/v1/locations?scope=owned"} {
		w = c.request("GET", path, "", nil)
		expectStatus(t, w, 200)
		if !strings.Contains(w.Body.String(), d.Device.ID) {
			t.Fatal("orphan lost", w.Body.String())
		}
	}
	expectStatus(t, c.request("PUT", "/api/v1/device/location", strings.Replace(upload, `"sequence":1`, `"sequence":2`, 1), headers), 200)
	expectStatus(t, c.request("POST", "/api/v1/families", `{"name":"new"}`, nil), 201)
	expectStatus(t, c.request("DELETE", "/api/v1/devices/"+d.Device.ID, "", nil), 204)
	expectStatus(t, c.request("DELETE", "/api/v1/devices/"+d.Device.ID+"/purge", "", nil), 204)
	expectStatus(t, c.request("GET", "/api/v1/device/me", "", headers), 401)
}

func TestOwnedScopeStrictParameters(t *testing.T) {
	c, _ := newAuthClient(t)
	c.login("owner", "test-password-only!")
	for _, endpoint := range []string{"/api/v1/devices", "/api/v1/locations"} {
		for _, q := range []string{"scope=owned&scope=owned", "scope=owned&family=x", "scope=", "scope=all"} {
			expectStatus(t, c.request("GET", endpoint+"?"+q, "", nil), 400)
		}
	}
}

func TestAdminEntryRedirect(t *testing.T) {
	h := New(testDB(t))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "http://family.test/?family=kept", nil))
	if w.Code != 302 || w.Header().Get("Location") != "/admin?family=kept" {
		t.Fatal("legacy entry not preserved", w.Code, w.Header())
	}
}

func TestFamilyPasscodeValidationAndMutationProtection(t *testing.T) {
	c, _ := newAuthClient(t)
	c.login("owner", "test-password-only!")
	w := c.request("POST", "/api/v1/families", `{"name":"test","passcode":"0000123"}`, nil)
	expectStatus(t, w, 201)
	f := decodeMap(t, w.Body.Bytes())
	if f["passcode"] != "0000123" || f["passcode_set"] != true {
		t.Fatal("passcode not delivered once")
	}
	key := f["public_id"].(string)
	path := "/api/v1/families/" + key
	for _, value := range []string{`null`, `1234567`, `""`, `"123456"`, `"12345678"`, `"１２３４５６７"`, `" 123456"`} {
		expectStatus(t, c.request("POST", "/api/v1/families", `{"name":"bad","passcode":`+value+`}`, nil), 400)
		expectStatus(t, c.request("PUT", path+"/passcode", `{"passcode":`+value+`}`, nil), 400)
	}
	for _, tc := range []struct{ method, path, body string }{{"PATCH", path, `{"name":"renamed"}`}, {"PUT", path + "/passcode", `{}`}, {"DELETE", path, ""}} {
		expectStatus(t, c.request(tc.method, tc.path, tc.body, map[string]string{"X-CSRF-Token": ""}), 403)
		expectStatus(t, c.request(tc.method, tc.path, tc.body, map[string]string{"Origin": "https://evil.test"}), 403)
	}
	w = c.request("GET", "/api/v1/families", "", nil)
	if strings.Contains(w.Body.String(), `"passcode":`) || strings.Contains(w.Body.String(), "argon2") {
		t.Fatal("stored passcode leaked")
	}
}
