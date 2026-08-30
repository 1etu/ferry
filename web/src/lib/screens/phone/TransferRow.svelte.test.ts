import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/svelte';
import { flushSync } from 'svelte';
import Icon from '$lib/icons/Icon.svelte';
import { type IconName } from '$lib/icons/nodes';
import TransferRow from './TransferRow.svelte';
import { type TransferPhase } from './entries';

function finishedAnimation(): Animation {
  const animation = { onfinish: null as (() => void) | null, cancel: vi.fn(), effect: null };
  queueMicrotask(() => animation.onfinish?.());
  return animation as unknown as Animation;
}

beforeAll(() => {
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: () => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }),
  });
  Element.prototype.animate = finishedAnimation;
});

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const base = {
  name: 'IMG_0001.HEIC',
  size: 12_400_000,
  sent: 5_208_000,
  isUpload: true,
  index: 0,
  oncancel: vi.fn(),
  onretry: vi.fn(),
};

describe('TransferRow', () => {
  it.each<[TransferPhase, boolean, string]>([
    ['queued', true, 'Waiting'],
    ['active', true, '42%'],
    ['stalled', true, 'Waiting for network'],
    ['needsFile', true, 'Tap to resume'],
    ['done', true, '12 MB'],
    ['failed', true, 'Failed'],
    ['failed', false, 'Unavailable'],
    ['canceled', true, 'Canceled'],
    ['active', false, '42%'],
  ])('%s (upload %s) reads %s', (phase, isUpload, subtitle) => {
    render(TransferRow, { props: { ...base, phase, isUpload } });
    expect(screen.getByText(subtitle)).toBeTruthy();
  });

  it.each<{ kind: string; isUpload: boolean; icon: IconName }>([
    { kind: 'an upload', isUpload: true, icon: 'arrowUp' },
    { kind: 'a download', isUpload: false, icon: 'arrowDown' },
  ])('leads $kind with $icon', ({ isUpload, icon }) => {
    const row = render(TransferRow, { props: { ...base, phase: 'active', isUpload } });
    const glyph = render(Icon, { props: { name: icon } });
    const leading = row.container.querySelector('.leading svg');
    expect(leading?.innerHTML).toBe(glyph.container.querySelector('svg')?.innerHTML);
  });

  it('offers cancel only while an upload moves', () => {
    const { rerender } = render(TransferRow, { props: { ...base, phase: 'active' } });
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeTruthy();
    void rerender({ ...base, phase: 'active', isUpload: false });
    flushSync();
    expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull();
  });

  it('makes a failed upload row the retry button', () => {
    render(TransferRow, { props: { ...base, phase: 'failed' } });
    screen.getByRole('button', { name: 'IMG_0001.HEIC' }).click();
    expect(base.onretry).toHaveBeenCalledOnce();
  });

  it('lands the ring at 100% before the check replaces it', async () => {
    const { container, rerender } = render(TransferRow, { props: { ...base, phase: 'active' } });
    await rerender({ ...base, phase: 'done', sent: base.size });
    expect(screen.getByText('100%')).toBeTruthy();
    expect(container.querySelector('[role="progressbar"]')?.getAttribute('aria-valuenow')).toBe(
      '100',
    );
    await vi.advanceTimersByTimeAsync(800);
    expect(screen.getByText('12 MB')).toBeTruthy();
    expect(container.querySelector('.check.fresh')).not.toBeNull();
    await vi.advanceTimersByTimeAsync(1500);
    expect(container.querySelector('.check.fresh')).toBeNull();
    expect(container.querySelector('.check')).not.toBeNull();
  });

  it('shows history rows settled, without the fresh check', () => {
    const { container } = render(TransferRow, { props: { ...base, phase: 'done' } });
    expect(container.querySelector('.check')).not.toBeNull();
    expect(container.querySelector('.check.fresh')).toBeNull();
  });
});
