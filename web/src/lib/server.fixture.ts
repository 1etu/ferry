import { equalBytes } from '@noble/ciphers/utils.js';
import { afterEach, beforeEach, vi } from 'vitest';
import type { Device, Transfer } from '$lib/api/client';
import { fromBase64Url, sealString, toBase64Url } from '$lib/seal/frames';
import {
  clientKeypair,
  confirmFor,
  deviceSecretFromPairing,
  firstUseDeviceSecret,
  proofFor,
  sessionKey,
  sharedSecret,
  type Keypair,
} from '$lib/seal/keys';

export type Reply = { status: number; body?: unknown; text?: string };
export type SentRequest = { method: string; target: string; body: unknown; sealId: string | null };

type SealRequestBody = { clientKey: string; proof?: string };

export const sealHeader = 'X-Ferry-Seal';
export const readyStates = { CONNECTING: 0, OPEN: 1, CLOSED: 2 } as const;

export const serverInfo = {
  name: 'egetu-pc',
  version: 'dev',
  origins: { local: 'http://egetu-pc.local:8080', ip: 'http://192.168.1.23:8080' },
};

export const pendingDevice: Device = {
  id: '01J9ZK0X5S8V7Q2M3N4P5R6T7W',
  name: 'iPhone',
  status: 'pending',
  createdAt: '2026-10-02T14:03:07.123Z',
};

export const approvedDevice: Device = { ...pendingDevice, status: 'approved' };

export const doneTransfer: Transfer = {
  id: '01J9ZK0X5S8V7Q2M3N4P5R6T7X',
  deviceId: approvedDevice.id,
  direction: 'in',
  name: 'Photo.jpg',
  size: 10,
  done: 10,
  status: 'done',
  createdAt: '2026-10-02T14:03:07.123Z',
  updatedAt: '2026-10-02T14:03:08.123Z',
};

export function errorBody(code: string): unknown {
  return { error: { code, message: code } };
}

export function responseFor(answer: Reply): Response {
  if (answer.text !== undefined) {
    return new Response(answer.text, {
      status: answer.status,
      headers: { 'Content-Type': 'text/html' },
    });
  }
  const body = answer.body === undefined ? null : JSON.stringify(answer.body);
  return new Response(body, {
    status: answer.status,
    headers: body === null ? {} : { 'Content-Type': 'application/json' },
  });
}

export class FakeEventSource extends EventTarget {
  static instances: FakeEventSource[] = [];

  readyState: number = readyStates.CONNECTING;
  readonly url: string;

  constructor(url: string) {
    super();
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  open(): void {
    this.readyState = readyStates.OPEN;
    this.dispatchEvent(new Event('open'));
  }

  emit(kind: string, payload: unknown): void {
    this.dispatchEvent(new MessageEvent(kind, { data: JSON.stringify(payload) }));
  }

  fail(): void {
    this.readyState = readyStates.CLOSED;
    this.dispatchEvent(new Event('error'));
  }

  close(): void {
    this.readyState = readyStates.CLOSED;
  }
}

Object.assign(FakeEventSource, readyStates);

class DocumentRelativeRequest extends Request {
  constructor(input: RequestInfo | URL, init?: RequestInit) {
    super(typeof input === 'string' ? new URL(input, location.href) : input, init);
  }
}

function targetOf(url: URL, method: string): string {
  const where = url.origin === location.origin ? url.pathname : `${url.origin}${url.pathname}`;
  return `${method} ${where}`;
}

async function readRequest(input: RequestInfo | URL, init?: RequestInit): Promise<SentRequest> {
  if (input instanceof Request) {
    const text = await input.text();
    const body: unknown = text ? JSON.parse(text) : undefined;
    const target = targetOf(new URL(input.url), input.method);
    return { method: input.method, target, body, sealId: input.headers.get(sealHeader) };
  }
  const method = init?.method ?? 'GET';
  const url = new URL(input instanceof URL ? input.href : input, location.href);
  return { method, target: targetOf(url, method), body: undefined, sealId: null };
}

export class FakeServer {
  sent: SentRequest[] = [];
  deviceSecret: Uint8Array | undefined;
  sessionKey: Uint8Array | undefined;
  sessionId: string | undefined;
  handshakes = 0;
  sealedTargets = new Set<string>();
  isStandalone = false;
  #replies = new Map<string, (Reply | (() => Reply))[]>();
  #keys: Keypair = clientKeypair();

