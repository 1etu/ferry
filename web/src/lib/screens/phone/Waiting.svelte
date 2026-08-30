<script lang="ts">
  import { fadeIn } from '$lib/motion/transitions';
  import { session } from '$lib/session.svelte';
  import ProgressRing from '$lib/ui/ProgressRing.svelte';
  import StatusLine from '$lib/ui/StatusLine.svelte';
  import TextButton from '$lib/ui/TextButton.svelte';
  import TopBar from '$lib/ui/TopBar.svelte';

  const title = $derived(session.server?.name ?? 'Ferry');
</script>

<div class="screen" in:fadeIn|global>
  <TopBar {title} />
  <div class="center">
    <ProgressRing />
    <StatusLine text="Allow this iPhone on your PC" />
  </div>
  <div class="actions">
    <TextButton label="Cancel" onclick={() => void session.forget()} />
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
    display: flex;
    flex: 1;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: var(--space-4);
  }

  .actions {
    display: grid;
    justify-items: center;
    padding-block: var(--space-4) calc(var(--safe-bottom) + var(--space-4));
  }
</style>
