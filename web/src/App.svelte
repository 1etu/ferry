<script lang="ts">
  import { onMount } from 'svelte';
  import { session } from '$lib/session.svelte';
  import Shell from '$lib/screens/desktop/Shell.svelte';
  import Connecting from '$lib/screens/phone/Connecting.svelte';
  import Pair from '$lib/screens/phone/Pair.svelte';
  import Waiting from '$lib/screens/phone/Waiting.svelte';
  import Home from '$lib/screens/phone/Home.svelte';

  onMount(() => {
    void session.boot();
    return () => {
      session.close();
    };
  });
</script>

{#if session.state.kind === 'pc'}
  <Shell />
{:else}
  <main>
    {#if session.state.kind === 'connecting'}
      <Connecting />
    {:else if session.state.kind === 'pair'}
      <Pair />
    {:else if session.state.kind === 'waiting'}
      <Waiting />
    {:else if session.state.kind === 'phone'}
      <Home />
    {/if}
  </main>
{/if}

<style>
  main {
    min-height: 100dvh;
  }
</style>
