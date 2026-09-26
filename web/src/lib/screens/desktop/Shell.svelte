<script lang="ts">
  import { files } from '$lib/files.svelte';
  import { type IconName } from '$lib/icons/nodes';
  import { network } from '$lib/network.svelte';
  import { pairing } from '$lib/pairing.svelte';
  import { session } from '$lib/session.svelte';
  import { settings } from '$lib/settings.svelte';
  import { update } from '$lib/update.svelte';
  import PushButton from '$lib/ui/PushButton.svelte';
  import Sidebar from '$lib/ui/Sidebar.svelte';
  import StatusLine from '$lib/ui/StatusLine.svelte';
  import Activity from './Activity.svelte';
  import ApprovalSheet from './ApprovalSheet.svelte';
  import Devices from './Devices.svelte';
  import PairingSheet from './PairingSheet.svelte';
  import Settings from './Settings.svelte';

  type SectionId = 'devices' | 'activity' | 'settings';

  const sections: { id: SectionId; label: string; icon: IconName }[] = [
    { id: 'devices', label: 'Devices', icon: 'smartphone' },
    { id: 'activity', label: 'Activity', icon: 'arrowUpDown' },
    { id: 'settings', label: 'Settings', icon: 'settings' },
  ];

  let selected = $state<SectionId>('devices');
  let isPairing = $state(false);
  let isPicking = $state(false);

  const title = $derived(sections.find((section) => section.id === selected)?.label ?? '');
  const approvedCount = $derived(
    pairing.devices.filter((device) => device.status === 'approved').length,
  );

  let knownApprovedCount = 0;

  $effect(() => {
    if (approvedCount > knownApprovedCount) isPairing = false;
    knownApprovedCount = approvedCount;
  });

  $effect(() => {
    void Promise.all([settings.load(), update.load(), network.load()]);
  });

  async function sendFiles() {
    isPicking = true;
    try {
      await files.pick();
    } finally {
      isPicking = false;
    }
  }

  function selectByShortcut(event: KeyboardEvent) {
    if (!event.ctrlKey || event.altKey || event.shiftKey || event.metaKey) return;
    const section = sections[Number(event.key) - 1];
    if (section === undefined || document.querySelector('[role="dialog"]') !== null) return;
    event.preventDefault();
    selected = section.id;
  }
</script>

<svelte:window onkeydown={selectByShortcut} />

<div class="shell desktop">
  <Sidebar items={sections} {selected} onselect={(id: SectionId) => (selected = id)} />
  <main class="pane" aria-labelledby="section-title">
    <div class="scroller">
      <div class="column">
        <header class="header">
          <div class="title-row">
            <h1 class="title text-title-1" id="section-title">{title}</h1>
            {#if selected === 'activity' && files.canPick}
              <PushButton label="Send Files…" busy={isPicking} onclick={() => void sendFiles()} />
            {/if}
          </div>
          <div class="connection">
            <StatusLine text={session.isConnected ? '' : 'Not Connected'} />
          </div>
        </header>
        {#if selected === 'devices'}
          <Devices onpair={() => (isPairing = true)} />
        {:else if selected === 'activity'}
          <Activity />
        {:else}
          <Settings />
        {/if}
      </div>
    </div>
  </main>
  <PairingSheet open={isPairing} onclose={() => (isPairing = false)} />
  <ApprovalSheet />
</div>

<style>
  .shell {
    display: flex;
    block-size: 100dvh;
    overflow: hidden;
    background: var(--window-background);
    color: var(--label);
  }

  .pane {
    position: relative;
    flex: 1;
    min-inline-size: 0;
    contain: layout;
  }

  .scroller {
    block-size: 100%;
    overflow-y: auto;
    overscroll-behavior: contain;
  }

  .column {
    display: flex;
    flex-direction: column;
    inline-size: min(100%, 560px);
    min-block-size: 100%;
    padding: var(--space-6);
  }

  .header {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    margin-block-end: var(--space-5);
  }

  .title-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    min-block-size: var(--push-button-height);
  }

  .title {
    min-inline-size: 0;
    margin: 0;
  }

  .connection {
    display: flex;
    margin-inline-start: calc(-1 * var(--margin));
  }
</style>
