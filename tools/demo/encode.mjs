import { spawn } from "node:child_process";
import { stat } from "node:fs/promises";
import { join } from "node:path";

const megabyte = 1_000_000;
const colorTags = ["-colorspace", "bt709", "-color_primaries", "bt709", "-color_trc", "bt709", "-color_range", "tv"];

function ffmpeg(args) {
  return new Promise((resolve, reject) => {
    const child = spawn("ffmpeg", ["-hide_banner", "-loglevel", "error", "-y", ...args], { stdio: "inherit" });
    child.on("error", reject);
    child.on("exit", (code) => (code === 0 ? resolve() : reject(new Error(`ffmpeg exited with ${code}`))));
  });
}

function sources({ framesDirectory, fps, tail }) {
  const first = join(framesDirectory, "00000.png");
  const seconds = (tail.blackMs + tail.riseMs) / 1000 + 1 / fps;
  const fade = `fade=t=in:st=${tail.blackMs / 1000}:d=${tail.riseMs / 1000}`;
  return {
    inputs: [
      "-framerate", String(fps), "-i", join(framesDirectory, "%05d.png"),
      "-loop", "1", "-framerate", String(fps), "-t", String(seconds), "-i", first,
    ],
    filter: [
      "-filter_complex",
      `[1:v]${fade}[tail];[0:v][tail]concat=n=2:v=1:a=0,scale=out_color_matrix=bt709:out_range=tv,format=yuv420p[out]`,
      "-map", "[out]",
    ],
  };
}

async function sizeOf(path) {
  return (await stat(path)).size;
}

async function fitBudget({ label, path, budget, qualities, encodeAt }) {
  for (const quality of qualities) {
    await encodeAt(quality);
    const size = await sizeOf(path);
    console.log(`${label} crf ${quality}: ${(size / megabyte).toFixed(2)} MB`);
    if (size <= budget) return { quality, size };
  }
  throw new Error(`${label} stays above ${budget / megabyte} MB`);
}

export async function encode({ framesDirectory, fps, tail, mp4, webm, poster }) {
  const { inputs, filter } = sources({ framesDirectory, fps, tail });
  const results = {};
  results.mp4 = await fitBudget({
    label: "mp4",
    path: mp4,
    budget: 4 * megabyte,
    qualities: [18, 20, 22, 24, 26, 28, 30],
    encodeAt: (crf) =>
      ffmpeg([
        ...inputs, ...filter,
        "-c:v", "libx264", "-profile:v", "high", "-preset", "slow", "-tune", "animation",
        "-crf", String(crf), "-pix_fmt", "yuv420p", ...colorTags, "-movflags", "+faststart", "-an", mp4,
      ]),
  });
  results.webm = await fitBudget({
    label: "webm",
    path: webm,
    budget: 3 * megabyte,
    qualities: [30, 32, 34, 35, 36, 38, 40, 42],
    encodeAt: (crf) =>
      ffmpeg([
        ...inputs, ...filter,
        "-c:v", "libvpx-vp9", "-b:v", "0", "-crf", String(crf), "-row-mt", "1", "-deadline", "good",
        "-cpu-used", "2", "-pix_fmt", "yuv420p", ...colorTags, "-an", webm,
      ]),
  });
  await ffmpeg(["-i", join(framesDirectory, "00000.png"), "-c:v", "libwebp", "-quality", "80", "-compression_level", "6", poster]);
  results.poster = { size: await sizeOf(poster) };
  return results;
}
