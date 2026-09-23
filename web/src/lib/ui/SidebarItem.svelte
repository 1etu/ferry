<script lang="ts">
  import Icon from '$lib/icons/Icon.svelte';
  import { type IconName } from '$lib/icons/nodes';
  import { pressable } from './press';

  type Props = {
    label: string;
    icon: IconName;
    isSelected: boolean;
    onselect: () => void;
  };

  const { label, icon, isSelected, onselect }: Props = $props();

  let isPressed = $state(false);
</script>

<button
  type="button"
  class="sidebar-item text-desktop-body"
  class:selected={isSelected}
  class:pressed={isPressed}
  aria-current={isSelected ? 'page' : undefined}
  onclick={onselect}
  {@attach pressable((pressed) => (isPressed = pressed))}
>
  <Icon name={icon} size={16} />
  <span class="label">{label}</span>
</button>

<style>
  .sidebar-item {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    inline-size: 100%;
    block-size: var(--sidebar-item-height);
    padding: 0 var(--space-2);
    border: 0;
    border-radius: var(--radius-s);
    background: none;
    color: var(--label);
    text-align: start;
    -webkit-user-select: none;
    user-select: none;
    transition: opacity var(--spring-quick-ms) var(--spring-quick);
  }

  @media (hover: hover) {
    .sidebar-item:hover {
      background: var(--sidebar-hover);
    }
  }

  .sidebar-item.selected {
    background: var(--system-blue);
    color: var(--white);
  }

  .sidebar-item:is(.pressed, :active) {
    opacity: var(--desktop-press-opacity);
  }

  .sidebar-item:focus-visible {
    outline-offset: calc(-1 * var(--focus-ring-width));
  }

  .sidebar-item.selected:focus-visible {
    outline-color: var(--white);
  }

  .label {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
</style>