  reply(target: string, status: number, body?: unknown): void {
    this.#replies.set(target, [{ status, body }]);
  }

  replyInOrder(target: string, ...replies: Reply[]): void {
    this.#replies.set(target, replies);
  }

  replyText(target: string, status: number, text: string): void {
    this.#replies.set(target, [{ status, text }]);
  }

  replyLazily(target: string, make: () => Reply): void {
    this.#replies.set(target, [make]);
  }

  targets(): string[] {
    return this.sent.map((request) => request.target);
  }

  adoptPairingSecret(pairingSecret: Uint8Array): void {
    this.deviceSecret = deviceSecretFromPairing(pairingSecret);
  }

  sealedName(plain: string): string {
    if (this.sessionKey === undefined) throw new Error('no sealed session on the server');
    return sealString(this.sessionKey, plain);
  }

  restart(): void {
    this.sessionId = undefined;
    this.sessionKey = undefined;
  }

  async fetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
    const request = await readRequest(input, init);
    this.sent.push(request);
    const answer = this.#answer(request);
    if (answer === undefined) throw new TypeError(`network error for ${request.target}`);
    return responseFor(answer);
  }

  #answer(request: SentRequest): Reply | undefined {
    if (this.sealedTargets.has(request.target) && request.sealId !== this.sessionId) {
      return { status: 403, body: errorBody('seal_expired') };
    }
    const queue = this.#replies.get(request.target);
    if (queue === undefined || queue.length === 0) {
      return request.target === 'POST /api/seal' ? this.#handshake(request.body) : undefined;
    }
    const next = queue.length > 1 ? queue.shift() : queue[0];
    if (next === undefined) return undefined;
    return typeof next === 'function' ? next() : next;
  }

  #handshake(body: unknown): Reply {
    const { clientKey, proof } = body as SealRequestBody;
    const publicKey = fromBase64Url(clientKey);
    if (publicKey === undefined) return { status: 400, body: errorBody('invalid_request') };
    const proofBytes = proof === undefined ? undefined : fromBase64Url(proof);
    const secret = this.deviceSecret;
    const isProven =
      secret === undefined
        ? proofBytes === undefined
        : proofBytes !== undefined && equalBytes(proofFor(secret, publicKey), proofBytes);
    if (!isProven) return { status: 403, body: errorBody('seal_invalid') };
    const shared = sharedSecret(this.#keys.secretKey, publicKey);
    const key = sessionKey(shared, secret, publicKey, this.#keys.publicKey);
    if (secret === undefined) {
      this.deviceSecret = firstUseDeviceSecret(shared, publicKey, this.#keys.publicKey);
    }
    this.handshakes += 1;
    this.sessionKey = key;
    this.sessionId = `S${String(this.handshakes)}`;
    return {
      status: 201,
      body: {
        sessionId: this.sessionId,
        serverKey: toBase64Url(this.#keys.publicKey),
        confirm: toBase64Url(confirmFor(key, this.#keys.publicKey)),
      },
    };
  }
}

export function useFakeServer(): { server: FakeServer } {
  const holder = { server: new FakeServer() };
  beforeEach(() => {
    vi.resetModules();
    localStorage.clear();
    holder.server = new FakeServer();
    FakeEventSource.instances = [];
    vi.stubGlobal('Request', DocumentRelativeRequest);
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL, init?: RequestInit) => holder.server.fetch(input, init)),
    );
    vi.stubGlobal('EventSource', FakeEventSource);
    vi.stubGlobal('matchMedia', (query: string) => ({
      matches: holder.server.isStandalone,
      media: query,
    }));
    history.replaceState(null, '', '/');
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });
  return holder;
}

export async function loadSession() {
  const module = await import('$lib/session.svelte');
  return module.session;
}
