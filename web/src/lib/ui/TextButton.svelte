<script lang="ts">
  import ProgressRing from './ProgressRing.svelte';
  import { pressable } from './press';

  type Props = {
    label: string;
    tone?: 'default' | 'destructive';
    onclick: () => void;
    busy?: boolean;
  };

  const { label, tone = 'default', onclick, busy = false }: Props = $props();

  let isPressed = $state(false);

  function press() {
    if (!busy) onclick();
  }
</script>

<button
  type="button"
  class="text-button text-body"
  class:destructive={tone === 'destructive'}
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
  .text-button {
    display: grid;
    place-items: center;
    min-block-size: var(--hit);
    padding: 0 var(--space-4);
    border: 0;
    border-radius: var(--radius-m);
    background: none;
    color: var(--system-blue);
    -webkit-user-select: none;
    user-select: none;
    transition: opacity var(--spring-quick-ms) var(--spring-quick);
  }

  .text-button > * {
    grid-area: 1 / 1;
  }

  .destructive {
    --ring-ink: var(--system-red);

    color: var(--system-red);
  }

  .text-button:not(.busy):is(.pressed, :active) {
    opacity: var(--press-opacity);
  }

  .busy {
    --ring-size: var(--busy-ring-size);

    cursor: default;
  }

  .busy .label {
    visibility: hidden;
  }

  .spinner {
    display: grid;
  }
</style>
