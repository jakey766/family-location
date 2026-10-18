import test from "node:test";
import assert from "node:assert/strict";
import { createMapView } from "../backend/internal/webui/assets/map.mjs";
const at = Date.now();
const p = {
  device_id: "a",
  name: "家人",
  latitude: 30,
  longitude: 120,
  accuracy_m: 20,
  captured_at: new Date(at).toISOString(),
  lease_expires_at: new Date(at + 60000).toISOString(),
};
function setup() {
  const overlays = new Set();
  const maps = [];
  class Map {
    constructor() {
      maps.push(this);
    }
    add(values) {
      for (const v of values) overlays.add(v);
    }
    remove(values) {
      for (const v of values) overlays.delete(v);
    }
    on(name, fn) {
      if (name === "complete") queueMicrotask(fn);
    }
    setFitView(markers) {
      this.fittedMarkers = markers;
    }
    setZoomAndCenter(z, pos) {
      this.center = pos;
    }
    destroy() {
      overlays.clear();
    }
    resize() {}
  }
  class Marker {
    constructor(opts) {
      this.opts = opts;
    }
    on(name, fn) {
      this.click = fn;
    }
    setPosition(p) {
      this.opts.position = p;
    }
    setContent(v) {
      this.opts.content = v;
    }
    // AMap JS API 2.0 uses a lowercase z (unlike its layer API).
    // https://a.amap.com/jsapi/static/doc/index.html#amapmarker
    setzIndex(value) {
      this.opts.zIndex = value;
    }
  }
  class Circle {
    constructor(opts) {
      this.opts = opts;
    }
    setCenter(p) {
      this.opts.center = p;
    }
    setRadius(v) {
      this.opts.radius = v;
    }
    setOptions(v) {
      Object.assign(this.opts, v);
    }
  }
  const doc = {
    createElement() {
      return { className: "", textContent: "" };
    },
  };
  const statuses = [];
  const selections = [];
  const view = createMapView(
    {},
    (s) => statuses.push(s),
    (id) => selections.push(id),
    { load: async () => ({ Map, Marker, Circle }), document: doc },
  );
  return { view, overlays, maps, statuses, selections };
}
test("unconfigured map reports fallback without loading SDK", async () => {
  const f = setup();
  await f.view.configure({ enabled: false });
  assert.equal(f.statuses.at(-1), "unconfigured");
  assert.equal(f.maps.length, 0);
});
test("one marker and precision circle per device, clear on removal, selection focuses", async () => {
  const f = setup();
  await f.view.configure({
    enabled: true,
    key: "test",
    service_host: "/_AMapService",
  });
  f.view.update([p, { ...p, device_id: "b", name: "另一位" }], "a", at);
  assert.equal(f.overlays.size, 4);
  f.view.focus("a");
  assert.deepEqual(f.maps[0].center, [120, 30]);
  f.view.update([{ ...p, accuracy_m: 0 }], null, at);
  assert.equal(f.overlays.size, 2);
  f.view.clear();
  assert.equal(f.overlays.size, 0);
  f.view.destroy();
});
test("late SDK response after logout cannot create map", async () => {
  let resolve;
  let created = 0;
  const statuses = [];
  const view = createMapView(
    {},
    (s) => statuses.push(s),
    () => {},
    { load: () => new Promise((r) => (resolve = r)), document: {} },
  );
  const pending = view.configure({ enabled: true, key: "test" });
  view.destroy();
  resolve({
    Map: class {
      constructor() {
        created++;
      }
    },
  });
  await pending;
  assert.equal(created, 0);
});
test("adding a location after an empty map fits it and survives subsequent updates", async (t) => {
  const f = setup();
  t.after(() => f.view.destroy());
  await f.view.configure({ enabled: true, key: "test" });
  f.view.update([], null, at);
  assert.equal(f.overlays.size, 0);

  f.view.update([p], null, at);
  const marker = [...f.overlays].find((overlay) => overlay.opts.position);
  assert.equal(marker.opts.zIndex, 100);
  assert.deepEqual(f.maps[0].fittedMarkers, [marker]);

  const updated = { ...p, longitude: 121, accuracy_m: 30 };
  f.view.update([updated], "a", at + 10000);
  assert.equal(f.overlays.size, 2);
  assert.deepEqual(marker.opts.position, [121, 30]);
  assert.equal(marker.opts.zIndex, 200);
  f.view.focus("a");
  assert.deepEqual(f.maps[0].center, [121, 30]);
  f.view.update([updated], null, at + 20000);
  assert.equal(marker.opts.zIndex, 100);
  assert.equal(f.statuses.at(-1), "ready");
});
