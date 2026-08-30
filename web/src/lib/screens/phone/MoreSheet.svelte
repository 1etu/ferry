<script lang="ts">
  import { session } from '$lib/session.svelte';
  import { transfers } from '$lib/transfers.svelte';
  import { uploadQueue, type UploadStatus } from '$lib/uploads/queue.svelte';
  import FilledButton from '$lib/ui/FilledButton.svelte';
  import Sheet from '$lib/ui/Sheet.svelte';
  import TextButton from '$lib/ui/TextButton.svelte';

  type Props = { open: boolean; onclose: () => void };

  const { open, onclose }: Props = $props();

  let isConfirmingForget = $state(false);

  const finishedStatuses: readonly UploadStatus[] = ['done', 'failed', 'canceled'];

  function close() {
    isConfirmingForget = false;
    onclose();
  }

  async function clearHistory() {
    close();
    for (const upload of [...uploadQueue.items]) {
      if (finishedStatuses.includes(upload.status)) uploadQueue.remove(upload.id);
    }
    await transfers.clearHistory();
  }

  async function forget() {
    close();
    await session.forget();
  }
</script>

<Sheet
  {open}
  onclose={close}
  label="More"
  {...isConfirmingForget ? { title: 'Forget This PC?' } : {}}
>
  {#if isConfirmingForget}
    <div class="confirm">
      <FilledButton label="Cancel" variant="secondary" onclick={close} />
      <FilledButton label="Forget" variant="destructive" onclick={() => void forget()} />
    </div>
  {:else}
    <div class="actions">
      <TextButton label="Clear History" onclick={() => void clearHistory()} />
      <TextButton
        label="Forget This PC"
        tone="destructive"
        onclick={() => (isConfirmingForget = true)}
      />
    </div>
  {/if}
</Sheet>

<style>
  .actions {
    display: grid;
    grid-auto-rows: var(--filled-button-height);
    padding-block-start: var(--space-2);
  }

  .confirm {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-3);
    padding-block-start: var(--space-2);
  }
</style>
