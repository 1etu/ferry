import { describe, expect, it, vi } from 'vitest';
import { toBase64Url } from '$lib/seal/frames';
import {
  approvedDevice,
  doneTransfer,
  errorBody,
  FakeEventSource,
  loadSession,
  pendingDevice,
  readyStates,
  serverInfo,
  useFakeServer,
} from './server.fixture';

const holder = useFakeServer();
const qrSecret = new Uint8Array(16).fill(7);
const fragment = `#s=${toBase64Url(qrSecret)}`;

describe('session.boot with a pair token', () => {
  it('redeems the token after a failed .local probe and waits for approval in a tab', async () => {
    const { server } = holder;
    history.replaceState(null, '', '/?pair=tok123');
    server.reply('GET /api/session', 200, { role: 'none', server: serverInfo });
    server.reply('POST /api/pair', 201, pendingDevice);
    const session = await loadSession();

    await session.boot();

    expect(server.targets()).toEqual([
      'GET /api/session',
      'GET http://egetu-pc.local:8080/api/health',
      'POST /api/pair',
    ]);
    expect(server.sent[2]?.body).toEqual({ token: 'tok123', name: 'iPhone', hasSecret: false });
    expect(session.state).toEqual({ kind: 'waiting', device: pendingDevice });
    expect(location.search).toBe('?pair=tok123');
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(FakeEventSource.instances[0]?.url).toBe('/api/events');
  });

  it('adopts the fragment secret, pairs with hasSecret and proves the handshake', async () => {
    const { server } = holder;
    history.replaceState(null, '', `/?pair=tok123${fragment}`);
    server.adoptPairingSecret(qrSecret);
    server.reply('GET /api/session', 200, { role: 'none', server: serverInfo });
    server.reply('POST /api/pair', 201, approvedDevice);
    const session = await loadSession();

    await session.boot();

    expect(server.sent[2]?.body).toEqual({ token: 'tok123', name: 'iPhone', hasSecret: true });
    expect(server.handshakes).toBe(1);
    expect(session.state).toEqual({ kind: 'phone', device: approvedDevice });
    expect(FakeEventSource.instances[0]?.url).toBe('/api/events?seal=S1');
    expect(localStorage.getItem('ferry.secret')).not.toBeNull();
    expect(location.hash).toBe(fragment);
  });

  it('strips the token and the fragment in standalone mode once redeemed', async () => {
    const { server } = holder;
    server.isStandalone = true;
    history.replaceState(null, '', `/?pair=tok123${fragment}`);
    server.adoptPairingSecret(qrSecret);
    server.reply('GET /api/session', 200, { role: 'none', server: serverInfo });
    server.reply('POST /api/pair', 201, approvedDevice);
    const session = await loadSession();

    await session.boot();

    expect(session.state).toEqual({ kind: 'phone', device: approvedDevice });
    expect(location.search).toBe('');
    expect(location.hash).toBe('');
  });

  it('skips the probe when already on the .local origin', async () => {
    const { server } = holder;
    history.replaceState(null, '', '/?pair=tok123');
    server.reply('GET /api/session', 200, {
      role: 'none',
      server: { ...serverInfo, origins: { ...serverInfo.origins, local: location.origin } },
    });
    server.reply('POST /api/pair', 201, pendingDevice);
    const session = await loadSession();

    await session.boot();

    expect(server.targets()).toEqual(['GET /api/session', 'POST /api/pair']);
  });

  it('moves to the .local origin with the fragment when the probe succeeds', async () => {
    const { server } = holder;
    const replace = vi.fn();
    vi.stubGlobal('location', {
      origin: 'http://192.168.1.23:8080',
      href: `http://192.168.1.23:8080/?pair=tok123${fragment}`,
      search: '?pair=tok123',
      hash: fragment,
      replace,
    });
    server.reply('GET /api/session', 200, { role: 'none', server: serverInfo });
    server.reply('GET http://egetu-pc.local:8080/api/health', 200, { app: 'ferry' });
    const session = await loadSession();

    await session.boot();

    expect(replace).toHaveBeenCalledWith(`http://egetu-pc.local:8080/?pair=tok123${fragment}`);
    expect(server.targets()).not.toContain('POST /api/pair');
    expect(session.state).toEqual({ kind: 'connecting' });
  });

  it.each([
    ['pairing_expired', 410, 'expired'],
    ['pairing_invalid', 404, 'expired'],
    ['rate_limited', 429, 'rateLimited'],
  ])('shows Pair after a token failure %s', async (code, status, pairStatus) => {
    const { server } = holder;
    history.replaceState(null, '', '/?pair=old');
    server.reply('GET /api/session', 200, { role: 'none', server: serverInfo });
    server.reply('POST /api/pair', status, errorBody(code));
    const session = await loadSession();

    await session.boot();

    expect(session.state).toEqual({ kind: 'pair', status: pairStatus });
    expect(location.search).toBe('');
  });
});

