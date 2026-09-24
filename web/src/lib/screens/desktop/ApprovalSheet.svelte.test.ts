import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import type { Device } from '$lib/api/client';

class DocumentRelativeRequest extends Request {
  constructor(input: RequestInfo | URL, init?: RequestInit) {
    super(typeof input === 'string' ? new URL(input, location.href) : input, init);
  }
}

function finishedAnimation(): Animation {
  const animation = { onfinish: null as (() => void) | null, cancel: vi.fn(), effect: null };
  queueMicrotask(() => animation.onfinish?.());
  return animation as unknown as Animation;
}

function pendingDevice(id: string, name: string): Device {
  return { id, name, status: 'pending', createdAt: `2026-10-02T14:0${id}:00.000Z` };
}

const first = pendingDevice('1', 'iPhone');
const second = pendingDevice('2', 'Ege’s iPhone');
const third = pendingDevice('3', 'iPad');
const fourth = pendingDevice('4', 'iPhone 16');
const sent: string[] = [];
const failing = new Set<string>();
const dismissTimeoutMs = 2000;

function fakeFetch(input: RequestInfo | URL): Promise<Response> {
  if (!(input instanceof Request)) return Promise.reject(new TypeError('unexpected fetch input'));
  const { pathname } = new URL(input.url);
  const request = `${input.method} ${pathname}`;
  sent.push(request);
  if (failing.has(request)) {
    return Promise.resolve(Response.json({ error: { code: 'internal' } }, { status: 500 }));
  }
  const approved = [first, second, third].find((device) => pathname.includes(device.id));
  if (input.method === 'POST' && approved) {
    return Promise.resolve(Response.json({ ...approved, status: 'approved' }));
  }
  return Promise.resolve(new Response(null, { status: 204 }));
}

async function loadModules() {
  const [{ pairing }, { default: ApprovalSheet }] = await Promise.all([
    import('$lib/pairing.svelte'),
    import('./ApprovalSheet.svelte'),
  ]);
  return { pairing, ApprovalSheet };
}

beforeAll(() => {
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
  vi.stubGlobal('Request', DocumentRelativeRequest);
  vi.stubGlobal('fetch', vi.fn(fakeFetch));
  Element.prototype.animate = finishedAnimation;
});

afterEach(() => {
  cleanup();
  sent.length = 0;
  failing.clear();
});

afterAll(() => {
  vi.unstubAllGlobals();
});

describe('ApprovalSheet', () => {
  it('asks about queued requests one at a time and answers the one shown', async () => {
    const { pairing, ApprovalSheet } = await loadModules();
    render(ApprovalSheet);
    pairing.applyDevice({ action: 'requested', device: first });
    pairing.applyDevice({ action: 'requested', device: second });

    expect(await screen.findByText('Allow “iPhone”?')).toBeTruthy();

    await fireEvent.click(screen.getByRole('button', { name: 'Allow' }));
    await waitFor(() => {
      expect(sent).toEqual(['POST /api/devices/1/approve']);
    });
    await waitFor(
      () => {
        expect(screen.getByText('Allow “Ege’s iPhone”?')).toBeTruthy();
      },
      { timeout: dismissTimeoutMs },
    );

    await fireEvent.click(screen.getByRole('button', { name: "Don't Allow" }));
    await waitFor(() => {
      expect(sent).toEqual(['POST /api/devices/1/approve', 'DELETE /api/devices/2']);
    });
    await waitFor(
      () => {
        expect(screen.queryByRole('dialog')).toBeNull();
      },
      { timeout: dismissTimeoutMs },
    );
  });

  it('asks again after a failed answer so the owner can retry', async () => {
    const { pairing, ApprovalSheet } = await loadModules();
    render(ApprovalSheet);
    failing.add('POST /api/devices/3/approve');
    pairing.applyDevice({ action: 'requested', device: third });

    await fireEvent.click(await screen.findByRole('button', { name: 'Allow' }));
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull();
    });
    await waitFor(
      () => {
        expect(screen.getByRole('dialog', { name: 'Allow “iPad”?' })).toBeTruthy();
      },
      { timeout: dismissTimeoutMs },
    );
    expect(sent).toEqual(['POST /api/devices/3/approve']);

    failing.clear();
    await fireEvent.click(screen.getByRole('button', { name: 'Allow' }));
    await waitFor(() => {
      expect(pairing.requests).toEqual([]);
    });
    expect(sent).toEqual(['POST /api/devices/3/approve', 'POST /api/devices/3/approve']);
    await waitFor(
      () => {
        expect(screen.queryByRole('dialog')).toBeNull();
      },
      { timeout: dismissTimeoutMs },
    );
  });

  it('stays open on Escape because only an answer dismisses it', async () => {
    const { pairing, ApprovalSheet } = await loadModules();
    render(ApprovalSheet);
    pairing.applyDevice({ action: 'requested', device: fourth });

    const dialog = await screen.findByRole('dialog', { name: 'Allow “iPhone 16”?' });
    await fireEvent.keyDown(dialog, { key: 'Escape' });
    expect(screen.getByRole('dialog', { name: 'Allow “iPhone 16”?' })).toBeTruthy();
    expect(sent).toEqual([]);

    await fireEvent.click(screen.getByRole('button', { name: "Don't Allow" }));
    await waitFor(() => {
      expect(sent).toEqual(['DELETE /api/devices/4']);
    });
    await waitFor(
      () => {
        expect(screen.queryByRole('dialog')).toBeNull();
      },
      { timeout: dismissTimeoutMs },
    );
  });
});
