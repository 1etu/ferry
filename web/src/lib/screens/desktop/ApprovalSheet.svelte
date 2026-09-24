<script lang="ts">
  import { SvelteSet } from 'svelte/reactivity';
  import { type Device } from '$lib/api/client';
  import { bouncy, smooth, springEasing } from '$lib/motion/spring';
  import { pairing } from '$lib/pairing.svelte';
  import FilledButton from '$lib/ui/FilledButton.svelte';
  import Sheet from '$lib/ui/Sheet.svelte';

  const dismissMs = springEasing(smooth).durationMs;

  const settledIds = new SvelteSet<string>();
  let dismissing = $state<Device>();

  const next = $derived(pairing.requests.find((device) => !settledIds.has(device.id)));
  const presented = $derived(dismissing ?? next);
  const isOpen = $derived(next !== undefined && dismissing === undefined);
  const question = $derived(presented === undefined ? '' : `Allow “${presented.name}”?`);

  $effect(() => {
    if (dismissing === undefined) return;
    const timer = setTimeout(() => {
      dismissing = undefined;
    }, dismissMs);
    return () => {
      clearTimeout(timer);
    };
  });

  async function settle(answer: (id: string) => Promise<boolean>) {
    const device = next;
    if (device === undefined) return;
    settledIds.add(device.id);
    dismissing = device;
    if (!(await answer(device.id))) settledIds.delete(device.id);
  }

  function deny() {
    void settle((id) => pairing.revoke(id));
  }

  function allow() {
    void settle((id) => pairing.approve(id));
  }
</script>

<Sheet open={isOpen} label={question} entrance={bouncy} placement="center">
  {#if presented}
    <div class="approval">
      <h2 class="question text-title-3">{question}</h2>
      <div class="answers">
        <FilledButton label="Don't Allow" variant="destructive" onclick={deny} />
        <FilledButton label="Allow" variant="primary" onclick={allow} />
      </div>
    </div>
  {/if}
</Sheet>

<style>
  .approval {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
    padding-block-start: var(--space-6);
  }

  .question {
    margin: 0;
    color: var(--label);
    text-align: center;
    overflow-wrap: anywhere;
  }

  .answers {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: var(--space-3);
  }
</style>
