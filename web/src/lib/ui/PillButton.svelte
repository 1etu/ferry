<script lang="ts">
  import ProgressRing from './ProgressRing.svelte';
  import { pressable } from './press';

  type Props = { label: string; onclick: () => void; disabled?: boolean; busy?: boolean };

  const { label, onclick, disabled = false, busy = false }: Props = $props();

  const fullyVisible = 0.99;

  let spacer: HTMLDivElement;
  let isCovering = $state(false);

  $effect(() => {
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) isCovering = entry.intersectionRatio < fullyVisible;
      },
      { threshold: [0, fullyVisible] },
    );
    observer.observe(spacer);
    return () => {
      observer.disconnect();
    };
  });

  let isPressed = $state(false);
</script>

<div class="spacer" bind:this={spacer}></div>
<div class="action-area" class:material-thin={isCovering}>
  <button
    type="button"
    class="pill text-headline"
    class:busy
    aria-label={label}
    aria-busy={busy}
    disabled={disabled || busy}
    {onclick}
    class:pressed={isPressed}
    {@attach pressable((pressed) => (isPressed = pressed))}
  >
    {#if busy}
      <ProgressRing />
    {:else}
      {label}
    {/if}
  </button>
</div>

<style>
  .spacer {
    block-size: calc(var(--pill-button-height) + 2 * var(--space-4) + var(--safe-bottom));
  }

  .action-area {
    position: fixed;
    inset-inline: 0;
    bottom: 0;
    z-index: var(--z-bar);
    display: grid;
    justify-items: center;
    padding: var(--space-4) 0 calc(var(--safe-bottom) + var(--space-4));
  }

  .pill {
    display: grid;
    place-items: center;
    inline-size: min(100% - 2 * var(--margin), var(--pill-button-max));
    block-size: var(--pill-button-height);
    padding: 0 var(--space-6);
    border: 0;
    border-radius: var(--radius-pill);
    background: var(--inverse-background);
    color: var(--inverse-label);
    -webkit-user-select: none;
    user-select: none;
    transition: transform var(--spring-snappy-ms) var(--spring-snappy);
  }

  .pill:not(:disabled):is(.pressed, :active) {
    transform: scale(var(--press-scale));
    transition: transform var(--spring-quick-ms) var(--spring-quick);
  }

  .pill:disabled:not(.busy) {
    opacity: var(--dimmed-opacity);
  }

  .busy {
    --ring-ink: var(--inverse-label);
    --ring-size: var(--busy-ring-size);
  }
</style>
