import { type TransitionConfig } from 'svelte/transition';
import { bouncy, reducedMotion, smooth, snappy, springEasing, type SpringSpec } from './spring';

const fadeDurationMs = 200;
const staggerStepMs = 40;
const staggeredRowCount = 8;
const riseDistancePx = 8;
const popFromScale = 0.8;
const presentFromScale = 0.96;
const dismissToScale = 0.98;

function springConfig(spec: SpringSpec, css: (t: number) => string): TransitionConfig {
  const { durationMs, ease } = springEasing(spec);
  return { duration: durationMs, easing: ease, css };
}

function pixels(value: string): number {
  return Number.parseFloat(value) || 0;
}

function parseOpacity(value: string): number {
  const opacity = Number.parseFloat(value);
  return Number.isNaN(opacity) ? 1 : opacity;
}

function restingOpacity(node: Element): number {
  return parseOpacity(getComputedStyle(node).opacity);
}

function opacityAt(t: number, resting: number): string {
  return String(Math.min(1, Math.max(0, t)) * resting);
}

function translateY(px: number): string {
  return `transform: translateY(${String(px)}px)`;
}

function scaleBetween(from: number, to: number, t: number): string {
  return `transform: scale(${String(from + (to - from) * t)})`;
}

function staggerDelayMs(index: number): number {
  const position = Math.min(Math.max(0, Math.trunc(index)), staggeredRowCount - 1);
  return position * staggerStepMs;
}

export function fadeIn(node: Element): TransitionConfig {
  const resting = restingOpacity(node);
  return { duration: fadeDurationMs, css: (t) => `opacity: ${opacityAt(t, resting)}` };
}

export function riseIn(node: Element, { index = 0 }: { index?: number } = {}): TransitionConfig {
  if (reducedMotion.current) return fadeIn(node);
  const resting = restingOpacity(node);
  return {
    ...springConfig(
      snappy,
      (t) => `opacity: ${opacityAt(t, resting)}; ${translateY((1 - t) * riseDistancePx)}`,
    ),
    delay: staggerDelayMs(index),
  };
}

function boxAt(t: number, resting: number, height: number): string {
  return (
    `overflow: hidden; min-height: 0; opacity: ${opacityAt(t, resting)}; ` +
    `height: ${String(t * height)}px`
  );
}

export function collapseOut(node: Element): TransitionConfig {
  if (reducedMotion.current) return fadeIn(node);
  const style = getComputedStyle(node);
  const resting = parseOpacity(style.opacity);
  const height = pixels(style.height);
  const paddingTop = pixels(style.paddingTop);
  const paddingBottom = pixels(style.paddingBottom);
  return springConfig(
    smooth,
    (t) =>
      `${boxAt(t, resting, height)}; ` +
      `padding-top: ${String(t * paddingTop)}px; padding-bottom: ${String(t * paddingBottom)}px`,
  );
}

export function expandIn(node: Element): TransitionConfig {
  if (reducedMotion.current) return fadeIn(node);
  const style = getComputedStyle(node);
  const resting = parseOpacity(style.opacity);
  const height = pixels(style.height);
  return springConfig(
    snappy,
    (t) => `${boxAt(t, resting, height)}; ${translateY((1 - t) * riseDistancePx)}`,
  );
}

export function popIn(node: Element): TransitionConfig {
  if (reducedMotion.current) return fadeIn(node);
  const resting = restingOpacity(node);
  return springConfig(
    bouncy,
    (t) => `opacity: ${opacityAt(t, resting)}; ${scaleBetween(popFromScale, 1, t)}`,
  );
}

export function scaleIn(node: Element, { spring }: { spring: SpringSpec }): TransitionConfig {
  if (reducedMotion.current) return fadeIn(node);
  const resting = restingOpacity(node);
  return springConfig(
    spring,
    (t) => `opacity: ${opacityAt(t, resting)}; ${scaleBetween(presentFromScale, 1, t)}`,
  );
}

export function scaleOut(node: Element): TransitionConfig {
  if (reducedMotion.current) return fadeIn(node);
  const resting = restingOpacity(node);
  return springConfig(
    smooth,
    (t) => `opacity: ${opacityAt(t, resting)}; ${scaleBetween(dismissToScale, 1, t)}`,
  );
}

export type SheetPlacement = 'bottom' | 'center';

export function sheetIn(
  node: Element,
  { spring, placement = 'bottom' }: { spring: SpringSpec; placement?: SheetPlacement },
): TransitionConfig {
  if (placement === 'center') return scaleIn(node, { spring });
  if (reducedMotion.current) return fadeIn(node);
  const height = node.getBoundingClientRect().height;
  return springConfig(spring, (t) => translateY((1 - t) * height));
}

export function sheetOut(
  node: Element,
  { fromPx, placement = 'bottom' }: { fromPx: number; placement?: SheetPlacement },
): TransitionConfig {
  if (placement === 'center') return scaleOut(node);
  if (reducedMotion.current) return fadeIn(node);
  const height = node.getBoundingClientRect().height;
  return springConfig(smooth, (t) => translateY(fromPx + (1 - t) * (height - fromPx)));
}
