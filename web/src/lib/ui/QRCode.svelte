<script lang="ts">
  import { encode } from 'uqr';

  type Props = { value: string; size?: number };

  const { value, size = 240 }: Props = $props();

  const matrix = $derived(encode(value, { ecc: 'M', border: 0 }));
  const darkModules = $derived(
    matrix.data.flatMap((row, y) => row.flatMap((isDark, x) => (isDark ? [{ x, y }] : []))),
  );
</script>

<svg
  class="qr"
  viewBox="0 0 {matrix.size} {matrix.size}"
  width={size}
  height={size}
  shape-rendering="crispEdges"
  aria-hidden="true"
>
  {#each darkModules as module (module.y * matrix.size + module.x)}
    <rect x={module.x} y={module.y} width="1" height="1" />
  {/each}
</svg>

<style>
  .qr {
    display: block;
    max-inline-size: 100%;
    block-size: auto;
    fill: var(--qr-ink);
  }
</style>
