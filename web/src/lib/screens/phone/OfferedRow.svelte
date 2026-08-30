<script lang="ts">
  import { type OfferedFile } from '$lib/api/client';
  import { formatBytes } from '$lib/format';
  import Icon from '$lib/icons/Icon.svelte';
  import { isStandalone } from '$lib/session.svelte';
  import Row from '$lib/ui/Row.svelte';

  type Props = { file: OfferedFile; index: number; highlight: boolean };

  const { file, index, highlight }: Props = $props();

  const opensInAppBrowser = isStandalone();

  const href = $derived(`/api/files/${encodeURIComponent(file.id)}/content`);
</script>

<Row
  title={file.name}
  subtitle={formatBytes(file.size)}
  {href}
  target={opensInAppBrowser ? '_blank' : '_self'}
  {index}
  {highlight}
>
  {#snippet trailing()}
    <span class="glyph"><Icon name="arrowDown" /></span>
  {/snippet}
</Row>

<style>
  .glyph {
    display: grid;
    place-items: center;
    inline-size: var(--hit);
    block-size: var(--hit);
    color: var(--system-blue);
    pointer-events: none;
  }
</style>
