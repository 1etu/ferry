import { describe, expect, it, vi } from 'vitest';
import { session } from '$lib/session.svelte';
import { fileOf, frame, only, sealExpiredBody, settle, useQueueHarness } from './queue.fixture';

const harness = useQueueHarness();

describe('add', () => {
  it('runs four uploads at a time and starts the next when one finishes', async () => {
    const { queue } = await harness.queueWith(
      fileOf('a.jpg', 1),
      fileOf('b.jpg', 2),
      fileOf('c.jpg', 3),
      fileOf('d.jpg', 4),
      fileOf('e.jpg', 5),
    );
    expect(harness.uploads).toHaveLength(4);
    expect(queue.items.map((item) => item.status)).toEqual([
      'queued',
      'uploading',
      'uploading',
      'uploading',
      'uploading',
    ]);
    harness.uploads[0]?.succeed();
    await settle();
    expect(harness.uploads).toHaveLength(5);
    expect(queue.items.map((item) => item.status)).toEqual([
      'uploading',
      'uploading',
      'uploading',
      'uploading',
      'done',
    ]);
  });

  it('names picker temp files from the type and time and keeps real names', async () => {
    const { queue } = await harness.queueWith(
      fileOf('tempImage4F2A.jpg', 1),
      fileOf('IMG_0001.HEIC', 2),
    );
    expect(queue.items.map((item) => item.name)).toEqual([
      'IMG_0001.HEIC',
      'Photo 2026-10-02 14.03.07.jpg',
    ]);
    expect(harness.uploads[0]?.options.filename).toBe('Photo 2026-10-02 14.03.07.jpg');
  });

  it('takes the transfer id from the upload URL and publishes progress once per frame', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.create('01J9ZK0X5S8V7Q2M3N4P5R6T7W');
    harness.uploads[0]?.progress(300);
    harness.uploads[0]?.progress(400);
    expect(only(queue)).toMatchObject({ transferId: '01J9ZK0X5S8V7Q2M3N4P5R6T7W', sent: 0 });
    await frame();
    expect(only(queue)).toMatchObject({ status: 'uploading', sent: 400 });
    harness.uploads[0]?.succeed();
    expect(only(queue)).toMatchObject({ status: 'done', sent: 1000 });
    await vi.advanceTimersByTimeAsync(1000);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('draws a fresh upload nonce per upload and gives it to the transport', async () => {
    await harness.queueWith(fileOf('a.jpg', 1), fileOf('b.jpg', 2));
    const nonces = harness.uploads.map((upload) => upload.options.nonce);
    expect(nonces[0]).toMatch(/^[A-Za-z0-9_-]{22}$/);
    expect(nonces[0]).not.toBe(nonces[1]);
  });

  it('resumes a waiting row when the same file is picked again instead of duplicating it', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.create('T1');
    await vi.advanceTimersByTimeAsync(1000);
    const restored = harness.newQueue();
    restored.restore();
    await restored.add([fileOf('a.jpg', 1)]);
    await settle();
    expect(restored.items).toHaveLength(1);
    expect(only(restored)).toMatchObject({ transferId: 'T1', status: 'uploading' });
    expect(harness.uploads[1]?.resumedFrom?.uploadUrl).toBe('/api/uploads/T1');
    expect(harness.uploads[1]?.options.nonce).toBe(harness.uploads[0]?.options.nonce);
    expect(queue.items).toHaveLength(1);
  });
});

describe('sealed session', () => {
  it('handshakes before the first upload when no session is live', async () => {
    harness.sessionId = undefined;
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    expect(harness.renewals).toEqual([undefined]);
    expect(harness.handshakes).toBe(1);
    expect(harness.uploads).toHaveLength(1);
    expect(only(queue).status).toBe('uploading');
  });

  it('keeps rows queued and retries the handshake after 5 s when it fails', async () => {
    harness.sessionId = undefined;
    harness.canHandshake = false;
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    expect(harness.uploads).toHaveLength(0);
    expect(only(queue).status).toBe('queued');
    harness.canHandshake = true;
    await vi.advanceTimersByTimeAsync(5000);
    expect(harness.handshakes).toBe(1);
    expect(harness.uploads).toHaveLength(1);
  });

  it('handshakes again and resumes with the same nonce after seal_expired', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.create('T1');
    harness.uploads[0]?.fail({ status: 403, body: sealExpiredBody });
    expect(only(queue).status).toBe('queued');
    await settle();
    expect(harness.renewals).toEqual(['S0']);
    expect(harness.handshakes).toBe(1);
    expect(harness.uploads).toHaveLength(2);
    expect(harness.uploads[1]?.resumedFrom?.uploadUrl).toBe('/api/uploads/T1');
    expect(harness.uploads[1]?.options.nonce).toBe(harness.uploads[0]?.options.nonce);
    expect(only(queue).status).toBe('uploading');
  });

  it('retries under the live session without a handshake when the session already changed', async () => {
    await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.create('T1');
    harness.sessionId = 'S9';
    harness.uploads[0]?.fail({ status: 403, body: sealExpiredBody });
    await settle();
    expect(harness.handshakes).toBe(0);
    expect(harness.uploads).toHaveLength(2);
  });

  it('shares one handshake between uploads that expire together', async () => {
    await harness.queueWith(fileOf('a.jpg', 1), fileOf('b.jpg', 2));
    harness.uploads[0]?.fail({ status: 403, body: sealExpiredBody });
    harness.uploads[1]?.fail({ status: 403, body: sealExpiredBody });
    await settle();
    expect(harness.handshakes).toBe(1);
    expect(harness.uploads).toHaveLength(4);
  });

  it('fails the row and resets the session on seal_invalid', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.fail({ status: 403, body: '{"error":{"code":"seal_invalid"}}' });
    expect(only(queue)).toMatchObject({ status: 'failed', error: 'seal_invalid' });
    expect(session.state.kind).toBe('pair');
  });
});

