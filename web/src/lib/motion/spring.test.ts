import { describe, expect, it } from 'vitest';
import {
  bouncy,
  quick,
  settlingTime,
  smooth,
  snappy,
  springEasing,
  springLinear,
  springPosition,
  type SpringSpec,
} from './spring';

type Preset = { name: string; spec: SpringSpec };

const designSystemCurves: (Preset & { curve: string })[] = [
  {
    name: 'smooth',
    spec: smooth,
    curve:
      'linear(0, 0.0229, 0.0789, 0.1533, 0.2362, 0.3209, 0.4031, 0.4803, 0.5511, 0.6147, 0.6712, 0.7208, 0.7639, 0.8011, 0.833, 0.8602, 0.8833, 0.9028, 0.9192, 0.933, 0.9445, 0.9542, 0.9622, 0.9688, 0.9744, 0.9789, 0.9827, 0.9858, 0.9884, 0.9905, 0.9922, 0.9936, 0.9948, 0.9958, 0.9966, 0.9972, 0.9977, 0.9981, 0.9985, 0.9988, 1)',
  },
  {
    name: 'snappy',
    spec: snappy,
    curve:
      'linear(0, 0.0212, 0.0748, 0.1486, 0.2335, 0.3225, 0.4109, 0.4953, 0.5736, 0.6446, 0.7076, 0.7628, 0.8102, 0.8505, 0.8842, 0.9121, 0.9348, 0.953, 0.9675, 0.9787, 0.9873, 0.9938, 0.9984, 1.0017, 1.0039, 1.0053, 1.006, 1.0063, 1.0062, 1.0059, 1.0055, 1.0049, 1.0044, 1.0038, 1.0033, 1.0028, 1.0023, 1.0019, 1.0016, 1.0013, 1)',
  },
  {
    name: 'bouncy',
    spec: bouncy,
    curve:
      'linear(0, 0.0293, 0.1034, 0.2047, 0.3193, 0.4368, 0.55, 0.6538, 0.7453, 0.8232, 0.8874, 0.9383, 0.9773, 1.0057, 1.0252, 1.0375, 1.0439, 1.046, 1.0449, 1.0416, 1.0371, 1.0318, 1.0264, 1.0212, 1.0164, 1.0121, 1.0084, 1.0054, 1.003, 1.0012, 0.9998, 0.9989, 0.9983, 0.998, 0.9979, 0.9979, 0.9981, 0.9983, 0.9985, 0.9988, 1)',
  },
];

const settlingTimes: (Preset & { seconds: number })[] = [
  { name: 'smooth', spec: smooth, seconds: 0.735 },
  { name: 'snappy', spec: snappy, seconds: 0.697 },
  { name: 'bouncy', spec: bouncy, seconds: 0.819 },
  { name: 'quick', spec: quick, seconds: 0.368 },
];

const overshoots: (Preset & { overshoot: number })[] = [
  { name: 'smooth', spec: smooth, overshoot: 0 },
  { name: 'snappy', spec: snappy, overshoot: 0.006 },
  { name: 'bouncy', spec: bouncy, overshoot: 0.046 },
];

describe('springLinear', () => {
  it.each(designSystemCurves)('reproduces the $name curve from the design system', (preset) => {
    expect(springLinear(preset.spec)).toBe(preset.curve);
  });

  it('honors the number of points', () => {
    expect(springLinear(smooth, 3)).toBe('linear(0, 0.9445, 1)');
  });
});

describe('settlingTime', () => {
  it.each(settlingTimes)('settles the $name spring at the design-system time', (preset) => {
    expect(settlingTime(preset.spec)).toBeCloseTo(preset.seconds, 3);
  });

  it.each(settlingTimes)('keeps the $name spring within epsilon after settling', (preset) => {
    const position = springPosition(preset.spec);
    const settle = settlingTime(preset.spec);
    for (let t = settle; t < settle + 2; t += 0.001) {
      expect(Math.abs(position(t) - 1)).toBeLessThanOrEqual(0.001);
    }
  });

  it('settles later for a tighter epsilon', () => {
    expect(settlingTime(smooth, 0.0001)).toBeGreaterThan(settlingTime(smooth));
  });
});

describe('springPosition', () => {
  it.each(settlingTimes)('starts the $name spring at zero and rests at one', (preset) => {
    const position = springPosition(preset.spec);
    expect(position(0)).toBeCloseTo(0, 12);
    expect(position(5)).toBeCloseTo(1, 6);
  });

  it.each(overshoots)('overshoots the $name spring by the design-system amount', (preset) => {
    const position = springPosition(preset.spec);
    let peak = 0;
    for (let t = 0; t < 3; t += 0.0005) peak = Math.max(peak, position(t));
    expect(Math.max(0, peak - 1)).toBeCloseTo(preset.overshoot, 3);
  });
});

describe('springEasing', () => {
  it('runs for the settling time and ends exactly at one', () => {
    const { durationMs, ease } = springEasing(snappy);
    expect(durationMs).toBe(697);
    expect(ease(0)).toBeCloseTo(0, 12);
    expect(ease(1)).toBe(1);
  });

  it('samples the same physics as the position function', () => {
    const { ease } = springEasing(bouncy);
    expect(ease(0.5)).toBeCloseTo(springPosition(bouncy)(0.4095), 10);
  });
});
