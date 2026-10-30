package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"family-location/backend/internal/location"
)

func TestLocationRoutesAuthentication(t *testing.T) {
	c, _ := newAuthClient(t)
	for _, route := range []struct{ method, path string }{{"POST", "/api/v1/device/sharing/start"}, {"POST", "/api/v1/device/sharing/stop"}, {"PUT", "/api/v1/device/location"}, {"GET", "/api/v1/locations"}} {
		expectStatus(t, c.request(route.method, route.path, `{}`, nil), 401)
	}
	c.login("owner", "test-password-only!")
	expectStatus(t, c.request("POST", "/api/v1/device/sharing/start", `{}`, nil), 401)
	expectStatus(t, c.request("GET", "/api/v1/locations", "", nil), 200)
}
func TestLocationHTTPContract(t *testing.T) {
	c, svc := newAuthClient(t)
	c.login("owner", "test-password-only!")
	session, e := svc.AuthenticateSession(context.Background(), c.cookie.Value)
	if e != nil {
		t.Fatal(e)
	}
	pairing, e := svc.CreatePairing(context.Background(), session.User)
	if e != nil {
		t.Fatal(e)
	}
	paired, e := svc.Pair(context.Background(), pairing.ID, pairing.Code, "模拟设备", "192.0.2.30")
	if e != nil {
		t.Fatal(e)
	}
	d := &apiTestClient{t: t, h: c.h, ip: "192.0.2.31"}
	headers := map[string]string{"Authorization": "Bearer " + paired.Token, "Origin": ""}
	expectStatus(t, d.request("GET", "/api/v1/locations", "", headers), 401)
	expectStatus(t, d.request("POST", "/api/v1/device/sharing/start", `{"family_id":99}`, headers), 400)
	w := d.request("POST", "/api/v1/device/sharing/start", `{}`, headers)
	expectStatus(t, w, 201)
	var sharing location.Sharing
	if e = json.Unmarshal(w.Body.Bytes(), &sharing); e != nil || sharing.SharingSessionID == "" {
		t.Fatal(e)
	}
	payload := map[string]any{"sharing_session_id": sharing.SharingSessionID, "sequence": 1, "latitude": 31.234567, "longitude": 121.456789, "coordinate_system": "GCJ-02", "accuracy_m": 12.5, "battery_percent": nil, "captured_at": time.Now().Add(-8 * time.Minute).UTC().Format(time.RFC3339Nano)}
	encode := func(v any) string {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	for _, field := range []string{"sharing_session_id", "sequence", "latitude", "longitude", "accuracy_m", "coordinate_system", "captured_at"} {
		value := payload[field]
		delete(payload, field)
		expectStatus(t, d.request("PUT", "/api/v1/device/location", encode(payload), headers), 400)
		payload[field] = nil
		expectStatus(t, d.request("PUT", "/api/v1/device/location", encode(payload), headers), 400)
		payload[field] = value
	}
	for _, invalid := range []string{`null`, `{}`, `{} {}`, `{"latitude":1e999}`, `{"battery_percent":1.5}`, `{"extra":true}`} {
		expectStatus(t, d.request("PUT", "/api/v1/device/location", invalid, headers), 400)
	}
	expectStatus(t, d.request("PUT", "/api/v1/device/location", `{"padding":"`+strings.Repeat("x", 8192)+`"}`, headers), 413)
	badType := map[string]string{"Authorization": headers["Authorization"], "Content-Type": "text/plain"}
	expectStatus(t, d.request("PUT", "/api/v1/device/location", encode(payload), badType), 415)
	body := encode(payload)
	expectStatus(t, d.request("PUT", "/api/v1/device/location", body, headers), 200)
	w = d.request("PUT", "/api/v1/device/location", body, headers)
	expectStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"accepted":false`) {
		t.Fatal("retry not ignored")
	}
	w = c.request("GET", "/api/v1/locations", "", nil)
	expectStatus(t, w, 200)
	var snapshot location.Snapshot
	if e = json.Unmarshal(w.Body.Bytes(), &snapshot); e != nil || len(snapshot.Locations) != 1 {
		t.Fatal(e)
	}
	loc := snapshot.Locations[0]
	if !loc.Stale || loc.BatteryPercent != nil || loc.CoordinateSystem != "GCJ-02" || loc.DeviceID != paired.Device.ID {
		t.Fatal("incorrect location response")
	}
	if strings.Contains(w.Body.String(), sharing.SharingSessionID) || strings.Contains(w.Body.String(), paired.Token) {
		t.Fatal("sensitive value exposed")
	}
	// A device cannot select its own family or another device in the payload.
	payload["device_id"] = "other"
	expectStatus(t, d.request("PUT", "/api/v1/device/location", encode(payload), headers), 400)
	delete(payload, "device_id")
	if _, e = svc.CreateAdmin(context.Background(), "other", "test-password-only!", "other"); e != nil {
		t.Fatal(e)
	}
	other := &apiTestClient{t: t, h: c.h, ip: "192.0.2.32"}
	other.login("other", "test-password-only!")
	w = other.request("GET", "/api/v1/locations", "", nil)
	expectStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"locations":[]`) {
		t.Fatal("cross-family location leak")
	}
	expectStatus(t, d.request("POST", "/api/v1/device/sharing/stop", encode(sharing), headers), 204)
	payload["sequence"] = 2
	expectStatus(t, d.request("PUT", "/api/v1/device/location", encode(payload), headers), 409)
	expectStatus(t, c.request("DELETE", "/api/v1/devices/"+paired.Device.ID, "", nil), 204)
	expectStatus(t, d.request("POST", "/api/v1/device/sharing/start", `{}`, headers), 401)
	// Multiple Authorization fields must not be silently selected.
	r := httptest.NewRequest(http.MethodPost, "/api/v1/device/sharing/start", strings.NewReader(`{}`))
	r.Header.Add("Authorization", headers["Authorization"])
	r.Header.Add("Authorization", headers["Authorization"])
	recorder := httptest.NewRecorder()
	c.h.ServeHTTP(recorder, r)
	expectStatus(t, recorder, 401)
}
