<script lang="ts">
  import { pressable } from './press';

  type Props = { value?: number; tone?: 'default' | 'stalled'; oncancel?: () => void };

  const { value, tone = 'default', oncancel }: Props = $props();

  const box = 28;
  const center = box / 2;
  const strokeWidth = 2.5;
  const radius = (box - strokeWidth) / 2;
  const circumference = 2 * Math.PI * radius;
  const indeterminateArc = 0.25;
  const stopSide = 8;
  const stopCornerRadius = 1.5;

  const fraction = $derived(value === undefined ? undefined : Math.min(1, Math.max(0, value)));
  const dashOffset = $derived(circumference * (1 - (fraction ?? indeterminateArc)));
  const percent = $derived(fraction === undefined ? undefined : Math.round(fraction * 100));

  let isPressed = $state(false);
</script>

<span
  class="progress"
  class:cancelable={oncancel !== undefined}
  class:stalled={tone === 'stalled'}
  class:pressed={isPressed}
>
  <svg
    class="ring"
    class:spinning={fraction === undefined}
    viewBox="0 0 {box} {box}"
    role="progressbar"
    aria-valuemin={0}
    aria-valuemax={100}
    aria-valuenow={percent}
  >
    <circle class="track" cx={center} cy={center} r={radius} stroke-width={strokeWidth} />
    <circle
      class="fill"
      cx={center}
      cy={center}
      r={radius}
      stroke-width={strokeWidth}
      stroke-dasharray={circumference}
      stroke-dashoffset={dashOffset}
      transform="rotate(-90 {center} {center})"
    />
    {#if oncancel && fraction !== undefined}
      <rect
        class="stop"
        x={center - stopSide / 2}
        y={center - stopSide / 2}
        width={stopSide}
        height={stopSide}
        rx={stopCornerRadius}
      />
    {/if}
  </svg>
  {#if oncancel}
    <button
      type="button"
      class="cancel"
      aria-label="Cancel"
      onclick={oncancel}
      {@attach pressable((pressed) => (isPressed = pressed))}
    ></button>
  {/if}
</span>

<style>
  .progress {
    position: relative;
    display: grid;
    flex: none;
    place-items: center;
  }

  .cancelable {
    inline-size: var(--hit);
    block-size: var(--hit);
    transition: transform var(--spring-snappy-ms) var(--spring-snappy);
  }

  .cancelable:is(.pressed, :has(.cancel:active)) {
    transform: scale(var(--press-scale));
    transition: transform var(--spring-quick-ms) var(--spring-quick);
  }

  .cancel {
    position: absolute;
    inset: 0;
    padding: 0;
    border: 0;
    border-radius: var(--radius-pill);
    background: none;
  }

  .ring {
    display: block;
    inline-size: var(--ring-size);
    block-size: var(--ring-size);
  }

  .spinning {
    animation: spin var(--spin-ms) var(--spin) infinite;
  }

  .track,
  .fill {
    fill: none;
  }

  .track {
    stroke: var(--tertiary-system-fill);
  }

  .fill {
    stroke: var(--ring-ink, var(--system-blue));
    transition:
      stroke-dashoffset var(--progress-ms) var(--spring-smooth),
      stroke var(--fade-ms) var(--fade);
  }

  .stalled .fill {
    stroke: var(--secondary-label);
  }

  .stop {
    fill: var(--ring-ink, var(--system-blue));
  }

  @keyframes spin {
    to {
      transform: rotate(1turn);
    }
  }
</style>
