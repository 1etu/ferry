import { describe, expect, it, vi } from 'vitest';
import { fileOf, only, storageKey, stored, useQueueHarness } from './queue.fixture';

const harness = useQueueHarness();

describe('persistence', () => {
  it('writes progress at most once per second, flushes on hide and settles at once', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1), fileOf('b.jpg', 2));
    const writes = vi.spyOn(Storage.prototype, 'setItem');
    harness.uploads[0]?.create('T1');
    harness.uploads[1]?.create('T2');
    harness.uploads[0]?.progress(300);
    expect(writes).not.toHaveBeenCalled();
    harness.setVisibility('hidden');
    expect(writes).toHaveBeenCalledOnce();
    expect(stored()).toEqual([
      expect.objectContaining({ transferId: 'T1', status: 'needsFile', sent: 300 }),
      expect.objectContaining({ transferId: 'T2', status: 'needsFile', sent: 0 }),
    ]);
    harness.uploads[0]?.progress(400);
    await vi.advanceTimersByTimeAsync(999);
    expect(writes).toHaveBeenCalledOnce();
    await vi.advanceTimersByTimeAsync(1);
    expect(writes).toHaveBeenCalledTimes(2);
    harness.uploads[1]?.succeed();
    expect(writes).toHaveBeenCalledTimes(3);
    const restored = harness.newQueue();
    restored.restore();
    expect(only(restored)).toMatchObject({ transferId: 'T1', status: 'needsFile', sent: 400 });
    expect(stored()[0]).toHaveProperty('nonce', harness.uploads[0]?.options.nonce);
    queue.remove(only(restored).id);
  });

  it('clears the stored rows the moment the last upload finishes', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.create('T1');
    harness.uploads[0]?.progress(300);
    expect(stored()).toHaveLength(1);
    harness.uploads[0]?.succeed();
    expect(stored()).toHaveLength(0);
    const restored = harness.newQueue();
    restored.restore();
    expect(restored.items).toHaveLength(0);
    queue.remove(only(queue).id);
  });

  it('reads storage once per queue', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.create('T1');
    await vi.advanceTimersByTimeAsync(1000);
    const restored = harness.newQueue();
    restored.restore();
    restored.remove(only(restored).id);
    restored.restore();
    expect(restored.items).toHaveLength(0);
    queue.remove(only(queue).id);
  });

  it('survives a storage that throws', async () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('QuotaExceededError');
    });
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    expect(only(queue).status).toBe('uploading');
  });

  it('ignores corrupt or foreign storage content', () => {
    localStorage.setItem(storageKey, '{not json');
    const corrupt = harness.newQueue();
    corrupt.restore();
    expect(corrupt.items).toHaveLength(0);
    localStorage.setItem(storageKey, JSON.stringify([{ id: 1 }, { id: 'x', status: 'done' }]));
    const foreign = harness.newQueue();
    foreign.restore();
    expect(foreign.items).toHaveLength(0);
  });
});
