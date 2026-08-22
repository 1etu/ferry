import createClient from 'openapi-fetch';
import { seal } from '$lib/seal/session.svelte';
import { session } from '$lib/session.svelte';
import { sealHeader } from '$lib/uploads/tus';
import type { components, paths } from './schema';

type Schemas = components['schemas'];

export type ErrorCode = Schemas['ErrorCode'];
export type Device = Schemas['Device'];
export type Transfer = Schemas['Transfer'];
export type OfferedFile = Schemas['OfferedFile'];
export type Pairing = Schemas['Pairing'];
export type PairRequest = Schemas['PairRequest'];
export type ServerInfo = Schemas['Session']['server'];
export type Settings = Schemas['Settings'];
export type SettingsPatch = Schemas['SettingsPatch'];
export type UpdateStatus = Schemas['UpdateStatus'];
export type Network = Schemas['Network'];

export type Outcome<T> = { data: T } | { code: ErrorCode };

type Attempt<T> = { data?: T; error?: unknown; response: Response };

const knownErrorCodes: Record<ErrorCode, true> = {
  invalid_request: true,
  unauthorized: true,
  forbidden: true,
  pending_approval: true,
  not_found: true,
  pairing_invalid: true,
  pairing_expired: true,
  rate_limited: true,
  too_large: true,
  no_space: true,
  file_missing: true,
  expired: true,
  conflict: true,
  unsupported: true,
  seal_expired: true,
  seal_invalid: true,
  internal: true,
};

const replays = new Map<string, Request>();

export const api = createClient<paths>({ baseUrl: '' });

export function isErrorCode(value: unknown): value is ErrorCode {
  return typeof value === 'string' && Object.hasOwn(knownErrorCodes, value);
}

export function errorCodeOf(body: unknown, status: number): ErrorCode {
  if (typeof body === 'object' && body !== null && 'error' in body) {
    const { error } = body;
    if (typeof error === 'object' && error !== null && 'code' in error && isErrorCode(error.code)) {
      return error.code;
    }
  }
  return status === 401 ? 'unauthorized' : 'internal';
}

export function isUnreadableReply(error: unknown): boolean {
  return error instanceof TypeError || error instanceof SyntaxError;
}

async function responseErrorCode(response: Response): Promise<ErrorCode | undefined> {
  if (response.ok) return undefined;
  let body: unknown;
  try {
    body = await response.clone().json();
  } catch {
    body = undefined;
  }
  return errorCodeOf(body, response.status);
}

export async function renewSeal(staleId: string | undefined): Promise<boolean> {
  const live = seal.session;
  if (live !== undefined && live.id !== staleId) return true;
  const outcome = await seal.handshake();
  if (outcome === 'invalid') session.reset();
  return outcome === 'ok';
}

async function replayWithFreshSeal(sent: Request, replay: Request): Promise<Response | undefined> {
  const staleId = sent.headers.get(sealHeader) ?? undefined;
  if (!(await renewSeal(staleId))) return undefined;
  const live = seal.session;
  if (live === undefined) return undefined;
  replay.headers.set(sealHeader, live.id);
  return fetch(replay);
}

api.use({
  onRequest({ request, id }) {
    const live = seal.session;
    if (live !== undefined) request.headers.set(sealHeader, live.id);
    replays.set(id, request.clone());
    return request;
  },
  async onResponse({ request, response, id }) {
    const replay = replays.get(id);
    replays.delete(id);
    if (replay === undefined || response.status !== 403) return response;
    if ((await responseErrorCode(response)) !== 'seal_expired') return response;
    return (await replayWithFreshSeal(request, replay)) ?? response;
  },
  onError({ id }) {
    replays.delete(id);
  },
});

export async function call<T>(request: Promise<Attempt<T>>): Promise<Outcome<T>> {
  let attempt: Attempt<T>;
  try {
    attempt = await request;
  } catch (error: unknown) {
    if (isUnreadableReply(error)) return { code: 'internal' };
    throw error;
  }
  if (attempt.response.ok) return { data: attempt.data as T };
  const code = errorCodeOf(attempt.error, attempt.response.status);
  if (attempt.response.status === 401 || code === 'seal_invalid') session.reset();
  return { code };
}
