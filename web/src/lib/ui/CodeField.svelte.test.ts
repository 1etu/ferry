import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte';
import CodeField from './CodeField.svelte';

afterEach(() => {
  cleanup();
});

type FieldState = 'idle' | 'checking' | 'wrong' | 'disabled';

function renderField(value: string, state: FieldState = 'idle') {
  const onchange = vi.fn();
  const oncomplete = vi.fn();
  const rendered = render(CodeField, { props: { value, onchange, oncomplete, state } });
  const input = screen.getByRole<HTMLInputElement>('textbox');
  const cells = Array.from(rendered.container.querySelectorAll('.cell'));
  return { ...rendered, input, cells, onchange, oncomplete };
}

async function typeInto(input: HTMLInputElement, text: string) {
  input.value = text;
  await fireEvent.input(input);
}

describe('CodeField', () => {
  it('asks iOS for the numeric one-time-code keyboard', () => {
    const { input } = renderField('');
    expect(input.getAttribute('inputmode')).toBe('numeric');
    expect(input.getAttribute('autocomplete')).toBe('one-time-code');
    expect(input.getAttribute('pattern')).toBe('[0-9]*');
    expect(input.maxLength).toBe(6);
  });

  it('keeps digits only and reports them', async () => {
    const { input, onchange, oncomplete } = renderField('');
    await typeInto(input, '4a8 3');
    expect(input.value).toBe('483');
    expect(onchange).toHaveBeenLastCalledWith('483');
    expect(oncomplete).not.toHaveBeenCalled();
  });

  it('completes on the sixth digit', async () => {
    const { input, oncomplete } = renderField('48392');
    await typeInto(input, '483921');
    expect(oncomplete).toHaveBeenCalledWith('483921');
  });

  it('mirrors the value into six cells', () => {
    const { cells } = renderField('4839');
    expect(cells.map((cell) => cell.textContent.trim())).toEqual(['4', '8', '3', '9', '', '']);
  });

  it('marks the next empty cell while focused', async () => {
    const { input, cells } = renderField('48');
    await fireEvent.focus(input);
    expect(cells.map((cell) => cell.classList.contains('active'))).toEqual([
      false,
      false,
      true,
      false,
      false,
      false,
    ]);
    await fireEvent.blur(input);
    expect(cells.some((cell) => cell.classList.contains('active'))).toBe(false);
  });

  it('locks input while checking and when disabled', () => {
    expect(renderField('483921', 'checking').input.readOnly).toBe(true);
    cleanup();
    expect(renderField('', 'disabled').input.disabled).toBe(true);
  });

  it('exposes the wrong state for the shake', () => {
    const { container } = renderField('', 'wrong');
    expect(container.querySelector('[data-state="wrong"]')).not.toBeNull();
  });
});
