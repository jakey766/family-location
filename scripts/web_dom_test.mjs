// Real API + DOM integration. This does not simulate layout or replace Safari QA.
import {
  JSDOM,
  CookieJar,
  VirtualConsole,
} from "../.cache/web-test/node_modules/jsdom/lib/api.js";
import { SourceTextModule } from "node:vm";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import assert from "node:assert/strict";
const base = process.env.WEB_TEST_URL;
const username = process.env.WEB_TEST_USERNAME;
const password = process.env.WEB_TEST_PASSWORD;
assert(base && username && password);
const jar = new CookieJar();
const errors = [];
const virtualConsole = new VirtualConsole();
virtualConsole.on("jsdomError", (e) => errors.push(e.message));
const dom = new JSDOM(
  await readFile("backend/internal/webui/assets/index.html", "utf8"),
  {
    url: base,
    runScripts: "outside-only",
    pretendToBeVisual: true,
    cookieJar: jar,
    virtualConsole,
  },
);
const { window } = dom;
const doc = window.document;
const $ = (id) => doc.getElementById(id);
window.requestAnimationFrame = (fn) => window.setTimeout(fn, 0);
// Node fetch requires its own AbortSignal brand across the vm realm.
window.AbortController = globalThis.AbortController;
let intercept = null;
const realRequest = async (path, options = {}) => {
  const url = new URL(path, base);
  const headers = new Headers(options.headers);
  const cookie = jar.getCookieStringSync(url.href);
  if (cookie) headers.set("Cookie", cookie);
  if (options.method && options.method !== "GET")
    headers.set("Origin", new URL(base).origin);
  let response;
  try {
    response = await fetch(url, { ...options, headers });
  } catch (e) {
    console.error("fetch error", url.pathname, e.name, e.message);
    throw e;
  }

  for (const cookie of response.headers.getSetCookie())
    jar.setCookieSync(cookie, url.href);
  return response;
};
window.fetch = (path, options) =>
  intercept
    ? intercept(path, options, realRequest)
    : realRequest(path, options);
