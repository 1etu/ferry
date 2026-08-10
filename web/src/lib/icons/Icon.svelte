<script lang="ts">
  import { nodes, type IconName } from './nodes';

  type Props = { name: IconName; size?: 16 | 20 | 24 };

  const { name, size = 20 }: Props = $props();
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="2"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
>
  {#each nodes[name] as [tag, attributes], index (index)}
    <svelte:element this={tag} {...attributes} />
  {/each}
</svg>

<style>
  svg {
    display: block;
    flex: none;
  }
</style>
