<script lang="ts">
  import { fadeIn } from '$lib/motion/transitions';
  import { reducedMotion } from '$lib/motion/spring';
  import { session, type PairStatus } from '$lib/session.svelte';
  import CodeField from '$lib/ui/CodeField.svelte';
  import StatusLine from '$lib/ui/StatusLine.svelte';
  import TopBar from '$lib/ui/TopBar.svelte';

  const statusText: Record<PairStatus, string> = {
    idle: 'Enter the code shown on your PC',
    wrong: 'Incorrect code',
    expired: 'Code expired',
    rateLimited: 'Try again in a minute',
    notAllowed: 'Not allowed',
  };

  const hasPhysicalKeyboard = matchMedia('(hover: hover) and (pointer: fine)').matches;

  let code = $state('');
  let isChecking = $state(false);

  const status = $derived(session.state.kind === 'pair' ? session.state.status : 'idle');
  const fieldState = $derived(isChecking ? 'checking' : status === 'wrong' ? 'wrong' : 'idle');

  function handleCodeChange(entered: string) {
    code = entered;
    session.dismissPairStatus();
  }

  async function submitCode(entered: string) {
    isChecking = true;
    await session.pairWithCode(entered);
    isChecking = false;
    if (status !== 'wrong' || reducedMotion.current) code = '';
  }

  function handleCodeComplete(entered: string) {
    void submitCode(entered);
  }

  function handleShakeEnd() {
    if (status === 'wrong') code = '';
  }
</script>

<div class="screen" in:fadeIn|global>
  <TopBar title="Ferry" />
  <StatusLine text={statusText[status]} tone={status === 'wrong' ? 'error' : 'default'} />
  <div class="field" onanimationend={handleShakeEnd}>
    <CodeField
      value={code}
      onchange={handleCodeChange}
      oncomplete={handleCodeComplete}
      state={fieldState}
      autofocus={hasPhysicalKeyboard}
    />
  </div>
</div>

<style>
  .screen {
    display: flex;
    flex-direction: column;
    min-block-size: 100dvh;
    padding-inline: var(--safe-left) var(--safe-right);
  }

  .field {
    padding: var(--space-6) var(--margin) calc(var(--safe-bottom) + var(--space-4));
  }
</style>
