import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { tick } from 'svelte';
import { cleanup, render, screen } from '@testing-library/svelte';
import type { Device, Pairing } from '$lib/api/client';

type PendingAnimation = { onfinish: (() => void) | null; cancel: () => void; effect: null };

const hint = "Scan with your iPhone's camera";
const pairAnother = 'Pair Another iPhone';
const pending: PendingAnimation[] = [];

const current: Pairing = {
  qrUrl: 'http://192.168.1.23:8080/?pair=k3bJz9xQ2mL8vN4pR7tW1a',
  localUrl: 'http://egetu-pc.local:8080',
  code: '483921',
  expiresAt: '2026-10-02T14:13:07.123Z',
};

const iphone: Device = {
  id: '01J9ZK0X5S8V7Q2M3N4P5R6T7A',
  name: 'iPhone',
  status: 'approved',
  createdAt: '2026-10-02T14:01:00.000Z',
};

function heldAnimation(): Animation {
  const animation: PendingAnimation = { onfinish: null, cancel: vi.fn(), effect: null };
  pending.push(animation);
  return animation as unknown as Animation;
}

async function finishAnimations() {
  while (pending.length > 0) {
    for (const animation of pending.splice(0)) animation.onfinish?.();
    await tick();
  }
}

async function loadModules() {
  const [{ pairing }, { default: Devices }] = await Promise.all([
    import('$lib/pairing.svelte'),
    import('./Devices.svelte'),
  ]);
  return { pairing, Devices };
}

beforeAll(() => {
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve(new Response(null, { status: 204 }))),
  );
  Element.prototype.animate = heldAnimation;
});

afterEach(() => {
  cleanup();
  pending.length = 0;
});

afterAll(() => {
  vi.unstubAllGlobals();
});

describe('Devices', () => {
  it('crossfades the pairing panel into the device list on the first approval', async () => {
    const { pairing, Devices } = await loadModules();
    pairing.applyPairing(current);
    render(Devices, { onpair: vi.fn() });
    expect(screen.getByText(hint)).toBeTruthy();
    expect(screen.queryByText(pairAnother)).toBeNull();

    pairing.applyDevice({ action: 'approved', device: iphone });
    await tick();
    expect(screen.getByText(pairAnother)).toBeTruthy();
    expect(screen.getByText(hint)).toBeTruthy();

    await finishAnimations();
    expect(screen.getByText(pairAnother)).toBeTruthy();
    expect(screen.queryByText(hint)).toBeNull();
  });
});
