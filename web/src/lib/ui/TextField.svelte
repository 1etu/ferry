<script lang="ts">
  type Props = {
    value: string;
    oncommit: (value: string) => void;
    label: string;
    maxlength?: number;
  };

  const { value, oncommit, label, maxlength }: Props = $props();

  let committed = $derived(value);
  let draft = $derived(committed);

  function commit() {
    const next = draft.trim();
    draft = next;
    if (next === committed) return;
    committed = next;
    oncommit(next);
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Enter') {
      event.preventDefault();
      commit();
    } else if (event.key === 'Escape' && draft !== committed) {
      event.preventDefault();
      event.stopPropagation();
      draft = committed;
    }
  }
</script>

<input
  type="text"
  class="text-field text-desktop-body"
  aria-label={label}
  value={draft}
  {maxlength}
  spellcheck="false"
  autocomplete="off"
  oninput={(event) => {
    draft = event.currentTarget.value;
  }}
  onkeydown={handleKeydown}
  onblur={commit}
/>

<style>
  .text-field {
    flex: none;
    inline-size: var(--text-field-width);
    max-inline-size: 100%;
    block-size: var(--text-field-height);
    margin: 0;
    padding: 0 var(--space-2);
    border: 0;
    border-radius: var(--radius-s);
    background: var(--tertiary-system-fill);
    color: var(--label);
  }

  .text-field::placeholder {
    color: var(--placeholder-text);
  }
</style>
