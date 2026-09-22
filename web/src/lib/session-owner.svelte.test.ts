import { describe, expect, it, vi } from 'vitest';
import { FakeEventSource, loadSession, serverInfo, useFakeServer } from './server.fixture';

const holder = useFakeServer();
const status = { current: '1.0.0', state: 'idle' };
const network = { firewall: 'allowed', profile: 'private' };
const ownerSettings = {
  name: 'egetu-pc',
  receivedDir: 'C:\\Downloads\\Ferry',
  startAtLogin: true,
  checkUpdates: true,
};

async function loadOwner() {
  const { server } = holder;
  server.reply('GET /api/session', 200, { role: 'owner', server: serverInfo });
  server.reply('GET /api/transfers', 200, []);
  server.reply('GET /api/files', 200, []);
  server.reply('GET /api/pairing', 200, { qrUrl: '', localUrl: '', code: '', expiresAt: '' });
  server.reply('GET /api/devices', 200, []);
  server.reply('GET /api/settings', 200, ownerSettings);
  server.reply('GET /api/update', 200, status);
  server.reply('GET /api/network', 200, network);
  const session = await loadSession();
  const { settings } = await import('$lib/settings.svelte');
  const { update } = await import('$lib/update.svelte');
  const { network: networkState } = await import('$lib/network.svelte');
  return { session, settings, update, networkState };
}

describe('owner', () => {
  it('loads the lists, settings, update and network before entering pc', async () => {
    const { server } = holder;
    const { session, settings, update, networkState } = await loadOwner();
    const kindsWhenListed: string[] = [];
    server.replyLazily('GET /api/settings', () => {
      kindsWhenListed.push(session.state.kind);
      return { status: 200, body: ownerSettings };
    });

    await session.boot();

    expect(kindsWhenListed).toEqual(['booting']);
    expect(session.state).toEqual({ kind: 'pc' });
    expect(settings.current).toEqual(ownerSettings);
    expect(update.status).toEqual(status);
    expect(networkState.state).toEqual(network);
    expect(FakeEventSource.instances[0]?.url).toBe('/api/events');
  });

  it('applies update and network events', async () => {
    const { session, update, networkState } = await loadOwner();
    await session.boot();

    FakeEventSource.instances[0]?.emit('update', { ...status, state: 'ready', available: '1.1.0' });
    FakeEventSource.instances[0]?.emit('network', { ...network, firewall: 'blocked' });

    expect(update.status?.state).toBe('ready');
    expect(networkState.state?.firewall).toBe('blocked');
  });

  it('reloads settings, update and network when the stream reopens', async () => {
    const { server } = holder;
    const { session, settings, update, networkState } = await loadOwner();
    await session.boot();
    server.reply('GET /api/settings', 200, { ...ownerSettings, name: 'renamed-pc' });
    server.reply('GET /api/update', 200, { ...status, state: 'ready' });
    server.reply('GET /api/network', 200, { ...network, firewall: 'blocked' });

    FakeEventSource.instances[0]?.open();

    await vi.waitFor(() => {
      expect(settings.current?.name).toBe('renamed-pc');
      expect(update.status?.state).toBe('ready');
      expect(networkState.state?.firewall).toBe('blocked');
    });
  });
});
