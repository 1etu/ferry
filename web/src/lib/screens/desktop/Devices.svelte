<script lang="ts">
  import { type TransitionConfig } from 'svelte/transition';
  import { type Device } from '$lib/api/client';
  import Icon from '$lib/icons/Icon.svelte';
  import { fadeIn, riseIn, scaleOut } from '$lib/motion/transitions';
  import { network } from '$lib/network.svelte';
  import { pairing } from '$lib/pairing.svelte';
  import IconButton from '$lib/ui/IconButton.svelte';
  import ListGroup from '$lib/ui/ListGroup.svelte';
  import Row from '$lib/ui/Row.svelte';
  import StatusLine from '$lib/ui/StatusLine.svelte';
  import TextButton from '$lib/ui/TextButton.svelte';
  import PairingPanel from './PairingPanel.svelte';

  type Props = { onpair: () => void };

  type ListedDevice = Device & { status: 'approved' | 'pending' };

  const { onpair }: Props = $props();

  const panelOverlapMs = 100;

  const statusText: Record<ListedDevice['status'], string> = {
    approved: 'Approved',
    pending: 'Waiting',
  };

  let isAllowing = $state(false);

  const isBlocked = $derived(network.state?.firewall === 'blocked');
  const listed = $derived(pairing.devices.filter(isListed));

  function isListed(device: Device): device is ListedDevice {
    return device.status !== 'revoked';
  }

  function riseUnderPanel(node: Element): TransitionConfig {
    return { ...riseIn(node), delay: panelOverlapMs };
  }

  async function allowThroughFirewall() {
    isAllowing = true;
    try {
      await network.allow();
    } finally {
      isAllowing = false;
    }
  }
</script>

{#if isBlocked}
  <div class="firewall" transition:fadeIn>
    <StatusLine text="Blocked by Windows Firewall" tone="error" />
    <span class="allow">
      <TextButton label="Allow" busy={isAllowing} onclick={() => void allowThroughFirewall()} />
    </span>
  </div>
{/if}

<div class="content">
  {#if pairing.hasApprovedDevices}
    <div class="layer devices" in:riseUnderPanel out:fadeIn>
      <ListGroup>
        {#each listed as device, index (device.id)}
          <Row title={device.name} subtitle={statusText[device.status]} {index}>
            {#snippet leading()}
              <Icon name="smartphone" />
            {/snippet}
            {#snippet trailing()}
              <IconButton
                icon="x"
                label="Remove"
                variant="plain"
                tone="destructive"
                onclick={() => void pairing.revoke(device.id)}
              />
            {/snippet}
          </Row>
        {/each}
      </ListGroup>
      <div class="pair-another">
        <TextButton label="Pair Another iPhone" onclick={onpair} />
      </div>
    </div>
  {:else}
    <div class="layer pairing" in:fadeIn out:scaleOut>
      <PairingPanel />
    </div>
  {/if}
</div>

<style>
  .firewall {
    display: flex;
    align-items: center;
    min-block-size: var(--hit);
    margin-block: calc(-1 * var(--space-4)) var(--space-3);
    margin-inline-start: calc(-1 * var(--margin));
  }

  .allow {
    display: flex;
    margin-inline-start: calc(-1 * var(--space-3));
  }

  .content {
    display: grid;
  }

  .layer {
    grid-area: 1 / 1;
    align-self: start;
    min-inline-size: 0;
  }

  .devices {
    margin-inline: calc(-1 * var(--margin));
  }

  .pair-another {
    display: flex;
    margin-block-start: calc(-1 * var(--space-3));
  }
</style>
