import { canvas, viewportPoint, worldPoint, worldRect } from "./layout.mjs";
import { Camera, Spring, glide, springs } from "./motion.mjs";

const frameSeconds = 1 / canvas.fps;
const openingZoom = 1.42;
const phoneRiseWorldPx = 56;
const rippleMs = 480;
const touchInMs = 90;
const touchOutMs = 260;

function locate([selector, text]) {
  const matches = [...document.querySelectorAll(selector)].filter((node) => {
    if (text === undefined) return true;
    const label = node.getAttribute("aria-label") ?? node.textContent ?? "";
    return label.trim() === text;
  });
  const node = matches.find((candidate) => candidate.getClientRects().length > 0);
  if (!node) return null;
  const rect = node.getBoundingClientRect();
  return { x: rect.x, y: rect.y, width: rect.width, height: rect.height };
}

function clamp01(value) {
  return Math.min(1, Math.max(0, value));
}

export class Director {
  constructor({ page, frames, layout }) {
    this.page = page;
    this.frames = frames;
    this.layout = layout;
    this.time = 0;
    const { window, shots } = layout;
    this.camera = new Camera({ ...shots.window, zoom: openingZoom });
    this.view = this.camera.step(0);
    this.phoneOffset = new Spring(phoneRiseWorldPx);
    this.phoneOpacity = new Spring(0);
    this.cursorOpacity = new Spring(0);
    this.cursorScale = new Spring(1);
    this.cursor = { x: window.x + window.width * 0.72, y: window.y + window.height * 0.82 };
    this.path = undefined;
    this.click = undefined;
    this.touch = undefined;
    this.tapping = false;
    this.fadeStart = Infinity;
    this.fadeMs = 1;
  }

  async rect(role, selector, text) {
    const local = await this.frames[role].evaluate(locate, [selector, text]);
    if (!local) throw new Error(`demo target missing: ${role} ${selector} ${text ?? ""}`);
    return { local, world: worldRect(this.layout, role, local) };
  }

  aim(shot, spec = springs.camera) {
    this.camera.aim(this.layout.shots[shot], spec);
  }

  risePhone() {
    this.phoneOffset.retarget(0, springs.camera);
    this.phoneOpacity.retarget(1, springs.camera);
  }

  showCursor() {
    this.cursorOpacity.retarget(1, springs.smooth);
  }

  async pointAt(role, selector, text) {
    const { world } = await this.rect(role, selector, text);
    const to = { x: world.cx + world.width * 0.16, y: world.cy + world.height * 0.2 };
    this.path = glide({ ...this.cursor }, to, this.time);
  }

  pressCursor() {
    this.cursorScale.retarget(0.86, springs.snappy);
    this.click = { at: this.time, x: this.cursor.x, y: this.cursor.y };
    return this.page.mouse.down();
  }

  releaseCursor() {
    this.cursorScale.retarget(1, springs.snappy);
    return this.page.mouse.up();
  }

  async touchDown(selector, text) {
    const { local } = await this.rect("phone", selector, text);
    const center = { x: local.x + local.width / 2, y: local.y + local.height / 2 };
    const { screen, viewport } = this.layout.device;
    const ratio = screen.width / viewport.width;
    this.touch = { down: this.time, up: Infinity, x: center.x * ratio, y: center.y * ratio };
    const point = viewportPoint(this.view, worldPoint(this.layout, "phone", center));
    this.tapping = true;
    await this.page.mouse.move(point.x, point.y);
    await this.page.mouse.down();
  }

  async touchUp() {
    this.touch.up = this.time;
    await this.page.mouse.up();
    this.tapping = false;
  }

  fadeOut(durationMs) {
    this.fadeStart = this.time;
    this.fadeMs = durationMs;
  }

  advance(timeMs) {
    this.time = timeMs;
    this.view = this.camera.step(frameSeconds);
    this.phoneOffset.step(frameSeconds);
    this.phoneOpacity.step(frameSeconds);
    this.cursorOpacity.step(frameSeconds);
    this.cursorScale.step(frameSeconds);
    if (this.path) {
      this.cursor = this.path.at(timeMs);
      if (timeMs >= this.path.endMs) this.path = undefined;
    }
  }

  async syncMouse() {
    if (this.tapping || this.cursorOpacity.target === 0) return;
    const point = viewportPoint(this.view, this.cursor);
    await this.page.mouse.move(point.x, point.y);
  }

  rippleState() {
    if (!this.click) return { x: 0, y: 0, scale: 1, opacity: 0 };
    const progress = clamp01((this.time - this.click.at) / rippleMs);
    const out = 1 - (1 - progress) ** 3;
    return { x: this.click.x, y: this.click.y, scale: 0.3 + 0.75 * out, opacity: 0.5 * (1 - progress) ** 1.6 };
  }

  touchState() {
    if (!this.touch) return { x: 0, y: 0, scale: 1, opacity: 0 };
    const { down, up, x, y } = this.touch;
    const rise = clamp01((this.time - down) / touchInMs);
    const fall = clamp01((this.time - up) / touchOutMs);
    const opacity = rise * (1 - fall) ** 1.4;
    return { x, y, scale: 0.86 + 0.14 * rise + 0.12 * fall, opacity };
  }

  state() {
    return {
      camera: this.view,
      window: { opacity: 1 },
      phone: { offset: this.phoneOffset.value, opacity: clamp01(this.phoneOpacity.value) },
      cursor: { ...this.cursor, scale: this.cursorScale.value, opacity: clamp01(this.cursorOpacity.value) },
      ripple: this.rippleState(),
      touch: this.touchState(),
      fade: clamp01((this.time - this.fadeStart) / this.fadeMs),
    };
  }
}
