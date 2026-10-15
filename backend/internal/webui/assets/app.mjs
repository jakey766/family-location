import {
  estimateServerTime,
  visibleLocations,
  locationStatus,
  relativeTime,
  remainingSeconds,
  errorMessage,
} from "./state.mjs";
import { createMapView } from "./map.mjs";
const $ = (id) => document.getElementById(id);
const state = {
  session: null,
  devices: [],
  locations: [],
  clock: null,
  selected: null,
  view: "locations",
  pairing: null,
  config: null,
  synced: false,
  connectionFailed: false,
  logoutPending: false,
};
let generation = 0,
  pairingGeneration = 0,
  logoutAttempt = 0,
  pollTimer,
  refreshing = false,
  pendingRevoke = null,
  previousFocus = null,
  suspended = false;
const requests = new Set();
const mapView = createMapView(
  $("map"),
  (status) => {
    const descriptions = {
      unconfigured: [
        "地图尚未配置",
        "目前仍可查看家人的位置文字信息。地图配置完成后会在这里显示。",
      ],
      loading: ["正在加载地图", "家人的位置和设备信息仍可在列表中查看。"],
      failed: [
        "地图暂时不可用",
        "请稍后刷新页面。下方仍可查看位置时间、精度和电量。",
      ],
    };
    $("map-fallback").hidden = status === "ready";
    $("fit-map").disabled = status !== "ready";
    if (descriptions[status]) {
      $("map-title").textContent = descriptions[status][0];
      $("map-description").textContent = descriptions[status][1];
    }
    if (status === "ready") renderLocations();
  },
  selectPerson,
);
function now() {
  return state.clock ? estimateServerTime(state.clock) : Date.now();
}
function message(text = "") {
  $("app-message").textContent = text;
  $("app-message").hidden = !text;
}
function invalidate() {
  generation++;
  clearTimeout(pollTimer);
  for (const controller of requests) controller.abort();
  requests.clear();
  refreshing = false;
}
async function request(path, { method = "GET", body } = {}) {
  const controller = new AbortController();
  requests.add(controller);
  const timer = setTimeout(() => controller.abort(), 10000);
  const headers = { Accept: "application/json" };
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (method !== "GET" && state.session)
    headers["X-CSRF-Token"] = state.session.csrf_token;
  try {
    const response = await fetch(path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      credentials: "same-origin",
      cache: "no-store",
      signal: controller.signal,
    });
    if (!response.ok) {
      const error = new Error("request failed");
      error.status = response.status;
      throw error;
    }
    return response.status === 204 ? null : await response.json();
  } finally {
    clearTimeout(timer);
    requests.delete(controller);
  }
}
function clearPairing() {
  pairingGeneration++;
  state.pairing = null;
  $("pairing-result").hidden = true;
  $("pairing-code").textContent = "";
  $("pairing-text").value = "";
  $("pairing-countdown").textContent = "";
  $("copy-status").textContent = "只分享给准备配对的家人。";
}
function clearLocations() {
  state.locations = [];
  state.synced = false;
  state.selected = null;
  mapView.clear();
  $("family-list").replaceChildren();
  $("sharing-count").textContent = "0 台设备有位置";
  $("family-empty").hidden = false;
}
function showLogin(text = "") {
  invalidate();
  clearPairing();
  clearLocations();
  mapView.destroy();
  closeConfirm(false);
  state.session = null;
  state.devices = [];
  state.config = null;
  state.clock = null;
  state.connectionFailed = false;
  state.logoutPending = false;
  $("devices-list").replaceChildren();
  $("username").textContent = "";
  $("login-password").value = "";
  $("loading").hidden = true;
  $("app-view").hidden = true;
  $("account").hidden = true;
  $("login-view").hidden = false;
  $("login-error").textContent = text;
  $("login-error").hidden = !text;
  $("logout").disabled = false;
  $("logout").textContent = "退出登录";
}
function handleError(error) {
  if (error.status === 401) {
    showLogin("登录已过期，请重新登录。");
    return true;
  }
  return false;
}
async function enterSession(session) {
  invalidate();
  state.session = session;
  state.logoutPending = false;
  state.config = null;
  $("username").textContent = session.user.username;
  $("loading").hidden = true;
  $("login-view").hidden = true;
  $("account").hidden = false;
  $("app-view").hidden = false;
  $("logout").textContent = "退出登录";
  message();
  switchView("locations");
  const current = generation;
  // Start data retrieval independently of third-party map loading.
  refresh();
  try {
    const config = await request("/api/v1/web-config");
    if (current !== generation || suspended) return;
    state.config = config;
    await mapView.configure(config.map);
    if (current === generation) renderLocations();
  } catch (error) {
    if (current === generation && !handleError(error))
      message("地图配置暂时无法读取；位置列表仍可使用，请刷新页面重试。");
  }
}
function scheduleRefresh() {
  clearTimeout(pollTimer);
  if (state.session && !state.logoutPending && !suspended)
    pollTimer = setTimeout(refresh, (state.config?.poll_seconds || 10) * 1000);
}
async function refresh() {
  if (!state.session || state.logoutPending || suspended || refreshing) return;
  refreshing = true;
  clearTimeout(pollTimer);
  $("refresh").disabled = true;
  const current = generation;
  try {
    const [positionResponse, devices] = await Promise.all([
      request("/api/v1/locations").then((snapshot) => ({
        snapshot,
        receivedAt: performance.now(),
      })),
      request("/api/v1/devices"),
    ]);
    if (current !== generation) return;
    const { snapshot, receivedAt } = positionResponse;
    const serverTime = Date.parse(snapshot.server_time);
    if (
      !Number.isFinite(serverTime) ||
      !Array.isArray(snapshot.locations) ||
      !Array.isArray(devices.devices)
    )
      throw new Error("invalid response");
    state.clock = { serverTime, receivedAt };
    state.devices = devices.devices;
    state.locations = snapshot.locations;
    state.synced = true;
    if (state.connectionFailed) message();
    state.connectionFailed = false;
    $("sync-status").textContent = "已同步 · 每 10 秒自动更新";
    renderLocations();
    renderDevices();
  } catch (error) {
    if (current !== generation) return;
    clearLocations();
    if (!handleError(error)) {
      state.connectionFailed = true;
      $("sync-status").textContent = "连接中断 · 暂不显示位置";
      message("未能确认家人的共享状态，已暂时隐藏位置。请检查网络后刷新。");
    }
  } finally {
    if (current === generation) {
      refreshing = false;
      $("refresh").disabled = false;
      scheduleRefresh();
    }
  }
}
function element(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}
function currentLocations() {
  return state.synced
    ? visibleLocations(state.locations, state.devices, now())
    : [];
}
function renderLocations() {
  if (!state.session || state.logoutPending || suspended) return;
  const points = currentLocations();
  const time = now();
  if (!points.some((p) => p.device_id === state.selected))
    state.selected = null;
  const fragment = document.createDocumentFragment();
  for (const point of points) {
    const stale = locationStatus(point, time) === "stale";
    const card = element(
      "article",
      `person-card${state.selected === point.device_id ? " selected" : ""}`,
    );
    card.dataset.deviceId = point.device_id;
    const top = element("div", "person-top");
    const avatar = element(
      "span",
      `avatar${stale ? " stale" : ""}`,
      Array.from(point.name || "家")[0],
    );
    avatar.setAttribute("aria-hidden", "true");
    const button = element("button", "person-name", point.name);
    button.type = "button";
    button.setAttribute("aria-label", `查看 ${point.name} 的位置`);
    button.addEventListener("click", () => selectPerson(point.device_id));
    top.append(
      avatar,
      button,
      element(
        "span",
        `status-badge${stale ? " stale" : ""}`,
        stale ? "位置已过期" : "最近定位",
      ),
    );
    const times = element("div", "person-times");
    const captured = element("div");
    captured.append(
      document.createTextNode("定位于 "),
      element("strong", "capture-age", relativeTime(point.captured_at, time)),
    );
    captured.title = new Date(point.captured_at).toLocaleString("zh-CN");
    const received = element(
      "span",
      "receive-age",
      `同步于 ${relativeTime(point.received_at, time)}`,
    );
    times.append(captured, received);
    const meta = element("div", "person-meta");
    meta.append(
      element("span", "", `精度 ±${Math.round(point.accuracy_m)} 米`),
      element(
        "span",
        "",
        point.battery_percent == null
          ? "电量未知"
          : `电量 ${point.battery_percent}%`,
      ),
    );
    card.append(
      top,
      times,
      meta,
      element(
        "p",
        "coordinates",
        `${point.latitude.toFixed(5)}, ${point.longitude.toFixed(5)}`,
      ),
    );
    fragment.append(card);
  }
  $("family-list").replaceChildren(fragment);
  $("family-empty").hidden = points.length > 0;
  $("sharing-count").textContent = `${points.length} 台设备有位置`;
  mapView.update(points, state.selected, time);
}
function tick() {
  if (!state.session || suspended || state.logoutPending) return;
  const points = currentLocations();
  const cards = [...$("family-list").children];
  let redraw = cards.length !== points.length;
  points.forEach((p, index) => {
    const card = cards[index];
    if (!card || card.dataset.deviceId !== p.device_id) {
      redraw = true;
      return;
    }
    const stale = locationStatus(p, now()) === "stale";
    if (
      card.querySelector(".status-badge").classList.contains("stale") !== stale
    ) {
      redraw = true;
      return;
    }
    card.querySelector(".capture-age").textContent = relativeTime(
      p.captured_at,
      now(),
    );
    card.querySelector(".receive-age").textContent =
      `同步于 ${relativeTime(p.received_at, now())}`;
  });
  if (redraw) renderLocations();
  if (state.pairing) {
    const seconds = remainingSeconds(state.pairing.expires_at, now());
    if (seconds === 0) {
      clearPairing();
      message("配对信息已过期，请重新生成。");
    } else
      $("pairing-countdown").textContent =
        `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")} 后过期`;
  }
}
function selectPerson(id) {
  state.selected = id;
  renderLocations();
  mapView.focus(id);
}
function renderDevices() {
  $("device-count").textContent = String(
    state.devices.filter((d) => !d.revoked_at).length,
  );
  // Avoid replacing the focused confirmation trigger during periodic refresh.
  if (pendingRevoke) return;
  const fragment = document.createDocumentFragment();
  for (const device of state.devices) {
    const row = element("div", "device-row");
    const avatar = element(
      "span",
      "avatar",
      Array.from(device.name || "家")[0],
    );
    avatar.setAttribute("aria-hidden", "true");
    const info = element("div", "device-info");
    info.append(
      element("h3", "", device.name),
      element(
        "p",
        "",
        device.revoked_at
          ? "已撤销 · 需要重新配对"
          : currentLocations().some((p) => p.device_id === device.id)
            ? "已配对 · 有可查看的位置"
            : "已配对 · 暂无可查看的位置",
      ),
    );
    row.append(avatar, info);
    if (!device.revoked_at) {
      const button = element("button", "text-button", "撤销");
      button.type = "button";
      button.setAttribute("aria-label", `撤销 ${device.name}`);
      button.addEventListener("click", () => openConfirm(device, button));
      row.append(button);
    }
    fragment.append(row);
  }
  $("devices-list").replaceChildren(fragment);
  $("devices-empty").hidden = state.devices.length > 0;
}
function switchView(view) {
  if (state.view !== view) clearPairing();
  state.view = view;
  $("locations-view").hidden = view !== "locations";
  $("devices-view").hidden = view !== "devices";
  $("page-heading").textContent =
    view === "locations" ? "家人在哪里" : "一起，把家人连起来";
  for (const name of ["locations", "devices"]) {
    if (view === name) $(`tab-${name}`).setAttribute("aria-current", "page");
    else $(`tab-${name}`).removeAttribute("aria-current");
  }
  if (view === "locations") requestAnimationFrame(() => mapView.resize());
}
function openConfirm(device, button) {
  pendingRevoke = device;
  previousFocus = button;
  $("confirm-description").textContent =
    `撤销“${device.name}”后，将立即停止显示其位置。设备需要重新配对才能继续共享。`;
  $("confirm-overlay").hidden = false;
  $("cancel-revoke").focus();
}
function closeConfirm(restore = true) {
  $("confirm-overlay").hidden = true;
  pendingRevoke = null;
  if (restore && previousFocus?.isConnected) previousFocus.focus();
  previousFocus = null;
}
$("confirm-overlay").addEventListener("keydown", (event) => {
  if (event.key === "Escape" && !$("confirm-revoke").disabled) {
    closeConfirm();
    return;
  }
  if (event.key === "Tab") {
    event.preventDefault();
    (document.activeElement === $("cancel-revoke")
      ? $("confirm-revoke")
      : $("cancel-revoke")
    ).focus();
  }
});
$("cancel-revoke").addEventListener("click", () => closeConfirm());
$("confirm-revoke").addEventListener("click", async () => {
  if (!pendingRevoke) return;
  const target = pendingRevoke;
  const current = generation;
  $("confirm-revoke").disabled = true;
  $("cancel-revoke").disabled = true;
  try {
    await request(`/api/v1/devices/${encodeURIComponent(target.id)}`, {
      method: "DELETE",
    });
    if (current !== generation) return;
    invalidate();
    state.devices = state.devices.map((d) =>
      d.id === target.id ? { ...d, revoked_at: new Date().toISOString() } : d,
    );
    state.locations = state.locations.filter((p) => p.device_id !== target.id);
    closeConfirm();
    renderLocations();
    renderDevices();
    message("设备已撤销，位置已移除。");
    refresh();
  } catch (error) {
    if (current === generation && !handleError(error)) {
      closeConfirm();
      message(errorMessage(error));
    }
  } finally {
    $("confirm-revoke").disabled = false;
    $("cancel-revoke").disabled = false;
  }
});
$("login-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const current = generation;
  $("login-submit").disabled = true;
  $("login-error").hidden = true;
  const password = $("login-password").value;
  try {
    const session = await request("/api/v1/auth/login", {
      method: "POST",
      body: { username: $("login-username").value.trim(), password },
    });
    if (current === generation) {
      $("login-password").value = "";
      await enterSession(session);
    }
  } catch (error) {
    if (current === generation) {
      $("login-error").textContent = errorMessage(error);
      $("login-error").hidden = false;
    }
  } finally {
    $("login-submit").disabled = false;
    $("login-password").value = "";
  }
});
$("logout").addEventListener("click", async () => {
  if (!state.session) return;
  invalidate();
  const current = generation;
  const attempt = ++logoutAttempt;
  state.logoutPending = true;
  clearPairing();
  clearLocations();
  mapView.destroy();
  closeConfirm(false);
  $("devices-list").replaceChildren();
  $("logout").disabled = true;
  $("refresh").disabled = true;
  try {
    await request("/api/v1/auth/logout", { method: "POST" });
    if (current === generation) showLogin();
  } catch (error) {
    if (current === generation) {
      if (error.status === 401) showLogin();
      else {
        message("退出未完成：服务端会话尚未确认撤销。请恢复网络后重试退出。");
        $("logout").textContent = "重试退出";
      }
    }
  } finally {
    if (attempt === logoutAttempt) $("logout").disabled = false;
  }
});
$("generate-pairing").addEventListener("click", async () => {
  if (!state.session || state.logoutPending) return;
  const current = generation;
  clearPairing();
  const pairingRequest = pairingGeneration;
  $("generate-pairing").disabled = true;
  message();
  try {
    const pairing = await request("/api/v1/pairing-codes", {
      method: "POST",
      body: {},
    });
    if (
      current !== generation ||
      pairingRequest !== pairingGeneration ||
      state.view !== "devices" ||
      suspended
    )
      return;
    state.pairing = pairing;
    $("pairing-code").textContent = pairing.code;
    $("pairing-text").value = JSON.stringify(
      {
        server_url: location.origin,
        pairing_id: pairing.pairing_id,
        code: pairing.code,
      },
      null,
      2,
    );
    $("pairing-result").hidden = false;
    tick();
  } catch (error) {
    if (current === generation && !handleError(error))
      message(errorMessage(error));
  } finally {
    $("generate-pairing").disabled = false;
  }
});
$("copy-pairing").addEventListener("click", async () => {
  if (!state.pairing) return;
  try {
    if (!navigator.clipboard?.writeText)
      throw new Error("clipboard unavailable");
    await navigator.clipboard.writeText($("pairing-text").value);
    $("copy-status").textContent = "已复制，只分享给准备配对的家人。";
  } catch {
    $("pairing-text").focus();
    $("pairing-text").select();
    $("copy-status").textContent =
      "当前浏览器无法自动复制，请长按上方文本，选择并复制。";
  }
});
$("tab-locations").addEventListener("click", () => switchView("locations"));
$("tab-devices").addEventListener("click", () => switchView("devices"));
$("empty-add").addEventListener("click", () => switchView("devices"));
$("fit-map").addEventListener("click", () => mapView.fit());
$("refresh").addEventListener("click", () => {
  message();
  refresh();
});
async function restore() {
  const current = generation;
  try {
    const session = await request("/api/v1/auth/me");
    if (current === generation && !suspended) await enterSession(session);
  } catch (error) {
    if (current === generation && !suspended)
      showLogin(
        error.status === 401 ? "" : "暂时无法连接服务，请检查网络后登录。",
      );
  }
}
function suspend() {
  suspended = true;
  invalidate();
  clearPairing();
  clearLocations();
  mapView.destroy();
  closeConfirm(false);
  $("devices-list").replaceChildren();
  $("sync-status").textContent = "重新打开后更新共享状态";
}
function resume() {
  if (document.hidden) return;
  suspended = false;
  if (state.logoutPending) {
    $("logout").disabled = false;
    $("logout").textContent = "重试退出";
    message("退出尚未完成，请重试退出以撤销服务端会话。");
    return;
  }
  invalidate();
  restore();
}
window.addEventListener("pagehide", suspend);
window.addEventListener("pageshow", (event) => {
  if (event.persisted) resume();
});
document.addEventListener("visibilitychange", () =>
  document.hidden ? suspend() : resume(),
);
window.addEventListener("offline", () => {
  if (state.session) {
    invalidate();
    clearLocations();
    $("sync-status").textContent = "网络已断开 · 暂不显示位置";
    message("网络已断开，位置暂时隐藏。恢复连接后会重新获取。");
    scheduleRefresh();
  }
});
window.addEventListener("online", () => {
  if (!state.logoutPending) resume();
});
setInterval(tick, 1000);
restore();
