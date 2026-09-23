<script lang="ts" generics="SectionId extends string">
  import { type IconName } from '$lib/icons/nodes';
  import SidebarItem from './SidebarItem.svelte';

  type Section = { id: SectionId; label: string; icon: IconName };

  type Props = {
    items: Section[];
    selected: SectionId;
    onselect: (id: SectionId) => void;
  };

  const { items, selected, onselect }: Props = $props();

  const stepByKey: Partial<Record<string, number>> = { ArrowDown: 1, ArrowUp: -1 };

  function moveSelection(event: KeyboardEvent, nav: HTMLElement) {
    const step = stepByKey[event.key];
    if (step === undefined || event.altKey || event.ctrlKey || event.metaKey) return;
    event.preventDefault();
    const buttons = Array.from(nav.querySelectorAll('button'));
    const focused = buttons.findIndex((button) => button === document.activeElement);
    const from = focused >= 0 ? focused : items.findIndex((section) => section.id === selected);
    const target = Math.min(Math.max(from + step, 0), items.length - 1);
    const next = items[target];
    if (next === undefined) return;
    if (next.id !== selected) onselect(next.id);
    buttons[target]?.focus();
  }

  function arrowKeys(nav: HTMLElement) {
    const handleKeydown = (event: KeyboardEvent) => {
      moveSelection(event, nav);
    };
    nav.addEventListener('keydown', handleKeydown);
    return () => {
      nav.removeEventListener('keydown', handleKeydown);
    };
  }
</script>

<aside class="sidebar">
  <svg class="wordmark" viewBox="0 0 162 44" fill="currentColor" role="img" aria-label="Ferry">
    <path d="M0 10H26A13 13 0 0 1 0 10ZM44 34H18A13 13 0 0 1 44 34Z" />
    <path
      d="M62 34L56 34L56 0.14L78.77 0.14L78.77 5.34L62 5.34L62 14.59L77.57 14.59L77.57 19.68L62 19.68L62 34ZM92.49 34.55L92.49 34.55Q88.9 34.55 86.26 32.93Q83.63 31.32 82.21 28.49Q80.78 25.66 80.78 22.02L80.78 22.02Q80.78 18.36 82.25 15.53Q83.72 12.7 86.31 11.08Q88.9 9.45 92.26 9.45L92.26 9.45Q95.72 9.45 98.31 11.05Q100.9 12.64 102.33 15.44Q103.76 18.25 103.76 21.93L103.76 21.93L103.76 23.5L86.4 23.5Q86.51 26.43 88.13 28.23Q89.74 30.02 92.63 30.02L92.63 30.02Q94.78 30.02 96.19 29.08Q97.6 28.14 98.1 26.59L98.1 26.59L103.47 26.59Q103.01 28.93 101.48 30.73Q99.94 32.52 97.63 33.53Q95.31 34.55 92.49 34.55ZM86.44 19.55L86.44 19.55L98.26 19.55Q97.99 16.98 96.41 15.49Q94.83 14 92.35 14L92.35 14Q89.85 14 88.3 15.49Q86.74 16.98 86.44 19.55ZM112.91 34L107.18 34L107.18 10L112.71 10L112.71 13.98L112.77 13.98Q113.43 11.93 114.85 10.85Q116.27 9.77 118.59 9.77L118.59 9.77Q119.18 9.77 119.65 9.81Q120.11 9.84 120.46 9.86L120.46 9.86L120.46 14.95Q120.14 14.91 119.36 14.84Q118.59 14.77 117.75 14.77L117.75 14.77Q115.71 14.77 114.31 16.18Q112.91 17.59 112.91 20.41L112.91 20.41L112.91 34ZM129.01 34L123.29 34L123.29 10L128.81 10L128.81 13.98L128.88 13.98Q129.54 11.93 130.96 10.85Q132.38 9.77 134.69 9.77L134.69 9.77Q135.29 9.77 135.75 9.81Q136.22 9.84 136.56 9.86L136.56 9.86L136.56 14.95Q136.24 14.91 135.47 14.84Q134.69 14.77 133.85 14.77L133.85 14.77Q131.81 14.77 130.41 16.18Q129.01 17.59 129.01 20.41L129.01 20.41L129.01 34ZM143.57 43.5L139.82 43.5L139.82 38.84L142.66 38.84Q143.82 38.84 144.47 38.22Q145.12 37.59 145.62 36.18L145.62 36.18L146.5 33.86L137.25 10L143.32 10L147.73 22.5Q148.18 23.84 148.62 25.17Q149.05 26.5 149.46 27.82L149.46 27.82Q150.27 25.18 151.18 22.5L151.18 22.5L155.57 10L161.57 10L150.84 37.95Q148.71 43.5 143.57 43.5L143.57 43.5Z"
    />
  </svg>
  <nav class="items" aria-label="Sections" {@attach arrowKeys}>
    {#each items as section (section.id)}
      <SidebarItem
        label={section.label}
        icon={section.icon}
        isSelected={section.id === selected}
        onselect={() => {
          onselect(section.id);
        }}
      />
    {/each}
  </nav>
</aside>

<style>
  .sidebar {
    display: flex;
    flex-direction: column;
    flex: none;
    gap: var(--space-5);
    inline-size: var(--sidebar-width);
    min-block-size: 100%;
    padding: var(--space-5) var(--space-2);
    border-inline-end: var(--hairline) solid var(--separator);
    background: var(--window-background);
    color: var(--label);
  }

  .wordmark {
    display: block;
    block-size: var(--wordmark-height);
    inline-size: auto;
    margin-inline: var(--space-2);
    align-self: flex-start;
  }

  .items {
    display: flex;
    flex-direction: column;
  }
</style>
