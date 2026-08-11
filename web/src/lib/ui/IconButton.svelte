<script lang="ts">
  import Icon from '$lib/icons/Icon.svelte';
  import { type IconName } from '$lib/icons/nodes';
  import { pressable } from './press';

  type Props = {
    icon: IconName;
    label: string;
    onclick: () => void;
    disabled?: boolean;
    variant?: 'filled' | 'plain';
    tone?: 'default' | 'destructive';
  };

  const {
    icon,
    label,
    onclick,
    disabled = false,
    variant = 'filled',
    tone = 'default',
  }: Props = $props();

  let isPressed = $state(false);
</script>

<button
  type="button"
  class="icon-button"
  class:plain={variant === 'plain'}
  class:destructive={tone === 'destructive'}
  aria-label={label}
  {disabled}
  {onclick}
  class:pressed={isPressed}
  {@attach pressable((pressed) => (isPressed = pressed))}
>
  <Icon name={icon} />
</button>

<style>
  .icon-button {
    display: grid;
    place-items: center;
    inline-size: var(--hit);
    block-size: var(--hit);
    padding: 0;
    border: 0;
    border-radius: var(--radius-pill);
    background: var(--tertiary-system-fill);
    color: var(--label);
    -webkit-user-select: none;
    user-select: none;
    transition: transform var(--spring-snappy-ms) var(--spring-snappy);
  }

  .plain {
    background: none;
    color: var(--system-blue);
  }

  .destructive {
    color: var(--system-red);
  }

  .icon-button:not(:disabled):is(.pressed, :active) {
    transform: scale(var(--press-scale));
    transition: transform var(--spring-quick-ms) var(--spring-quick);
  }

  .icon-button:disabled {
    color: var(--tertiary-label);
  }
</style>
