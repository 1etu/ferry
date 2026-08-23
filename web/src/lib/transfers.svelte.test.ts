import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Transfer } from '$lib/api/client';

class DocumentRelativeRequest extends Request {
  constructor(input: RequestInfo | URL, init?: RequestInit) {
    super(typeof input === 'string' ? new URL(input, location.href) : input, init);
  }
}

const upload: Transfer = {
  id: '01J9ZK0X5S8V7Q2M3N4P5R6T7W',
  deviceId: '01J9ZK0X5S8V7Q2M3N4P5R6T7X',
  direction: 'in',
  name: 'Photo.jpg',
  size: 100,
  done: 40,
  status: 'active',
  createdAt: '2026-10-02T14:03:07.000Z',
  updatedAt: '2026-10-02T14:03:08.000Z',
};

function at(updatedAt: string, change: Partial<Transfer>): Transfer {
  return { ...upload, ...change, updatedAt };
}

let listed: Transfer[];
let transfers: Awaited<ReturnType<typeof loadTransfers>>;

async function loadTransfers() {
  const module = await import('./transfers.svelte');
  return module.transfers;
}

beforeAll(async () => {
  vi.stubGlobal('Request', DocumentRelativeRequest);
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve(Response.json(listed))),
  );
  transfers = await loadTransfers();
});

beforeEach(() => {
  transfers.clear();
  listed = [];
});

afterAll(() => {
  vi.unstubAllGlobals();
});

describe('transfers.apply', () => {
  it('takes an update that is newer or as new as the stored row', () => {
    transfers.apply(upload);
    transfers.apply(at(upload.updatedAt, { done: 60 }));
    expect(transfers.items).toEqual([at(upload.updatedAt, { done: 60 })]);
    transfers.apply(at('2026-10-02T14:03:09.5Z', { done: 100, status: 'done' }));
    expect(transfers.items[0]).toMatchObject({ status: 'done', done: 100 });
  });

  it('ignores an update older than the stored row', () => {
    const finished = at('2026-10-02T14:03:10.000Z', { done: 100, status: 'done' });
    transfers.apply(finished);
    transfers.apply(at('2026-10-02T14:03:09.000Z', { done: 90, status: 'canceled' }));
    expect(transfers.items).toEqual([finished]);
  });

  it.each(['done', 'failed', 'canceled'] as const)(
    'never turns a %s row back to active',
    (status) => {
      const finished = at('2026-10-02T14:03:10.000Z', { status });
      transfers.apply(finished);
      transfers.apply(at('2026-10-02T14:03:11.000Z', { done: 99 }));
      expect(transfers.items).toEqual([finished]);
    },
  );
});

describe('transfers.load', () => {
  it('keeps a row an event made newer than the fetched list and drops unlisted rows', async () => {
    const canceled = at('2026-10-02T14:03:10.000Z', { status: 'canceled' });
    const other = { ...upload, id: '01J9ZK0X5S8V7Q2M3N4P5R6T7Y' };
    transfers.apply(canceled);
    transfers.apply(at(other.updatedAt, { id: '01J9ZK0X5S8V7Q2M3N4P5R6T7Z' }));
    listed = [upload, other];
    await transfers.load();
    expect(transfers.items).toEqual([other, canceled]);
  });
});
