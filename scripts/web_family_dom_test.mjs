// Entry isolation and lifecycle tests; DOM only, not a Safari rendering test.
import {
  JSDOM,
  VirtualConsole,
} from "../.cache/web-test/node_modules/jsdom/lib/api.js";
import { SourceTextModule } from "node:vm";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import assert from "node:assert/strict";
const root = new URL("../backend/internal/webui/assets/", import.meta.url);
const json = (data, status = 200) =>
  new Response(JSON.stringify(data), {
    status,
    headers: { "Content-Type": "application/json" },
  });
const pause = () => new Promise((r) => setTimeout(r, 30));
async function until(check, label) {
  for (let i = 0; i < 150; i++) {
    if (check()) return;
    await pause();
  }
  throw new Error(`timeout: ${label}`);
}
async function page(name, url, fetch) {
  const errors = [];
  const vc = new VirtualConsole();
  vc.on("jsdomError", (e) => errors.push(e.message));
  const dom = new JSDOM(
    await readFile(
      new URL(`${name === "app" ? "index" : "family"}.html`, root),
      "utf8",
    ),
    {
      url,
      runScripts: "outside-only",
      pretendToBeVisual: true,
      virtualConsole: vc,
    },
  );
  const w = dom.window;
  w.AbortController = globalThis.AbortController;
  w.fetch = fetch;
  const modules = new Map();
  async function load(url) {
    if (modules.has(url.href)) return modules.get(url.href);
    const mod = new SourceTextModule(await readFile(url, "utf8"), {
      context: dom.getInternalVMContext(),
      identifier: url.href,
    });
    modules.set(url.href, mod);
    await mod.link((s, r) => load(new URL(s, r.identifier)));
    return mod;
  }
  await (await load(new URL(`${name}.mjs`, root))).evaluate();
  const $ = (id) => w.document.getElementById(id);
  return {
    w,
    $,
    errors,
    close: () => w.close(),
    submit: (id) =>
      $(id).dispatchEvent(new w.Event("submit", { cancelable: true })),
    select: (id) => {
      $("family-select").value = id;
      $("family-select").dispatchEvent(new w.Event("change"));
    },
  };
}
const stamp = () => new Date().toISOString();
const point = {
  device_id: "device",
  name: "家人",
  latitude: 28.1,
  longitude: 112.9,
  accuracy_m: 10,
  coordinate_system: "GCJ-02",
  battery_percent: 80,
  captured_at: new Date(Date.now() - 600000).toISOString(),
  received_at: stamp(),
  lease_expires_at: new Date(Date.now() + 600000).toISOString(),
};
async function adminTests() {
  let families = [
      { id: "1", public_id: "family1", name: "旧家庭", passcode_set: false },
    ],
    calls = [],
    passcodeGate = null;
  const fetch = async (path, options = {}) => {
    calls.push([path, options]);
    const method = options.method || "GET";
    if (path === "/api/v1/auth/me")
      return json({
        user: { username: "管理员", family_id: "1" },
        csrf_token: "csrf",
      });
    if (path === "/api/v1/families" && method === "GET")
      return json({ families });
    if (path === "/api/v1/families" && method === "POST") {
      const f = {
        id: "2",
        public_id: "family2",
        name: JSON.parse(options.body).name,
        passcode_set: true,
      };
      families.push(f);
      return json({ ...f, passcode: "0123456" });
    }
    if (path === "/api/v1/families/family2" && method === "PATCH") {
      families[1].name = JSON.parse(options.body).name;
      return json(families[1]);
    }
    if (path.endsWith("/passcode")) {
      if (passcodeGate) await passcodeGate;
      return json({ passcode: JSON.parse(options.body).passcode || "0012345" });
    }
    if (path.startsWith("/api/v1/families/") && method === "DELETE") {
      families = families.filter((f) => !path.endsWith(f.public_id));
      return new Response(null, { status: 204 });
    }
    if (path.startsWith("/api/v1/locations"))
      return json({
        server_time: stamp(),
        locations: path.includes("scope=owned") ? [point] : [],
      });
    if (path.startsWith("/api/v1/devices"))
      return json({
        devices: path.includes("scope=owned")
          ? [
              { id: "device", name: "无家庭设备", families: [] },
              {
                id: "revoked",
                name: "旧设备",
                revoked_at: stamp(),
                families: [],
              },
            ]
          : [],
      });
    if (path === "/api/v1/web-config")
      return json({ map: { enabled: false }, poll_seconds: 60 });
    throw new Error(`unexpected ${method} ${path}`);
  };
  const p = await page("app", "http://localhost/admin", fetch);
  const { $, submit, select } = p;
  try {
    await until(
      () => $("family-select").options.length > 0 && !$("refresh").disabled,
      "admin initial",
    );
    assert.equal(
      $("family-select").selectedOptions[0].textContent,
      "所有设备",
      "admin must default to all owned devices",
    );
    assert(calls.some(([url]) => url === "/api/v1/locations?scope=owned"));
    assert.equal(
      $("family-list").children.length,
      1,
      "orphan location is visible",
    );
    assert.equal($("generate-pairing").disabled, true);
    assert.equal(p.w.document.querySelectorAll(".remove-member").length, 0);
    assert.equal(p.w.document.querySelectorAll(".purge-device").length, 1);
    select("family1");
    await until(() => !$("refresh").disabled, "old family");
    assert.match($("family-passcode-status").textContent, /待设置/);
    assert.equal($("copy-family").disabled, true);
    $("regenerate-passcode").click();
    await until(() => !$("passcode-result").hidden, "set old family passcode");
    assert.equal($("current-passcode").value, "0012345");
    assert.equal($("copy-family").disabled, false);
    select("");
    assert.equal(
      $("current-passcode").value,
      "",
      "selection clears once-only code",
    );
    await until(() => !$("refresh").disabled, "all devices");
    $("family-name").value = "新家";
    submit("create-family-form");
    await until(
      () => $("family-select").value === "family2" && !$("refresh").disabled,
      "created family",
    );
    assert.equal($("current-passcode").value, "0123456");
    assert.equal($("family-link").value, "http://localhost/family?key=family2");
    $("rename-family-name").value = "新名字";
    submit("rename-family-form");
    await until(
      () => $("family-select").selectedOptions[0].textContent === "新名字",
      "rename",
    );
    $("family-passcode").value = "0000001";
    submit("family-passcode-form");
    await until(
      () => $("current-passcode").value === "0000001",
      "manual leading-zero code",
    );
    let releasePasscode;
    passcodeGate = new Promise((resolve) => (releasePasscode = resolve));
    const writesBefore = calls.filter(([path]) =>
      path.endsWith("/passcode"),
    ).length;
    $("family-passcode").value = "0000002";
    submit("family-passcode-form");
    $("regenerate-passcode").click();
    await pause();
    assert.equal(
      calls.filter(([path]) => path.endsWith("/passcode")).length,
      writesBefore + 1,
      "manual/random passcode writes must be mutually exclusive",
    );
    releasePasscode();
    passcodeGate = null;
    await until(
      () => $("current-passcode").value === "0000002",
      "passcode write resolves once",
    );
    $("delete-family").click();
    assert.equal($("family-confirm-overlay").hidden, false);
    $("cancel-delete-family").click();
    assert.equal(families.length, 2);
    $("delete-family").click();
    $("confirm-delete-family").click();
    await until(
      () => $("family-select").value === "" && !$("refresh").disabled,
      "delete selects all",
    );
    select("family1");
    await until(() => !$("refresh").disabled, "last family");
    $("delete-family").click();
    $("confirm-delete-family").click();
    await until(
      () => $("family-select").options.length === 1 && !$("refresh").disabled,
      "last family deleted",
    );
    assert.equal($("family-list").children.length, 1);
    assert.deepEqual(p.errors, []);
    console.log(
      "PASS: admin all-devices/orphan location, old-family passcode setup, once-only codes, fixed link, rename and delete-last-family",
    );
  } finally {
    p.close();
  }
}
async function viewerTests() {
  let authorized = false,
    deleted = false,
    hold = null,
    calls = [],
    mapEnabled = false;
  const snapshot = () => ({
    family: { public_id: "family1", name: "只读家庭" },
    devices: [
      { id: "device", name: "家人" },
      { id: "stopped", name: "未共享成员" },
    ],
    locations: [point],
    server_time: stamp(),
    expires_at: new Date(Date.now() + 604800000).toISOString(),
  });
  const fetch = async (path, options = {}) => {
    calls.push(path);
    if (path.endsWith("/session") && options.method === "POST") {
      assert.equal(JSON.parse(options.body).passcode, "0123456");
      authorized = true;
      return json({ expires_at: snapshot().expires_at });
    }
    if (path.endsWith("/session") && options.method === "DELETE") {
      authorized = false;
      return new Response(null, { status: 204 });
    }
    if (path.endsWith("/snapshot")) {
      const response = json(snapshot(), deleted ? 404 : authorized ? 200 : 401);
      if (hold) await hold;
      return response;
    }
    if (path.endsWith("/web-config"))
      return json({
        map: {
          enabled: mapEnabled,
          key: "test",
          service_host: "/_AMapService",
        },
      });
    throw new Error(`non-viewer request ${path}`);
  };
  const p = await page("family", "http://localhost/family?key=family1", fetch);
  const { $, submit } = p;
  try {
    await until(() => !$("family-auth").hidden, "passcode gate");
    assert.equal($("family-list").textContent, "");
    assert.equal(
      p.w.document.querySelector('script[src="/assets/app.mjs"]'),
      null,
    );
    $("family-passcode").value = "0123456";
    submit("family-auth-form");
    await until(() => !$("family-view").hidden, "viewer authenticated");
    assert.match($("family-list").textContent, /精度 ±10 米/);
    assert.match($("family-list").textContent, /80%/);
    assert.match($("family-list").textContent, /更新延迟/);
    assert.match($("family-list").textContent, /未共享成员/);
    assert.match($("family-list").textContent, /暂无可查看的位置/);
    assert.equal($("family-passcode").value, "");
    assert.equal(p.w.localStorage.length + p.w.sessionStorage.length, 0);
    p.w.dispatchEvent(new p.w.Event("offline"));
    assert.match($("sync-status").textContent, /最后位置/);
    p.w.dispatchEvent(new p.w.Event("online"));
    await until(() => !$("refresh").disabled, "online");
    authorized = false;
    $("refresh").click();
    await until(() => !$("family-auth").hidden, "passcode changed");
    assert.equal($("family-list").textContent, "");
    assert.equal($("family-name").textContent, "");
    $("family-passcode").value = "0123456";
    submit("family-auth-form");
    await until(() => !$("family-view").hidden, "relogin");
    let release;
    hold = new Promise((r) => (release = r));
    $("refresh").click();
    await pause();
    p.w.dispatchEvent(new p.w.Event("pagehide"));
    release();
    hold = null;
    await pause();
    assert.equal(
      $("family-list").textContent,
      "",
      "late snapshot cannot restore background data",
    );
    const show = new p.w.Event("pageshow");
    Object.defineProperty(show, "persisted", { value: true });
    p.w.dispatchEvent(show);
    await until(() => !$("family-view").hidden, "resume");
    mapEnabled = true;
    p.w.dispatchEvent(new p.w.Event("pagehide"));
    p.w.dispatchEvent(show);
    await until(
      () =>
        p.w.document.querySelector('script[src^="https://webapi.amap.com/"]'),
      "map SDK script",
    );
    p.w.document
      .querySelector('script[src^="https://webapi.amap.com/"]')
      .dispatchEvent(new p.w.Event("error"));
    await until(
      () => $("map-title").textContent.includes("不可用"),
      "map loading failure",
    );
    const configsBefore = calls.filter((url) =>
      url.endsWith("/web-config"),
    ).length;
    $("refresh").click();
    await until(() => !$("refresh").disabled, "refresh after SDK error");
    assert(
      calls.filter((url) => url.endsWith("/web-config")).length > configsBefore,
      "refresh must retry failed map initialization",
    );
    deleted = true;
    $("refresh").click();
    await until(() => !$("family-invalid").hidden, "deleted family");
    assert.equal($("family-list").textContent, "");
    assert.deepEqual(p.errors, []);
    console.log(
      "PASS: independent read-only viewer, leading-zero passcode, original stale times, no-position members, offline/resume, revocation/deletion clears data and late response isolation",
    );
  } finally {
    p.close();
  }
  for (const query of [
    "",
    "?key=",
    "?key=family1&key=family2",
    "?key=bad%2Fkey",
  ]) {
    let count = 0;
    const invalid = await page(
      "family",
      `http://localhost/family${query}`,
      async () => {
        count++;
        return json({}, 401);
      },
    );
    try {
      assert.equal(invalid.$("family-invalid").hidden, false);
      assert.equal(count, 0, "invalid key must not request fallback family");
    } finally {
      invalid.close();
    }
  }
  console.log(
    "PASS: missing, empty, duplicate and malformed keys rejected without fallback",
  );
}
async function viewerExpiryTest() {
  const p = await page(
    "family",
    "http://localhost/family?key=family1",
    async (path) => {
      if (path.endsWith("/web-config"))
        return json({ map: { enabled: false } });
      const serverTime = Date.now();
      return json({
        family: { public_id: "family1", name: "短期授权测试" },
        devices: [{ id: "device", name: "家人" }],
        locations: [point],
        server_time: new Date(serverTime).toISOString(),
        expires_at: new Date(serverTime + 180).toISOString(),
      });
    },
  );
  try {
    await until(
      () => !p.$("family-view").hidden,
      "short-lived viewer snapshot",
    );
    p.w.dispatchEvent(new p.w.Event("offline"));
    assert.equal(p.$("family-list").children.length, 1);
    await until(
      () => !p.$("family-auth").hidden,
      "fixed expiry clears while offline",
    );
    assert.equal(p.$("family-list").textContent, "");
    assert.equal(p.$("family-name").textContent, "");
    assert.match(p.$("family-auth-error").textContent, /到期/);
    console.log(
      "PASS: fixed authorization deadline clears viewer data even while offline",
    );
  } finally {
    p.close();
  }
}
async function viewerBodyCancellationTest() {
  let bodySignal,
    bodyStarted = false;
  const p = await page(
    "family",
    "http://localhost/family?key=family1",
    async (path, options) => ({
      status: 200,
      ok: true,
      json: () =>
        new Promise((resolve, reject) => {
          bodySignal = options.signal;
          bodyStarted = true;
          bodySignal.addEventListener(
            "abort",
            () => reject(new Error("body cancelled")),
            { once: true },
          );
        }),
    }),
  );
  try {
    await until(() => bodyStarted, "snapshot headers arrived but body stalled");
    p.w.dispatchEvent(new p.w.Event("offline"));
    assert.equal(
      bodySignal.aborted,
      true,
      "offline must cancel body parsing after headers arrive",
    );
    await pause();
    assert.equal(p.$("family-list").textContent, "");
    console.log(
      "PASS: interrupted response body remains cancellable after headers arrive",
    );
  } finally {
    p.close();
  }
}
await adminTests();
await viewerTests();
await viewerExpiryTest();
await viewerBodyCancellationTest();

