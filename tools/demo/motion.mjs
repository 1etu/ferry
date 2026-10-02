import { smooth, snappy, springEasing } from "../../web/src/lib/motion/spring.ts";

export const springs = { camera: { ...smooth, duration: 0.9 }, push: { ...smooth, duration: 1.4 }, smooth, snappy };

export class Spring {
  constructor(value) {
    this.value = value;
    this.target = value;
    this.velocity = 0;
    this.spec = smooth;
  }

  retarget(target, spec = smooth) {
    this.target = target;
    this.spec = spec;
  }

  jump(value) {
    this.value = value;
    this.target = value;
    this.velocity = 0;
  }

  step(seconds) {
    const omega = (2 * Math.PI) / this.spec.duration;
    const zeta = 1 - this.spec.bounce;
    const x0 = this.value - this.target;
    const v0 = this.velocity;
    const decay = Math.exp(-zeta * omega * seconds);
    let x;
    let v;
    if (zeta >= 1) {
      const b = v0 + omega * x0;
      x = (x0 + b * seconds) * decay;
      v = (v0 - omega * b * seconds) * decay;
    } else {
      const damped = omega * Math.sqrt(1 - zeta * zeta);
      const a = x0;
      const b = (v0 + zeta * omega * x0) / damped;
      const cos = Math.cos(damped * seconds);
      const sin = Math.sin(damped * seconds);
      x = decay * (a * cos + b * sin);
      v = decay * ((damped * b - zeta * omega * a) * cos - (zeta * omega * b + damped * a) * sin);
    }
    this.value = this.target + x;
    this.velocity = v;
    return this.value;
  }
}

export class Camera {
  constructor({ x, y, zoom }) {
    this.x = new Spring(x);
    this.y = new Spring(y);
    this.zoom = new Spring(Math.log(zoom));
  }

  aim({ x, y, zoom }, spec = smooth) {
    this.x.retarget(x, spec);
    this.y.retarget(y, spec);
    this.zoom.retarget(Math.log(zoom), spec);
  }

  step(seconds) {
    return { x: this.x.step(seconds), y: this.y.step(seconds), zoom: Math.exp(this.zoom.step(seconds)) };
  }
}

export function glide(from, to, startMs) {
  const distance = Math.hypot(to.x - from.x, to.y - from.y);
  const spec = { duration: Math.min(0.62, Math.max(0.42, 0.34 + distance / 1600)), bounce: 0 };
  const { durationMs, ease } = springEasing(spec);
  const bend = Math.min(60, distance * 0.14);
  const nx = distance === 0 ? 0 : -(to.y - from.y) / distance;
  const ny = distance === 0 ? 0 : (to.x - from.x) / distance;
  const control = {
    x: (from.x + to.x) / 2 + nx * bend,
    y: (from.y + to.y) / 2 + ny * bend,
  };
  return {
    endMs: startMs + durationMs,
    at(timeMs) {
      const t = ease(Math.min(1, Math.max(0, (timeMs - startMs) / durationMs)));
      const u = 1 - t;
      return {
        x: u * u * from.x + 2 * u * t * control.x + t * t * to.x,
        y: u * u * from.y + 2 * u * t * control.y + t * t * to.y,
      };
    },
  };
}
