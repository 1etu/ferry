<script lang="ts">
  import { type Snippet } from 'svelte';
  import { collapseOut, riseIn } from '$lib/motion/transitions';
  import RowSubtitle from './RowSubtitle.svelte';
  import { pressable } from './press';

  type Props = {
    title: string;
    subtitle?: string;
    subtitleKey?: string;
    tone?: 'default' | 'error';
    onclick?: () => void;
    href?: string;
    target?: '_self' | '_blank';
    trailing?: Snippet;
    leading?: Snippet;
    index?: number;
    highlight?: boolean;
  };

  const {
    title,
    subtitle,
    subtitleKey,
    tone = 'default',
    onclick,
    href,
    target = '_self',
    trailing,
    leading,
    index = 0,
    highlight = false,
  }: Props = $props();

  const id = $props.id();
  const titleId = `${id}-title`;
  const subtitleId = `${id}-subtitle`;

  let isPressed = $state(false);
</script>

<li
  class="row"
  class:has-leading={leading !== undefined}
  class:pressed={isPressed}
  class:highlight
  in:riseIn={{ index }}
  out:collapseOut
>
  {#if leading}
    <span class="leading">{@render leading()}</span>
  {/if}
  <div class="content">
    <span class="title text-body" id={titleId}>{title}</span>
    {#if subtitle}
      <RowSubtitle id={subtitleId} text={subtitle} key={subtitleKey} isError={tone === 'error'} />
    {/if}
  </div>
  {#if href}
    <a
      class="action"
      {href}
      {target}
      rel={target === '_blank' ? 'noopener' : undefined}
      aria-labelledby={titleId}
      aria-describedby={subtitle ? subtitleId : undefined}
      {@attach pressable((pressed) => (isPressed = pressed))}
    ></a>
  {:else if onclick}
    <button
      type="button"
      class="action"
      aria-labelledby={titleId}
      aria-describedby={subtitle ? subtitleId : undefined}
      {onclick}
      {@attach pressable((pressed) => (isPressed = pressed))}
    ></button>
  {/if}
  <span class="trailing">{@render trailing?.()}</span>
</li>

<style>
  .row {
    position: relative;
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(var(--hit), auto);
    align-items: center;
    gap: var(--space-3);
    min-block-size: var(--row-min-height);
    padding: var(--row-padding-block) var(--row-padding-inline);
    transition: background-color var(--fade-ms) var(--fade);
  }

  .has-leading {
    grid-template-columns: var(--row-leading) minmax(0, 1fr) minmax(var(--hit), auto);
  }

  .row:not(:first-child)::after {
    content: '';
    position: absolute;
    inset-block-start: 0;
    inset-inline: var(--row-padding-inline) 0;
    block-size: var(--hairline);
    background: var(--separator);
  }

  .pressed,
  .row:has(> .action:active) {
    background: var(--quaternary-system-fill);
    transition: none;
  }

  .highlight {
    animation: arrival-highlight var(--highlight-ms) var(--fade);
  }

  @keyframes arrival-highlight {
    from {
      background-color: var(--quaternary-system-fill);
    }
  }

  .leading {
    display: grid;
    place-items: center;
    color: var(--label);
  }

  .content {
    display: flex;
    flex-direction: column;
    min-inline-size: 0;
  }

  .title {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
    color: var(--label);
  }

  .action {
    position: absolute;
    inset: 0;
    padding: 0;
    border: 0;
    background: none;
    -webkit-touch-callout: none;
  }

  .action:focus-visible {
    outline-offset: calc(-1 * var(--focus-ring-width));
  }

  .trailing {
    position: relative;
    display: grid;
    place-items: center;
    margin-block: calc(-1 * var(--row-trailing-bleed));
    margin-inline-end: calc((var(--ring-size) - var(--hit)) / 2);
  }

  .trailing > :global(*) {
    grid-area: 1 / 1;
  }

  .trailing:has(> :global([role='switch'], input)) {
    justify-items: end;
    margin-inline-end: 0;
  }

  .trailing:has(> :global(.text-button)) {
    justify-items: end;
    margin-inline-end: calc(-1 * var(--row-padding-inline));
  }

  .trailing > :global(.text-button) {
    padding-inline-end: var(--row-padding-inline);
  }
</style>
