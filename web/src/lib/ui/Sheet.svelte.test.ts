import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import { createRawSnippet, tick } from 'svelte';
import Sheet from './Sheet.svelte';

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
  Element.prototype.getAnimations = () => [];
});

afterEach(() => {
  cleanup();
  document.body.replaceChildren();
});

const children = createRawSnippet(() => ({
  render: () => '<div><button>First</button><button>Last</button></div>',
}));

function renderSheet(open = true) {
  const onclose = vi.fn();
  const rendered = render(Sheet, { props: { open, onclose, title: 'Pair', children } });
  return { ...rendered, onclose };
}

function renderCenteredSheet(onclose?: () => void) {
  const props = { open: true, title: 'Pair', placement: 'center' as const, children };
  return render(Sheet, { props: onclose ? { ...props, onclose } : props });
}

function renderUndismissableSheet() {
  return render(Sheet, { props: { open: true, label: 'Allow “iPhone”?', children } });
}

function dialog(): HTMLElement {
  return screen.getByRole('dialog');
}

async function press(key: string, shiftKey = false) {
  const target = document.activeElement ?? document.body;
  await fireEvent.keyDown(target, { key, shiftKey });
}

function grabber(): Element {
  const element = dialog().querySelector('.grabber');
  if (element === null) throw new Error('grabber not rendered');
  return element;
}

function scrollContent(scrollTop: number) {
  Object.defineProperty(dialog(), 'scrollTop', { configurable: true, value: scrollTop });
}

function touchEvent(type: 'touchstart' | 'touchmove', clientY: number): TouchEvent {
  const event = new TouchEvent(type, { bubbles: true, cancelable: true });
  Object.defineProperty(event, 'touches', { value: [{ clientY }] });
  return event;
}

function isTouchMoveHeld(from: Element, distance: number): boolean {
  from.dispatchEvent(touchEvent('touchstart', 100));
  const move = touchEvent('touchmove', 100 + distance);
  from.dispatchEvent(move);
  return move.defaultPrevented;
}

async function drag(distance: number, from: Element = dialog()) {
  await fireEvent.pointerDown(from, { clientY: 100, isPrimary: true, button: 0, pointerId: 1 });
  await fireEvent.pointerMove(window, { clientY: 100 + distance, pointerId: 1 });
  await fireEvent.pointerUp(window, { clientY: 100 + distance, pointerId: 1 });
}

