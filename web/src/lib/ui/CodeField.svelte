<script lang="ts">
  type CodeFieldState = 'idle' | 'checking' | 'wrong' | 'disabled';

  type Props = {
    value: string;
    onchange: (value: string) => void;
    oncomplete: (value: string) => void;
    state: CodeFieldState;
    autofocus?: boolean;
  };

  const { value, onchange, oncomplete, state: fieldState, autofocus = false }: Props = $props();

  const codeLength = 6;

  let input: HTMLInputElement;
  let isFocused = $state(false);

  const isLocked = $derived(fieldState === 'checking' || fieldState === 'disabled');
  const digits = $derived(Array.from(Array(codeLength).keys(), (index) => value.charAt(index)));
  const activeIndex = $derived(
    isFocused && !isLocked ? Math.min(value.length, codeLength - 1) : undefined,
  );

  function handleInput() {
    const entered = input.value.replace(/\D/g, '').slice(0, codeLength);
    input.value = entered;
    onchange(entered);
    if (entered.length === codeLength) oncomplete(entered);
  }

  $effect(() => {
    if (autofocus) input.focus();
  });
</script>

<div class="code-field" data-state={fieldState}>
  <input
    bind:this={input}
    class="input text-title-2"
    type="text"
    inputmode="numeric"
    autocomplete="one-time-code"
    maxlength={codeLength}
    pattern="[0-9]*"
    aria-label="Enter the code shown on your PC"
    {value}
    disabled={fieldState === 'disabled'}
    readonly={fieldState === 'checking'}
    oninput={handleInput}
    onfocus={() => (isFocused = true)}
    onblur={() => (isFocused = false)}
  />
  <div class="cells" aria-hidden="true">
    {#each digits as digit, index (index)}
      <span class="cell text-title-2 text-tabular" class:active={index === activeIndex}>
        {digit}
      </span>
    {/each}
  </div>
</div>

<style>
  .code-field {
    position: relative;
    inline-size: fit-content;
    margin-inline: auto;
  }

  .input {
    position: absolute;
    inset: 0;
    inline-size: 100%;
    block-size: 100%;
    padding: 0;
    border: 0;
    opacity: 0;
    background: none;
    color: transparent;
    caret-color: transparent;
  }

  .cells {
    display: flex;
    gap: var(--space-2);
    pointer-events: none;
    transition: opacity var(--fade-ms) var(--fade);
  }

  .cell {
    display: grid;
    place-items: center;
    inline-size: var(--code-cell-width);
    block-size: var(--code-cell-height);
    border-radius: var(--radius-s);
    background: var(--tertiary-system-fill);
    color: var(--label);
  }

  .active {
    outline: var(--focus-ring-width) solid var(--system-blue);
    outline-offset: calc(-1 * var(--focus-ring-width));
  }

  [data-state='checking'] .cells,
  [data-state='disabled'] .cells {
    opacity: var(--dimmed-opacity);
  }

  @media (prefers-reduced-motion: no-preference) {
    [data-state='wrong'] .cells {
      animation: code-shake var(--shake-ms) var(--fade);
    }
  }
</style>
