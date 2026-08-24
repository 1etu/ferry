import { describe, expect, it, vi } from 'vitest';
import { toBase64Url } from '$lib/seal/frames';
import {
  approvedDevice,
  doneTransfer,
  errorBody,
  FakeEventSource,
  loadSession,
  readyStates,
  serverInfo,
  useFakeServer,
} from './server.fixture';

const holder = useFakeServer();
const health = { app: 'ferry', name: serverInfo.name, version: serverInfo.version };
const approvedSession = { role: 'device', device: approvedDevice, server: serverInfo };

async function bootApproved() {
  const { server } = holder;
  server.reply('GET /api/session', 200, approvedSession);
  server.reply('GET /api/health', 200, health);
  server.replyLazily('GET /api/transfers', () => ({
    status: 200,
    body: [{ ...doneTransfer, name: server.sealedName('Photo.jpg') }],
  }));
  server.reply('GET /api/files', 200, []);
  const session = await loadSession();
  await session.boot();
  return session;
}

describe('approved device', () => {
  it('handshakes and loads the unsealed lists before entering phone', async () => {
    const { server } = holder;
    server.reply('GET /api/session', 200, approvedSession);
    server.reply('GET /api/files', 200, []);
    const session = await loadSession();
    const { transfers } = await import('$lib/transfers.svelte');
    const kindsWhenListed: string[] = [];
    server.replyLazily('GET /api/transfers', () => {
      kindsWhenListed.push(session.state.kind);
      return { status: 200, body: [{ ...doneTransfer, name: server.sealedName('Photo.jpg') }] };
    });

    await session.boot();

    expect(kindsWhenListed).toEqual(['booting']);
    expect(transfers.items).toEqual([doneTransfer]);
    expect(session.state).toEqual({ kind: 'phone', device: approvedDevice });
    expect(server.handshakes).toBe(1);
    expect(FakeEventSource.instances[0]?.url).toBe('/api/events?seal=S1');
    const listed = server.sent.find((request) => request.target === 'GET /api/transfers');
    expect(listed?.sealId).toBe('S1');
  });

  it('handshakes again and retries once when the open refresh answers seal_expired', async () => {
    const { server } = holder;
    server.sealedTargets.add('GET /api/transfers');
    await bootApproved();
    const { transfers } = await import('$lib/transfers.svelte');
    const bootRequests = server.sent.length;
    server.replyLazily('GET /api/transfers', () => ({
      status: 200,
      body: [
        { ...doneTransfer, name: server.sealedName(`Photo-${String(server.handshakes)}.jpg`) },
      ],
    }));
    server.restart();

    FakeEventSource.instances[0]?.open();

    await vi.waitFor(() => {
      expect(transfers.items[0]?.name).toBe('Photo-2.jpg');
    });
    expect(server.handshakes).toBe(2);
    const sealIds = server.sent
      .slice(bootRequests)
      .filter((request) => request.target === 'GET /api/transfers')
      .map((request) => request.sealId);
    expect(sealIds).toEqual(['S1', 'S2']);
  });

  it('renews the session and reopens the stream after the PC restarted', async () => {
    vi.useFakeTimers();
    const { server } = holder;
    const session = await bootApproved();
    server.restart();

    FakeEventSource.instances[0]?.fail();
    await vi.advanceTimersByTimeAsync(5000);

    await vi.waitFor(() => {
      expect(FakeEventSource.instances).toHaveLength(2);
    });
    expect(server.handshakes).toBe(2);
    expect(FakeEventSource.instances[1]?.url).toBe('/api/events?seal=S2');
    expect(session.state).toEqual({ kind: 'phone', device: approvedDevice });
  });

  it('returns to Pair and forgets the secret when the proof is rejected', async () => {
    const { server } = holder;
    localStorage.setItem('ferry.secret', toBase64Url(new Uint8Array(32).fill(9)));
    server.adoptPairingSecret(new Uint8Array(16).fill(1));
    server.reply('GET /api/session', 200, approvedSession);
    const session = await loadSession();

    await session.boot();

    expect(session.state).toEqual({ kind: 'pair', status: 'idle' });
    expect(localStorage.getItem('ferry.secret')).toBeNull();
    expect(FakeEventSource.instances).toHaveLength(0);
  });

  it('stays on Pair and opens no stream when a list answers 401 during boot', async () => {
    const { server } = holder;
    server.reply('GET /api/session', 200, approvedSession);
    server.reply('GET /api/transfers', 401, errorBody('unauthorized'));
    server.reply('GET /api/files', 401, errorBody('unauthorized'));
    const session = await loadSession();

    await session.boot();

    expect(session.state).toEqual({ kind: 'pair', status: 'idle' });
    expect(FakeEventSource.instances).toHaveLength(0);
  });

  it('resets to Pair and closes the stream when the open refresh answers 401', async () => {
    const { server } = holder;
    const session = await bootApproved();
    server.reply('GET /api/transfers', 401, errorBody('unauthorized'));
    const source = FakeEventSource.instances[0];

    source?.open();

    await vi.waitFor(() => {
      expect(session.state).toEqual({ kind: 'pair', status: 'idle' });
    });
    expect(source?.readyState).toBe(readyStates.CLOSED);
  });

  it('hands canceled uploads to the upload queue on boot, on refresh and from events', async () => {
    const { server } = holder;
    const canceled = { ...doneTransfer, done: 4, status: 'canceled' };
    server.reply('GET /api/session', 200, approvedSession);
    server.replyLazily('GET /api/transfers', () => ({
      status: 200,
      body: [{ ...canceled, name: server.sealedName('Photo.jpg') }],
    }));
    server.reply('GET /api/files', 200, []);
    const session = await loadSession();
    const { uploadQueue } = await import('$lib/uploads/queue.svelte');
    const applied = vi.spyOn(uploadQueue, 'applyTransfer');

    await session.boot();
    expect(applied).toHaveBeenCalledExactlyOnceWith(canceled);
    applied.mockClear();

    const source = FakeEventSource.instances[0];
    source?.open();
    await vi.waitFor(() => {
      expect(applied).toHaveBeenCalledExactlyOnceWith(canceled);
    });
    applied.mockClear();

    source?.emit('transfer', { ...canceled, name: server.sealedName('Photo.jpg') });
    expect(applied).toHaveBeenCalledExactlyOnceWith(canceled);
  });
});

