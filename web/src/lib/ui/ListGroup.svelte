<script lang="ts">
  import { type Snippet } from 'svelte';

  type Props = { header?: string; children: Snippet };

  const { header, children }: Props = $props();
</script>

<section class="list-group">
  {#if header}
    <h2 class="header text-footnote">{header}</h2>
  {/if}
  <ul class="rows">
    {@render children()}
  </ul>
</section>

<style>
  .header {
    margin: 0;
    padding: 0 var(--space-4) var(--space-2) calc(var(--margin) + var(--row-padding-inline));
    color: var(--secondary-label);
    text-transform: uppercase;
  }

  .rows {
    margin: 0 var(--margin) var(--space-5);
    padding: 0;
    overflow: hidden;
    list-style: none;
    border-radius: var(--radius-m);
    background: var(--secondary-system-grouped-background);
  }
</style>
