import { createRequire } from "node:module";
import { readdir } from "node:fs/promises";
import { join } from "node:path";

const require = createRequire(new URL("../../web/package.json", import.meta.url));
const sharp = require("sharp");

const movingPx = 0.6;
const thumb = { width: 240, height: 135 };

function cameraSpeed(previous, current) {
  const pan = Math.hypot(current.camera.x - previous.camera.x, current.camera.y - previous.camera.y) * current.camera.zoom;
  const zoom = Math.abs(Math.log(current.camera.zoom / previous.camera.zoom)) * 960;
  return { x: (current.camera.x - previous.camera.x) * current.camera.zoom, size: pan + zoom };
}

function hitches(velocities) {
  const found = [];
  for (let index = 1; index < velocities.length - 1; index += 1) {
    const [before, here, after] = [velocities[index - 1], velocities[index], velocities[index + 1]];
    const isMoving = Math.abs(before) > movingPx && Math.abs(after) > movingPx;
    const stalls = Math.abs(here) < 0.25 * Math.min(Math.abs(before), Math.abs(after));
    const flips = Math.sign(before) !== Math.sign(here) && Math.sign(here) !== Math.sign(after) && Math.abs(here) > movingPx;
    if (isMoving && (stalls || flips)) found.push(index);
  }
  return found;
}

function series(motion, pick) {
  return motion.slice(1).map((current, index) => pick(motion[index], current));
}

async function imageDiffs(framesDirectory) {
  const names = (await readdir(framesDirectory)).filter((name) => name.endsWith(".png")).sort();
  const diffs = [];
  let previous;
  for (const name of names) {
    const pixels = await sharp(join(framesDirectory, name)).resize(thumb).greyscale().raw().toBuffer();
    if (previous) {
      let total = 0;
      for (let index = 0; index < pixels.length; index += 1) total += Math.abs(pixels[index] - previous[index]);
      diffs.push(total / pixels.length);
    }
    previous = pixels;
  }
  return diffs;
}

function median(values) {
  const sorted = [...values].sort((a, b) => a - b);
  return sorted[Math.floor(sorted.length / 2)] ?? 0;
}

function imageFindings(diffs) {
  const frozen = [];
  const jumps = [];
  for (let index = 1; index < diffs.length - 1; index += 1) {
    if (diffs[index] < 0.02 && diffs[index - 1] > 0.4 && diffs[index + 1] > 0.4) frozen.push(index + 1);
    const around = median(diffs.slice(Math.max(0, index - 6), index + 7));
    if (diffs[index] > 2.5 && diffs[index] > 5 * around) jumps.push(index + 1);
  }
  return { frozen, jumps };
}

export async function checkMotion({ motion, framesDirectory, every }) {
  const camera = series(motion, (previous, current) => cameraSpeed(previous, current).x);
  const cameraSize = series(motion, (previous, current) => cameraSpeed(previous, current).size);
  const cursorX = series(motion, (previous, current) => current.cursor.x - previous.cursor.x);
  const cursorY = series(motion, (previous, current) => current.cursor.y - previous.cursor.y);
  const phone = series(motion, (previous, current) => current.phone - previous.phone);
  const motionHitches = {
    camera: hitches(camera),
    cursorX: hitches(cursorX),
    cursorY: hitches(cursorY),
    phone: hitches(phone),
  };
  const diffs = every === 1 ? await imageDiffs(framesDirectory) : [];
  const images = imageFindings(diffs);
  const peak = (values) => Math.max(...values.map(Math.abs)).toFixed(1);
  const summary = [
    `motion: ${motion.length} frames; peak camera ${peak(cameraSize)} px/frame, cursor ${peak(cursorX)}/${peak(cursorY)} px/frame`,
    `motion hitches: ${JSON.stringify(motionHitches)}`,
    `image frozen-in-motion frames: ${JSON.stringify(images.frozen)}`,
    `image jump frames: ${JSON.stringify(images.jumps)}`,
  ].join("\n");
  return { summary, diffs, motionHitches, images };
}
