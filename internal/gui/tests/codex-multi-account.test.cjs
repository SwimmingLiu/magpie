// The gateway and native choices have different values and must both stay
// visible. Picking the gateway choice edits the main model, not subagents.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const native = { value: "gpt-6.1-sol", label: "GPT-6.1-Sol", group: "CUSTOM", direct: "custom", icon: "openai" };
const gateway = { value: "codex/gpt-6.1-sol", label: "GPT-6.1-Sol", group: "Codex", ref: "codex/gpt-6.1-sol", note: "Codex · via magpie", icon: "openai" };
const row = '.row.agent[data-id="codex"]';

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of ["en", "zh", "zh-TW", "ja", "de"]) {
    test(`${engine} ${lang}: Codex keeps both paths and picks the gateway main model`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await browser.newPage({ viewport: { width: 1100, height: 650 }, reducedMotion: "reduce" });
      page.setDefaultTimeout(5000);
      const errors = [], posts = [];
      let value = native.value, wired = false;
      const state = () => ({ agents: [{ id: "codex", name: "Codex", icon: "codex-color", path: "/fixture/config.toml", wired,
        fields: [{ key: "model", label: "model", value, options: [native, gateway] },
          { key: "subagent_model", label: "subagent model", value: "", options: [gateway] }] }],
        profiles: [], settings: { lang, theme: "light" } });
      page.on("pageerror", (e) => errors.push(e.message));
      await page.route("**/*", async (route) => {
        const req = route.request(), url = new URL(req.url());
        const json = (data) => route.fulfill({ json: data });
        if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
        if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
        if (url.pathname === "/api/state") return json(state());
        if (url.pathname === "/api/set") {
          posts.push(req.postDataJSON());
          value = posts.at(-1).value;
          wired = true;
          return json(state());
        }
        if (url.pathname === "/api/usage/quotas") return json([]);
        if (url.pathname === "/api/groups") return json({ groups: [] });
        if (url.pathname === "/api/plugins") return json({ plugins: [] });
        if (url.pathname === "/api/agents/cli") return json({ agents: {}, pending: false });
        if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true } });
        if (url.pathname.startsWith("/api/")) return json({});
        const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
        const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
        await route.fulfill({ body: await fs.readFile(file), contentType });
      });
      await page.goto("http://magpie.test/?view=agents");
      const start = page.locator(`${row} > .field.ag-start`);
      await start.waitFor();
      assert.equal(await start.getAttribute("data-key"), "model");
      const top = await page.evaluate(() => [scrollY, document.querySelector("#view-agents").scrollTop]);
      await start.click();
      const choices = page.locator("#pop #list li", { hasText: "GPT-6.1-Sol" });
      await choices.first().waitFor();
      assert.equal(await choices.count(), 2, "native and gateway choices must not collapse");
      assert.equal(await choices.locator(".badge.path.direct").count(), 1, "native choice says direct");
      assert.equal(await choices.locator(".badge.path.via").count(), 1, "gateway choice says via magpie");
      assert.deepEqual(posts, [], "opening the picker writes nothing");
      if (process.env.ARTIFACT_DIR) {
        await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
        await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-two-paths.png`) });
      }
      await choices.last().click();
      await page.waitForFunction((r) => document.querySelector(r + " .ag-conn")?.getAttribute("aria-checked") === "true", row);
      assert.deepEqual(posts, [{ agent: "codex", field: "model", value: gateway.value }]);
      assert.deepEqual(await page.evaluate(() => [scrollY, document.querySelector("#view-agents").scrollTop]), top);
      assert.deepEqual(errors, []);
      if (process.env.ARTIFACT_DIR) await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-selected.png`) });
    });
  }
}
