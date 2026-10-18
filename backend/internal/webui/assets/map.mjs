import { locationStatus } from "./state.mjs";
let sdkPromise;
function loadSDK(config) {
  if (window.AMap) return Promise.resolve(window.AMap);
  if (sdkPromise) return sdkPromise;
  window._AMapSecurityConfig = {
    serviceHost: location.origin + config.service_host,
  };
  sdkPromise = new Promise((resolve, reject) => {
    const script = document.createElement("script");
    let finished = false;
    const end = (error) => {
      if (finished) return;
      finished = true;
      clearTimeout(timer);
      script.onload = script.onerror = null;
      if (error) {
        script.remove();
        reject(error);
      } else resolve(window.AMap);
    };
    const timer = setTimeout(() => end(new Error("map timeout")), 12000);
    script.src = `https://webapi.amap.com/maps?v=2.0&key=${encodeURIComponent(config.key)}`;
    script.async = true;
    script.onload = () =>
      end(window.AMap ? null : new Error("map unavailable"));
    script.onerror = () => end(new Error("map unavailable"));
    document.head.append(script);
  }).catch((error) => {
    sdkPromise = null;
    throw error;
  });
  return sdkPromise;
}
export function createMapView(container, onStatus, onSelect, options = {}) {
  const load = options.load || loadSDK;
  const doc = options.document || document;
  let map,
    sdk,
    generation = 0,
    timer,
    fitPending = true;
  const points = new Map();
  function clear() {
    if (map && points.size)
      map.remove([...points.values()].flatMap((v) => [v.marker, v.circle]));
    points.clear();
    fitPending = true;
  }
  function destroy() {
    generation++;
    clearTimeout(timer);
    clear();
    if (map) map.destroy();
    map = null;
    sdk = null;
  }
  async function configure(config) {
    destroy();
    const current = generation;
    if (!config.enabled) {
      onStatus("unconfigured");
      return;
    }
    onStatus("loading");
    try {
      const loaded = await load(config);
      if (current !== generation) return;
      sdk = loaded;
      map = new sdk.Map(container, {
        viewMode: "2D",
        zoom: 11,
        resizeEnable: true,
      });
      timer = setTimeout(() => {
        if (current === generation) {
          destroy();
          onStatus("failed");
        }
      }, 12000);
      map.on("complete", () => {
        if (current === generation) {
          clearTimeout(timer);
          onStatus("ready");
        }
      });
      map.on("error", () => {
        if (current === generation) {
          destroy();
          onStatus("failed");
        }
      });
    } catch {
      if (current === generation) {
        destroy();
        onStatus("failed");
      }
    }
  }
  function update(locations, selected, now) {
    if (!map) return;
    const wanted = new Set(locations.map((p) => p.device_id));
    for (const [id, entry] of points)
      if (!wanted.has(id)) {
        map.remove([entry.marker, entry.circle]);
        points.delete(id);
      }
    for (const point of locations) {
      const stale = locationStatus(point, now) === "stale";
      const color = stale ? "#98621e" : "#235b46";
      const position = [point.longitude, point.latitude];
      const content = doc.createElement("div");
      content.className = `map-marker${stale ? " stale" : ""}${selected === point.device_id ? " selected" : ""}`;
      content.textContent = Array.from(point.name || "家")[0];
      let entry = points.get(point.device_id);
      if (!entry) {
        const marker = new sdk.Marker({
          position,
          content,
          anchor: "center",
          title: point.name,
        });
        const circle = new sdk.Circle({
          center: position,
          radius: point.accuracy_m,
          strokeColor: color,
          strokeOpacity: 0.4,
          strokeWeight: 1,
          fillColor: color,
          fillOpacity: 0.08,
        });
        marker.on("click", () => onSelect(point.device_id));
        entry = { marker, circle, point };
        points.set(point.device_id, entry);
        map.add([circle, marker]);
      } else {
        entry.marker.setPosition(position);
        entry.marker.setContent(content);
        entry.circle.setCenter(position);
        entry.circle.setRadius(point.accuracy_m);
        entry.circle.setOptions({ strokeColor: color, fillColor: color });
        entry.point = point;
      }
      entry.marker.setzIndex(selected === point.device_id ? 200 : 100);
    }
    if (!points.size) fitPending = true;
    if (fitPending && points.size) {
      fit();
      fitPending = false;
    }
  }
  function fit() {
    if (map && points.size)
      map.setFitView(
        [...points.values()].map((v) => v.marker),
        false,
        [55, 55, 55, 55],
        16,
      );
  }
  function focus(id) {
    const entry = points.get(id);
    if (map && entry)
      map.setZoomAndCenter(16, [entry.point.longitude, entry.point.latitude]);
  }
  return {
    configure,
    update,
    clear,
    destroy,
    fit,
    focus,
    resize: () => {
      if (map) map.resize();
    },
  };
}
