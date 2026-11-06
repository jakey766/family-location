import {
  chromium,
  webkit,
} from "../.cache/web-test/node_modules/playwright/index.mjs";
import assert from "node:assert/strict";
import { mkdir } from "node:fs/promises";
const base = process.env.WEB_TEST_URL;
const username = process.env.WEB_TEST_USERNAME;
const password = process.env.WEB_TEST_PASSWORD;
assert(base && username && password, "missing temporary test environment");
await mkdir(".cache/web-screenshots", { recursive: true });
for (const engine of process.env.WEB_TEST_BROWSER === "chromium"
  ? ["chromium"]
  : ["chromium", "webkit"]) {
  const browser = await (engine === "chromium"
    ? chromium.launch(
        process.env.WEB_TEST_CHROME_CHANNEL
          ? { channel: process.env.WEB_TEST_CHROME_CHANNEL }
          : {},
      )
    : webkit.launch());
  try {
    const context = await browser.newContext({
      viewport: { width: 390, height: 844 },
      deviceScaleFactor: 1,
      isMobile: true,
      hasTouch: true,
    });
    const page = await context.newPage();
    const failures = [];
    page.on("pageerror", (error) => failures.push(error.name));
    await page.goto(base + "/admin");
    await page.locator("#login-view").waitFor({ state: "visible" });
    await page.locator("#login-username").fill(username);
    await page.locator("#login-password").fill(password);
    await page.locator("#login-submit").click();
    await page.locator("#app-view").waitFor({ state: "visible" });
    await page.waitForFunction(
      () => document.querySelectorAll(".person-card").length === 2,
    );
    assert.match(await page.locator("#map-title").textContent(), /未配置/);
    assert.match(await page.locator("#family-list").textContent(), /最后位置/);
    assert.match(await page.locator("#family-list").textContent(), /电量未知/);
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
      true,
      "mobile overflow",
    );
    await page.screenshot({
      path: `.cache/web-screenshots/${engine}-locations.png`,
      fullPage: true,
    });
    await page.reload();
    await page.waitForFunction(
      () => document.querySelectorAll(".person-card").length === 2,
    );
    assert.equal(await page.locator("#generate-pairing").isDisabled(), true);
    const familyKey = await page
      .locator("#family-select option")
      .evaluateAll((options) => options.find((o) => o.value).value);
    await page.locator("#family-select").selectOption(familyKey);
    await page.waitForFunction(
      () => !document.querySelector("#refresh").disabled,
    );
    await page.locator("#tab-devices").click();
    await page.locator("#regenerate-passcode").click();
    await page.locator("#passcode-result").waitFor({ state: "visible" });
    const passcode = await page.locator("#current-passcode").inputValue();
    assert.match(passcode, /^[0-9]{7}$/);
    const familyURL = await page.locator("#family-link").inputValue();
    assert.equal(familyURL, `${base}/family?key=${familyKey}`);
    const viewerContext = await browser.newContext({
      viewport: { width: 390, height: 844 },
      isMobile: true,
      hasTouch: true,
    });
    const viewer = await viewerContext.newPage();
    viewer.on("pageerror", (error) => failures.push(error.name));
    await viewer.goto(familyURL);
    await viewer.locator("#family-auth").waitFor({ state: "visible" });
    await viewer.locator("#family-passcode").fill(passcode);
    await viewer.locator("#family-auth-submit").click();
    await viewer.locator("#family-view").waitFor({ state: "visible" });
    assert.equal(await viewer.locator(".person-card").count(), 2);
    assert.equal(await viewer.locator("#family-select").count(), 0);
    assert.equal(
      await viewer.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
      true,
      "viewer mobile overflow",
    );
    await viewer.screenshot({
      path: `.cache/web-screenshots/${engine}-family.png`,
      fullPage: true,
    });
    await viewer.reload();
    await viewer.locator("#family-view").waitFor({ state: "visible" });
    await viewer.locator("#family-logout").click();
    await viewer.locator("#family-auth").waitFor({ state: "visible" });
    assert.equal(await viewer.locator("#family-list").textContent(), "");
    await viewerContext.close();
    await page.locator("#generate-pairing").click();
    await page.locator("#pairing-result").waitFor({ state: "visible" });
    const code = await page.locator("#pairing-code").textContent();
    assert.match(code, /^\d{8}$/);
    const pairing = JSON.parse(
      await page.locator("#pairing-text").inputValue(),
    );
    assert(pairing.pairing_id && pairing.code === code);
    await page.locator("#tab-locations").click();
    assert.equal(await page.locator("#pairing-text").inputValue(), "");
    await page.locator("#tab-devices").click();
    await page.locator("#devices-list button").first().click();
    await page.locator("#confirm-overlay").waitFor({ state: "visible" });
    await page.locator("#cancel-revoke").click();
    assert.equal(await page.locator("#confirm-overlay").isVisible(), false);
    await page.locator("#tab-locations").click();
    await page.locator("#logout").click();
    await page.locator("#login-view").waitFor({ state: "visible" });
    assert.equal(await page.locator("#family-list").textContent(), "");
    assert.equal(
      await page.evaluate(() => localStorage.length + sessionStorage.length),
      0,
    );
    assert.deepEqual(failures, [], "browser errors");
    console.log(
      `PASS: ${engine} mobile login/session, list/stale, map fallback, pairing, confirmation, logout, independent read-only family viewer`,
    );
    await context.close();
  } finally {
    await browser.close();
  }
}