describe('Sheet', () => {
  it('renders nothing while closed', () => {
    renderSheet(false);
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('is a labelled modal dialog with the title as heading', () => {
    renderSheet();
    expect(dialog().getAttribute('aria-modal')).toBe('true');
    expect(screen.getByRole('dialog', { name: 'Pair' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Pair' })).toBeTruthy();
  });

  it('is named by the label when it has no title', () => {
    renderUndismissableSheet();
    expect(screen.getByRole('dialog', { name: 'Allow “iPhone”?' })).toBeTruthy();
    expect(screen.queryByRole('heading')).toBeNull();
  });

  it('moves focus to the panel, not a control, and keeps Tab inside', async () => {
    renderSheet();
    const first = screen.getByRole('button', { name: 'First' });
    const last = screen.getByRole('button', { name: 'Last' });
    expect(document.activeElement).toBe(dialog());
    await press('Tab', true);
    expect(document.activeElement).toBe(last);
    await press('Tab');
    expect(document.activeElement).toBe(first);
    await press('Tab', true);
    expect(document.activeElement).toBe(last);
  });

  it('closes on Escape', async () => {
    const { onclose } = renderSheet();
    await press('Escape');
    expect(onclose).toHaveBeenCalledOnce();
  });

  it('closes on a scrim tap', async () => {
    const { onclose, container } = renderSheet();
    const scrim = container.querySelector('.scrim');
    if (scrim === null) throw new Error('scrim not rendered');
    await fireEvent.click(scrim);
    expect(onclose).toHaveBeenCalledOnce();
  });

  it('closes when dragged down more than 80 px', async () => {
    const { onclose } = renderSheet();
    await drag(81);
    expect(onclose).toHaveBeenCalledOnce();
  });

  it('stays open on Escape, scrim tap and drag when it has no onclose', async () => {
    const { container } = renderUndismissableSheet();
    await press('Escape');
    const scrim = container.querySelector('.scrim');
    if (scrim === null) throw new Error('scrim not rendered');
    await fireEvent.click(scrim);
    await drag(120);
    expect(screen.getByRole('dialog')).toBeTruthy();
    expect(dialog().style.transform).toBe('');
  });

  it('springs back when dragged 80 px or less', async () => {
    const { onclose } = renderSheet();
    await drag(80);
    expect(onclose).not.toHaveBeenCalled();
    expect(dialog().style.transform).toBe('');
  });

  it('follows the pointer while dragging', async () => {
    renderSheet();
    await fireEvent.pointerDown(dialog(), { clientY: 0, isPrimary: true, button: 0, pointerId: 1 });
    await fireEvent.pointerMove(window, { clientY: 30, pointerId: 1 });
    expect(dialog().style.transform).toBe('translateY(30px)');
  });

  it('snaps back instead of dismissing when the pointer is canceled', async () => {
    const { onclose } = renderSheet();
    await fireEvent.pointerDown(dialog(), { clientY: 0, isPrimary: true, button: 0, pointerId: 1 });
    await fireEvent.pointerMove(window, { clientY: 120, pointerId: 1 });
    await fireEvent.pointerCancel(window, { clientY: 120, pointerId: 1 });
    expect(onclose).not.toHaveBeenCalled();
    expect(dialog().style.transform).toBe('');
  });

  it('leaves a drag on scrolled content to scrolling', async () => {
    const { onclose } = renderSheet();
    scrollContent(120);
    await drag(120);
    expect(onclose).not.toHaveBeenCalled();
    expect(dialog().style.transform).toBe('');
  });

  it('leaves an upward swipe to scrolling even if it turns down later', async () => {
    const { onclose } = renderSheet();
    await fireEvent.pointerDown(dialog(), {
      clientY: 100,
      isPrimary: true,
      button: 0,
      pointerId: 1,
    });
    await fireEvent.pointerMove(window, { clientY: 90, pointerId: 1 });
    await fireEvent.pointerMove(window, { clientY: 220, pointerId: 1 });
    await fireEvent.pointerUp(window, { clientY: 220, pointerId: 1 });
    expect(onclose).not.toHaveBeenCalled();
    expect(dialog().style.transform).toBe('');
  });

  it('drags from the grabber even when the content is scrolled', async () => {
    const { onclose } = renderSheet();
    scrollContent(120);
    await drag(81, grabber());
    expect(onclose).toHaveBeenCalledOnce();
  });

  it('holds the native scroll only for a downward touch at the top or on the grabber', () => {
    renderSheet();
    expect(isTouchMoveHeld(dialog(), 20)).toBe(true);
    expect(isTouchMoveHeld(dialog(), -20)).toBe(false);
    scrollContent(120);
    expect(isTouchMoveHeld(dialog(), 20)).toBe(false);
    expect(isTouchMoveHeld(grabber(), 20)).toBe(true);
  });

  it('takes over a running entrance when touched', async () => {
    renderSheet();
    const finish = vi.fn();
    dialog().getAnimations = () => [{ finish } as unknown as Animation];
    await fireEvent.pointerDown(dialog(), { clientY: 0, isPrimary: true, button: 0, pointerId: 1 });
    await fireEvent.pointerMove(window, { clientY: 10, pointerId: 1 });
    expect(finish).toHaveBeenCalledOnce();
    expect(dialog().style.transform).toBe('translateY(10px)');
  });

  it('returns focus to the opener when closed', async () => {
    const opener = document.createElement('button');
    document.body.append(opener);
    opener.focus();
    const { rerender } = renderSheet();
    expect(document.activeElement).not.toBe(opener);
    await rerender({ open: false });
    await tick();
    expect(document.activeElement).toBe(opener);
  });

  describe('centered', () => {
    it('has no grabber and keeps its heading', () => {
      renderCenteredSheet(vi.fn());
      expect(dialog().querySelector('.grabber')).toBeNull();
      expect(dialog().classList.contains('centered')).toBe(true);
      expect(screen.getByRole('heading', { name: 'Pair' })).toBeTruthy();
    });

    it('traps focus like the bottom sheet', async () => {
      renderCenteredSheet(vi.fn());
      expect(document.activeElement).toBe(dialog());
      await press('Tab', true);
      expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Last' }));
      await press('Tab');
      expect(document.activeElement).toBe(screen.getByRole('button', { name: 'First' }));
    });

    it('ignores drags', async () => {
      const onclose = vi.fn();
      renderCenteredSheet(onclose);
      await fireEvent.pointerDown(dialog(), {
        clientY: 0,
        isPrimary: true,
        button: 0,
        pointerId: 1,
      });
      await fireEvent.pointerMove(window, { clientY: 200, pointerId: 1 });
      expect(dialog().style.transform).toBe('');
      await fireEvent.pointerUp(window, { clientY: 200, pointerId: 1 });
      expect(onclose).not.toHaveBeenCalled();
    });

    it('closes on Escape and the scrim when dismissable', async () => {
      const onclose = vi.fn();
      const { container } = renderCenteredSheet(onclose);
      await press('Escape');
      const scrim = container.querySelector('.scrim');
      if (scrim === null) throw new Error('scrim not rendered');
      await fireEvent.click(scrim);
      expect(onclose).toHaveBeenCalledTimes(2);
    });

    it('stays open without onclose', async () => {
      renderCenteredSheet();
      await press('Escape');
      expect(screen.getByRole('dialog')).toBeTruthy();
    });
  });
});
