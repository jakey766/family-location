// Family-only entry: never requests administrator endpoints or stores passcodes.
import {
  estimateServerTime,
  visibleLocations,
  locationStatus,
  relativeTime,
} from "./state.mjs";
import { createMapView } from "./map.mjs";
const $ = (id) => document.getElementById(id);
const keys = new URL(location.href).searchParams.getAll("key");
const key =
  keys.length === 1 && /^[A-Za-z0-9_-]{1,128}$/.test(keys[0]) ? keys[0] : null;
const endpoint = key ? `/api/v1/family/${encodeURIComponent(key)}` : null;
let generation = 0,
  suspended = false,
  refreshing = false,
  snapshot = null,
  clock = null,
  selected = null,
  config = null,
  timer,
  expiryTimer,
  mapLoading = false,
  logoutPending = false,
  logoutAttempt = 0;
const requests = new Set();
const mapView = createMapView(
  $("map"),
  (status) => {
    $("map-fallback").hidden = status === "ready";
    $("fit-map").disabled = status !== "ready";
    const descriptions = {
      unconfigured: ["地图尚未配置", "仍可查看位置、精度、电量与定位时间。"],
      loading: ["正在加载地图", "下方仍可查看家人的位置文字信息。"],
      failed: ["地图暂时不可用", "请稍后刷新重试，下方位置列表仍可使用。"],
    };
    if (descriptions[status]) {
      $("map-title").textContent = descriptions[status][0];
      $("map-description").textContent = descriptions[status][1];
    }
    if (status === "failed") config = null;
    if (status === "ready") render();
  },
  (id) => {
    selected = id;
    render();
    mapView.focus(id);
  },
);
const now = () => (clock ? estimateServerTime(clock) : Date.now());
function message(text = "") {
  $("viewer-message").textContent = text;
  $("viewer-message").hidden = !text;
}
function invalidate() {
  generation++;
  clearTimeout(timer);
  clearTimeout(expiryTimer);
  for (const r of requests) r.abort();
  requests.clear();
  refreshing = false;
  mapLoading = false;
}
function clear() {
  snapshot = null;
  clock = null;
  selected = null;
  config = null;
  mapView.destroy();
  $("family-list").replaceChildren();
  $("family-name").textContent = "";
  $("sharing-count").textContent = "0 台设备有位置";
  $("family-passcode").value = "";
  $("family-view").hidden = true;
  message();
}
function gate(text = "", invalid = false) {
  invalidate();
  clear();
  $("loading").hidden = true;
  $("family-auth").hidden = invalid;
  $("family-invalid").hidden = !invalid;
  $("family-auth-error").textContent = text;
  $("family-auth-error").hidden = !text;
  $("family-auth-submit").disabled = false;
  $("refresh").disabled = false;
}
function denied(error) {
  if (error.status === 404) {
    gate("", true);
    return true;
  }
  if (error.status === 401 || error.status === 403) {
    gate("访问已过期、口令已更改或尚未设置，请重新验证或联系管理员。");
    return true;
  }
  return false;
}
async function request(path, options = {}) {
  const controller = new AbortController();
  requests.add(controller);
  const timeout = setTimeout(() => controller.abort(), 10000);
  try {
    const response = await fetch(endpoint + path, {
      method: options.method || "GET",
      credentials: "same-origin",
      cache: "no-store",
      headers: {
        Accept: "application/json",
        ...(options.body ? { "Content-Type": "application/json" } : {}),
      },
      body: options.body ? JSON.stringify(options.body) : undefined,
      signal: controller.signal,
    });
    if (!response.ok) {
      const error = new Error("request failed");
      error.status = response.status;
      throw error;
    }
    return response.status === 204 ? null : await response.json();
  } finally {
    clearTimeout(timeout);
    requests.delete(controller);
  }
}
function schedule() {
  clearTimeout(timer);
  if (snapshot && !suspended && !logoutPending)
    timer = setTimeout(refresh, (config?.poll_seconds || 10) * 1000);
}
function armExpiry() {
  clearTimeout(expiryTimer);
  if (!snapshot) return;
  const delay = Date.parse(snapshot.expires_at) - now();
  if (delay <= 0) {
    gate("访问已到期，请重新输入家庭口令。");
    return;
  }
  expiryTimer = setTimeout(
    () => {
      if (snapshot) gate("访问已到期，请重新输入家庭口令。");
    },
    Math.min(delay, 2147483647),
  );
}
async function loadMap() {
  if (!snapshot || config || mapLoading || suspended || logoutPending) return;
  const epoch = generation;
  mapLoading = true;
  try {
    const result = await request("/web-config");
    if (epoch !== generation) return;
    config = result;
    await mapView.configure(result.map);
    if (epoch === generation) render();
  } catch (error) {
    if (epoch === generation && !denied(error))
      message("地图配置暂时无法读取，位置列表仍可使用；刷新后重试。");
  } finally {
    if (epoch === generation) mapLoading = false;
  }
}
async function refresh() {
  if (!key || suspended || refreshing || logoutPending) return;
  const epoch = generation;
  refreshing = true;
  $("refresh").disabled = true;
  clearTimeout(timer);
  try {
    const result = await request("/snapshot");
    const receivedAt = performance.now();
    if (epoch !== generation || suspended) return;
    const serverTime = Date.parse(result.server_time),
      expires = Date.parse(result.expires_at);
    if (
      !Number.isFinite(serverTime) ||
      !Number.isFinite(expires) ||
      result.family?.public_id !== key ||
      !Array.isArray(result.devices) ||
      !Array.isArray(result.locations)
    )
      throw new Error("invalid snapshot");
    snapshot = result;
    clock = { serverTime, receivedAt };
    if (expires <= now()) {
      gate("访问已到期，请重新输入家庭口令。");
      return;
    }
    $("loading").hidden = true;
    $("family-auth").hidden = true;
    $("family-invalid").hidden = true;
    $("family-view").hidden = false;
    $("family-name").textContent = result.family.name;
    $("family-passcode").value = "";
    $("sync-status").textContent = "已同步 · 每 10 秒自动更新";
    message();
    render();
    armExpiry();
    loadMap();
  } catch (error) {
    if (epoch !== generation) return;
    if (!denied(error)) {
      if (snapshot) {
        $("sync-status").textContent = "连接中断 · 显示最后位置";
        message("连接中断，共享状态尚未确认，恢复网络后会重试。");
      } else gate("暂时无法连接，请检查网络后重试访问。");
    }
  } finally {
    if (epoch === generation) {
      refreshing = false;
      $("refresh").disabled = false;
      schedule();
    }
  }
}
function node(tag, cls, text) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text !== undefined) n.textContent = text;
  return n;
}
function render() {
  if (!snapshot || suspended || logoutPending) return;
  if (Date.parse(snapshot.expires_at) <= now()) {
    gate("访问已到期，请重新输入家庭口令。");
    return;
  }
  const points = visibleLocations(snapshot.locations, snapshot.devices, now());
  const byID = new Map(points.map((p) => [p.device_id, p]));
  if (!byID.has(selected)) selected = null;
  const cards = snapshot.devices.map((device) => {
    const point = byID.get(device.id);
    const card = node(
      "article",
      `person-card${selected === device.id ? " selected" : ""}`,
    );
    card.dataset.deviceId = device.id;
    const top = node("div", "person-top");
    const stale = point && locationStatus(point, now()) === "stale";
    const name = node(point ? "button" : "h3", "person-name", device.name);
    if (point) {
      name.type = "button";
      name.addEventListener("click", () => {
        selected = device.id;
        render();
        mapView.focus(device.id);
      });
    }
    top.append(
      node(
        "span",
        `avatar${stale ? " stale" : ""}`,
        Array.from(device.name || "家")[0],
      ),
      name,
      node(
        "span",
        `status-badge${stale ? " stale" : ""}`,
        point
          ? stale
            ? "最后位置 · 更新延迟"
            : "最近定位"
          : device.revoked_at
            ? "已撤销"
            : "暂无位置",
      ),
    );
    card.append(top);
    if (point) {
      const times = node("div", "person-times");
      times.append(
        node(
          "strong",
          "capture-age",
          `定位于 ${relativeTime(point.captured_at, now())}`,
        ),
        node(
          "span",
          "receive-age",
          `同步于 ${relativeTime(point.received_at, now())}`,
        ),
      );
      const raw = node(
        "time",
        "coordinates",
        `原始定位时间：${new Date(point.captured_at).toLocaleString("zh-CN")}`,
      );
      raw.dateTime = point.captured_at;
      const meta = node("div", "person-meta");
      meta.append(
        node("span", "", `精度 ±${Math.round(point.accuracy_m)} 米`),
        node(
          "span",
          "",
          point.battery_percent == null
            ? "电量未知"
            : `电量 ${point.battery_percent}%`,
        ),
      );
      card.append(
        times,
        raw,
        meta,
        node(
          "p",
          "coordinates",
          `${point.latitude.toFixed(5)}, ${point.longitude.toFixed(5)}`,
        ),
      );
    } else
      card.append(
        node(
          "p",
          "muted",
          device.revoked_at
            ? "设备已撤销，暂无可查看的位置。"
            : "暂无可查看的位置，家人可能尚未开启或已停止共享。",
        ),
      );
    return card;
  });
  $("family-list").replaceChildren(...cards);
  $("family-empty").hidden = cards.length > 0;
  $("sharing-count").textContent = `${points.length} 台设备有位置`;
  mapView.update(points, selected, now());
}
$("family-auth-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!key || suspended || $("family-auth-submit").disabled) return;
  const passcode = $("family-passcode").value;
  if (!/^[0-9]{7}$/.test(passcode)) {
    $("family-auth-error").hidden = false;
    $("family-auth-error").textContent = "请输入 7 位数字口令。";
    return;
  }
  invalidate();
  const epoch = generation;
  $("family-auth-submit").disabled = true;
  $("family-auth-error").hidden = true;
  try {
    await request("/session", { method: "POST", body: { passcode } });
    if (epoch !== generation) return;
    $("family-passcode").value = "";
    await refresh();
  } catch (error) {
    if (epoch !== generation) return;
    if (error.status === 404) gate("", true);
    else {
      $("family-auth-error").hidden = false;
      $("family-auth-error").textContent =
        error.status === 429
          ? "尝试过于频繁，请稍后再试。"
          : error.status === 401 || error.status === 403
            ? "家庭口令不正确或尚未设置，请联系分享链接的家人。"
            : "暂时无法验证，请检查网络后重试。";
    }
  } finally {
    if (epoch === generation) {
      $("family-auth-submit").disabled = false;
      $("family-passcode").value = "";
    }
  }
});
$("family-logout").addEventListener("click", async () => {
  if (!key || $("family-logout").disabled) return;
  invalidate();
  const attempt = ++logoutAttempt;
  logoutPending = true;
  clear();
  $("family-view").hidden = false;
  $("family-logout").disabled = true;
  $("refresh").disabled = true;
  try {
    await request("/session", { method: "DELETE" });
    if (attempt === logoutAttempt) {
      logoutPending = false;
      gate();
    }
  } catch (error) {
    if (attempt === logoutAttempt) {
      if (error.status === 401 || error.status === 404) {
        logoutPending = false;
        gate("", error.status === 404);
      } else {
        message("退出尚未完成，请恢复网络后重试退出。");
        $("family-logout").textContent = "重试退出此家庭";
      }
    }
  } finally {
    if (attempt === logoutAttempt) $("family-logout").disabled = false;
  }
});
$("refresh").addEventListener("click", refresh);
$("retry-session").addEventListener("click", refresh);
$("fit-map").addEventListener("click", () => mapView.fit());
function suspend() {
  suspended = true;
  invalidate();
  clear();
  $("family-auth").hidden = true;
  $("loading").hidden = false;
}
function resume() {
  if (document.hidden || !key) return;
  suspended = false;
  if (logoutPending) {
    $("loading").hidden = true;
    $("family-view").hidden = false;
    $("family-logout").disabled = false;
    message("退出尚未完成，请重试退出此家庭。");
    return;
  }
  invalidate();
  refresh();
}
window.addEventListener("pagehide", suspend);
window.addEventListener("pageshow", (event) => {
  if (event.persisted) resume();
});
document.addEventListener("visibilitychange", () =>
  document.hidden ? suspend() : resume(),
);
window.addEventListener("offline", () => {
  if (!key) return;
  invalidate();
  $("family-passcode").value = "";
  $("family-auth-submit").disabled = false;
  if (snapshot) {
    $("sync-status").textContent = "网络已断开 · 显示最后位置";
    message("网络已断开，共享状态尚未确认。恢复连接后会重新获取。");
    armExpiry();
    schedule();
  }
});
window.addEventListener("online", () => {
  if (!suspended && !logoutPending) refresh();
});
setInterval(() => {
  if (snapshot) render();
}, 1000);
if (key) refresh();
else gate("", true);