describe('session.boot without a token', () => {
  it('shows Pair', async () => {
    const { server } = holder;
    server.reply('GET /api/session', 200, { role: 'none', server: serverInfo });
    const session = await loadSession();

    await session.boot();

    expect(session.state).toEqual({ kind: 'pair', status: 'idle' });
    expect(server.targets()).toEqual(['GET /api/session']);
    expect(session.server).toEqual(serverInfo);
    expect(FakeEventSource.instances).toHaveLength(0);
  });

  it('retries while the server is unreachable', async () => {
    vi.useFakeTimers();
    const { server } = holder;
    const session = await loadSession();

    await session.boot();
    expect(session.state).toEqual({ kind: 'connecting' });

    server.reply('GET /api/session', 200, { role: 'none', server: serverInfo });
    await vi.advanceTimersByTimeAsync(2000);
    expect(session.state).toEqual({ kind: 'pair', status: 'idle' });
  });

  it('shows the PC view for the owner with an unsealed stream', async () => {
    const { server } = holder;
    server.reply('GET /api/session', 200, { role: 'owner', server: serverInfo });
    const session = await loadSession();

    await session.boot();

    expect(session.state).toEqual({ kind: 'pc' });
    expect(FakeEventSource.instances[0]?.url).toBe('/api/events');
    expect(server.handshakes).toBe(0);
  });

  it('remembers the originals hint per browser context', async () => {
    const session = await loadSession();
    expect(session.originalsHintShown).toBe(false);
    session.markOriginalsHint();
    expect(session.originalsHintShown).toBe(true);
    expect(localStorage.getItem('ferry.originalsHintShown')).toBe('1');
    vi.resetModules();
    const reloaded = await loadSession();
    expect(reloaded.originalsHintShown).toBe(true);
  });
});

describe('session.pairWithCode', () => {
  it.each([
    ['pairing_invalid', 404, 'wrong'],
    ['pairing_expired', 410, 'expired'],
    ['rate_limited', 429, 'rateLimited'],
  ])('maps %s to the Pair status', async (code, status, pairStatus) => {
    const { server } = holder;
    server.reply('GET /api/session', 200, { role: 'none', server: serverInfo });
    server.reply('POST /api/pair', status, errorBody(code));
    const session = await loadSession();
    await session.boot();

    await session.pairWithCode('123456');

    expect(server.sent.at(-1)?.body).toEqual({ code: '123456', name: 'iPhone', hasSecret: false });
    expect(session.state).toEqual({ kind: 'pair', status: pairStatus });
    session.dismissPairStatus();
    expect(session.state).toEqual({ kind: 'pair', status: 'idle' });
  });

  it('forgets a stale secret, waits for approval, then trusts on first use', async () => {
    const { server } = holder;
    localStorage.setItem('ferry.secret', toBase64Url(new Uint8Array(32).fill(9)));
    server.reply('GET /api/session', 200, { role: 'none', server: serverInfo });
    server.reply('POST /api/pair', 201, pendingDevice);
    server.reply('GET /api/transfers', 200, []);
    server.reply('GET /api/files', 200, []);
    const session = await loadSession();
    await session.boot();

    await session.pairWithCode('123456');
    expect(localStorage.getItem('ferry.secret')).toBeNull();
    expect(session.state).toEqual({ kind: 'waiting', device: pendingDevice });

    FakeEventSource.instances[0]?.emit('device', { action: 'approved', device: approvedDevice });
    await vi.waitFor(() => {
      expect(session.state).toEqual({ kind: 'phone', device: approvedDevice });
    });
    expect(server.handshakes).toBe(1);
    expect(
      server.sent.find((request) => request.target === 'POST /api/seal')?.body,
    ).not.toHaveProperty('proof');
    expect(localStorage.getItem('ferry.secret')).not.toBeNull();
    expect(FakeEventSource.instances[1]?.url).toBe('/api/events?seal=S1');
  });
});

