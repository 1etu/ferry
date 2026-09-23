import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import TextField from './TextField.svelte';

afterEach(() => {
  cleanup();
});

function renderField(value = 'egetu-pc') {
  const oncommit = vi.fn();
  const rendered = render(TextField, { props: { value, oncommit, label: 'Name', maxlength: 63 } });
  return { ...rendered, oncommit };
}

function field(): HTMLInputElement {
  const element = screen.getByRole('textbox', { name: 'Name' });
  if (!(element instanceof HTMLInputElement)) throw new Error('not an input');
  return element;
}

async function type(text: string) {
  await fireEvent.input(field(), { target: { value: text } });
}

async function press(key: string) {
  await fireEvent.keyDown(field(), { key });
}

describe('TextField', () => {
  it('is a labelled text input showing the value', () => {
    renderField();
    expect(field().type).toBe('text');
    expect(field().value).toBe('egetu-pc');
    expect(field().maxLength).toBe(63);
  });

  it('commits the trimmed value on Enter', async () => {
    const { oncommit } = renderField();
    await type('  Studio  ');
    await press('Enter');
    expect(oncommit).toHaveBeenCalledExactlyOnceWith('Studio');
    expect(field().value).toBe('Studio');
  });

  it('commits on blur', async () => {
    const { oncommit } = renderField();
    await type('Studio');
    await fireEvent.blur(field());
    expect(oncommit).toHaveBeenCalledExactlyOnceWith('Studio');
  });

  it('does not commit an unchanged value', async () => {
    const { oncommit } = renderField();
    await type(' egetu-pc ');
    await press('Enter');
    await fireEvent.blur(field());
    expect(oncommit).not.toHaveBeenCalled();
  });

  it('does not commit the same value twice', async () => {
    const { oncommit } = renderField();
    await type('Studio');
    await press('Enter');
    await fireEvent.blur(field());
    expect(oncommit).toHaveBeenCalledOnce();
  });

  it('restores the last committed value on Escape', async () => {
    const { oncommit } = renderField();
    await type('Studio');
    await press('Enter');
    await type('Office');
    await press('Escape');
    expect(field().value).toBe('Studio');
    await fireEvent.blur(field());
    expect(oncommit).toHaveBeenCalledOnce();
  });

  it('lets Escape through when there is nothing to restore', () => {
    renderField();
    const escape = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true });
    const reachedParent = vi.fn();
    document.body.addEventListener('keydown', reachedParent);
    field().dispatchEvent(escape);
    document.body.removeEventListener('keydown', reachedParent);
    expect(reachedParent).toHaveBeenCalledOnce();
    expect(escape.defaultPrevented).toBe(false);
  });

  it('shows a new value from the parent', async () => {
    const { rerender } = renderField();
    await rerender({ value: 'Studio' });
    expect(field().value).toBe('Studio');
  });
});
