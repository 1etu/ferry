<script lang="ts">
  import ProgressRing from './ProgressRing.svelte';
  import { pressable } from './press';

  type Props = { label: string; onclick: () => void; busy?: boolean };

  const { label, onclick, busy = false }: Props = $props();

  let isPressed = $state(false);

  function press() {
    if (!busy) onclick();
  }
</script>

<button
  type="button"
  class="push-button text-body"
  class:busy
  class:pressed={isPressed && !busy}
  aria-label={label}
  aria-busy={busy}
  aria-disabled={busy}
  onclick={press}
  {@attach pressable((pressed) => (isPressed = pressed))}
>
  <span class="label">{label}</span>
  {#if busy}
    <span class="spinner"><ProgressRing /></span>
  {/if}
</button>

<style>
  .push-button {
    position: relative;
    display: grid;
    flex: none;
    place-items: center;
    block-size: var(--push-button-height);
    padding: 0 var(--space-3);
    border: 0;
    border-radius: var(--push-button-radius);
    background: var(--secondary-system-fill);
    color: var(--label);
    white-space: nowrap;
    -webkit-user-select: none;
    user-select: none;
    transition: opacity var(--spring-quick-ms) var(--spring-quick);
  }

  .push-button::before {
    content: '';
    position: absolute;
    inset-inline: 0;
    inset-block: calc((var(--push-button-height) - var(--hit)) / 2);
  }

  .push-button > * {
    grid-area: 1 / 1;
  }

  .push-button:not(.busy):is(.pressed, :active) {
    opacity: var(--desktop-press-opacity);
  }

  .busy {
    --ring-size: var(--busy-ring-size);
    --ring-ink: var(--label);

    cursor: default;
  }

  .busy .label {
    visibility: hidden;
  }

  .spinner {
    display: grid;
  }
</style>
