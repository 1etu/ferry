<script lang="ts">
  import IconButton from '$lib/ui/IconButton.svelte';
  import Sheet from '$lib/ui/Sheet.svelte';
  import PairingPanel from './PairingPanel.svelte';

  type Props = { open: boolean; onclose: () => void };

  const { open, onclose }: Props = $props();
</script>

<Sheet {open} {onclose} title="Pair" placement="center">
  <div class="close">
    <IconButton icon="x" label="Close" variant="plain" onclick={onclose} />
  </div>
  <div class="body">
    <PairingPanel />
  </div>
</Sheet>

<style>
  .close {
    position: absolute;
    inset-block-start: 0;
    inset-inline-end: 0;
    color: var(--secondary-label);
  }

  .close :global(.icon-button) {
    color: inherit;
  }

  .body {
    display: grid;
    justify-items: center;
    padding-block: var(--space-2) var(--space-2);
  }
</style>
