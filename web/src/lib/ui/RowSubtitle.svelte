<script lang="ts">
  import { type TransitionConfig } from 'svelte/transition';
  import { reducedMotion, smooth, springEasing } from '$lib/motion/spring';
  import { fadeIn } from '$lib/motion/transitions';

  type Props = { id: string; text: string; key: string | undefined; isError: boolean };

  const { id, text, key, isError }: Props = $props();

  function crossfade(node: Element): TransitionConfig {
    if (reducedMotion.current) return fadeIn(node);
    const { durationMs, ease } = springEasing(smooth);
    return { duration: durationMs, easing: ease, css: (t) => `opacity: ${String(t)}` };
  }
</script>

<span class="subtitle text-footnote text-tabular" {id}>
  {#key key}
    <span class="text" class:error={isError} transition:crossfade>{text}</span>
  {/key}
</span>

<style>
  .subtitle {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    min-inline-size: 0;
  }

  .text {
    grid-area: 1 / 1;
    min-inline-size: 0;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    color: var(--secondary-label);
  }

  .text.error {
    color: var(--system-red);
  }
</style>
