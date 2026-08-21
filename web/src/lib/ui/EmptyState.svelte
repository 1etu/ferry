<script lang="ts">
  type Props = { text: string };

  const { text }: Props = $props();
</script>

<p class="empty-state text-title-3">{text}</p>

<style>
  .empty-state {
    display: grid;
    flex: 1;
    place-items: center;
    margin: 0;
    padding: var(--space-10) var(--margin);
    color: var(--tertiary-label);
    text-align: center;
  }
</style>
