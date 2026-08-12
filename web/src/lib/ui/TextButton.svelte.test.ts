import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import TextButton from './TextButton.svelte';

afterEach(() => {
  cleanup();
});

describe('TextButton', () => {
  it('calls onclick when idle', async () => {
    const onclick = vi.fn();
    render(TextButton, { props: { label: 'Change…', onclick } });
    await fireEvent.click(screen.getByRole('button', { name: 'Change…' }));
    expect(onclick).toHaveBeenCalledOnce();
    expect(screen.queryByRole('progressbar')).toBeNull();
  });

  it('keeps its name, shows a spinner and ignores clicks while busy', async () => {
    const onclick = vi.fn();
    render(TextButton, { props: { label: 'Change…', onclick, busy: true } });
    const button = screen.getByRole('button', { name: 'Change…' });
    expect(button.getAttribute('aria-busy')).toBe('true');
    expect(screen.getByRole('progressbar')).toBeTruthy();
    await fireEvent.click(button);
    expect(onclick).not.toHaveBeenCalled();
  });
});
