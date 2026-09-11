import { equalBytes } from '@noble/ciphers/utils.js';
import { api, errorCodeOf, isUnreadableReply } from '$lib/api/client';
import type { components } from '$lib/api/schema';
import { fromBase64Url, openString, sealString, toBase64Url } from './frames';
import {
  clientKeypair,
  confirmFor,
  deviceSecretFromPairing,
  firstUseDeviceSecret,
  keySize,
  pairingSecretSize,
  proofFor,
  sessionKey,
  sharedSecret,
  type Keypair,
} from './keys';

export type SealSession = { id: string; key: Uint8Array };
export type HandshakeOutcome = 'ok' | 'invalid' | 'failed';

type SealRequest = components['schemas']['SealRequest'];
type SealResponse = components['schemas']['SealResponse'];
type SealReply = { data?: unknown; error?: unknown; response: Response };

const secretStorageKey = 'ferry.secret';
const fragmentParam = 's';

function readStoredSecret(): Uint8Array | undefined {
  let raw: string | null;
  try {
    raw = localStorage.getItem(secretStorageKey);
  } catch {
    return undefined;
  }
  if (raw === null) return undefined;
  const secret = fromBase64Url(raw);
  return secret?.length === keySize ? secret : undefined;
}

function writeStoredSecret(secret: Uint8Array | undefined): boolean {
  try {
    if (secret === undefined) localStorage.removeItem(secretStorageKey);
    else localStorage.setItem(secretStorageKey, toBase64Url(secret));
    return true;
  } catch {
    return false;
  }
}

function fragmentSecret(): Uint8Array | undefined {
  const raw = new URLSearchParams(location.hash.slice(1)).get(fragmentParam);
  if (raw === null) return undefined;
  const secret = fromBase64Url(raw);
  return secret?.length === pairingSecretSize ? secret : undefined;
}

async function postHandshake(body: SealRequest): Promise<SealReply | undefined> {
  try {
    return await api.POST('/api/seal', { body });
  } catch (error: unknown) {
    if (isUnreadableReply(error)) return undefined;
    throw error;
  }
}

function sealResponseOf(body: unknown): SealResponse | undefined {
  if (typeof body !== 'object' || body === null) return undefined;
  if (!('sessionId' in body && 'serverKey' in body && 'confirm' in body)) return undefined;
  const { sessionId, serverKey, confirm } = body;
  if (typeof sessionId !== 'string' || typeof serverKey !== 'string') return undefined;
  return typeof confirm === 'string' ? { sessionId, serverKey, confirm } : undefined;
}

function sharedOrUndefined(secretKey: Uint8Array, serverKey: Uint8Array): Uint8Array | undefined {
  try {
    return sharedSecret(secretKey, serverKey);
  } catch {
    return undefined;
  }
}

export class Seal {
  #session = $state<SealSession | undefined>();
  #secret: Uint8Array | undefined = readStoredSecret();
  #inFlight: Promise<HandshakeOutcome> | undefined;
  #keypair: () => Keypair;

  constructor(keypair: () => Keypair = clientKeypair) {
    this.#keypair = keypair;
  }

  get session(): SealSession | undefined {
    return this.#session;
  }

  hasSecret(): boolean {
    return this.#secret !== undefined;
  }

  adoptFragmentSecret(): boolean {
    const pairingSecret = fragmentSecret();
    if (pairingSecret === undefined) return false;
    this.#setSecret(deviceSecretFromPairing(pairingSecret));
    return true;
  }

  handshake(): Promise<HandshakeOutcome> {
    this.#inFlight ??= this.#runHandshake().finally(() => {
      this.#inFlight = undefined;
    });
    return this.#inFlight;
  }

  sealName(plain: string): string {
    const session = this.#session;
    if (session === undefined) throw new Error('sealName without a sealed session');
    return sealString(session.key, plain);
  }

  openName(sealed: string): string | undefined {
    const session = this.#session;
    return session === undefined ? undefined : openString(session.key, sealed);
  }

  reset(): void {
    this.#session = undefined;
    this.#setSecret(undefined);
  }

  #setSecret(secret: Uint8Array | undefined): void {
    this.#secret = secret;
    writeStoredSecret(secret);
  }

  async #runHandshake(): Promise<HandshakeOutcome> {
    const { secretKey, publicKey } = this.#keypair();
    const secret = this.#secret;
    const body: SealRequest = { clientKey: toBase64Url(publicKey) };
    if (secret !== undefined) body.proof = toBase64Url(proofFor(secret, publicKey));
    const reply = await postHandshake(body);
    if (reply === undefined) return 'failed';
    if (!reply.response.ok) {
      const code = errorCodeOf(reply.error, reply.response.status);
      return code === 'seal_invalid' ? 'invalid' : 'failed';
    }
    const response = sealResponseOf(reply.data);
    if (response === undefined) return 'failed';
    const serverKey = fromBase64Url(response.serverKey);
    const confirm = fromBase64Url(response.confirm);
    if (serverKey?.length !== keySize || confirm === undefined) return 'failed';
    const shared = sharedOrUndefined(secretKey, serverKey);
    if (shared === undefined) return 'failed';
    const key = sessionKey(shared, secret, publicKey, serverKey);
    if (!equalBytes(confirmFor(key, serverKey), confirm)) return 'failed';
    if (secret === undefined) this.#setSecret(firstUseDeviceSecret(shared, publicKey, serverKey));
    this.#session = { id: response.sessionId, key };
    return 'ok';
  }
}

export const seal = new Seal();
