import { describe, expect, it } from 'vitest';
import { formatBytes, formatPercent } from './format';

describe('formatBytes', () => {
  it.each([
    [0, '0 B'],
    [1, '1 B'],
    [999, '999 B'],
    [1000, '1 KB'],
    [1500, '1.5 KB'],
    [9_640_000, '9.6 MB'],
    [9_960_000, '10 MB'],
    [12_400_000, '12 MB'],
    [999_400, '999 KB'],
    [999_600, '1 MB'],
    [1_234_567_890, '1.2 GB'],
    [68_719_476_736, '69 GB'],
    [5_000_000_000_000, '5000 GB'],
  ])('formats %d bytes as %s', (bytes, expected) => {
    expect(formatBytes(bytes)).toBe(expected);
  });

  it.each([
    [-5, '0 B'],
    [Number.NaN, '0 B'],
    [Number.POSITIVE_INFINITY, '0 B'],
  ])('treats invalid input %d as zero', (bytes, expected) => {
    expect(formatBytes(bytes)).toBe(expected);
  });
});

describe('formatPercent', () => {
  it.each([
    [0, '0%'],
    [0.5, '50%'],
    [0.999, '99%'],
    [1, '100%'],
    [1.2, '100%'],
    [-0.1, '0%'],
    [Number.NaN, '0%'],
  ])('formats %d as %s', (fraction, expected) => {
    expect(formatPercent(fraction)).toBe(expected);
  });
});
