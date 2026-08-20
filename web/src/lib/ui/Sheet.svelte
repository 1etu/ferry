<script lang="ts">
  import { type Snippet } from 'svelte';
  import { snappy, type SpringSpec } from '$lib/motion/spring';
  import { fadeIn, sheetIn, sheetOut, type SheetPlacement } from '$lib/motion/transitions';
  import { sheetDrag } from './sheet-drag';

  type Props = {
    open: boolean;
    onclose?: () => void;
    title?: string;
    label?: string;
    entrance?: SpringSpec;
    placement?: SheetPlacement;
    children: Snippet;
  };

  const {
    open,
    onclose,
    title,
    label,
    entrance = snappy,
    placement = 'bottom',
    children,
  }: Props = $props();

  const dismissDistancePx = 80;
  const focusableSelector =
    'a[href], button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])';

  let dragOffset = $state(0);
  let isDragging = $state(false);
  let opener: HTMLElement | undefined;
  let handle = $state<HTMLElement>();

  function wrapFocus(event: KeyboardEvent, panel: HTMLElement) {
    const focusable = Array.from(panel.querySelectorAll<HTMLElement>(focusableSelector));
    const first = focusable.at(0);
    const last = focusable.at(-1);
    if (first === undefined || last === undefined) {
      event.preventDefault();
      return;
    }
    const isLeavingStart = document.activeElement === first || document.activeElement === panel;
    if (event.shiftKey && isLeavingStart) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  }

  function trapFocus(panel: HTMLElement) {
    opener = document.activeElement instanceof HTMLElement ? document.activeElement : undefined;
    panel.focus();
    const handleKeydown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && onclose) {
        event.preventDefault();
        onclose();
      } else if (event.key === 'Tab') {
        wrapFocus(event, panel);
      }
    };
    panel.addEventListener('keydown', handleKeydown);
    return () => {
      panel.removeEventListener('keydown', handleKeydown);
    };
  }

  const followDrag = sheetDrag({
    handle: () => handle,
    onmove: (offset) => {
      isDragging = true;
      dragOffset = offset;
    },
    onrelease: () => {
      isDragging = false;
      if (dragOffset > dismissDistancePx && onclose) onclose();
      else dragOffset = 0;
    },
    oncancel: () => {
      isDragging = false;
      dragOffset = 0;
    },
  });

  const isCentered = $derived(placement === 'center');

  $effect(() => {
    if (open) {
      dragOffset = 0;
      return;
    }
    const target = opener;
    opener = undefined;
    if (target?.isConnected) target.focus();
  });
</script>

{#if open}
  <div class="scrim" aria-hidden="true" onclick={onclose} transition:fadeIn></div>
  <div
    class="panel"
    class:centered={isCentered}
    class:dragging={isDragging}
    role="dialog"
    aria-modal="true"
    aria-label={title ?? label}
    tabindex="-1"
    style:transform={dragOffset > 0 ? `translateY(${String(dragOffset)}px)` : undefined}
    in:sheetIn={{ spring: entrance, placement }}
    out:sheetOut={{ fromPx: dragOffset, placement }}
    {@attach trapFocus}
    {@attach !isCentered && followDrag}
  >
    <div class="handle" bind:this={handle}>
      {#if !isCentered}
        <div class="grabber"></div>
      {/if}
      {#if title}
        <h2 class="title text-headline">{title}</h2>
      {/if}
    </div>
    {@render children()}
  </div>
{/if}

<style>
  .scrim {
    position: fixed;
    inset: 0;
    z-index: var(--z-scrim);
    background: var(--scrim);
    touch-action: none;
  }

  .panel {
    --system-grouped-background: var(--system-grouped-background-elevated);
    --secondary-system-grouped-background: var(--secondary-system-grouped-background-elevated);
    --tertiary-system-grouped-background: var(--tertiary-system-grouped-background-elevated);

    position: fixed;
    inset-inline: 0;
    inset-block-end: calc(-1 * var(--space-10));
    z-index: var(--z-sheet);
    max-inline-size: var(--content-max);
    max-block-size: calc(100dvh - var(--safe-top));
    margin-inline: auto;
    padding: var(--space-2) var(--margin)
      calc(var(--safe-bottom) + var(--space-4) + var(--space-10));
    overflow-y: auto;
    overscroll-behavior: contain;
    border-start-start-radius: var(--radius-xl);
    border-start-end-radius: var(--radius-xl);
    background: var(--system-grouped-background);
    color: var(--label);
    touch-action: pan-y;
    transition: transform var(--spring-snappy-ms) var(--spring-snappy);
  }

  .centered {
    inset: 0;
    inline-size: min(100% - 2 * var(--margin), var(--content-max));
    block-size: fit-content;
    max-block-size: calc(100% - 2 * var(--margin));
    margin: auto;
    padding: var(--space-2) var(--margin) var(--margin);
    border-radius: var(--radius-l);
  }

  .panel:focus-visible {
    outline: none;
  }

  .dragging {
    -webkit-user-select: none;
    user-select: none;
    transition: none;
  }

  .handle {
    touch-action: none;
  }

  .grabber {
    inline-size: var(--grabber-width);
    block-size: var(--grabber-height);
    margin-inline: auto;
    border-radius: var(--radius-pill);
    background: var(--tertiary-label);
  }

  .title {
    margin: 0;
    padding: var(--space-3);
    text-align: center;
  }
</style>
