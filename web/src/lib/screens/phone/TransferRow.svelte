<script lang="ts">
  import { untrack } from 'svelte';
  import { formatBytes, formatPercent } from '$lib/format';
  import Icon from '$lib/icons/Icon.svelte';
  import { smooth, springEasing } from '$lib/motion/spring';
  import { fadeIn, popIn } from '$lib/motion/transitions';
  import DirectionGlyph from '$lib/ui/DirectionGlyph.svelte';
  import ProgressRing from '$lib/ui/ProgressRing.svelte';
  import Row from '$lib/ui/Row.svelte';
  import { type TransferPhase } from './entries';

  type Props = {
    name: string;
    size: number;
    sent: number;
    isUpload: boolean;
    phase: TransferPhase;
    index: number;
    oncancel: () => void;
    onretry: () => void;
  };

  type Glyph = 'spinner' | 'ring' | 'check' | 'retry' | 'alert' | 'none';

  const { name, size, sent, isUpload, phase, index, oncancel, onretry }: Props = $props();

  const ringLandingMs = springEasing(smooth).durationMs;
  const freshCheckMs = 1500;

  let isLanding = $state(false);
  let isCheckFresh = $state(false);
  let shownPhase = untrack(() => phase);
  let landingTimer: ReturnType<typeof setTimeout> | undefined;
  let freshTimer: ReturnType<typeof setTimeout> | undefined;

  const fraction = $derived(size > 0 ? Math.min(1, sent / size) : 0);
  const isMoving = $derived(phase === 'queued' || phase === 'active' || phase === 'stalled');
  const isRetryable = $derived(isUpload && (phase === 'needsFile' || phase === 'failed'));

  function glyphFor(): Glyph {
    if (isLanding) return 'ring';
    switch (phase) {
      case 'queued':
        return 'spinner';
      case 'active':
      case 'stalled':
        return 'ring';
      case 'needsFile':
        return 'retry';
      case 'done':
        return 'check';
      case 'failed':
        return isUpload ? 'retry' : 'alert';
      case 'canceled':
        return 'none';
    }
  }

  function subtitleFor(): string {
    if (isLanding) return formatPercent(1);
    switch (phase) {
      case 'queued':
        return 'Waiting';
      case 'active':
        return formatPercent(fraction);
      case 'stalled':
        return 'Waiting for network';
      case 'needsFile':
        return 'Tap to resume';
      case 'done':
        return formatBytes(size);
      case 'failed':
        return isUpload ? 'Failed' : 'Unavailable';
      case 'canceled':
        return 'Canceled';
    }
  }

  const glyph = $derived(glyphFor());
  const subtitle = $derived(subtitleFor());
  const subtitleKey = $derived(isLanding ? 'active' : phase);
  const cancel = $derived(isUpload && isMoving && !isLanding ? { oncancel } : {});
  const action = $derived(isRetryable ? { onclick: onretry } : {});

  function showCheck() {
    isLanding = false;
    isCheckFresh = true;
    freshTimer = setTimeout(() => {
      isCheckFresh = false;
    }, freshCheckMs);
  }

  function land() {
    isLanding = true;
    landingTimer = setTimeout(showCheck, ringLandingMs);
  }

  $effect.pre(() => {
    const previous = shownPhase;
    shownPhase = phase;
    const wasMoving = previous === 'queued' || previous === 'active' || previous === 'stalled';
    if (phase === 'done' && wasMoving) land();
  });

  $effect(() => () => {
    clearTimeout(landingTimer);
    clearTimeout(freshTimer);
  });
</script>

<Row
  title={name}
  {subtitle}
  {subtitleKey}
  {index}
  tone={phase === 'failed' ? 'error' : 'default'}
  {...action}
>
  {#snippet leading()}
    <DirectionGlyph isIncoming={!isUpload} />
  {/snippet}
  {#snippet trailing()}
    <span class="glyphs">
      {#if glyph === 'spinner'}
        <span class="glyph" transition:fadeIn><ProgressRing {...cancel} /></span>
      {:else if glyph === 'ring'}
        <span class="glyph" transition:fadeIn>
          <ProgressRing
            value={isLanding ? 1 : fraction}
            tone={phase === 'stalled' ? 'stalled' : 'default'}
            {...cancel}
          />
        </span>
      {:else if glyph === 'check'}
        <span class="glyph check" class:fresh={isCheckFresh} in:popIn out:fadeIn>
          <Icon name="check" />
        </span>
      {:else if glyph === 'retry'}
        <span class="glyph passive" transition:fadeIn><Icon name="rotateCcw" /></span>
      {:else if glyph === 'alert'}
        <span class="glyph passive alert" transition:fadeIn><Icon name="alert" /></span>
      {/if}
    </span>
  {/snippet}
</Row>

<style>
  .glyphs {
    display: grid;
    place-items: center;
  }

  .glyph {
    display: grid;
    grid-area: 1 / 1;
    place-items: center;
    inline-size: var(--hit);
    block-size: var(--hit);
  }

  .passive,
  .check {
    pointer-events: none;
  }

  .passive {
    color: var(--system-blue);
  }

  .alert {
    color: var(--system-red);
  }

  .check {
    color: var(--secondary-label);
    transition: color var(--fade-ms) var(--fade);
  }

  .check.fresh {
    color: var(--system-green);
  }
</style>
