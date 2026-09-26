<script lang="ts">
  import { type TransitionConfig } from 'svelte/transition';
  import { files } from '$lib/files.svelte';
  import { collapseOut, expandIn, fadeIn, riseIn } from '$lib/motion/transitions';
  import { transfers } from '$lib/transfers.svelte';
  import EmptyState from '$lib/ui/EmptyState.svelte';
  import ListGroup from '$lib/ui/ListGroup.svelte';
  import TextButton from '$lib/ui/TextButton.svelte';
  import OfferedRow from './OfferedRow.svelte';
  import TransferRow from './TransferRow.svelte';

  const emptyOverlapMs = 100;

  let isSettled = $state(false);

  const hasOffered = $derived(files.items.length > 0);
  const hasTransfers = $derived(transfers.items.length > 0);
  const hasHistory = $derived(transfers.items.some((transfer) => transfer.status !== 'active'));

  $effect(() => {
    const frame = requestAnimationFrame(() => {
      isSettled = true;
    });
    return () => {
      cancelAnimationFrame(frame);
    };
  });

  function riseAfterEmpty(node: Element): TransitionConfig {
    return { ...riseIn(node), delay: emptyOverlapMs };
  }
</script>

<div class="activity">
  {#if !hasOffered && !hasTransfers}
    <div class="layer empty" in:fadeIn out:fadeIn>
      <EmptyState text="No Transfers" />
    </div>
  {:else}
    <div class="layer groups" in:riseAfterEmpty out:fadeIn>
      {#if hasOffered}
        <div class="group" in:expandIn out:collapseOut>
          <ListGroup header="Offered">
            {#each files.items as file, index (file.id)}
              <OfferedRow {file} {index} />
            {/each}
          </ListGroup>
        </div>
      {/if}
      {#if hasTransfers}
        <div class="group" in:riseIn out:collapseOut>
          <ListGroup header="Transfers">
            {#each transfers.items as transfer, index (transfer.id)}
              <TransferRow {transfer} {index} isArrival={isSettled} />
            {/each}
          </ListGroup>
        </div>
      {/if}
      {#if hasHistory}
        <div class="clear" transition:fadeIn>
          <TextButton label="Clear History" onclick={() => void transfers.clearHistory()} />
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .activity {
    display: grid;
    flex: 1;
  }

  .layer {
    grid-area: 1 / 1;
    min-inline-size: 0;
  }

  .empty {
    display: flex;
    flex-direction: column;
  }

  .groups {
    align-self: start;
    margin-inline: calc(-1 * var(--margin));
  }

  .group {
    display: flow-root;
  }

  .clear {
    display: flex;
    margin-block-start: calc(-1 * var(--space-3));
  }
</style>
