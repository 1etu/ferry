<script lang="ts">
  import Icon from '$lib/icons/Icon.svelte';

  type Props = { isIncoming: boolean };

  const { isIncoming }: Props = $props();
</script>

<span class="direction"><Icon name={isIncoming ? 'arrowDown' : 'arrowUp'} /></span>

<style>
  .direction {
    display: grid;
    place-items: center;
    inline-size: var(--row-leading);
    block-size: var(--row-leading);
    border-radius: var(--radius-pill);
    background: var(--tertiary-system-fill);
    color: var(--label);
  }
</style>
