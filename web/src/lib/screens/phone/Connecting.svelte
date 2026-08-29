<script lang="ts">
  import { fadeIn } from '$lib/motion/transitions';
  import ProgressRing from '$lib/ui/ProgressRing.svelte';
  import TopBar from '$lib/ui/TopBar.svelte';
</script>

<div class="screen" in:fadeIn|global>
  <TopBar title="Ferry" />
  <div class="center">
    <ProgressRing />
  </div>
</div>

<style>
  .screen {
    display: flex;
    flex-direction: column;
    min-block-size: 100dvh;
    padding-inline: var(--safe-left) var(--safe-right);
  }

  .center {
    display: grid;
    flex: 1;
    place-items: center;
    padding-block-end: calc(var(--safe-bottom) + var(--bar-height));
  }
</style>
