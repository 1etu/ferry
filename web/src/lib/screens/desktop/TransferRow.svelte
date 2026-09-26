<script lang="ts">
  import { untrack } from 'svelte';
  import { api, call, type Transfer } from '$lib/api/client';
  import { formatBytes, formatPercent } from '$lib/format';
  import Icon from '$lib/icons/Icon.svelte';
  import { smooth, springEasing } from '$lib/motion/spring';
  import { fadeIn, popIn } from '$lib/motion/transitions';
  import { transfers } from '$lib/transfers.svelte';
  import DirectionGlyph from '$lib/ui/DirectionGlyph.svelte';
  import ProgressRing from '$lib/ui/ProgressRing.svelte';
  import Row from '$lib/ui/Row.svelte';

  type Props = { transfer: Transfer; index: number; isArrival: boolean };

  type Finish = 'pending' | 'landing' | 'fresh' | 'settled';

  const { transfer, index, isArrival }: Props = $props();

  const landingMs = springEasing(smooth).durationMs;
  const freshCheckMs = 1500;
  const stallAfterMs = 15000;

  let finish = $state<Finish>(
    untrack(() => (transfer.status === 'done' && !isArrival ? 'settled' : 'pending')),
  );
  let isHighlighted = $state(untrack(() => isArrival && transfer.direction === 'in'));
  let isStalled = $state(false);

  const status = $derived(transfer.status);
  const done = $derived(transfer.done);
  const size = $derived(transfer.size);
  const isIncoming = $derived(transfer.direction === 'in');
  const showsCheck = $derived(finish === 'fresh' || finish === 'settled');
  const isLanding = $derived(status === 'done' && !showsCheck);
  const fraction = $derived(isLanding ? 1 : progressOf(done, size));
  const showsRing = $derived(status === 'active' || isLanding);
  const subtitleKey = $derived(isLanding ? 'active' : status);
  const subtitle = $derived(isLanding ? formatPercent(1) : subtitleFor(transfer));
  const rowAction = $derived(isIncoming && showsCheck ? { onclick: revealFile } : {});
  const ringAction = $derived(
    isIncoming && status === 'active' ? { oncancel: cancelTransfer } : {},
  );

  $effect(() => {
    if (status !== 'done' || untrack(() => finish) !== 'pending') return;
    finish = 'landing';
    const timers = [
      setTimeout(showCheck, landingMs),
      setTimeout(() => {
        finish = 'settled';
      }, landingMs + freshCheckMs),
    ];
    return () => {
      for (const timer of timers) clearTimeout(timer);
    };
  });

  $effect(() => {
    isStalled = false;
    if (status !== 'active' || done >= size) return;
    const timer = setTimeout(() => {
      isStalled = true;
    }, stallAfterMs);
    return () => {
      clearTimeout(timer);
    };
  });

  function progressOf(sent: number, total: number): number | undefined {
    return sent > 0 && total > 0 ? sent / total : undefined;
  }

  function subtitleFor({ status: current, direction, error }: Transfer): string {
    switch (current) {
      case 'active':
        return formatPercent(size > 0 ? done / size : 0);
      case 'done':
        return direction === 'in' ? formatBytes(size) : 'Sent';
      case 'failed':
        return error === 'file_missing' ? 'Unavailable' : 'Failed';
      case 'canceled':
        return 'Canceled';
    }
  }

  function showCheck() {
    finish = 'fresh';
    if (!isIncoming) return;
    isHighlighted = false;
    requestAnimationFrame(() =>
      requestAnimationFrame(() => {
        isHighlighted = true;
      }),
    );
  }

  function revealFile() {
    void call(api.POST('/api/received/open', { body: { transferId: transfer.id } }));
  }

  function cancelTransfer() {
    void transfers.remove(transfer.id);
  }
</script>

<Row
  title={transfer.name}
  {subtitle}
  {subtitleKey}
  tone={status === 'failed' ? 'error' : 'default'}
  {index}
  highlight={isHighlighted}
  {...rowAction}
>
  {#snippet leading()}
    <DirectionGlyph {isIncoming} />
  {/snippet}
  {#snippet trailing()}
    {#if showsRing && fraction === undefined}
      <span class="glyph" transition:fadeIn><ProgressRing {...ringAction} /></span>
    {:else if showsRing && fraction !== undefined}
      <span class="glyph" transition:fadeIn>
        <ProgressRing value={fraction} tone={isStalled ? 'stalled' : 'default'} {...ringAction} />
      </span>
    {:else if showsCheck}
      <span class="glyph check" class:fresh={finish === 'fresh'} in:popIn>
        <Icon name="check" />
      </span>
    {:else if status === 'failed'}
      <span class="glyph failed" in:fadeIn><Icon name="alert" /></span>
    {/if}
  {/snippet}
</Row>

<style>
  .glyph {
    display: grid;
    place-items: center;
  }

  .check {
    color: var(--secondary-label);
    transition: color var(--fade-ms) var(--fade);
  }

  .check.fresh {
    color: var(--system-green);
  }

  .failed {
    color: var(--system-red);
  }
</style>
