import { spawn } from "node:child_process";
import { createRequire } from "node:module";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";
import { createFerry } from "./api.mjs";
import { checkMotion } from "./check.mjs";
import { Director } from "./director.mjs";
import { encode } from "./encode.mjs";
import { compareWithApp } from "./verify.mjs";
import { canvas, layoutFor, measureBezel, placeholderDevice } from "./layout.mjs";
import { compareAtMs, cues, durationMs, pickedFiles, prerollMs, script, tail } from "./timeline.mjs";

const here = fileURLToPath(new URL(".", import.meta.url));
const root = resolve(here, "../..");
const require = createRequire(join(root, "web/package.json"));
const { chromium } = require("@playwright/test");

const { values: options } = parseArgs({
  options: {
    bezel: { type: "string" },
    keep: { type: "boolean", default: false },
    until: { type: "string" },
    every: { type: "string", default: "1" },
    "no-encode": { type: "boolean", default: false },
    compare: { type: "string" },
  },
});

function run(command, args) {
  return new Promise((done, fail) => {
    const child = spawn(command, args, { cwd: root, stdio: "inherit", shell: process.platform === "win32" });
    child.on("exit", (code) => (code === 0 ? done() : fail(new Error(`${command} exited with ${code}`))));
  });
}

const frameMs = 1000 / canvas.fps;
const work = await mkdtemp(join(tmpdir(), "ferry-demo-"));
const appDirectory = join(work, "app");
const framesDirectory = join(work, "frames");
await run("pnpm", ["-C", "web", "exec", "vite", "build", "--outDir", `"${appDirectory}"`, "--emptyOutDir", "--logLevel", "warn"]);

const device = options.bezel ? await measureBezel(resolve(options.bezel)) : placeholderDevice;
const layout = layoutFor(device);
const { serve } = await import("./server.mjs");
const server = await serve({
  appDirectory,
  stageDirectory: here,
  logoDirectory: join(root, "assets/logo"),
  bezelPath: device.image,
  layout,
});

const pending = [];
let virtualNow = 0;
const api = createFerry({
  origin: server.origin,
  script,
  now: () => virtualNow,
  emit: (role, kind, payload, at) => {
    pending.push({ role, kind, payload, at });
    pending.sort((a, b) => a.at - b.at);
  },
});

const browser = await chromium.launch({ args: ["--force-color-profile=srgb", "--hide-scrollbars"] });
const context = await browser.newContext({
  viewport: { width: canvas.width, height: canvas.height },
  deviceScaleFactor: 1,
  colorScheme: "dark",
});
await context.addInitScript({ path: join(here, "clock.js") });
await context.addInitScript({ path: join(here, "mocks.js") });
const roleOf = (frame) => frame.name() || new URL(frame.url()).searchParams.get("role");
await context.exposeBinding("__ferryApi", (source, request) => api.handle(roleOf(source.frame), request));
const page = await context.newPage();
page.on("pageerror", (error) => console.error("page error:", error.message));
page.on("filechooser", (chooser) => void chooser.setFiles(pickedFiles));
await page.goto(`${server.origin}/stage/stage.html?phone=${encodeURIComponent(api.pairQuery)}`);
await page.evaluate(() => globalThis.__stage.ready);
const frames = { stage: page.mainFrame(), pc: page.frame({ name: "pc" }), phone: page.frame({ name: "phone" }) };
const { top, bottom } = device.insets;
await frames.phone.addStyleTag({
  content: `@font-face{font-family:"Inter Demo";font-weight:100 900;src:url(/stage/InterVariable.woff2) format("woff2")}html:root{--font:"Inter Demo",sans-serif;--safe-top:${top}px;--safe-bottom:${bottom}px}`,
});
const apps = [frames.pc, frames.phone];

async function deliver() {
  while (pending.length > 0 && pending[0].at <= virtualNow) {
    const { role, kind, payload } = pending.shift();
    await frames[role].evaluate(([name, data]) => globalThis.__mock.emit(name, data), [kind, payload]);
  }
}