// Real HTTP adds server-side authorization and Cookie isolation to the DOM checks.
async function realViewerTests() {
  const { CookieJar } =
    await import("../.cache/web-test/node_modules/jsdom/lib/api.js");
  const base = process.env.WEB_TEST_URL,
    adminJar = new CookieJar(),
    viewerJar = new CookieJar();
  const client =
    (jar) =>
    async (path, options = {}) => {
      const url = new URL(path, base),
        headers = new Headers(options.headers);
      const cookie = jar.getCookieStringSync(url.href);
      if (cookie) headers.set("Cookie", cookie);
      if (options.method && options.method !== "GET")
        headers.set("Origin", base);
      const response = await fetch(url, { ...options, headers });
      for (const value of response.headers.getSetCookie())
        jar.setCookieSync(value, url.href);
      return response;
    };
  const admin = client(adminJar),
    viewer = client(viewerJar);
  const login = await admin("/api/v1/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      username: process.env.WEB_TEST_USERNAME,
      password: process.env.WEB_TEST_PASSWORD,
    }),
  });
  assert.equal(login.status, 200);
  const session = await login.json();
  const write = async (path, method, body) => {
    const response = await admin(path, {
      method,
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": session.csrf_token,
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    assert(response.ok, `${method} ${path} status ${response.status}`);
    return response.status === 204 ? null : response.json();
  };
  const first = await write("/api/v1/families", "POST", {
    name: "只读联调家庭A",
    passcode: "0123456",
  });
  const second = await write("/api/v1/families", "POST", {
    name: "只读联调家庭B",
    passcode: "1234567",
  });
  const owned = await (await admin("/api/v1/devices?scope=owned")).json();
  const device = owned.devices.find((d) => !d.revoked_at);
  assert(device);
  await write(
    `/api/v1/families/${first.public_id}/devices/${device.id}`,
    "PUT",
    {},
  );
  const p = await page(
    "family",
    `${base}/family?key=${first.public_id}`,
    viewer,
  );
  const { $, submit } = p;
  let p2;
  try {
    await until(() => !$("family-auth").hidden, "real viewer initially gated");
    assert.equal(
      (await viewer("/api/v1/auth/me")).status,
      401,
      "viewer must not need admin session",
    );
    $("family-passcode").value = "0123456";
    submit("family-auth-form");
    await until(() => !$("family-view").hidden, "real viewer passcode login");
    assert.equal($("family-name").textContent, first.name);
    assert.equal($("family-list").children.length, 1);
    const one = await (
      await viewer(`/api/v1/family/${first.public_id}/snapshot`)
    ).json();
    assert(one.expires_at);
    const again = await (
      await viewer(`/api/v1/family/${first.public_id}/snapshot`)
    ).json();
    assert.equal(
      again.expires_at,
      one.expires_at,
      "polling must not slide seven-day expiration",
    );
    assert.equal((await viewer("/api/v1/devices?scope=owned")).status, 401);
    assert.equal(
      (await viewer(`/api/v1/devices/${device.id}/config`)).status,
      401,
    );
    assert.equal((await viewer("/api/v1/families")).status, 401);
    assert.equal(
      (await viewer(`/api/v1/family/${second.public_id}/snapshot`)).status,
      401,
    );
    assert.equal(
      (await viewer(`/api/v1/family/${first.public_id}/web-config`)).status,
      200,
    );
    assert.equal(
      (
        await viewer(`/api/v1/family/${second.public_id}/session`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ passcode: "1234567" }),
        })
      ).status,
      200,
    );
    p2 = await page("family", `${base}/family?key=${second.public_id}`, viewer);
    await until(
      () => !p2.$("family-view").hidden,
      "second-family saved cookie",
    );
    $("family-logout").click();
    await until(() => !$("family-auth").hidden, "logout only first family");
    assert.equal(
      (await viewer(`/api/v1/family/${first.public_id}/snapshot`)).status,
      401,
    );
    assert.equal(
      (await viewer(`/api/v1/family/${second.public_id}/snapshot`)).status,
      200,
      "other family survives logout",
    );
    $("family-passcode").value = "0123456";
    submit("family-auth-form");
    await until(() => !$("family-view").hidden, "first reauthenticated");
    await write(`/api/v1/families/${first.public_id}/passcode`, "PUT", {
      passcode: "0000001",
    });
    $("refresh").click();
    await until(
      () => !$("family-auth").hidden,
      "real passcode change invalidates viewer",
    );
    assert.equal($("family-list").textContent, "");
    assert.equal(
      (await viewer(`/api/v1/family/${second.public_id}/snapshot`)).status,
      200,
    );
    $("family-passcode").value = "0000001";
    submit("family-auth-form");
    await until(
      () => !$("family-view").hidden,
      "new leading-zero passcode works",
    );
    await write(`/api/v1/families/${first.public_id}`, "DELETE");
    $("refresh").click();
    await until(
      () => !$("family-invalid").hidden,
      "deleted real family link invalid",
    );
    assert.equal($("family-name").textContent, "");
    assert.equal($("family-list").textContent, "");
    assert.equal(
      (await admin(`/api/v1/devices/${device.id}/config`)).status,
      200,
      "family deletion preserves managed device",
    );
    assert.deepEqual(p.errors, []);
    assert.deepEqual(p2.errors, []);
    console.log(
      "PASS: real backend independent viewer Cookie, fixed expiry, multi-family login/logout, management rejection, passcode revocation, deleted link and device preservation",
    );
  } finally {
    p.close();
    p2?.close();
    await write(`/api/v1/families/${second.public_id}`, "DELETE");
  }
}
if (process.env.WEB_TEST_URL) await realViewerTests();
