<script lang="ts">
  import { fadeIn } from '$lib/motion/transitions';
  import { session } from '$lib/session.svelte';
  import IconButton from '$lib/ui/IconButton.svelte';
  import PillButton from '$lib/ui/PillButton.svelte';
  import StatusLine from '$lib/ui/StatusLine.svelte';
  import TopBar from '$lib/ui/TopBar.svelte';
  import { uploadQueue } from '$lib/uploads/queue.svelte';
  import { isConvertedPhoto } from './entries';
  import MoreSheet from './MoreSheet.svelte';
  import TransferList from './TransferList.svelte';

  const originalsHint = 'For originals, choose Options › Current when picking photos.';
  const originalsHintMs = 8000;

  let sendInput: HTMLInputElement;
  let resumeInput: HTMLInputElement;
  let useResumeFile: ((file: File) => void) | undefined;
  let isMoreOpen = $state(false);
  let isShowingOriginalsHint = $state(false);
  let originalsHintTimer: ReturnType<typeof setTimeout> | undefined;

  const title = $derived(session.server?.name ?? 'Ferry');

  const statusText = $derived.by(() => {
    if (!session.isConnected) return 'Not Connected';
    return isShowingOriginalsHint ? originalsHint : '';
  });

  $effect(() => () => {
    clearTimeout(originalsHintTimer);
  });

  function showOriginalsHintOnce(chosen: readonly File[]) {
    const now = new Date();
    if (session.originalsHintShown || !chosen.some((file) => isConvertedPhoto(file, now))) return;
    session.markOriginalsHint();
    isShowingOriginalsHint = true;
    originalsHintTimer = setTimeout(() => {
      isShowingOriginalsHint = false;
    }, originalsHintMs);
  }

  function takeFiles(input: HTMLInputElement): File[] {
    const chosen = Array.from(input.files ?? []);
    input.value = '';
    return chosen;
  }

  function handleSendSelect() {
    const chosen = takeFiles(sendInput);
    if (chosen.length === 0) return;
    showOriginalsHintOnce(chosen);
    void uploadQueue.add(chosen);
  }

  function pickResumeFile(use: (file: File) => void) {
    useResumeFile = use;
    resumeInput.click();
  }

  function handleResumeSelect() {
    const [file] = takeFiles(resumeInput);
    const use = useResumeFile;
    useResumeFile = undefined;
    if (file !== undefined) use?.(file);
  }
</script>

<div class="screen" in:fadeIn|global>
  <TopBar {title}>
    {#snippet trailing()}
      <IconButton icon="ellipsis" label="More" onclick={() => (isMoreOpen = true)} />
    {/snippet}
  </TopBar>
  <div class="status">
    <div class="status-line"><StatusLine text={statusText} /></div>
  </div>
  <TransferList onpickfile={pickResumeFile} />
  <input
    bind:this={sendInput}
    class="file-input"
    type="file"
    multiple
    tabindex="-1"
    aria-hidden="true"
    onchange={handleSendSelect}
  />
  <input
    bind:this={resumeInput}
    class="file-input"
    type="file"
    tabindex="-1"
    aria-hidden="true"
    onchange={handleResumeSelect}
  />
  <PillButton
    label="Send"
    onclick={() => {
      sendInput.click();
    }}
  />
</div>

<MoreSheet open={isMoreOpen} onclose={() => (isMoreOpen = false)} />

<style>
  .screen {
    display: flex;
    flex-direction: column;
    min-block-size: 100dvh;
    padding-inline: var(--safe-left) var(--safe-right);
  }

  .status {
    position: relative;
  }

  .status-line {
    position: absolute;
    inset-inline: 0;
    inset-block-start: calc(-1 * var(--space-4));
  }

  .file-input {
    position: absolute;
    inline-size: 1px;
    block-size: 1px;
    opacity: 0;
    pointer-events: none;
  }
</style>
