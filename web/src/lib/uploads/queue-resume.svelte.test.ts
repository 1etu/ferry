import { describe, expect, it, vi } from 'vitest';
import { type UploadQueue } from './queue.svelte';
import {
  fileOf,
  frame,
  only,
  previousUpload,
  serverTransfer,
  settle,
  stored,
  useQueueHarness,
} from './queue.fixture';

const harness = useQueueHarness();

describe('cancel and remove', () => {
  it('terminates a running upload and frees its slot', async () => {
    const { queue } = await harness.queueWith(
      fileOf('a.jpg', 1),
      fileOf('b.jpg', 2),
      fileOf('c.jpg', 3),
      fileOf('d.jpg', 4),
      fileOf('e.jpg', 5),
    );
    const [waiting, , , , running] = queue.items;
    if (waiting === undefined || running === undefined) throw new Error('expected five items');
    await queue.cancel(running.id);
    await settle();
    expect(harness.uploads[0]?.aborts).toEqual([true]);
    expect(queue.items.find((item) => item.id === running.id)?.status).toBe('canceled');
    expect(queue.items.find((item) => item.id === waiting.id)?.status).toBe('uploading');
  });

  it('cancels a queued item before it starts', async () => {
    const { queue } = await harness.queueWith(
      fileOf('a.jpg', 1),
      fileOf('b.jpg', 2),
      fileOf('c.jpg', 3),
      fileOf('d.jpg', 4),
      fileOf('e.jpg', 5),
    );
    const [waiting] = queue.items;
    if (waiting === undefined) throw new Error('expected items');
    await queue.cancel(waiting.id);
    harness.uploads[0]?.succeed();
    await settle();
    expect(harness.uploads).toHaveLength(4);
  });

  it('terminates the stored upload of a row that needs its file', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.create('T1');
    await vi.advanceTimersByTimeAsync(1000);
    const restored = harness.newQueue();
    restored.restore();
    await restored.cancel(only(restored).id);
    expect(harness.terminated).toEqual(['/api/uploads/T1']);
    expect(only(restored).status).toBe('canceled');
    queue.remove(only(queue).id);
  });

  it('removes rows and terminates the ones still on the server', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.create('T1');
    await vi.advanceTimersByTimeAsync(1000);
    const restored = harness.newQueue();
    restored.restore();
    restored.remove(only(restored).id);
    expect(restored.items).toHaveLength(0);
    expect(harness.terminated).toEqual(['/api/uploads/T1']);
    await vi.advanceTimersByTimeAsync(1000);
    expect(stored()).toHaveLength(0);
    queue.remove(only(queue).id);
  });
});

describe('server cancel', () => {
  it('stops an upload the owner canceled without terminating it again', async () => {
    const { queue } = await harness.queueWith(
      fileOf('a.jpg', 1),
      fileOf('b.jpg', 2),
      fileOf('c.jpg', 3),
      fileOf('d.jpg', 4),
      fileOf('e.jpg', 5),
    );
    harness.uploads[0]?.create('T1');
    queue.applyTransfer(serverTransfer('T1', 'canceled'));
    await settle();
    expect(queue.items.find((item) => item.transferId === 'T1')?.status).toBe('canceled');
    expect(harness.uploads[0]?.aborts).toEqual([false]);
    expect(harness.terminated).toEqual([]);
    expect(harness.uploads).toHaveLength(5);
  });

  it('turns the failure from the interrupted PATCH into canceled', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.create('T1');
    harness.uploads[0]?.fail({ status: 400, body: 'upload was terminated' });
    expect(only(queue).status).toBe('failed');
    queue.applyTransfer(serverTransfer('T1', 'canceled'));
    expect(only(queue)).toMatchObject({ status: 'canceled' });
    expect(only(queue).error).toBeUndefined();
    await vi.advanceTimersByTimeAsync(1000);
    expect(stored()).toHaveLength(0);
  });

  it('ignores the 400 when the canceled event came first', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.create('T1');
    queue.applyTransfer(serverTransfer('T1', 'canceled'));
    harness.uploads[0]?.fail({ status: 400, body: 'upload was terminated' });
    expect(only(queue).status).toBe('canceled');
  });

  it.each(['active', 'failed'] as const)('leaves the row alone for %s', async (status) => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.create('T1');
    queue.applyTransfer(serverTransfer('T1', status));
    expect(only(queue).status).toBe('uploading');
    expect(harness.uploads[0]?.aborts).toEqual([]);
  });

  it('settles a restored row whose upload the server already finished', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.create('T1');
    await vi.advanceTimersByTimeAsync(1000);
    const restored = harness.newQueue();
    restored.restore();
    expect(only(restored).status).toBe('needsFile');
    restored.applyTransfer(serverTransfer('T1', 'done'));
    expect(only(restored)).toMatchObject({ status: 'done', sent: 1000 });
    expect(stored()).toHaveLength(0);
    queue.remove(only(queue).id);
  });

  it('settles an uploading row the server reports done and lets its request finish', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.create('T1');
    queue.applyTransfer(serverTransfer('T1', 'done'));
    expect(only(queue).status).toBe('done');
    expect(harness.uploads[0]?.aborts).toEqual([]);
    harness.uploads[0]?.fail(null);
    expect(only(queue).status).toBe('done');
  });

  it('keeps a finished upload done', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.create('T1');
    harness.uploads[0]?.succeed();
    queue.applyTransfer(serverTransfer('T1', 'canceled'));
    expect(only(queue).status).toBe('done');
  });
});

