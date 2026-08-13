<script lang="ts">
  import QRCode from './QRCode.svelte';

  type Props = { qrValue: string; code: string };

  const { qrValue, code }: Props = $props();

  const groupSize = 3;
  const thinSpace = String.fromCodePoint(0x2009);

  const groupedCode = $derived(`${code.slice(0, groupSize)}${thinSpace}${code.slice(groupSize)}`);
</script>

<div class="card">
  <QRCode value={qrValue} />
  <p class="code text-title-2 text-tabular">{groupedCode}</p>
</div>

<style>
  .card {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--space-4);
    inline-size: min(100% - 2 * var(--margin), var(--card-max));
    margin-inline: auto;
    padding: var(--space-6);
    border-radius: var(--radius-xl);
    background: var(--qr-card);
  }

  .code {
    margin: 0;
    color: var(--qr-ink);
  }
</style>
