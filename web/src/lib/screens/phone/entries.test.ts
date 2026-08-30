import { describe, expect, it } from 'vitest';
import { type Transfer } from '$lib/api/client';
import { type UploadItem } from '$lib/uploads/queue.svelte';
import { ArrivalOrder, isConvertedPhoto, mergeEntries } from './entries';

function transfer(id: string, overrides: Partial<Transfer> = {}): Transfer {
  return {
    id,
    deviceId: 'D1',
    direction: 'out',
    name: `${id}.pdf`,
    size: 100,
    done: 0,
    status: 'active',
    createdAt: '2026-10-02T14:00:00.000Z',
    updatedAt: '2026-10-02T14:00:00.000Z',
    ...overrides,
  };
}

function upload(id: string, overrides: Partial<UploadItem> = {}): UploadItem {
  return {
    id,
    name: `${id}.jpg`,
    size: 100,
    type: 'image/jpeg',
    status: 'queued',
    sent: 0,
    ...overrides,
  };
}

describe('mergeEntries', () => {
  it('folds a queue row and its server transfer into one row keyed by the queue id', () => {
    const entries = mergeEntries(
      [upload('u1', { transferId: '01B', status: 'uploading', sent: 40 })],
      [transfer('01B', { direction: 'in', name: 'Photo.jpg', done: 30 })],
    );
    expect(entries).toEqual([
      {
        key: 'u1',
        name: 'Photo.jpg',
        size: 100,
        sent: 40,
        isUpload: true,
        phase: 'active',
        uploadId: 'u1',
      },
    ]);
  });

  it('shows a server cancel over the local upload state', () => {
    const [entry] = mergeEntries(
      [upload('u1', { transferId: '01B', status: 'uploading' })],
      [transfer('01B', { direction: 'in', status: 'canceled' })],
    );
    expect(entry?.phase).toBe('canceled');
  });

  const uploads = [
    upload('u3'),
    upload('u2', { transferId: '01D', status: 'uploading' }),
    upload('u1', { transferId: '01B', status: 'done' }),
  ];
  const served = (downloadStatus: Transfer['status']) => [
    transfer('01E', { status: 'done' }),
    transfer('01D', { direction: 'in' }),
    transfer('01C', { status: downloadStatus }),
    transfer('01B', { direction: 'in', status: 'done' }),
    transfer('01A', { status: 'failed' }),
  ];

  it('orders newest first across both sources, with picks that have no transfer yet on top', () => {
    const entries = mergeEntries(uploads, served('active'));
    expect(entries.map((entry) => entry.key)).toEqual(['u3', '01E', 'u2', '01C', 'u1', '01A']);
  });

  it('keeps every row in place when a phase changes', () => {
    const before = mergeEntries(uploads, served('active')).map((entry) => entry.key);
    const after = mergeEntries(uploads, served('done')).map((entry) => entry.key);
    expect(after).toEqual(before);
  });

  it('keeps queue rows in queue order even when their ids disagree with it', () => {
    const entries = mergeEntries(
      [upload('u1', { transferId: '01A' }), upload('u2', { transferId: '01C' })],
      [transfer('01C', { direction: 'in' }), transfer('01B'), transfer('01A', { direction: 'in' })],
    );
    expect(entries.map((entry) => entry.key)).toEqual(['01B', 'u1', 'u2']);
  });

  it('maps a download to a non-upload row with its served bytes', () => {
    const [entry] = mergeEntries([], [transfer('01A', { done: 42 })]);
    expect(entry).toMatchObject({
      isUpload: false,
      sent: 42,
      phase: 'active',
      uploadId: undefined,
    });
  });

  it('keeps every row exactly once for large lists', () => {
    const uploads = [...Array(500).keys()].map((index) =>
      upload(`u${String(index)}`, { transferId: `T${String(index).padStart(4, '0')}` }),
    );
    const served = uploads.map((item) => transfer(item.transferId ?? '', { direction: 'in' }));
    const entries = mergeEntries(uploads, [...served, transfer('ZZZZ')]);
    expect(entries).toHaveLength(501);
    expect(new Set(entries.map((entry) => entry.key)).size).toBe(501);
  });
});

describe('ArrivalOrder', () => {
  it('numbers only unseen keys, in list order, and forgets them once settled', () => {
    const order = new ArrivalOrder(['a', 'b']);
    expect([...order.indexes(['x', 'a', 'y', 'b', 'z'])]).toEqual([
      ['x', 0],
      ['y', 1],
      ['z', 2],
    ]);
    order.settle(['x', 'a', 'y', 'b', 'z']);
    expect(order.indexes(['x', 'a']).size).toBe(0);
    expect(order.isNew('w')).toBe(true);
    expect(order.isNew('x')).toBe(false);
  });
});

describe('isConvertedPhoto', () => {
  const now = new Date(2026, 9, 2, 14, 3, 7);

  it.each([
    ['tempImageAbc123.jpg', 'image/jpeg', true],
    ['image.jpg', 'image/jpeg', true],
    ['IMG_4821.HEIC', 'image/heic', false],
    ['Report.pdf', 'application/pdf', false],
    ['image.mov', 'video/quicktime', false],
  ])('%s (%s) is converted: %s', (name, type, expected) => {
    expect(isConvertedPhoto(new File(['x'], name, { type }), now)).toBe(expected);
  });
});
