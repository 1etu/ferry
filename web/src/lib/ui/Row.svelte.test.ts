import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import { createRawSnippet } from 'svelte';
import Row from './Row.svelte';

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

afterEach(() => {
  cleanup();
});

function subtitleId(text: string): string | undefined {
  return screen.getByText(text).closest('.subtitle')?.id;
}

function rowElement(container: HTMLElement): HTMLLIElement {
  const row = container.querySelector('li');
  if (row === null) throw new Error('row not rendered');
  return row;
}

describe('Row', () => {
  it('renders title and subtitle without a button when it has no action', () => {
    render(Row, { props: { title: 'IMG_0001.HEIC', subtitle: '12 MB' } });
    expect(screen.getByText('IMG_0001.HEIC')).toBeTruthy();
    expect(screen.getByText('12 MB')).toBeTruthy();
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('makes the whole row a button named by the title and described by the subtitle', async () => {
    const onclick = vi.fn();
    render(Row, { props: { title: 'Report.pdf', subtitle: '42%', onclick } });
    const button = screen.getByRole('button', { name: 'Report.pdf' });
    expect(button.getAttribute('aria-describedby')).toBe(subtitleId('42%'));
    await fireEvent.click(button);
    expect(onclick).toHaveBeenCalledOnce();
  });

  it('covers the row with a link instead of a button when it has an href', () => {
    const href = '/api/files/f1/content';
    render(Row, { props: { title: 'Report.pdf', subtitle: '12 MB', href, target: '_blank' } });
    const link = screen.getByRole('link', { name: 'Report.pdf' });
    expect(link.getAttribute('href')).toBe(href);
    expect(link.getAttribute('target')).toBe('_blank');
    expect(link.getAttribute('rel')).toBe('noopener');
    expect(link.getAttribute('aria-describedby')).toBe(subtitleId('12 MB'));
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('opens a link in the same context by default without a rel', () => {
    render(Row, { props: { title: 'a', href: '/api/files/f1/content' } });
    const link = screen.getByRole('link');
    expect(link.getAttribute('target')).toBe('_self');
    expect(link.getAttribute('rel')).toBeNull();
  });

  it('colors the subtitle for the error tone only', () => {
    render(Row, { props: { title: 'a', subtitle: 'Failed', tone: 'error' } });
    render(Row, { props: { title: 'b', subtitle: 'Canceled' } });
    expect(screen.getByText('Failed').classList.contains('error')).toBe(true);
    expect(screen.getByText('Canceled').classList.contains('error')).toBe(false);
  });

  it('updates the subtitle in place under one key and replaces it when the key changes', async () => {
    const { rerender } = render(Row, {
      props: { title: 'a', subtitle: '42%', subtitleKey: 'active' },
    });
    const first = screen.getByText('42%');
    await rerender({ title: 'a', subtitle: '43%', subtitleKey: 'active' });
    expect(screen.getByText('43%')).toBe(first);
    await rerender({ title: 'a', subtitle: 'Sent', subtitleKey: 'done' });
    expect(screen.getByText('Sent')).not.toBe(first);
    expect(subtitleId('Sent')).toBeTruthy();
  });

  it('renders the trailing and leading snippets', () => {
    const trailing = createRawSnippet(() => ({ render: () => '<button>Retry</button>' }));
    const leading = createRawSnippet(() => ({ render: () => '<span>glyph</span>' }));
    const { container } = render(Row, { props: { title: 'a', trailing, leading } });
    expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy();
    expect(screen.getByText('glyph')).toBeTruthy();
    expect(rowElement(container).classList.contains('has-leading')).toBe(true);
  });

  it('shows press feedback while the pointer is down', async () => {
    const { container } = render(Row, { props: { title: 'a', onclick: vi.fn() } });
    const button = screen.getByRole('button');
    await fireEvent.pointerDown(button, { isPrimary: true, button: 0 });
    expect(rowElement(container).classList.contains('pressed')).toBe(true);
    await fireEvent.pointerUp(button, { isPrimary: true, button: 0 });
    expect(rowElement(container).classList.contains('pressed')).toBe(false);
  });

  it('plays the arrival highlight only when asked', () => {
    const { container } = render(Row, { props: { title: 'a', highlight: true } });
    const quiet = render(Row, { props: { title: 'b' } });
    expect(rowElement(container).classList.contains('highlight')).toBe(true);
    expect(rowElement(quiet.container).classList.contains('highlight')).toBe(false);
  });

  it('replays the highlight when it turns on later', async () => {
    const { container, rerender } = render(Row, { props: { title: 'a' } });
    await rerender({ title: 'a', highlight: true });
    expect(rowElement(container).classList.contains('highlight')).toBe(true);
  });
});
