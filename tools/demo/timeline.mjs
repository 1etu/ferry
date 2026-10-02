import { springs } from "./motion.mjs";

export const prerollMs = 1500;
export const fadeOutMs = 500;
export const tail = { blackMs: 150, riseMs: 600 };
export const durationMs = 32400;
export const compareAtMs = 31500;

const photo = (name, size, start, duration, pcDoneLagMs) => ({
  name,
  size,
  start: start + prerollMs,
  duration,
  pcDoneLagMs,
});

export const script = {
  pairAt: 4300 + prerollMs,
  approveLagMs: 160,
  uploads: [
    photo("IMG_2041.HEIC", 3_214_806, 12400, 3600, 2400),
    photo("IMG_2042.HEIC", 2_786_112, 12550, 3800, 2400),
    photo("IMG_2045.HEIC", 4_152_390, 12700, 4000, 2400),
  ],
  offered: { name: "Itinerary.pdf", size: 1_843_200, pickMs: 900, phoneLagMs: 2400, duration: 1100 },
};

export const pickedFiles = script.uploads.map(({ name, size }) => ({
  name,
  mimeType: "image/heic",
  buffer: Buffer.alloc(size, name.charCodeAt(7)),
}));

export function cues(director, api) {
  const d = director;
  return [
    [0, () => d.aim("window", springs.push)],
    [3300, () => d.aim("devices")],
    [3900, () => d.risePhone()],
    [5000, () => d.showCursor()],
    [5300, () => d.aim("windowDetail")],
    [5700, () => d.pointAt("pc", "button", "Allow")],
    [6900, () => d.pressCursor()],
    [7000, () => d.releaseCursor()],
    [8100, () => d.aim("devices")],
    [8900, () => d.pointAt("pc", "button", "Activity")],
    [9800, () => d.pressCursor()],
    [9900, () => d.releaseCursor()],
    [10300, () => d.aim("phone")],
    [11800, () => d.touchDown("button", "Send")],
    [11920, () => d.touchUp()],
    [12400, () => d.aim("phoneDetail")],
    [17900, () => d.aim("window")],
    [21000, () => d.aim("windowDetail")],
    [21200, () => d.pointAt("pc", "button", "Send Files…")],
    [22400, () => d.pressCursor()],
    [22500, () => d.releaseCursor()],
    [24400, () => d.aim("phoneDetail")],
    [27000, () => d.touchDown('a[href*="/api/files/"]')],
    [27120, () => d.touchUp()],
    [27200, () => api.startDownload(27200 + prerollMs)],
    [29600, () => d.aim("devices")],
    [31900, () => d.fadeOut(fadeOutMs)],
  ];
}