describe('resume', () => {
  async function restoredAfterReload(): Promise<{
    restored: UploadQueue;
    id: string;
    staleNonce: string;
  }> {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.create('T1');
    harness.uploads[0]?.progress(500);
    await frame();
    harness.setVisibility('hidden');
    const staleNonce = (stored()[0] as { nonce: string }).nonce;
    const restored = harness.newQueue();
    restored.restore();
    queue.remove(only(queue).id);
    harness.uploads = [];
    harness.terminated = [];
    return { restored, id: only(restored).id, staleNonce };
  }

  it('resumes at the stored URL with the stored nonce when the picked file matches', async () => {
    const { restored, id } = await restoredAfterReload();
    await restored.resume(id, fileOf('renamed.jpg', 1));
    await settle();
    expect(harness.uploads[0]?.resumedFrom?.uploadUrl).toBe('/api/uploads/T1');
    expect(harness.uploads[0]?.starts).toBe(1);
    expect(harness.uploads[0]?.options.nonce).toBe((stored()[0] as { nonce: string }).nonce);
    expect(only(restored)).toMatchObject({ status: 'uploading', transferId: 'T1', sent: 500 });
    expect(harness.terminated).toEqual([]);
  });

  it('prefers the entry tus-js-client stored for that URL', async () => {
    const { restored, id } = await restoredAfterReload();
    harness.previousByFingerprint.set('ferry-1000-91a18ad5', [
      previousUpload('/api/uploads/OTHER'),
      previousUpload('/api/uploads/T1'),
    ]);
    await restored.resume(id, fileOf('a.jpg', 1));
    await settle();
    expect(harness.uploads[0]?.options.fingerprint).toBe('ferry-1000-91a18ad5');
    expect(harness.uploads[0]?.resumedFrom).toMatchObject({
      uploadUrl: '/api/uploads/T1',
      urlStorageKey: 'tus::key',
    });
  });

  it('starts over with a different file, a fresh nonce, and terminates the stale upload', async () => {
    const { restored, id, staleNonce } = await restoredAfterReload();
    await restored.resume(id, fileOf('tempImage1.mov', 9, 'video/quicktime', 2000));
    await settle();
    expect(harness.terminated).toEqual(['/api/uploads/T1']);
    expect(harness.uploads[0]?.resumedFrom).toBeUndefined();
    expect(harness.uploads[0]?.options.nonce).not.toBe(staleNonce);
    expect(only(restored)).toMatchObject({
      status: 'uploading',
      name: 'Video 2026-10-02 14.03.07.mov',
      size: 2000,
      sent: 0,
    });
    expect(only(restored).transferId).toBeUndefined();
  });

  it('matches the picked file to whichever row is waiting for it', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1), fileOf('b.jpg', 2));
    harness.uploads[0]?.create('T1');
    harness.uploads[1]?.create('T2');
    await vi.advanceTimersByTimeAsync(1000);
    const restored = harness.newQueue();
    restored.restore();
    for (const item of queue.items) queue.remove(item.id);
    const [second, first] = restored.items;
    if (first === undefined || second === undefined) throw new Error('expected two items');
    await restored.resume(first.id, fileOf('b.jpg', 2));
    await settle();
    expect(restored.items.find((item) => item.id === second.id)?.status).toBe('uploading');
    expect(restored.items.find((item) => item.id === first.id)?.status).toBe('needsFile');
    expect(harness.uploads[2]?.resumedFrom?.uploadUrl).toBe('/api/uploads/T2');
  });

  it('retries a failed row with a newly picked file', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.fail({ status: 413 });
    await queue.resume(only(queue).id, fileOf('small.jpg', 1, 'image/jpeg', 10));
    await settle();
    expect(only(queue)).toMatchObject({ status: 'uploading', size: 10 });
    expect(only(queue).error).toBeUndefined();
  });
});
