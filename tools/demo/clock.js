(() => {
  if (globalThis.__clock) return;

  const RealDate = Date;
  const realRequestFrame = requestAnimationFrame.bind(globalThis);
  const realAnimate = Element.prototype.animate;
  const epoch = new RealDate(2026, 9, 2, 9, 41, 0).getTime();
  const parentClock = (() => {
    try {
      return globalThis.parent !== globalThis ? globalThis.parent.__clock : undefined;
    } catch {
      return undefined;
    }
  })();

  let now = parentClock ? parentClock.now() : 0;
  let nextId = 1;
  let order = 0;
  const timers = new Map();
  const frames = new Map();
  const tracked = new WeakMap();
  const errors = [];

  const channel = new MessageChannel();
  const waiting = [];
  channel.port1.onmessage = () => waiting.shift()?.();
  const macrotask = () =>
    new Promise((resolve) => {
      waiting.push(resolve);
      channel.port2.postMessage(0);
    });

  function report(error) {
    errors.push(String(error?.stack ?? error));
  }

  function schedule(callback, delay, args, interval) {
    const id = nextId++;
    const wait = Math.max(0, Number(delay) || 0);
    timers.set(id, { at: now + wait, callback, args, interval, order: order++ });
    return id;
  }

  globalThis.setTimeout = (callback, delay, ...args) => schedule(callback, delay, args, undefined);
  globalThis.setInterval = (callback, delay, ...args) =>
    schedule(callback, delay, args, Math.max(1, Number(delay) || 0));
  globalThis.clearTimeout = (id) => {
    timers.delete(id);
  };
  globalThis.clearInterval = globalThis.clearTimeout;
  globalThis.requestAnimationFrame = (callback) => {
    const id = nextId++;
    frames.set(id, callback);
    return id;
  };
  globalThis.cancelAnimationFrame = (id) => {
    frames.delete(id);
  };
  performance.now = () => now;

  class VirtualDate extends RealDate {
    constructor(...args) {
      if (args.length === 0) super(epoch + now);
      else super(...args);
    }

    static now() {
      return epoch + now;
    }
  }
  globalThis.Date = VirtualDate;

  function adopt(animation) {
    let record = tracked.get(animation);
    if (record) return record;
    record = { start: now, finished: false };
    tracked.set(animation, record);
    try {
      animation.pause();
      animation.currentTime = 0;
    } catch (error) {
      report(error);
    }
    return record;
  }

  Element.prototype.animate = function animate(...args) {
    const animation = realAnimate.apply(this, args);
    adopt(animation);
    return animation;
  };

  function adoptFrom(event) {
    const target = event.target;
    if (!(target instanceof Element)) return;
    for (const animation of target.getAnimations()) adopt(animation);
  }
  addEventListener("transitionrun", adoptFrom, true);
  addEventListener("animationstart", adoptFrom, true);

  function nextDue(limit) {
    let best;
    for (const entry of timers) {
      const timer = entry[1];
      if (timer.at > limit) continue;
      if (!best || timer.at < best[1].at || (timer.at === best[1].at && timer.order < best[1].order)) {
        best = entry;
      }
    }
    return best;
  }

  function fire([id, timer]) {
    now = Math.max(now, timer.at);
    if (timer.interval === undefined) timers.delete(id);
    else {
      timer.at += timer.interval;
      timer.order = order++;
    }
    if (typeof timer.callback !== "function") return;
    try {
      timer.callback(...timer.args);
    } catch (error) {
      report(error);
    }
  }

  function sync() {
    let finished = 0;
    let running = 0;
    for (const animation of document.getAnimations()) {
      const record = adopt(animation);
      if (record.finished) continue;
      const end = animation.effect?.getComputedTiming().endTime ?? 0;
      const local = (now - record.start) * (animation.playbackRate || 1);
      if (Number.isFinite(end) && local >= end) {
        record.finished = true;
        finished += 1;
        try {
          animation.finish();
        } catch (error) {
          report(error);
        }
        continue;
      }
      running += 1;
      if (animation.playState !== "paused") animation.pause();
      animation.currentTime = local;
    }
    return { finished, running };
  }

  globalThis.__clock = {
    now: () => now,
    async runUntil(limit) {
      let count = 0;
      for (let due = nextDue(limit); due && count < 10000; due = nextDue(limit)) {
        fire(due);
        count += 1;
        await macrotask();
      }
      now = Math.max(now, limit);
      return count;
    },
    async frame() {
      const callbacks = [...frames.values()];
      frames.clear();
      for (const callback of callbacks) {
        try {
          callback(now);
        } catch (error) {
          report(error);
        }
      }
      await macrotask();
      return callbacks.length;
    },
    sync,
    paint: () => new Promise((resolve) => realRequestFrame(() => realRequestFrame(resolve))),
    errors: () => errors.splice(0),
  };
})();
