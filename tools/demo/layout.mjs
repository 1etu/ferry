import { createRequire } from "node:module";

const require = createRequire(new URL("../../web/package.json", import.meta.url));

export const canvas = { width: 1920, height: 1080, fps: 60 };

const desktop = { width: 780, height: 540, caption: 32, radius: 8 };
const phoneHeight = 657;
const gap = 300;
const appleScreenDensity = 3;
const zoomCap = 2.2;
const frameMargin = { x: 80, y: 60 };
const leftSubjectLine = canvas.width * 0.3;
const detailZoom = { window: 2.1, phone: 2.0 };
const detailClearance = { phoneTop: 30, window: 40 };

export const placeholderDevice = {
  image: null,
  size: { width: 429, height: 888 },
  screen: { x: 18, y: 18, width: 393, height: 852, radius: 55 },
  viewport: { width: 393, height: 852 },
  insets: { top: 59, bottom: 34 },
};

export async function measureBezel(path) {
  const sharp = require("sharp");
  const { data, info } = await sharp(path).ensureAlpha().raw().toBuffer({ resolveWithObject: true });
  const alpha = (x, y) => data[(y * info.width + x) * info.channels + 3];
  const clear = (x, y) => alpha(x, y) < 8;
  const middleY = Math.round(info.height / 2);
  let left = Math.round(info.width / 2);
  let right = left;
  while (left > 0 && clear(left - 1, middleY)) left -= 1;
  while (right < info.width - 1 && clear(right + 1, middleY)) right += 1;
  const column = Math.round(left + (right - left) * 0.75);
  let top = middleY;
  let bottom = middleY;
  while (top > 0 && clear(column, top - 1)) top -= 1;
  while (bottom < info.height - 1 && clear(column, bottom + 1)) bottom += 1;
  const scale = 1 / appleScreenDensity;
  const width = (right - left + 1) * scale;
  const height = (bottom - top + 1) * scale;
  return {
    image: path,
    size: { width: info.width * scale, height: info.height * scale },
    screen: { x: left * scale, y: top * scale, width, height, radius: 0 },
    viewport: { width: Math.round(width), height: Math.round(height) },
    insets: { top: 62, bottom: 34 },
  };
}

function zoomToFit({ width, height }) {
  const fitX = (canvas.width - 2 * frameMargin.x) / width;
  const fitY = (canvas.height - 2 * frameMargin.y) / height;
  return Math.min(zoomCap, fitX, fitY);
}

function centered(rect) {
  return { x: rect.x + rect.width / 2, y: rect.y + rect.height / 2, zoom: zoomToFit(rect) };
}

function onLeftSubjectLine(rect) {
  const { x, y, zoom } = centered(rect);
  return { x: x + (canvas.width / 2 - leftSubjectLine) / zoom, y, zoom };
}

function union(a, b) {
  const x = Math.min(a.x, b.x);
  const y = Math.min(a.y, b.y);
  return { x, y, width: Math.max(a.x + a.width, b.x + b.width) - x, height: Math.max(a.y + a.height, b.y + b.height) - y };
}

function windowDetail(window) {
  const zoom = detailZoom.window;
  return { x: window.x + window.width / 2, y: window.y + window.caption + canvas.height / 2 / zoom, zoom };
}

function phoneDetail(window, phone) {
  const zoom = detailZoom.phone;
  const windowRight = window.x + window.width;
  const x = Math.max(phone.x + phone.width / 2, windowRight + canvas.width / 2 / zoom + detailClearance.window);
  return { x, y: phone.y + canvas.height / 2 / zoom - detailClearance.phoneTop, zoom };
}

function shotsFor(window, phone) {
  const windowRect = { x: window.x, y: window.y, width: window.width, height: window.height + window.caption };
  const phoneRect = { x: phone.x, y: phone.y, width: phone.width, height: phone.height };
  return {
    window: centered(windowRect),
    windowDetail: windowDetail(window),
    devices: centered(union(windowRect, phoneRect)),
    phone: onLeftSubjectLine(phoneRect),
    phoneDetail: phoneDetail(window, phone),
  };
}

export function layoutFor(device) {
  const phoneScale = phoneHeight / device.size.height;
  const phoneWidth = device.size.width * phoneScale;
  const windowHeight = desktop.height + desktop.caption;
  const left = (canvas.width - (desktop.width + gap + phoneWidth)) / 2;
  const window = { ...desktop, x: left, y: Math.round((canvas.height - windowHeight) / 2) };
  const phone = {
    x: left + desktop.width + gap,
    y: Math.round((canvas.height - phoneHeight) / 2),
    width: phoneWidth,
    height: phoneHeight,
    scale: phoneScale,
  };
  return {
    canvas,
    device: { ...device, image: device.image ? "/bezel.png" : null },
    window,
    phone,
    shots: shotsFor(window, phone),
  };
}

export function worldPoint(layout, role, local) {
  if (role === "pc") {
    return { x: layout.window.x + local.x, y: layout.window.y + layout.window.caption + local.y };
  }
  const { screen, viewport } = layout.device;
  const { phone } = layout;
  return {
    x: phone.x + (screen.x + (local.x * screen.width) / viewport.width) * phone.scale,
    y: phone.y + (screen.y + (local.y * screen.height) / viewport.height) * phone.scale,
  };
}

export function worldRect(layout, role, rect) {
  const a = worldPoint(layout, role, { x: rect.x, y: rect.y });
  const b = worldPoint(layout, role, { x: rect.x + rect.width, y: rect.y + rect.height });
  return { x: a.x, y: a.y, width: b.x - a.x, height: b.y - a.y, cx: (a.x + b.x) / 2, cy: (a.y + b.y) / 2 };
}

export function viewportPoint(camera, point) {
  return {
    x: (point.x - camera.x) * camera.zoom + canvas.width / 2,
    y: (point.y - camera.y) * camera.zoom + canvas.height / 2,
  };
}
