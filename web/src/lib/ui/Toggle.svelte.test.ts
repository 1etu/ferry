import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import Toggle from './Toggle.svelte';

afterEach(() => {
  cleanup();
});

function renderToggle(checked: boolean, disabled = false) {
  const onchange = vi.fn();
  const rendered = render(Toggle, {
    props: { checked, onchange, label: 'Start at Login', disabled },
  });
  return { ...rendered, onchange };
}

function toggle(): HTMLElement {
  return screen.getByRole('switch', { name: 'Start at Login' });
}

describe('Toggle', () => {
  it('is a native button exposed as a labelled switch', () => {
    renderToggle(false);
    expect(toggle().tagName).toBe('BUTTON');
    expect(toggle().getAttribute('type')).toBe('button');
    expect(toggle().getAttribute('aria-checked')).toBe('false');
  });

  it('reports the opposite state when activated', async () => {
    const off = renderToggle(false);
    await fireEvent.click(toggle());
    expect(off.onchange).toHaveBeenCalledWith(true);
    off.unmount();
    const on = renderToggle(true);
    expect(toggle().getAttribute('aria-checked')).toBe('true');
    await fireEvent.click(toggle());
    expect(on.onchange).toHaveBeenCalledWith(false);
  });

  it('follows the checked prop instead of flipping itself', async () => {
    const { rerender, onchange } = renderToggle(false);
    await fireEvent.click(toggle());
    expect(onchange).toHaveBeenCalledOnce();
    expect(toggle().getAttribute('aria-checked')).toBe('false');
    await rerender({ checked: true });
    expect(toggle().getAttribute('aria-checked')).toBe('true');
  });

  it('is natively disabled when disabled', () => {
    renderToggle(false, true);
    expect(toggle().hasAttribute('disabled')).toBe(true);
  });

  it('is keyboard focusable', () => {
    renderToggle(false);
    toggle().focus();
    expect(document.activeElement).toBe(toggle());
  });
});
