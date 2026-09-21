import { describe, expect, it } from 'vitest';
import { errorBody, useFakeServer } from './server.fixture';

const holder = useFakeServer();

const current = {
  name: 'egetu-pc',
  receivedDir: 'C:\\Users\\egetu\\Downloads\\Ferry',
  startAtLogin: true,
  checkUpdates: true,
};

async function loadStores() {
  const [{ settings }, { update }, { network }] = await Promise.all([
    import('$lib/settings.svelte'),
    import('$lib/update.svelte'),
    import('$lib/network.svelte'),
  ]);
  return { settings, update, network };
}

describe('settings', () => {
  it('loads, patches and takes the settings the picker returns', async () => {
    const { server } = holder;
    server.reply('GET /api/settings', 200, current);
    server.reply('PATCH /api/settings', 200, { ...current, name: 'Office PC' });
    server.reply('POST /api/settings/received-dir/pick', 200, {
      ...current,
      receivedDir: 'D:\\In',
    });
    const { settings } = await loadStores();

    await settings.load();
    expect(settings.current).toEqual(current);
    expect(await settings.patch({ name: 'Office PC' })).toBeUndefined();
    expect(server.sent.at(-1)?.body).toEqual({ name: 'Office PC' });
    expect(settings.current?.name).toBe('Office PC');
    await settings.pickReceivedDir();
    expect(settings.current?.receivedDir).toBe('D:\\In');
    expect(settings.canPickReceivedDir).toBe(true);
  });

  it('returns the error code of a rejected patch and hides the picker off Windows', async () => {
    const { server } = holder;
    server.reply('PATCH /api/settings', 400, errorBody('invalid_request'));
    server.reply('POST /api/settings/received-dir/pick', 501, errorBody('unsupported'));
    const { settings } = await loadStores();

    expect(await settings.patch({ name: '' })).toBe('invalid_request');
    expect(settings.current).toBeUndefined();
    await settings.pickReceivedDir();
    expect(settings.canPickReceivedDir).toBe(false);
  });
});

describe('update', () => {
  it('checks, applies and reports a conflict when nothing is ready', async () => {
    const { server } = holder;
    const checking = { current: '1.0.0', state: 'checking' };
    server.reply('GET /api/update', 200, { current: '1.0.0', state: 'idle' });
    server.reply('POST /api/update/check', 202, checking);
    server.replyInOrder(
      'POST /api/update/apply',
      { status: 409, body: errorBody('conflict') },
      { status: 202 },
    );
    const { update } = await loadStores();

    await update.load();
    expect(update.status?.state).toBe('idle');
    await update.check();
    expect(update.status).toEqual(checking);
    expect(await update.apply()).toBe('conflict');
    update.applyEvent({ ...checking, state: 'ready', available: '1.1.0' });
    expect(await update.apply()).toBeUndefined();
  });
});

describe('network', () => {
  it('loads the firewall state and reports a declined UAC prompt', async () => {
    const { server } = holder;
    server.reply('GET /api/network', 200, { firewall: 'blocked', profile: 'public' });
    server.replyInOrder(
      'POST /api/network/allow',
      { status: 409, body: errorBody('conflict') },
      { status: 204 },
    );
    const { network } = await loadStores();

    await network.load();
    expect(network.state).toEqual({ firewall: 'blocked', profile: 'public' });
    expect(await network.allow()).toBe('conflict');
    expect(await network.allow()).toBeUndefined();
    network.applyEvent({ firewall: 'allowed', profile: 'public' });
    expect(network.state?.firewall).toBe('allowed');
  });
});
