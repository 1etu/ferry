<script lang="ts">
  import { type OfferedFile } from '$lib/api/client';
  import { files } from '$lib/files.svelte';
  import { formatBytes } from '$lib/format';
  import IconButton from '$lib/ui/IconButton.svelte';
  import Row from '$lib/ui/Row.svelte';

  type Props = { file: OfferedFile; index: number };

  const { file, index }: Props = $props();
</script>

<Row title={file.name} subtitle={formatBytes(file.size)} {index}>
  {#snippet trailing()}
    <IconButton
      icon="x"
      label="Remove"
      variant="plain"
      tone="destructive"
      onclick={() => void files.remove(file.id)}
    />
  {/snippet}
</Row>