describe('stall watchdog', () => {
  it('restarts an upload with no progress for 15 s while online', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    const upload = harness.uploads[0];
    upload?.create('T1');
    upload?.progress(10);
    await vi.advanceTimersByTimeAsync(14999);
    expect(only(queue).status).toBe('uploading');
    await vi.advanceTimersByTimeAsync(1);
    expect(only(queue).status).toBe('stalled');
    expect(upload?.aborts).toEqual([false]);
    await vi.advanceTimersByTimeAsync(1);
    expect(upload?.starts).toBe(2);
    upload?.progress(20);
    await frame();
    expect(only(queue)).toMatchObject({ status: 'uploading', sent: 20 });
  });

  it('checks with a 5 s threshold when the page becomes visible again', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    const upload = harness.uploads[0];
    upload?.create('T1');
    harness.setVisibility('hidden');
    await vi.advanceTimersByTimeAsync(6000);
    expect(upload?.starts).toBe(1);
    harness.setVisibility('visible');
    expect(only(queue).status).toBe('stalled');
    await settle();
    expect(upload?.starts).toBe(2);
  });

  it('waits while offline and restarts on the online event', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    const upload = harness.uploads[0];
    upload?.create('T1');
    harness.isOnline = false;
    await vi.advanceTimersByTimeAsync(30000);
    expect(upload?.starts).toBe(1);
    expect(only(queue).status).toBe('uploading');
    harness.isOnline = true;
    window.dispatchEvent(new Event('online'));
    await settle();
    expect(upload?.starts).toBe(2);
  });

  it('retries a network failure after 5 s with a fresh upload', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.create('T1');
    harness.uploads[0]?.fail(null);
    expect(only(queue).status).toBe('stalled');
    await vi.advanceTimersByTimeAsync(5000);
    expect(harness.uploads).toHaveLength(2);
    expect(harness.uploads[1]?.starts).toBe(1);
    expect(harness.uploads[1]?.resumedFrom?.uploadUrl).toBe('/api/uploads/T1');
    harness.uploads[1]?.progress(5);
    await frame();
    expect(only(queue).status).toBe('uploading');
  });

  it('queues again after a 429 and honors Retry-After', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.fail({ status: 429, retryAfter: '7' });
    expect(only(queue).status).toBe('queued');
    await vi.advanceTimersByTimeAsync(5000);
    expect(harness.uploads).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(5000);
    expect(harness.uploads).toHaveLength(2);
    expect(only(queue).status).toBe('uploading');
  });
});

describe('error mapping', () => {
  it.each([
    [{ status: 413, body: '{"error":{"code":"too_large","message":"x"}}' }, 'too_large'],
    [{ status: 507 }, 'no_space'],
    [{ status: 403, body: '{"error":{"code":"pending_approval"}}' }, 'pending_approval'],
    [{ status: 400, body: 'not json' }, 'invalid_request'],
    [{ status: 418 }, 'internal'],
  ])('fails %j as %s', async (response, code) => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.fail(response);
    expect(only(queue)).toMatchObject({ status: 'failed', error: code });
    await vi.advanceTimersByTimeAsync(1000);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('keeps retrying server errors', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.fail({ status: 500 });
    expect(only(queue).status).toBe('stalled');
  });

  it('treats a vanished upload as canceled', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.create('T1');
    harness.uploads[0]?.fail({ status: 404 });
    expect(only(queue)).toMatchObject({ status: 'canceled' });
  });

  it('resets the session on 401', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.fail({ status: 401 });
    expect(only(queue)).toMatchObject({ status: 'failed', error: 'unauthorized' });
    expect(session.state.kind).toBe('pair');
  });

  it('fails locally raised errors as internal', async () => {
    const { queue } = await harness.queueWith(fileOf('a.jpg', 1));
    harness.uploads[0]?.options.onError(new Error('tus: no file'));
    expect(only(queue)).toMatchObject({ status: 'failed', error: 'internal' });
  });
});