async function advanceTo(virtualMs) {
  virtualNow = virtualMs;
  await deliver();
  await frames.stage.evaluate((t) => globalThis.__clock.runUntil(t), virtualMs);
  for (let round = 0; round < 60; round += 1) {
    const ran = await Promise.all(apps.map((frame) => frame.evaluate((t) => globalThis.__clock.runUntil(t), virtualMs)));
    if (round === 0) await Promise.all(apps.map((frame) => frame.evaluate(() => globalThis.__clock.frame())));
    await frames.stage.evaluate(() => globalThis.__clock.paint());
    const synced = await Promise.all(
      apps.map((frame) => frame.evaluate(() => ({ ...globalThis.__clock.sync(), pending: globalThis.__mock.pending() }))),
    );
    const busy = ran.some((count) => count > 0) || synced.some((state) => state.finished > 0 || state.pending > 0);
    if (round > 0 && !busy) break;
  }
}

const director = new Director({ page, frames, layout });
await frames.stage.evaluate((state) => globalThis.__stage.apply(state), director.state());
for (let virtualMs = 0; virtualMs < prerollMs; virtualMs += frameMs) await advanceTo(virtualMs);

const client = await context.newCDPSession(page);
const until = options.until ? Number(options.until) : durationMs;
const every = Number(options.every);
const queue = cues(director, api);
const motion = [];
const frameCount = Math.round((Math.min(until, durationMs) / 1000) * canvas.fps);
await rm(framesDirectory, { recursive: true, force: true });
await mkdir(framesDirectory, { recursive: true });
const started = Date.now();

for (let index = 0; index < frameCount; index += 1) {
  const time = index * frameMs;
  while (queue.length > 0 && queue[0][0] <= time) {
    director.time = time;
    await queue.shift()[1]();
  }
  director.advance(time);
  const state = director.state();
  await frames.stage.evaluate((next) => globalThis.__stage.apply(next), state);
  await director.syncMouse();
  await advanceTo(time + prerollMs);
  motion.push({ camera: state.camera, cursor: { x: state.cursor.x, y: state.cursor.y }, phone: state.phone.offset });
  if (index % every === 0) {
    const shot = await client.send("Page.captureScreenshot", { format: "png" });
    await writeFile(join(framesDirectory, `${String(index / every).padStart(5, "0")}.png`), Buffer.from(shot.data, "base64"));
  }
  if (index % 120 === 0) console.log(`frame ${index}/${frameCount} (${((Date.now() - started) / 1000).toFixed(0)} s)`);
}

const errors = (await Promise.all(apps.map((frame) => frame.evaluate(() => globalThis.__clock.errors())))).flat();
if (errors.length > 0) console.error(`app errors:\n${errors.join("\n")}`);
if (options.compare && every === 1) {
  const index = Math.min(frameCount, Math.round((compareAtMs / 1000) * canvas.fps)) - 1;
  const result = await compareWithApp({
    context,
    origin: server.origin,
    layout,
    camera: motion[index].camera,
    framePath: join(framesDirectory, `${String(index).padStart(5, "0")}.png`),
    outDirectory: resolve(options.compare),
  });
  console.log(`side by side: ${result.sideBySide} (mean grey difference ${result.meanDifference.toFixed(2)} of 255)`);
}
await browser.close();
server.close();

const report = await checkMotion({ motion, framesDirectory, every });
console.log(report.summary);

if (!options["no-encode"] && every === 1 && frameCount === Math.round((durationMs / 1000) * canvas.fps)) {
  const site = join(root, "site");
  const sizes = await encode({
    framesDirectory,
    fps: canvas.fps,
    tail,
    mp4: join(site, "demo.mp4"),
    webm: join(site, "demo.webm"),
    poster: join(site, "demo-poster.webp"),
  });
  console.log(JSON.stringify(sizes));
}

if (options.keep) console.log(`frames kept in ${framesDirectory}`);
else await rm(work, { recursive: true, force: true });
