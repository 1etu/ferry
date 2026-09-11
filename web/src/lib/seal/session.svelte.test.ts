import { hexToBytes } from '@noble/hashes/utils.js';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import vectors from '../../../../internal/seal/testdata/vectors.json';
import { type Reply, responseFor } from '$lib/server.fixture';
import { fromBase64Url, toBase64Url } from './frames';

type HandshakeVector = (typeof vectors.handshakes)[number];
type SentRequest = { method: string; path: string; body: unknown };

function handshakeAt(index: number): HandshakeVector {
  const vector = vectors.handshakes[index];
  if (vector === undefined) throw new Error(`fixture lacks handshake ${String(index)}`);
  return vector;
}

const proven = handshakeAt(0);
const firstUse = handshakeAt(1);
const sessionId = 'c2Vzc2lvbi1pZC0xNg';
const secretStorageKey = 'ferry.secret';

class DocumentRelativeRequest extends Request {
  constructor(input: RequestInfo | URL, init?: RequestInit) {
    super(typeof input === 'string' ? new URL(input, location.href) : input, init);
  }
}

let replies: Reply[];
let sent: SentRequest[];

async function fakeFetch(input: RequestInfo | URL): Promise<Response> {
  if (!(input instanceof Request)) throw new TypeError('unexpected fetch input');
  const text = await input.text();
  sent.push({ method: input.method, path: new URL(input.url).pathname, body: JSON.parse(text) });
  const answer = replies.shift();
  if (answer === undefined) throw new TypeError('network error');
  return responseFor(answer);
}

function keypairOf(vector: HandshakeVector) {
  return () => ({
    secretKey: hexToBytes(vector.clientPrivate),
    publicKey: hexToBytes(vector.clientPublic),
  });
}

function serverReply(vector: HandshakeVector, confirm = vector.confirmBase64url): Reply {
  return {
    status: 201,
    body: { sessionId, serverKey: vector.serverKeyBase64url, confirm },
  };
}

function errorReply(status: number, code: string): Reply {
  return { status, body: { error: { code, message: code } } };
}

function storeSecret(vector: HandshakeVector): void {
  localStorage.setItem(secretStorageKey, toBase64Url(hexToBytes(vector.deviceSecret)));
}

function storedSecret(): Uint8Array | undefined {
  const raw = localStorage.getItem(secretStorageKey);
  return raw === null ? undefined : fromBase64Url(raw);
}

async function newSeal(keypair = keypairOf(proven)) {
  const { Seal } = await import('./session.svelte');
  return new Seal(keypair);
}

