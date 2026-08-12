<script lang="ts">
  import { pressable } from './press';

  type Props = {
    label: string;
    variant: 'primary' | 'secondary' | 'destructive';
    onclick: () => void;
  };

  const { label, variant, onclick }: Props = $props();

  let isPressed = $state(false);
</script>

<button
  type="button"
  class="filled-button text-headline {variant}"
  {onclick}
  class:pressed={isPressed}
  {@attach pressable((pressed) => (isPressed = pressed))}
>
  {label}
</button>

<style>
  .filled-button {
    inline-size: 100%;
    block-size: var(--filled-button-height);
    padding: 0 var(--space-4);
    border: 0;
    border-radius: var(--radius-m);
    -webkit-user-select: none;
    user-select: none;
    transition: transform var(--spring-snappy-ms) var(--spring-snappy);
  }

  .filled-button:is(.pressed, :active) {
    transform: scale(var(--press-scale));
    transition: transform var(--spring-quick-ms) var(--spring-quick);
  }

  .primary {
    background: var(--system-blue);
    color: var(--white);
  }

  .secondary {
    background: var(--tertiary-system-fill);
    color: var(--label);
  }

  .destructive {
    background: var(--tertiary-system-fill);
    color: var(--system-red);
  }
</style>