describe('stored uploads', () => {
  it('restores them before the loaded transfers settle them', async () => {
    const { server } = holder;
    localStorage.setItem(
      'ferry.uploads',
      JSON.stringify([
        {
          id: 'local1',
          transferId: doneTransfer.id,
          name: 'Photo.jpg',
          size: 10,
          type: 'image/jpeg',
          status: 'needsFile',
          sent: 4,
          uploadUrl: `/api/uploads/${doneTransfer.id}`,
        },
      ]),
    );
    const { uploadQueue } = await import('$lib/uploads/queue.svelte');

    const session = await bootApproved();

    expect(session.state.kind).toBe('phone');
    expect(uploadQueue.items).toHaveLength(1);
    expect(uploadQueue.items[0]).toMatchObject({ transferId: doneTransfer.id, status: 'done' });
    expect(localStorage.getItem('ferry.uploads')).toBeNull();
    server.reply('GET /api/transfers', 200, []);
  });
});

describe('event stream', () => {
  async function homeWithClosedStream() {
    vi.useFakeTimers();
    const session = await bootApproved();
    FakeEventSource.instances[0]?.fail();
    return session;
  }

  it('returns to Pair when the stream closes and the server no longer knows the device', async () => {
    const { server } = holder;
    const session = await homeWithClosedStream();
    server.reply('GET /api/session', 200, { role: 'none', server: serverInfo });

    await vi.advanceTimersByTimeAsync(5000);

    await vi.waitFor(() => {
      expect(session.state).toEqual({ kind: 'pair', status: 'idle' });
    });
    expect(FakeEventSource.instances).toHaveLength(1);
  });

  it('reports the server as down while unreachable and keeps the state', async () => {
    const { server } = holder;
    const session = await homeWithClosedStream();
    server.reply('GET /api/health', 500);

    await vi.advanceTimersByTimeAsync(5000);

    await vi.waitFor(() => {
      expect(session.isConnected).toBe(false);
    });
    expect(session.state).toEqual({ kind: 'phone', device: approvedDevice });
  });
});
