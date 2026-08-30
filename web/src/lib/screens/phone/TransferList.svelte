<script lang="ts">
  import { untrack } from 'svelte';
  import { type TransitionConfig } from 'svelte/transition';
  import { files } from '$lib/files.svelte';
  import { collapseOut, expandIn, fadeIn, riseIn } from '$lib/motion/transitions';
  import { transfers } from '$lib/transfers.svelte';
  import EmptyState from '$lib/ui/EmptyState.svelte';
  import ListGroup from '$lib/ui/ListGroup.svelte';
  import { uploadQueue } from '$lib/uploads/queue.svelte';
  import { ArrivalOrder, mergeEntries, type TransferEntry } from './entries';
  import OfferedRow from './OfferedRow.svelte';
  import TransferRow from './TransferRow.svelte';

  type Props = { onpickfile: (use: (file: File) => void) => void };

  const { onpickfile }: Props = $props();

  const emptyStateOverlapMs = 100;

  const entries = $derived(mergeEntries(uploadQueue.items, transfers.items));
  const offeredIds = $derived(files.items.map((file) => file.id));
  const entryKeys = $derived(entries.map((entry) => entry.key));

  const offeredAtStart = new ArrivalOrder(untrack(() => offeredIds));
  const offeredArrivals = new ArrivalOrder(untrack(() => offeredIds));
  const entryArrivals = new ArrivalOrder(untrack(() => entryKeys));
  const offeredIndexes = $derived(offeredArrivals.indexes(offeredIds));
  const entryIndexes = $derived(entryArrivals.indexes(entryKeys));

  $effect(() => {
    offeredArrivals.settle(offeredIds);
    entryArrivals.settle(entryKeys);
  });

  function riseAfterEmptyState(node: Element): TransitionConfig {
    return { ...riseIn(node), delay: emptyStateOverlapMs };
  }

  function cancelEntry({ key, uploadId }: TransferEntry) {
    if (uploadId === undefined) void transfers.remove(key);
    else void uploadQueue.cancel(uploadId);
  }

  function retryEntry({ uploadId }: TransferEntry) {
    if (uploadId === undefined) onpickfile((file) => void uploadQueue.add([file]));
    else onpickfile((file) => void uploadQueue.resume(uploadId, file));
  }
</script>

<div class="content">
  {#if offeredIds.length === 0 && entries.length === 0}
    <div class="empty" transition:fadeIn><EmptyState text="No Transfers" /></div>
  {:else}
    <div class="lists" in:riseAfterEmptyState out:collapseOut>
      {#if offeredIds.length > 0}
        <div class="group" in:expandIn out:collapseOut>
          <ListGroup>
            {#each files.items as file (file.id)}
              <OfferedRow
                {file}
                index={offeredIndexes.get(file.id) ?? 0}
                highlight={offeredAtStart.isNew(file.id)}
              />
            {/each}
          </ListGroup>
        </div>
      {/if}
      {#if entries.length > 0}
        <div class="group" in:riseIn out:collapseOut>
          <ListGroup>
            {#each entries as entry (entry.key)}
              <TransferRow
                name={entry.name}
                size={entry.size}
                sent={entry.sent}
                isUpload={entry.isUpload}
                phase={entry.phase}
                index={entryIndexes.get(entry.key) ?? 0}
                oncancel={() => {
                  cancelEntry(entry);
                }}
                onretry={() => {
                  retryEntry(entry);
                }}
              />
            {/each}
          </ListGroup>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .content {
    display: grid;
    flex: 1;
    grid-template: minmax(0, 1fr) / minmax(0, 1fr);
    inline-size: min(100%, var(--content-max));
    margin-inline: auto;
    padding-block-start: var(--space-3);
  }

  .empty,
  .lists {
    grid-area: 1 / 1;
  }

  .empty {
    display: flex;
    flex-direction: column;
  }

  .lists {
    align-self: start;
  }

  .group {
    display: flow-root;
  }
</style>
