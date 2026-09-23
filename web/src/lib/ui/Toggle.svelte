<script lang="ts">
  type Props = {
    checked: boolean;
    onchange: (checked: boolean) => void;
    label: string;
    disabled?: boolean;
  };

  const { checked, onchange, label, disabled = false }: Props = $props();
</script>

<button
  type="button"
  class="toggle"
  role="switch"
  aria-checked={checked}
  aria-label={label}
  {disabled}
  onclick={() => {
    onchange(!checked);
  }}
>
  <span class="knob"></span>
</button>

<style>
  .toggle {
    position: relative;
    display: block;
    flex: none;
    inline-size: var(--toggle-width);
    block-size: var(--toggle-height);
    padding: 0;
    border: 0;
    border-radius: var(--radius-pill);
    background: var(--tertiary-system-fill);
    transition: background-color var(--fade-ms) var(--fade);
  }

  .toggle::before {
    content: '';
    position: absolute;
    inset-inline: 0;
    inset-block: calc((var(--toggle-height) - var(--hit)) / 2);
  }

  .toggle[aria-checked='true'] {
    background: var(--system-blue);
  }

  .toggle:disabled {
    opacity: var(--dimmed-opacity);
  }

  .knob {
    position: absolute;
    inset-block-start: calc((var(--toggle-height) - var(--toggle-knob)) / 2);
    inset-inline-start: calc((var(--toggle-height) - var(--toggle-knob)) / 2);
    inline-size: var(--toggle-knob);
    block-size: var(--toggle-knob);
    border-radius: var(--radius-pill);
    background: var(--inverse-background);
    transition: transform var(--spring-snappy-ms) var(--spring-snappy);
  }

  .toggle[aria-checked='true'] .knob {
    transform: translateX(calc(var(--toggle-width) - var(--toggle-height)));
  }
</style>
