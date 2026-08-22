import { describe, expect, it } from 'vitest';
import { errorBody, type Reply, useFakeServer } from '$lib/server.fixture';

const holder = useFakeServer();

const brokenReplies: { name: string; reply: Reply }[] = [
  {
    name: 'an HTML error page from a proxy',
    reply: { status: 502, text: '<html>Bad Gateway</html>' },
  },
  { name: 'an empty error body', reply: { status: 500 } },
  { name: 'an error body without an error object', reply: { status: 500, body: {} } },
  { name: 'a null error object', reply: { status: 500, body: { error: null } } },
  { name: 'an error object without a code', reply: { status: 500, body: { error: {} } } },
  { name: 'an unknown error code', reply: { status: 500, body: { error: { code: 'bogus' } } } },
  { name: 'an HTML success page', reply: { status: 200, text: '<html>Captive portal</html>' } },
];

async function callHealth() {
  const { api, call } = await import('./client');
  return call(api.GET('/api/health'));
}

describe('call', () => {
  it.each(brokenReplies)('maps $name to internal', async ({ reply }) => {
    holder.server.replyInOrder('GET /api/health', reply);
    await expect(callHealth()).resolves.toEqual({ code: 'internal' });
  });

  it('maps a network error to internal', async () => {
    await expect(callHealth()).resolves.toEqual({ code: 'internal' });
  });

  it('keeps a known error code', async () => {
    holder.server.reply('GET /api/health', 429, errorBody('rate_limited'));
    await expect(callHealth()).resolves.toEqual({ code: 'rate_limited' });
  });

  it('maps a bodiless 401 to unauthorized and resets the session', async () => {
    holder.server.reply('GET /api/health', 401);
    const { session } = await import('$lib/session.svelte');

    await expect(callHealth()).resolves.toEqual({ code: 'unauthorized' });

    expect(session.state).toEqual({ kind: 'pair', status: 'idle' });
  });
});
