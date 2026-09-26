<script lang="ts">
  import { api, call, type UpdateStatus } from '$lib/api/client';
  import { settings } from '$lib/settings.svelte';
  import ListGroup from '$lib/ui/ListGroup.svelte';
  import Row from '$lib/ui/Row.svelte';
  import TextButton from '$lib/ui/TextButton.svelte';
  import TextField from '$lib/ui/TextField.svelte';
  import Toggle from '$lib/ui/Toggle.svelte';
  import { update } from '$lib/update.svelte';

  const nameMaxLength = 64;
  const pathMaxLength = 44;
  const ellipsis = '…';

  let isPickingFolder = $state(false);

  const current = $derived(settings.current);
  const status = $derived(update.status);
  const isReady = $derived(status?.state === 'ready');

  function middleTruncated(path: string): string {
    if (path.length <= pathMaxLength) return path;
    const kept = pathMaxLength - ellipsis.length;
    const head = Math.ceil(kept / 2);
    return `${path.slice(0, head)}${ellipsis}${path.slice(path.length - (kept - head))}`;
  }

  function updateSubtitle(state: UpdateStatus | undefined): string {
    switch (state?.state) {
      case 'idle':
        return state.checkedAt === undefined ? '' : 'Up to date';
      case 'checking':
        return 'Checking';
      case 'downloading':
        return `Downloading ${state.available ?? ''}`.trim();
      case 'ready':
        return 'Ready to install';
      case 'failed':
        return 'Could not update';
      case 'disabled':
      case undefined:
        return '';
    }
  }

  async function rename(name: string) {
    const isRejected = name === '' || (await settings.patch({ name })) !== undefined;
    if (isRejected) await settings.load();
  }

  async function pickFolder() {
    isPickingFolder = true;
    try {
      await settings.pickReceivedDir();
    } finally {
      isPickingFolder = false;
    }
  }

  function openReceivedFolder() {
    void call(api.POST('/api/received/open'));
  }
</script>

{#if current}
  <div class="groups">
    <ListGroup header="General">
      <Row title="Name">
        {#snippet trailing()}
          <TextField
            value={current.name}
            label="Name"
            maxlength={nameMaxLength}
            oncommit={(name: string) => void rename(name)}
          />
        {/snippet}
      </Row>
      <Row title="Start at Login">
        {#snippet trailing()}
          <Toggle
            checked={current.startAtLogin}
            label="Start at Login"
            onchange={(startAtLogin: boolean) => void settings.patch({ startAtLogin })}
          />
        {/snippet}
      </Row>
    </ListGroup>
    <ListGroup header="Files">
      <Row
        title="Received Files"
        subtitle={middleTruncated(current.receivedDir)}
        onclick={openReceivedFolder}
      >
        {#snippet trailing()}
          {#if settings.canPickReceivedDir}
            <TextButton label="Change…" busy={isPickingFolder} onclick={() => void pickFolder()} />
          {/if}
        {/snippet}
      </Row>
    </ListGroup>
    <ListGroup header="Updates">
      <Row title="Check Automatically">
        {#snippet trailing()}
          <Toggle
            checked={current.checkUpdates}
            label="Check Automatically"
            onchange={(checkUpdates: boolean) => void settings.patch({ checkUpdates })}
          />
        {/snippet}
      </Row>
      <Row
        title="Ferry {status?.current ?? ''}"
        subtitle={updateSubtitle(status)}
        tone={status?.state === 'failed' ? 'error' : 'default'}
      >
        {#snippet trailing()}
          {#if isReady}
            <TextButton label="Restart to Update" onclick={() => void update.apply()} />
          {:else}
            <TextButton label="Check for Updates" onclick={() => void update.check()} />
          {/if}
        {/snippet}
      </Row>
    </ListGroup>
    <a
      class="link text-footnote"
      href="https://github.com/1etu/ferry"
      target="_blank"
      rel="noopener"
    >
      github.com/1etu/ferry
    </a>
  </div>
{/if}

<style>
  .groups {
    display: flex;
    flex-direction: column;
    margin-inline: calc(-1 * var(--margin));
  }

  .link {
    align-self: flex-start;
    margin-inline-start: calc(var(--margin) + var(--row-padding-inline));
    color: var(--tertiary-label);
    text-decoration: none;
  }

  .link:hover {
    color: var(--secondary-label);
  }
</style>
