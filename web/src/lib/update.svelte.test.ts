import { describe, expect, it } from 'vitest';
import { type UpdateStatus } from '$lib/api/client';
import { useFakeServer } from './server.fixture';

const holder = useFakeServer();
const idle: UpdateStatus = { current: '1.0.0', state: 'idle' };
const checking: UpdateStatus = { ...idle, state: 'checking' };
const ready: UpdateStatus = { ...idle, state: 'ready', available: '1.1.0' };

async function loadUpdate() {
  const module = await import('$lib/update.svelte');
  return module.update;
}

describe('update.check', () => {
  it('applies the reply when no event arrived meanwhile', async () => {
    const { server } = holder;
    server.reply('POST /api/update/check', 202, checking);
    const update = await loadUpdate();

    await update.check();

    expect(update.status).toEqual(checking);
  });

  it('keeps the state an event delivered while the request was in flight', async () => {
    const { server } = holder;
    const update = await loadUpdate();
    server.replyLazily('POST /api/update/check', () => {
      update.applyEvent(ready);
      return { status: 202, body: checking };
    });

    await update.check();

    expect(update.status).toEqual(ready);
  });

  it('keeps a newer event over a stale load reply too', async () => {
    const { server } = holder;
    const update = await loadUpdate();
    server.replyLazily('GET /api/update', () => {
      update.applyEvent(ready);
      return { status: 200, body: idle };
    });

    await update.load();

    expect(update.status).toEqual(ready);
  });
});
