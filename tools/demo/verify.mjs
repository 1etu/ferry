import { createRequire } from "node:module";
import { join } from "node:path";
import { viewportPoint } from "./layout.mjs";

const require = createRequire(new URL("../../web/package.json", import.meta.url));
const sharp = require("sharp");

async function settle(page, fromMs, untilMs) {
  for (let time = fromMs; time <= untilMs; time += 50) {
    await page.evaluate((t) => globalThis.__clock.runUntil(t), time);
    await page.evaluate(() => globalThis.__clock.frame());
    await page.evaluate(() => globalThis.__clock.paint());
    await page.evaluate(() => globalThis.__clock.sync());
  }
}

export async function compareWithApp({ context, origin, layout, camera, framePath, outDirectory }) {
  const page = await context.newPage();
  await page.setViewportSize({ width: layout.window.width, height: layout.window.height });
  await page.goto(`${origin}/?role=pc`);
  await settle(page, 0, 1500);
  await page.getByRole("button", { name: "Activity" }).click();
  await page.getByRole("button", { name: "Send Files…" }).hover();
  await settle(page, 1550, 3500);
  const appShot = await page.screenshot({ type: "png" });
  await page.close();

  const topLeft = viewportPoint(camera, { x: layout.window.x, y: layout.window.y + layout.window.caption });
  const width = Math.round(layout.window.width * camera.zoom);
  const height = Math.round(layout.window.height * camera.zoom);
  const fromVideo = await sharp(framePath)
    .extract({ left: Math.round(topLeft.x), top: Math.round(topLeft.y), width, height })
    .resize(layout.window.width, layout.window.height, { kernel: "lanczos3" })
    .png()
    .toBuffer();
  const [a, b] = await Promise.all(
    [fromVideo, appShot].map((image) => sharp(image).removeAlpha().greyscale().raw().toBuffer()),
  );
  let total = 0;
  for (let index = 0; index < a.length; index += 1) total += Math.abs(a[index] - b[index]);
  const meanDifference = total / a.length;
  const sideBySide = join(outDirectory, "side-by-side.png");
  await sharp({
    create: { width: layout.window.width * 2 + 24, height: layout.window.height, channels: 3, background: "#000" },
  })
    .composite([
      { input: fromVideo, left: 0, top: 0 },
      { input: appShot, left: layout.window.width + 24, top: 0 },
    ])
    .png()
    .toFile(sideBySide);
  return { sideBySide, meanDifference };
}
