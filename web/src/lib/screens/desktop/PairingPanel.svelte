<script lang="ts">
  import { type TransitionConfig } from 'svelte/transition';
  import { fadeIn } from '$lib/motion/transitions';
  import { pairing } from '$lib/pairing.svelte';
  import Card from '$lib/ui/Card.svelte';
  import Pill from '$lib/ui/Pill.svelte';

  const current = $derived(pairing.current);
  const host = $derived(current === undefined ? undefined : new URL(current.localUrl).host);

  function holdUnderFade(node: Element): TransitionConfig {
    return { duration: fadeIn(node).duration ?? 0 };
  }
</script>

{#if current}
  <div class="panel">
    <div class="cards">
      {#key current.qrUrl}
        <div class="card" in:fadeIn out:holdUnderFade>
          <Card qrValue={current.qrUrl} code={current.code} />
        </div>
      {/key}
    </div>
    {#if host}
      <Pill text={host} copyValue={current.localUrl} />
    {/if}
    <p class="hint text-footnote">Scan with your iPhone's camera</p>
  </div>
{/if}

<style>
  .panel {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-3);
    inline-size: min(
      var(--card-max) + 2 * var(--margin),
      100dvh - 5 * var(--space-10) - var(--space-2)
    );
    max-inline-size: calc(100% + 2 * var(--margin));
    margin-inline: calc(-1 * var(--margin));
  }

  .cards {
    display: grid;
    inline-size: 100%;
    margin-block-end: var(--space-2);
  }

  .card {
    grid-area: 1 / 1;
  }

  .hint {
    margin: 0;
    color: var(--secondary-label);
    text-align: center;
  }
</style>
