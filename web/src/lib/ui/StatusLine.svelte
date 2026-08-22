<script lang="ts">
  import { fadeIn } from '$lib/motion/transitions';

  type Props = { text: string; tone?: 'default' | 'error' };

  const { text, tone = 'default' }: Props = $props();
</script>

{#if text}
  <p class="status-line text-footnote" class:error={tone === 'error'} transition:fadeIn>
    {text}
  </p>
{/if}

<style>
  .status-line {
    margin: 0;
    padding: 0 var(--margin);
    color: var(--secondary-label);
    text-align: center;
  }

  .error {
    color: var(--system-red);
  }
</style>
