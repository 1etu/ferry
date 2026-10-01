import { createServer } from "node:http";
import { createRequire } from "node:module";
import { readFile, writeFile } from "node:fs/promises";
import { extname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = fileURLToPath(new URL(".", import.meta.url));
const root = resolve(here, "../..");
const require = createRequire(join(root, "web/package.json"));
const { chromium } = require("@playwright/test");
const sharp = require("sharp");

const [threeDirectory, query = "", previewPath] = process.argv.slice(2);
if (!threeDirectory) {
  console.error("usage: node tools/coin/render.mjs <three package dir> [query] [preview.png]");
  process.exit(2);
}

const types = { ".html": "text/html", ".js": "text/javascript", ".svg": "image/svg+xml" };
const mounts = [
  ["/three/", resolve(threeDirectory)],
  ["/logo/", join(root, "assets/logo")],
  ["/", here],
];

const server = createServer(async (request, response) => {
  const path = decodeURIComponent(new URL(request.url, "http://localhost").pathname);
  const [prefix, directory] = mounts.find(([mount]) => path.startsWith(mount));
  try {
    const body = await readFile(join(directory, path.slice(prefix.length) || "coin.html"));
    response.writeHead(200, { "content-type": types[extname(path)] ?? "text/html" });
    response.end(body);
  } catch {
    response.writeHead(404);
    response.end();
  }
});
await new Promise((done) => server.listen(0, "127.0.0.1", done));
const origin = `http://127.0.0.1:${server.address().port}`;

const browser = await chromium.launch({ args: ["--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--ignore-gpu-blocklist"] });

async function renderCoin(size) {
  const page = await browser.newPage({ viewport: { width: size, height: size } });
  page.on("pageerror", (error) => console.error(error));
  await page.goto(`${origin}/coin.html?size=${size}&${query}`);
  await page.waitForFunction(() => window.coinReady === true, null, { timeout: 120000 });
  const url = await page.evaluate(() => document.querySelector("canvas").toDataURL("image/png"));
  await page.close();
  return Buffer.from(url.split(",")[1], "base64");
}

async function renderCard(coin) {
  const wordmark = (await readFile(join(root, "assets/logo/wordmark.svg"), "utf8")).replace(/viewBox="[^"]*"/, 'class="wordmark" viewBox="56 0 106 44"');
  const page = await browser.newPage({ viewport: { width: 1200, height: 630 } });
  await page.setContent(`<!doctype html><html><body style="margin:0;width:1200px;height:630px;background:#000;display:flex;align-items:center;justify-content:center;gap:56px;color:#f5f5f7">
    <img src="data:image/png;base64,${coin.toString("base64")}" width="400" height="400" alt="">
    <style>.wordmark{width:220px;height:auto}</style>${wordmark}
  </body></html>`);
  const card = await page.screenshot({ type: "png" });
  await page.close();
  return card;
}

const source = await renderCoin(1440);
if (previewPath) {
  await writeFile(previewPath, source);
} else {
  const site = join(root, "site");
  await sharp(source).resize(720, 720, { kernel: "lanczos3" }).webp({ quality: 80, alphaQuality: 90, effort: 6, smartSubsample: true }).toFile(join(site, "coin.webp"));
  await sharp(source).resize(720, 720, { kernel: "lanczos3" }).png({ compressionLevel: 9, palette: true, quality: 90 }).toFile(join(site, "coin.png"));
  await sharp(await renderCard(source)).jpeg({ quality: 86, mozjpeg: true }).toFile(join(site, "og.jpg"));
}

await browser.close();
server.close();
