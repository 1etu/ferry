import { MediaQuery } from 'svelte/reactivity';

export type SpringSpec = { duration: number; bounce: number };

export const smooth: SpringSpec = { duration: 0.5, bounce: 0 };
export const snappy: SpringSpec = { duration: 0.5, bounce: 0.15 };
export const bouncy: SpringSpec = { duration: 0.5, bounce: 0.3 };
export const quick: SpringSpec = { duration: 0.25, bounce: 0 };

const defaultEpsilon = 0.001;
const defaultPoints = 41;
const scanStepsPerDuration = 1000;
const bisectionRounds = 60;

function naturalFrequency(spec: SpringSpec): number {
  return (2 * Math.PI) / spec.duration;
}

function dampingRatio(spec: SpringSpec): number {
  return 1 - spec.bounce;
}

export function springPosition(spec: SpringSpec): (t: number) => number {
  const omega = naturalFrequency(spec);
  const zeta = dampingRatio(spec);
  if (zeta >= 1) {
    return (t) => 1 - (1 + omega * t) * Math.exp(-omega * t);
  }
  const dampedOmega = omega * Math.sqrt(1 - zeta * zeta);
  const sineWeight = (zeta * omega) / dampedOmega;
  return (t) =>
    1 -
    Math.exp(-zeta * omega * t) *
      (Math.cos(dampedOmega * t) + sineWeight * Math.sin(dampedOmega * t));
}

function envelopeHorizon(spec: SpringSpec, epsilon: number): number {
  const omega = naturalFrequency(spec);
  const zeta = dampingRatio(spec);
  if (zeta >= 1) return (2 * Math.log(2 / epsilon)) / omega;
  return Math.log(1 / (epsilon * Math.sqrt(1 - zeta * zeta))) / (zeta * omega);
}

function lastExcursion(spec: SpringSpec, epsilon: number): number {
  const position = springPosition(spec);
  const isOutside = (t: number) => Math.abs(position(t) - 1) > epsilon;
  const step = spec.duration / scanStepsPerDuration;
  let inside = envelopeHorizon(spec, epsilon);
  while (inside > 0 && !isOutside(inside - step)) inside -= step;
  let outside = Math.max(0, inside - step);
  for (let round = 0; round < bisectionRounds; round += 1) {
    const middle = (outside + inside) / 2;
    if (isOutside(middle)) outside = middle;
    else inside = middle;
  }
  return inside;
}

export function settlingTime(spec: SpringSpec, epsilon = defaultEpsilon): number {
  return Math.ceil(lastExcursion(spec, epsilon) * 1000) / 1000;
}

export function springEasing(spec: SpringSpec): {
  durationMs: number;
  ease: (t: number) => number;
} {
  const settle = settlingTime(spec);
  const position = springPosition(spec);
  return {
    durationMs: Math.round(settle * 1000),
    ease: (t) => (t >= 1 ? 1 : position(t * settle)),
  };
}

function formatStop(value: number): string {
  return String(Math.round(value * 10000) / 10000);
}

export function springLinear(spec: SpringSpec, points = defaultPoints): string {
  const { ease } = springEasing(spec);
  const stops = Array.from(Array(points).keys(), (index) => formatStop(ease(index / (points - 1))));
  return `linear(${stops.join(', ')})`;
}

let reducedMotionQuery: MediaQuery | undefined;

export const reducedMotion: { readonly current: boolean } = {
  get current() {
    reducedMotionQuery ??= new MediaQuery('(prefers-reduced-motion: reduce)');
    return reducedMotionQuery.current;
  },
};
