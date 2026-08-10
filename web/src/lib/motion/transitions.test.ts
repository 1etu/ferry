import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { type TransitionConfig } from 'svelte/transition';
import { bouncy, smooth, snappy, springEasing } from './spring';
import {
  collapseOut,
  expandIn,
  fadeIn,
  popIn,
  riseIn,
  scaleIn,
  scaleOut,
  sheetIn,
  sheetOut,
} from './transitions';

let prefersReducedMotion = false;

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: () => ({
      get matches() {
        return prefersReducedMotion;
      },
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    }),
  });
});

afterEach(() => {
  prefersReducedMotion = false;
});

function node(): HTMLElement {
  return document.createElement('div');
}

function frame(config: TransitionConfig, t: number): string {
  if (config.css === undefined) throw new Error('transition has no css');
  return config.css(t, 1 - t);
}

function expectPlainFade(config: TransitionConfig) {
  expect(config.duration).toBe(200);
  expect(config.delay ?? 0).toBe(0);
  expect(config.easing).toBeUndefined();
}

describe('riseIn', () => {
  it('rises 8 px on the snappy spring', () => {
    const config = riseIn(node());
    expect(config.duration).toBe(springEasing(snappy).durationMs);
    expect(frame(config, 0)).toContain('translateY(8px)');
    expect(frame(config, 1)).toContain('translateY(0px)');
    expect(frame(config, 0)).toContain('opacity: 0');
  });

  it('staggers by 40 ms per index', () => {
    expect(riseIn(node()).delay).toBe(0);
    expect(riseIn(node(), { index: 0 }).delay).toBe(0);
    expect(riseIn(node(), { index: 3 }).delay).toBe(120);
    expect(riseIn(node(), { index: 7 }).delay).toBe(280);
  });

  it('lets rows beyond the eighth arrive with the eighth', () => {
    expect(riseIn(node(), { index: 8 }).delay).toBe(280);
    expect(riseIn(node(), { index: 200 }).delay).toBe(280);
  });

  it('becomes an immediate fade without movement under Reduce Motion', () => {
    prefersReducedMotion = true;
    const config = riseIn(node(), { index: 5 });
    expectPlainFade(config);
    expect(frame(config, 0)).not.toContain('transform');
  });
});

describe('popIn', () => {
  it('scales from 0.8 on the bouncy spring and overshoots', () => {
    const config = popIn(node());
    expect(config.duration).toBe(springEasing(bouncy).durationMs);
    expect(frame(config, 0)).toContain('scale(0.8)');
    expect(frame(config, 1)).toContain('scale(1)');
    const steps = Array.from(Array(101).keys(), (step) => step / 100);
    const peak = Math.max(...steps.map((progress) => config.easing?.(progress) ?? 0));
    expect(peak).toBeGreaterThan(1);
    expect(frame(config, peak)).toContain('opacity: 1;');
  });

  it('only fades under Reduce Motion', () => {
    prefersReducedMotion = true;
    const config = popIn(node());
    expectPlainFade(config);
    expect(frame(config, 0)).not.toContain('scale');
  });
});

describe('scaleIn and scaleOut', () => {
  it('presents from 0.96 on the given spring', () => {
    const config = scaleIn(node(), { spring: bouncy });
    expect(config.duration).toBe(springEasing(bouncy).durationMs);
    expect(frame(config, 0)).toContain('scale(0.96)');
    expect(frame(config, 0)).toContain('opacity: 0');
  });

  it('dismisses to 0.98 on the smooth spring', () => {
    const config = scaleOut(node());
    expect(config.duration).toBe(springEasing(smooth).durationMs);
    expect(frame(config, 0)).toContain('scale(0.98)');
    expect(frame(config, 1)).toContain('scale(1)');
  });

  it('only fade under Reduce Motion', () => {
    prefersReducedMotion = true;
    expectPlainFade(scaleIn(node(), { spring: snappy }));
    expectPlainFade(scaleOut(node()));
  });
});

describe('collapseOut', () => {
  it('collapses height on the smooth spring', () => {
    const config = collapseOut(node());
    expect(config.duration).toBe(springEasing(smooth).durationMs);
    expect(frame(config, 0)).toContain('height: 0px');
  });

  it('fades with the height change left instant under Reduce Motion', () => {
    prefersReducedMotion = true;
    const config = collapseOut(node());
    expectPlainFade(config);
    expect(frame(config, 0)).not.toContain('height');
  });
});

describe('expandIn', () => {
  it('grows from zero height while rising on the snappy spring', () => {
    const element = node();
    element.style.height = '96px';
    const config = expandIn(element);
    expect(config.duration).toBe(springEasing(snappy).durationMs);
    expect(config.delay ?? 0).toBe(0);
    expect(frame(config, 0)).toContain('height: 0px');
    expect(frame(config, 0)).toContain('overflow: hidden');
    expect(frame(config, 0)).toContain('translateY(8px)');
    expect(frame(config, 0)).toContain('opacity: 0');
    expect(frame(config, 1)).toContain('height: 96px');
    expect(frame(config, 1)).toContain('translateY(0px)');
  });

  it('fades with the height change left instant under Reduce Motion', () => {
    prefersReducedMotion = true;
    const config = expandIn(node());
    expectPlainFade(config);
    expect(frame(config, 0)).not.toContain('height');
    expect(frame(config, 0)).not.toContain('transform');
  });
});

describe('sheet transitions', () => {
  it('slide on the entrance spring and dismiss from the drag offset', () => {
    expect(sheetIn(node(), { spring: bouncy }).duration).toBe(springEasing(bouncy).durationMs);
    const config = sheetOut(node(), { fromPx: 30 });
    expect(config.duration).toBe(springEasing(smooth).durationMs);
    expect(frame(config, 1)).toContain('translateY(30px)');
  });

  it('only fade under Reduce Motion', () => {
    prefersReducedMotion = true;
    expectPlainFade(sheetIn(node(), { spring: snappy }));
    expectPlainFade(sheetOut(node(), { fromPx: 0 }));
  });
});

describe('fadeIn', () => {
  it('lasts 200 ms and respects the resting opacity', () => {
    const element = node();
    element.style.opacity = '0.5';
    const config = fadeIn(element);
    expect(config.duration).toBe(200);
    expect(frame(config, 1)).toBe('opacity: 0.5');
  });
});
