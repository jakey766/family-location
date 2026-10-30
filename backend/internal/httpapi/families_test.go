package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"family-location/backend/internal/auth"
)

func decodeMap(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestMultipleFamiliesAndDeviceConfiguration(t *testing.T) {
	c, svc := newAuthClient(t)
	expectStatus(t, c.request("GET", "/api/v1/families", "", nil), 401)
	c.login("owner", "test-password-only!")
	w := c.request("GET", "/api/v1/families", "", nil)
	expectStatus(t, w, 200)
	families := decodeMap(t, w.Body.Bytes())["families"].([]any)
	if len(families) != 1 {
		t.Fatal("default family missing")
	}
	first := families[0].(map[string]any)["public_id"].(string)
	if len(first) < 12 {
		t.Fatal("family identifier too short")
	}
	expectStatus(t, c.request("POST", "/api/v1/families", `{"name":"second"}`, map[string]string{"X-CSRF-Token": ""}), 403)
	w = c.request("POST", "/api/v1/families", `{"name":"第二个家庭"}`, nil)
	expectStatus(t, w, 201)
	second := decodeMap(t, w.Body.Bytes())["public_id"].(string)
	if second == first {
		t.Fatal("duplicate family identifier")
	}
	w = c.request("POST", "/api/v1/pairing-codes?family="+first, `{}`, nil)
	expectStatus(t, w, 201)
	var pairing auth.Pairing
	if err := json.Unmarshal(w.Body.Bytes(), &pairing); err != nil {
		t.Fatal(err)
	}
	paired, err := svc.Pair(context.Background(), pairing.ID, pairing.Code, "测试设备", "192.0.2.90")
	if err != nil {
		t.Fatal(err)
	}
	id := paired.Device.ID
	membership := "/api/v1/families/" + second + "/devices/" + id
	expectStatus(t, c.request("PUT", membership, `{}`, nil), 204)
	expectStatus(t, c.request("PUT", membership, `{}`, nil), 204)
	for _, f := range []string{first, second} {
		w = c.request("GET", "/api/v1/devices?family="+f, "", nil)
		expectStatus(t, w, 200)
		if !strings.Contains(w.Body.String(), id) {
			t.Fatal("member missing")
		}
	}
	w = c.request("GET", "/api/v1/devices?scope=owned", "", nil)
	expectStatus(t, w, 200)
	owned := decodeMap(t, w.Body.Bytes())["devices"].([]any)
	memberships, ok := owned[0].(map[string]any)["families"].([]any)
	if !ok || len(memberships) != 2 {
		t.Fatal("owned device missing family memberships")
	}
	cfgPath := "/api/v1/devices/" + id + "/config"
	w = c.request("GET", cfgPath, "", nil)
	expectStatus(t, w, 200)
	cfg := decodeMap(t, w.Body.Bytes())
	if cfg["version"] != float64(1) || cfg["moving_interval_seconds"] != float64(120) || cfg["stationary_interval_seconds"] != float64(300) || cfg["moving_high_accuracy"] != false || cfg["quiet_hours_start"] != "22:00" || cfg["quiet_hours_end"] != "08:00" {
		t.Fatal("wrong config defaults", cfg)
	}
	changed := `{"moving_interval_seconds":180,"stationary_interval_seconds":600,"moving_high_accuracy":true,"quiet_hours_start":"12:30","quiet_hours_end":"13:45"}`
	w = c.request("PUT", cfgPath, changed, nil)
	expectStatus(t, w, 200)
	if decodeMap(t, w.Body.Bytes())["version"] != float64(2) {
		t.Fatal("config version not incremented")
	}
	for _, bad := range []string{`{}`, strings.Replace(changed, "180", "30", 1), strings.Replace(changed, "180", "181", 1), strings.Replace(changed, "600", "120", 1), strings.Replace(changed, "600", "3660", 1), strings.Replace(changed, "12:30", "24:00", 1), strings.Replace(changed, "12:30", "13:45", 1), strings.Replace(changed, "true", "null", 1), strings.Replace(changed, `"12:30"`, `"1:30"`, 1)} {
		expectStatus(t, c.request("PUT", cfgPath, bad, nil), 400)
	}
	headers := map[string]string{"Authorization": "Bearer " + paired.Token}
	w = c.request("POST", "/api/v1/device/sharing/start", `{}`, headers)
	expectStatus(t, w, 201)
	session := decodeMap(t, w.Body.Bytes())["sharing_session_id"].(string)
	payload := fmt.Sprintf(`{"sharing_session_id":%q,"sequence":1,"latitude":31,"longitude":121,"coordinate_system":"GCJ-02","accuracy_m":10,"captured_at":%q}`, session, time.Now().Add(-8*time.Minute).UTC().Format(time.RFC3339))
	for _, accepted := range []bool{true, false} {
		w = c.request("PUT", "/api/v1/device/location", payload, headers)
		expectStatus(t, w, 200)
		result := decodeMap(t, w.Body.Bytes())
		if result["accepted"] != accepted || result["config"].(map[string]any)["version"] != float64(2) {
			t.Fatal("upload missing current config")
		}
	}
	for _, f := range []string{first, second} {
		w = c.request("GET", "/api/v1/locations?family="+f, "", nil)
		expectStatus(t, w, 200)
		locations := decodeMap(t, w.Body.Bytes())["locations"].([]any)
		if len(locations) != 1 {
			t.Fatal("family location missing")
		}
		p := locations[0].(map[string]any)
		if p["stale"] != false || p["stale_after_seconds"] != float64(720) {
			t.Fatal("configured stale threshold ignored")
		}
	}
	expectStatus(t, c.request("DELETE", membership, "", nil), 204)
	w = c.request("GET", "/api/v1/locations?family="+second, "", nil)
	if !strings.Contains(w.Body.String(), `"locations":[]`) {
		t.Fatal("removed family still sees device")
	}
	expectStatus(t, c.request("GET", "/api/v1/device/me", "", headers), 200)
	expectStatus(t, c.request("GET", "/api/v1/devices?family=", "", nil), 404)
	expectStatus(t, c.request("GET", "/api/v1/locations?family=missing", "", nil), 404)
	if _, err := svc.CreateAdmin(context.Background(), "stranger", "test-password-only!", "third"); err != nil {
		t.Fatal(err)
	}
	other := &apiTestClient{t: t, h: c.h, ip: "192.0.2.91"}
	other.login("stranger", "test-password-only!")
	for _, path := range []string{"/api/v1/devices?family=" + first, "/api/v1/locations?family=" + second, cfgPath} {
		expectStatus(t, other.request("GET", path, "", nil), 404)
	}
	expectStatus(t, other.request("PUT", membership, `{}`, nil), 404)
	expectStatus(t, other.request("PUT", cfgPath, changed, nil), 404)
	expectStatus(t, other.request("DELETE", "/api/v1/devices/"+id, "", nil), 404)
	expectStatus(t, c.request("PUT", membership, `{}`, nil), 204)
	expectStatus(t, c.request("DELETE", "/api/v1/devices/"+id, "", nil), 204)
	expectStatus(t, c.request("GET", "/api/v1/device/me", "", headers), 401)
	for _, f := range []string{first, second} {
		w = c.request("GET", "/api/v1/locations?family="+f, "", nil)
		if !strings.Contains(w.Body.String(), `"locations":[]`) {
			t.Fatal("revoked location visible")
		}
	}
}

func TestMalformedFamilyNeverFallsBack(t *testing.T) {
	c, _ := newAuthClient(t)
	c.login("owner", "test-password-only!")
	for _, query := range []string{"family=%ZZ", "family=a;b", "family=", "family=a&family=b"} {
		for _, endpoint := range []string{"/api/v1/devices", "/api/v1/locations", "/api/v1/pairing-codes"} {
			method, body := "GET", ""
			if endpoint == "/api/v1/pairing-codes" {
				method, body = "POST", "{}"
			}
			expectStatus(t, c.request(method, endpoint+"?"+query, body, nil), 404)
		}
	}
}

func TestNonDefaultPairingAndUnassignedDevices(t *testing.T) {
	c, svc := newAuthClient(t)
	c.login("owner", "test-password-only!")
	w := c.request("POST", "/api/v1/families", `{"name":"second"}`, nil)
	expectStatus(t, w, 201)
	f := decodeMap(t, w.Body.Bytes())["public_id"].(string)
	w = c.request("POST", "/api/v1/pairing-codes?family="+f, `{}`, nil)
	expectStatus(t, w, 201)
	var p auth.Pairing
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	paired, err := svc.Pair(context.Background(), p.ID, p.Code, "other family device", "192.0.2.98")
	if err != nil {
		t.Fatal(err)
	}
	w = c.request("GET", "/api/v1/devices", "", nil)
	if strings.Contains(w.Body.String(), paired.Device.ID) {
		t.Fatal("new family device leaked into default family")
	}
	expectStatus(t, c.request("DELETE", "/api/v1/families/"+f+"/devices/"+paired.Device.ID, "", nil), 204)
	w = c.request("GET", "/api/v1/devices?scope=owned", "", nil)
	if !strings.Contains(w.Body.String(), paired.Device.ID) || !strings.Contains(w.Body.String(), `"families":[]`) {
		t.Fatal("unassigned device lost ownership")
	}
	expectStatus(t, c.request("GET", "/api/v1/devices/"+paired.Device.ID+"/config", "", nil), 200)
	expectStatus(t, c.request("DELETE", "/api/v1/devices/"+paired.Device.ID, "", nil), 204)
}