describe('session.boot with a pending device', () => {
  const pendingSession = { role: 'device', device: pendingDevice, server: serverInfo };

  it('waits, then handshakes, loads the lists and reconnects sealed on approval', async () => {
    const { server } = holder;
    server.reply('GET /api/session', 200, pendingSession);
    server.reply('GET /api/transfers', 200, []);
    server.reply('GET /api/files', 200, []);
    const session = await loadSession();

    await session.boot();
    expect(session.state).toEqual({ kind: 'waiting', device: pendingDevice });
    expect(FakeEventSource.instances[0]?.url).toBe('/api/events');

    FakeEventSource.instances[0]?.emit('device', { action: 'approved', device: approvedDevice });
    await vi.waitFor(() => {
      expect(session.state).toEqual({ kind: 'phone', device: approvedDevice });
    });
    expect(server.targets()).toContain('GET /api/files');
    expect(FakeEventSource.instances[0]?.readyState).toBe(readyStates.CLOSED);
    expect(FakeEventSource.instances[1]?.url).toBe('/api/events?seal=S1');
  });

  it('returns to Pair with Not allowed when revoked and drops the launch token', async () => {
    const { server } = holder;
    history.replaceState(null, '', `/?pair=tok123${fragment}`);
    server.reply('GET /api/session', 200, pendingSession);
    const session = await loadSession();

    await session.boot();
    expect(location.search).toBe('?pair=tok123');
    const source = FakeEventSource.instances[0];
    source?.emit('device', { action: 'revoked', device: { ...pendingDevice, status: 'revoked' } });

    expect(session.state).toEqual({ kind: 'pair', status: 'notAllowed' });
    expect(source?.readyState).toBe(readyStates.CLOSED);
    expect(location.search).toBe('');
    expect(location.hash).toBe('');
  });

  it('stays on Pair with Not allowed when revoked while the lists load', async () => {
    const { server } = holder;
    server.reply('GET /api/session', 200, pendingSession);
    server.reply('GET /api/files', 200, []);
    const session = await loadSession();
    const { uploadQueue } = await import('$lib/uploads/queue.svelte');
    const applied = vi.spyOn(uploadQueue, 'applyTransfer');
    await session.boot();
    const source = FakeEventSource.instances[0];
    server.replyLazily('GET /api/transfers', () => {
      source?.emit('device', {
        action: 'revoked',
        device: { ...approvedDevice, status: 'revoked' },
      });
      return { status: 200, body: [{ ...doneTransfer, name: server.sealedName('Photo.jpg') }] };
    });

    source?.emit('device', { action: 'approved', device: approvedDevice });

    await vi.waitFor(() => {
      expect(applied).toHaveBeenCalledOnce();
    });
    expect(session.state).toEqual({ kind: 'pair', status: 'notAllowed' });
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(source?.readyState).toBe(readyStates.CLOSED);
  });
});
