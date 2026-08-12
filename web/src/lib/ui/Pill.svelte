<script lang="ts">
  import Icon from '$lib/icons/Icon.svelte';
  import { pressable } from './press';

  type Props = { text: string; copyValue: string };

  const { text, copyValue }: Props = $props();

  const confirmationMs = 1500;

  let isCopied = $state(false);
  let resetTimer: ReturnType<typeof setTimeout> | undefined;

  function showConfirmation() {
    clearTimeout(resetTimer);
    isCopied = true;
    resetTimer = setTimeout(() => {
      isCopied = false;
    }, confirmationMs);
  }

  function hideConfirmation() {
    clearTimeout(resetTimer);
    isCopied = false;
  }

  function copyToClipboard() {
    navigator.clipboard.writeText(copyValue).then(showConfirmation, hideConfirmation);
  }

  $effect(() => () => {
    clearTimeout(resetTimer);
  });

  let isPressed = $state(false);
</script>

<button
  type="button"
  class="pill text-subheadline text-tabular"
  class:copied={isCopied}
  onclick={copyToClipboard}
  class:pressed={isPressed}
  {@attach pressable((pressed) => (isPressed = pressed))}
>
  <span class="text">{text}</span>
  <span class="glyphs">
    <span class="glyph copy"><Icon name="copy" size={16} /></span>
    <span class="glyph check"><Icon name="check" size={16} /></span>
  </span>
</button>

<style>
  .pill {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    max-inline-size: 100%;
    block-size: var(--pill-height);
    padding: 0 var(--space-3) 0 var(--space-4);
    border: 0;
    border-radius: var(--radius-pill);
    background: var(--tertiary-system-fill);
    color: var(--label);
    -webkit-user-select: none;
    user-select: none;
    transition: transform var(--spring-snappy-ms) var(--spring-snappy);
  }

  .pill::before {
    content: '';
    position: absolute;
    inset: calc((var(--pill-height) - var(--hit)) / 2) 0;
  }

  .pill:is(.pressed, :active) {
    transform: scale(var(--press-scale));
    transition: transform var(--spring-quick-ms) var(--spring-quick);
  }

  .text {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .glyphs {
    display: grid;
  }

  .glyph {
    grid-area: 1 / 1;
    transition:
      opacity var(--fade-ms) var(--fade),
      transform var(--spring-bouncy-ms) var(--spring-bouncy);
  }

  .copy {
    color: var(--secondary-label);
  }

  .check {
    color: var(--system-green);
    opacity: 0;
    transform: scale(var(--copy-swap-scale));
  }

  .copied .copy {
    opacity: 0;
    transform: scale(var(--copy-swap-scale));
  }

  .copied .check {
    opacity: 1;
    transform: none;
  }
</style>
