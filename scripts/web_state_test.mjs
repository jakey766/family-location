import test from "node:test";
import assert from "node:assert/strict";
import {
  estimateServerTime,
  visibleLocations,
  locationStatus,
  relativeTime,
  remainingSeconds,
  errorMessage,
} from "../backend/internal/webui/assets/state.mjs";
const now = Date.parse("2026-10-15T12:00:00Z");
const point = (overrides = {}) => ({
  device_id: "a",
  captured_at: new Date(now - 60_000).toISOString(),
  lease_expires_at: new Date(now + 60_000).toISOString(),
  coordinate_system: "GCJ-02",
  latitude: 30,
  longitude: 120,
  ...overrides,
});
test("server time advances using elapsed monotonic time, independent of phone wall clock", () => {
  assert.equal(
    estimateServerTime({ serverTime: now, receivedAt: 100 }, 2100),
    now + 2000,
  );
  assert.equal(
    estimateServerTime({ serverTime: now, receivedAt: 100 }, 50),
    now,
  );
});
test("expiry and revoked device remove positions at the exact boundary", () => {
  const locations = [
    point(),
    point({ device_id: "b" }),
    point({ device_id: "c", lease_expires_at: new Date(now).toISOString() }),
    point({ device_id: "d", latitude: 100 }),
  ];
  const devices = [
    { id: "a", revoked_at: null },
    { id: "b", revoked_at: "revoked" },
    { id: "c" },
    { id: "d" },
  ];
  assert.deepEqual(
    visibleLocations(locations, devices, now).map((p) => p.device_id),
    ["a"],
  );
  assert.deepEqual(visibleLocations(locations, devices, now + 60_000), []);
  assert.deepEqual(visibleLocations(locations, [], now), []);
});
test("stale is based on original capture, not recent receipt", () => {
  assert.equal(
    locationStatus(
      point({
        captured_at: new Date(now - 300_000).toISOString(),
        received_at: new Date(now).toISOString(),
      }),
      now,
    ),
    "stale",
  );
  assert.equal(locationStatus(point(), now), "fresh");
  assert.equal(locationStatus(point(), now + 60_000), "expired");
  assert.equal(
    locationStatus(point({ captured_at: "invalid" }), now),
    "invalid",
  );
});
test("time labels and pairing countdown handle skew and exact expiry", () => {
  assert.equal(relativeTime(new Date(now + 1000).toISOString(), now), "刚刚");
  assert.equal(
    relativeTime(new Date(now - 3600_000).toISOString(), now),
    "1 小时前",
  );
  assert.equal(remainingSeconds(new Date(now + 1500).toISOString(), now), 2);
  assert.equal(remainingSeconds(new Date(now).toISOString(), now), 0);
  assert.equal(remainingSeconds("bad", now), 0);
  assert.match(errorMessage({ status: 429 }), /频繁/);
  assert.match(errorMessage({ status: 403 }), /刷新/);
});