beforeEach(() => {
  vi.resetModules();
  replies = [];
  sent = [];
  localStorage.clear();
  vi.stubGlobal('Request', DocumentRelativeRequest);
  vi.stubGlobal('fetch', vi.fn(fakeFetch));
  history.replaceState(null, '', '/');
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('device secret', () => {
  it('derives the device secret from the QR fragment and keeps it', async () => {
    history.replaceState(
      null,
      '',
      `/?pair=tok#s=${toBase64Url(hexToBytes(vectors.pairing.secret))}`,
    );
    const seal = await newSeal();

    expect(seal.hasSecret()).toBe(false);
    expect(seal.adoptFragmentSecret()).toBe(true);
    expect(seal.hasSecret()).toBe(true);
    expect(storedSecret()).toEqual(hexToBytes(vectors.pairing.deviceSecret));
  });

  it.each([
    { name: 'no fragment', url: '/?pair=tok' },
    { name: 'a secret of the wrong length', url: '/#s=AAEC' },
    { name: 'a secret in standard base64', url: '/#s=AAECAwQFBgcICQoLDA0ODw==' },
  ])('ignores $name', async ({ url }) => {
    history.replaceState(null, '', url);
    const seal = await newSeal();
    expect(seal.adoptFragmentSecret()).toBe(false);
    expect(seal.hasSecret()).toBe(false);
  });

  it('loads a stored secret on construction', async () => {
    storeSecret(proven);
    const seal = await newSeal();
    expect(seal.hasSecret()).toBe(true);
  });

  it('ignores a stored secret of the wrong length', async () => {
    localStorage.setItem(secretStorageKey, 'AAEC');
    const seal = await newSeal();
    expect(seal.hasSecret()).toBe(false);
  });
});

describe('handshake', () => {
  it('proves the stored secret and derives the key the server derives', async () => {
    storeSecret(proven);
    replies.push(serverReply(proven));
    const seal = await newSeal();

    await expect(seal.handshake()).resolves.toBe('ok');

    expect(sent).toEqual([
      {
        method: 'POST',
        path: '/api/seal',
        body: { clientKey: proven.clientKeyBase64url, proof: proven.proofBase64url },
      },
    ]);
    expect(seal.session?.id).toBe(sessionId);
    expect(seal.session?.key).toEqual(hexToBytes(proven.key));
  });

  it('trusts on first use: sends no proof and stores the derived secret once confirm verifies', async () => {
    replies.push(serverReply(firstUse));
    const seal = await newSeal(keypairOf(firstUse));

    await expect(seal.handshake()).resolves.toBe('ok');

    expect(sent[0]?.body).toEqual({ clientKey: firstUse.clientKeyBase64url });
    expect(seal.session?.key).toEqual(hexToBytes(firstUse.key));
    expect(storedSecret()).toEqual(hexToBytes(firstUse.derivedDeviceSecret));
    expect(seal.hasSecret()).toBe(true);
  });

  it('refuses a wrong confirm and stores nothing', async () => {
    replies.push(serverReply(firstUse, proven.confirmBase64url));
    const seal = await newSeal(keypairOf(firstUse));

    await expect(seal.handshake()).resolves.toBe('failed');

    expect(seal.session).toBeUndefined();
    expect(seal.hasSecret()).toBe(false);
    expect(storedSecret()).toBeUndefined();
  });

  it.each([
    { name: 'a server key of the wrong length', serverKey: 'abcd' },
    { name: 'the all-zero server key', serverKey: toBase64Url(new Uint8Array(32)) },
  ])('fails on $name', async ({ serverKey }) => {
    replies.push({ status: 201, body: { sessionId, serverKey, confirm: proven.confirmBase64url } });
    const seal = await newSeal();
    await expect(seal.handshake()).resolves.toBe('failed');
    expect(seal.session).toBeUndefined();
  });

  it.each([
    { code: 'seal_invalid', status: 403, outcome: 'invalid' },
    { code: 'pending_approval', status: 403, outcome: 'failed' },
    { code: 'rate_limited', status: 429, outcome: 'failed' },
  ])('maps $code to $outcome', async ({ code, status, outcome }) => {
    replies.push(errorReply(status, code));
    const seal = await newSeal();
    await expect(seal.handshake()).resolves.toBe(outcome);
  });

  it('fails on a network error', async () => {
    const seal = await newSeal();
    await expect(seal.handshake()).resolves.toBe('failed');
  });

  it.each([
    {
      name: 'an HTML error page from a proxy',
      reply: { status: 502, text: '<html>Bad Gateway</html>' },
    },
    { name: 'an empty error body', reply: { status: 500 } },
    { name: 'an error body without an error object', reply: { status: 500, body: {} } },
    { name: 'a null error object', reply: { status: 403, body: { error: null } } },
    { name: 'an error object without a code', reply: { status: 403, body: { error: {} } } },
    { name: 'an unknown error code', reply: { status: 403, body: { error: { code: 'bogus' } } } },
    { name: 'an HTML success page', reply: { status: 201, text: '<html>Captive portal</html>' } },
    { name: 'an empty success body', reply: { status: 201 } },
    { name: 'a null success body', reply: { status: 201, body: null } },
    { name: 'a success body without keys', reply: { status: 201, body: { sessionId } } },
    {
      name: 'a success body with non-string keys',
      reply: { status: 201, body: { sessionId, serverKey: 1, confirm: 2 } },
    },
  ])('fails on $name without throwing', async ({ reply }) => {
    storeSecret(proven);
    replies.push(reply);
    const seal = await newSeal();

    await expect(seal.handshake()).resolves.toBe('failed');

    expect(seal.session).toBeUndefined();
    expect(seal.hasSecret()).toBe(true);
  });

  it('shares one request between overlapping calls', async () => {
    storeSecret(proven);
    replies.push(serverReply(proven));
    const seal = await newSeal();

    const outcomes = await Promise.all([seal.handshake(), seal.handshake()]);

    expect(outcomes).toEqual(['ok', 'ok']);
    expect(sent).toHaveLength(1);
  });

  it('replaces the session on a later handshake', async () => {
    storeSecret(proven);
    const replaced = serverReply(proven);
    replies.push(serverReply(proven), {
      ...replaced,
      body: { ...(replaced.body as object), sessionId: 'bmV4dC1zZXNzaW9uLWlk' },
    });
    const seal = await newSeal();

    await seal.handshake();
    await seal.handshake();

    expect(seal.session?.id).toBe('bmV4dC1zZXNzaW9uLWlk');
  });
});

describe('names', () => {
  it('seals and opens names under the session key', async () => {
    storeSecret(proven);
    replies.push(serverReply(proven));
    const seal = await newSeal();
    await seal.handshake();

    const sealed = seal.sealName('IMG_0001.HEIC');

    expect(sealed).not.toContain('IMG_0001');
    expect(seal.openName(sealed)).toBe('IMG_0001.HEIC');
    expect(seal.openName('not-a-sealed-name')).toBeUndefined();
  });

  it('cannot seal without a session and opens nothing', async () => {
    const seal = await newSeal();
    expect(() => seal.sealName('a.jpg')).toThrow();
    expect(seal.openName('AAAA')).toBeUndefined();
  });
});

describe('reset', () => {
  it('forgets the session and the device secret so a code pairing can trust on first use', async () => {
    storeSecret(proven);
    replies.push(serverReply(proven));
    const seal = await newSeal();
    await seal.handshake();

    seal.reset();

    expect(seal.session).toBeUndefined();
    expect(seal.hasSecret()).toBe(false);
    expect(localStorage.getItem(secretStorageKey)).toBeNull();
  });
});
