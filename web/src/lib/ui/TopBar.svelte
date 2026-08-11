<script lang="ts">
  import { type Snippet } from 'svelte';

  type Props = { title: string; leading?: Snippet; trailing?: Snippet };

  const { title, leading, trailing }: Props = $props();

  let bar: HTMLElement;
  let sentinel: HTMLDivElement;
  let isScrolled = $state(false);

  $effect(() => {
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) isScrolled = !entry.isIntersecting;
      },
      { rootMargin: `-${String(bar.offsetHeight)}px 0px 0px 0px` },
    );
    observer.observe(sentinel);
    return () => {
      observer.disconnect();
    };
  });
</script>

<header
  class="bar"
  class:material-thin={isScrolled}
  data-scrolled={isScrolled ? '' : undefined}
  bind:this={bar}
>
  <div class="bar-content">
    <div class="slot">{@render leading?.()}</div>
    <p class="bar-title text-headline" aria-hidden="true">{title}</p>
    <div class="slot">{@render trailing?.()}</div>
  </div>
</header>
<div class="bar-space"></div>
<h1 class="large-title text-title-1">{title}</h1>
<div bind:this={sentinel}></div>

<style>
  .bar {
    position: fixed;
    inset-block-start: 0;
    inset-inline: 0;
    z-index: var(--z-bar);
    padding-block-start: max(var(--safe-top), var(--space-2));
    transition: background-color var(--fade-ms) var(--fade);
  }

  .bar-content {
    display: grid;
    grid-template-columns: var(--hit) minmax(0, 1fr) var(--hit);
    align-items: center;
    gap: var(--space-2);
    inline-size: min(100%, var(--content-max));
    block-size: var(--bar-height);
    margin-inline: auto;
    padding-inline: var(--margin);
  }

  .slot {
    display: grid;
    place-items: center;
  }

  .bar-title {
    margin: 0;
    overflow: hidden;
    color: var(--label);
    text-align: center;
    white-space: nowrap;
    text-overflow: ellipsis;
    opacity: 0;
    transition: opacity var(--fade-ms) var(--fade);
  }

  [data-scrolled] .bar-title {
    opacity: 1;
  }

  .bar-space {
    block-size: calc(max(var(--safe-top), var(--space-2)) + var(--bar-height));
  }

  .large-title {
    margin: var(--space-8) 0 var(--space-5);
    padding-inline: var(--margin);
    color: var(--label);
    text-align: center;
    overflow-wrap: anywhere;
  }
</style>