const modules = new Map();
const context = dom.getInternalVMContext();
async function load(file) {
  if (modules.has(file)) return modules.get(file);
  const mod = new SourceTextModule(await readFile(file, "utf8"), {
    context,
    identifier: file,
  });
  modules.set(file, mod);
  await mod.link((specifier, reference) =>
    load(fileURLToPath(new URL(specifier, "file://" + reference.identifier))),
  );
  return mod;
}
const app = await load(
  fileURLToPath(
    new URL("../backend/internal/webui/assets/app.mjs", import.meta.url),
  ),
);
async function until(fn, label) {
  const deadline = Date.now() + 4000;
  while (!fn()) {
    if (Date.now() > deadline)
      throw new Error(
        "timeout: " +
          label +
          "; UI: " +
          $("login-error").textContent +
          " / " +
          $("app-message").textContent +
          "; errors: " +
          errors.join(";"),
      );
    await new Promise((r) => setTimeout(r, 10));
  }
}
async function settle() {
  await new Promise((r) => setTimeout(r, 40));
}
function click(id) {
  $(id).click();
}
async function login() {
  $("login-username").value = username;
  $("login-password").value = password;
  $("login-form").dispatchEvent(
    new window.Event("submit", { cancelable: true }),
  );
  await until(
    () => !$("app-view").hidden && !$("refresh").disabled,
    "login and sync",
  );
}
function defer() {
  let resolve;
  const promise = new Promise((r) => (resolve = r));
  return { promise, resolve };
}
try {
  await app.evaluate();
  await until(() => !$("login-view").hidden, "initial unauthenticated");
  await login();
  await until(
    () => doc.querySelectorAll(".person-card").length === 2,
    "two devices",
  );
  await until(() => $("map-title").textContent.includes("未配置"), "map fallback loaded");
  assert.match($("map-title").textContent, /未配置/);
  assert.match($("family-list").textContent, /最后位置/);
  assert.match($("family-list").textContent, /电量未知/);
  assert.equal($("login-password").value, "");
  assert.equal(window.localStorage.length + window.sessionStorage.length, 0);
  console.log(
    "PASS: DOM + real HTTP login/CSRF, two devices, original-time stale, optional battery, map fallback",
  );
  assert($("family-select"), "family selector missing");
  assert(new URL(window.location.href).searchParams.get("family"), "link has no family identifier");
  click("tab-devices");
  click("generate-pairing");
  await until(() => !$("pairing-result").hidden, "pairing");
  const pairing = JSON.parse($("pairing-text").value);
  assert.match(pairing.code, /^\d{8}$/);
  assert(pairing.pairing_id && pairing.server_url === base);
  click("copy-pairing");
  await settle();
  assert.match($("copy-status").textContent, /长按/);
  click("tab-locations");
  assert.equal($("pairing-text").value, "");
  // A pending generation must not bring a pairing code back after leave/re-entry.
  click("tab-devices");
  const pairGate = defer();
  let pairReady = false;
  intercept = async (path, options, next) => {
    const response = await next(path, options);
    if (path.split("?")[0] === "/api/v1/pairing-codes") {
      pairReady = true;
      await pairGate.promise;
    }
    return response;
  };
  click("generate-pairing");
  await until(() => pairReady, "pair response held");
  click("tab-locations");
  click("tab-devices");
  pairGate.resolve();
  await settle();
  intercept = null;
  assert.equal(
    $("pairing-result").hidden,
    true,
    "late pairing response restored after leaving management",
  );
  console.log(
    "PASS: pairing generation, HTTP clipboard fallback, leave-page invalidation",
  );
  // Network failure retains a clearly marked last snapshot; expired points remain stale.
  click("tab-locations");
  window.dispatchEvent(new window.Event("offline"));
  assert.equal(doc.querySelectorAll(".person-card").length, 2);
  assert.match($("sync-status").textContent, /最后位置/);
  window.dispatchEvent(new window.Event("online"));
  await until(
    () => !$("refresh").disabled && $("sync-status").textContent.includes("已同步"),
    "online reload",
  );
  const delayedSnapshot = await (await realRequest("/api/v1/locations")).json();
  intercept = async (path, options, next) => {
    if (path.split("?")[0] === "/api/v1/locations") {
      const current = Date.now();
      return new Response(
        JSON.stringify({
          server_time: new Date(current).toISOString(),
          locations: delayedSnapshot.locations.map((p) => ({
            ...p,
            lease_expires_at: new Date(current + 500).toISOString(),
          })),
        }),
        { headers: { "Content-Type": "application/json" } },
      );
    }
    const response = await next(path, options);
    if (path.split("?")[0] === "/api/v1/devices")
      await new Promise((resolve) => setTimeout(resolve, 900));
    return response;
  };
  click("refresh");
  await until(() => !$("refresh").disabled, "slow devices response");
  assert.equal(
    doc.querySelectorAll(".person-card").length,
    2,
    "slow devices response lost last positions",
  );
  assert.equal(doc.querySelectorAll(".status-badge.stale").length, 2);
  intercept = null;
  click("refresh");
  await until(
    () => !$("refresh").disabled,
    "after slow devices check",
  );
  const saved = await (await realRequest("/api/v1/locations")).json();
  intercept = async (path, options, next) =>
    path.split("?")[0] === "/api/v1/locations"
      ? new Response(
          JSON.stringify({
            ...saved,
            locations: saved.locations.map((p) => ({
              ...p,
              lease_expires_at: saved.server_time,
            })),
          }),
          { headers: { "Content-Type": "application/json" } },
        )
      : next(path, options);
  click("refresh");
  await until(() => !$("refresh").disabled, "expiry refresh");
  assert.equal(doc.querySelectorAll(".person-card").length, 2);
  assert.equal(doc.querySelectorAll(".status-badge.stale").length, 2);
  intercept = null;
  click("refresh");
  await until(
    () => !$("refresh").disabled,
    "after expiry check",
  );
  console.log("PASS: offline snapshot, server time / stale last positions, recovery");
  // Malicious names stay literal text. This creates only an isolated test device.
  const session = await (await realRequest("/api/v1/auth/me")).json();
  const freshPair = await (
    await realRequest("/api/v1/pairing-codes", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": session.csrf_token,
      },
      body: "{}",
    })
  ).json();
  await realRequest("/api/v1/devices/pair", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      pairing_id: freshPair.pairing_id,
      code: freshPair.code,
      name: "<img src=x onerror=alert(1)>",
    }),
  });
  click("refresh");
  await until(() => !$("refresh").disabled, "device reload");
  click("tab-devices");
  assert.match($("devices-list").textContent, /<img/);
  assert.equal($("devices-list").querySelectorAll("img").length, 0);
  const revoke = $("devices-list").querySelector("button");
  revoke.click();
  click("cancel-revoke");
  assert.equal($("confirm-overlay").hidden, true);
  revoke.click();
  click("confirm-revoke");
  await until(
    () => $("confirm-overlay").hidden && !$("refresh").disabled,
    "revoke complete",
  );
  click("tab-locations");
  assert.equal(doc.querySelectorAll(".person-card").length, 1);
  console.log(
    "PASS: safe text rendering, revoke cancel/confirm, immediate removal",
  );
  // Multi-family membership, settings and response isolation use the real backend.
  const firstFamily = $("family-select").value;
  click("tab-devices");
  $("family-name").value = "测试第二家庭";
  $("create-family-form").dispatchEvent(new window.Event("submit", {cancelable:true}));
  await until(() => $("family-select").value !== firstFamily && !$("refresh").disabled, "new family selected");
  const secondFamily = $("family-select").value;
  assert.equal(doc.querySelectorAll(".person-card").length, 0);
  assert.equal(new URL(window.location.href).searchParams.get("family"), secondFamily);
  assert.equal($("devices-list").children.length, 0);
  const sharedPoint = (await (await realRequest(`/api/v1/locations?family=${firstFamily}`)).json()).locations[0];
  const ownedRow = [...$("owned-devices-list").children].find((r) => r.dataset.deviceId === sharedPoint.device_id);
  ownedRow.querySelector(".add-member").click();
  await until(() => $("devices-list").children.length === 1 && !$("refresh").disabled, "existing device joined second family");
  click("tab-locations");
  assert.equal(doc.querySelectorAll(".person-card").length, 1);
  click("copy-family"); await settle();
  assert.match($("family-link-status").textContent, /长按/);
  click("tab-devices");
  $("devices-list").querySelector(".config-device").click();
  await until(() => !$("config-fields").disabled, "configuration loaded");
  assert.match($("config-device-families").textContent, /测试第二家庭/);
  assert.equal($("moving-minutes").value, "2");
  assert.equal($("stationary-minutes").value, "5");
  assert.equal($("moving-high-accuracy").checked, false);
  assert.equal($("quiet-start").value, "22:00");
  assert.equal($("quiet-end").value, "08:00");
  $("moving-minutes").value = "3";
  $("stationary-minutes").value = "10";
  $("moving-high-accuracy").checked = true;
  $("quiet-start").value = "12:30";
  $("quiet-end").value = "13:45";
  $("device-config-form").dispatchEvent(new window.Event("submit", {cancelable:true}));
  await until(() => $("config-status").textContent.includes("已保存"), "config saved");
  const savedConfig = await (await realRequest(`/api/v1/devices/${sharedPoint.device_id}/config`)).json();
  assert.deepEqual(savedConfig, {version:2,moving_interval_seconds:180,stationary_interval_seconds:600,moving_high_accuracy:true,quiet_hours_start:"12:30",quiet_hours_end:"13:45"});
  $("quiet-end").value="12:30";
  $("device-config-form").dispatchEvent(new window.Event("submit", {cancelable:true}));
  assert.match($("config-status").textContent, /不能相同/);
  assert.equal((await (await realRequest(`/api/v1/devices/${sharedPoint.device_id}/config`)).json()).version, 2);
  $("quiet-end").value="13:45";
  intercept=(path,options,next) => options?.method === "PUT" && path.endsWith("/config") ? Promise.resolve(new Response("{}",{status:503})) : next(path,options);
  $("device-config-form").dispatchEvent(new window.Event("submit", {cancelable:true}));
  await until(() => $("config-status").textContent.includes("保存失败"), "failed save preserved input");
  assert.equal($("moving-minutes").value,"3");
  intercept=null;
  const configGate=defer(); let configReady=false;
  intercept=async(path,options,next)=>{
    const response=await next(path,options);
    if(options?.method==="PUT" && path.endsWith("/config")){configReady=true;await configGate.promise;}
    return response;
  };
  $("moving-minutes").value="4";
  $("device-config-form").dispatchEvent(new window.Event("submit",{cancelable:true}));
  await until(()=>configReady,"save response held before offline");
  window.dispatchEvent(new window.Event("offline"));
  assert.equal($("moving-minutes").value,"4","offline event erased unsaved input");
  assert.equal($("settings-view").hidden,false);
  configGate.resolve(); await settle(); intercept=null;
  window.dispatchEvent(new window.Event("online"));
  await until(()=>!$("refresh").disabled,"online config draft retained");
  assert.equal($("moving-minutes").value,"4");
  assert.equal($("settings-view").hidden,false);
  click("back-devices");
  $("devices-list").querySelector(".remove-member").click();
  await until(() => $("devices-list").children.length===0 && !$("refresh").disabled, "removed only from second family");
  assert.equal((await (await realRequest(`/api/v1/locations?family=${firstFamily}`)).json()).locations.length,1);
  const selectFamily = (id) => { $("family-select").value=id; $("family-select").dispatchEvent(new window.Event("change")); };
  const oldFamilyGate=defer(); let oldFamilyReady=false;
  intercept=async(path,options,next) => {
    const response=await next(path,options);
    if(path===`/api/v1/locations?family=${firstFamily}`) {oldFamilyReady=true;await oldFamilyGate.promise;}
    return response;
  };
  selectFamily(firstFamily);
  await until(()=>oldFamilyReady,"old family response held");
  selectFamily(secondFamily);
  await until(()=>!$("refresh").disabled,"new family response");
  oldFamilyGate.resolve(); await settle(); intercept=null;
  assert.equal(doc.querySelectorAll(".person-card").length,0,"late response leaked prior family");
  let configLoads=0;
  intercept=(path,options,next)=>{if(path==="/api/v1/web-config")configLoads++;return next(path,options);};
  window.history.replaceState(null,"",`${base}/?family=missing`);
  window.dispatchEvent(new window.Event("popstate"));
  await until(()=>$("app-message").textContent.includes("无权访问"),"invalid family rejected");
  assert.equal(doc.querySelectorAll(".person-card").length,0);
  assert.equal($("generate-pairing").disabled,true);
  selectFamily(firstFamily);
  await until(()=>!$("refresh").disabled && doc.querySelectorAll(".person-card").length===1,"return to default family");
  await until(()=>configLoads>0,"map config restored after invalid family link");
  intercept=null;
  const mapGate=defer();let heldMapReady=false,mapRequests=0;
  intercept=async(path,options,next)=>{
    const response=await next(path,options);
    if(path==="/api/v1/web-config") {
      mapRequests++;
      if(mapRequests===1){heldMapReady=true;await mapGate.promise;}
    }
    return response;
  };
  window.dispatchEvent(new window.Event("popstate"));
  await until(()=>heldMapReady,"map configuration response held");
  selectFamily(secondFamily);
  await until(()=>mapRequests>=2 && !$("refresh").disabled,"map configuration retried after family switch");
  mapGate.resolve();await settle();intercept=null;
  assert.equal(doc.querySelectorAll(".person-card").length,0);
  selectFamily(firstFamily);
  await until(()=>!$("refresh").disabled && doc.querySelectorAll(".person-card").length===1,"restore family after map race");
  const familiesGate=defer();let familiesReady=false,familyRequests=0;
  intercept=async(path,options,next)=>{
    const response=await next(path,options);
    if(path==="/api/v1/families") {familyRequests++;if(familyRequests===1){familiesReady=true;await familiesGate.promise;}}
    return response;
  };
  window.dispatchEvent(new window.Event("popstate"));
  await until(()=>familiesReady,"family initialization held");
  window.dispatchEvent(new window.Event("offline"));
  familiesGate.resolve();await settle();
  window.dispatchEvent(new window.Event("online"));
  await until(()=>familyRequests>=2 && !$("refresh").disabled && doc.querySelectorAll(".person-card").length===1,"family initialization resumes online");
  intercept=null;
  let initializationAttempts=0;
  intercept=(path,options,next)=>{
    if(path==="/api/v1/families" && ++initializationAttempts===1) return Promise.resolve(new Response("{}",{status:503}));
    return next(path,options);
  };
  window.dispatchEvent(new window.Event("popstate"));
  await until(()=>initializationAttempts===1 && !$("refresh").disabled && $("app-message").textContent.includes("暂时无法连接"),"family initialization temporary failure");
  assert.equal(doc.querySelectorAll(".person-card").length,0);
  click("refresh");
  await until(()=>initializationAttempts>=2 && !$("refresh").disabled && doc.querySelectorAll(".person-card").length===1,"manual refresh retries family initialization");
  intercept=null;
  console.log("PASS: family creation/link, membership reuse/removal, device settings including pause window, failed save, invalid link and late response isolation");
  // A lifecycle transition during failed logout must leave an actionable retry.
  const logoutGate = defer();
  let logoutReady = false;
  intercept = async (path, options, next) => {
    if (path === "/api/v1/auth/logout") {
      logoutReady = true;
      await logoutGate.promise;
      return new Response("{}", { status: 503 });
    }
    return next(path, options);
  };
  click("logout");
  await until(() => logoutReady, "held logout");
  assert.equal(doc.querySelectorAll(".person-card").length, 0);
  window.dispatchEvent(new window.Event("pagehide"));
  const show = new window.Event("pageshow");
  Object.defineProperty(show, "persisted", { value: true });
  window.dispatchEvent(show);
  logoutGate.resolve();
  await settle();
  intercept = null;
  assert.equal(
    $("logout").disabled,
    false,
    "failed logout after resume cannot be retried",
  );
  click("logout");
  await until(() => !$("login-view").hidden, "logout retry");
  assert.equal($("family-list").textContent, "");
  assert.equal($("devices-list").textContent, "");
  assert.equal($("pairing-text").value, "");
  assert.equal((await realRequest("/api/v1/auth/me")).status, 401);
  assert.deepEqual(errors, []);
  console.log(
    "PASS: logout lifecycle race, retry, server session revocation, sensitive DOM cleared",
  );
} finally {
  window.close();
}
