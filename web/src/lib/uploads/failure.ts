import { isErrorCode, type ErrorCode } from '$lib/api/client';
import { type DetailedError } from './tus';

export type Failure =
  | { status: 'failed'; code: ErrorCode }
  | { status: 'canceled' }
  | { status: 'stalled' | 'queued'; delayMs: number };

const retryDelayMs = 5000;
const busyRetryDelayMs = 10000;
const codeByStatus: Partial<Record<number, ErrorCode>> = {
  400: 'invalid_request',
  401: 'unauthorized',
  403: 'forbidden',
  413: 'too_large',
  429: 'rate_limited',
  507: 'no_space',
};

function parsedBody(body: string): unknown {
  try {
    return JSON.parse(body);
  } catch {
    return undefined;
  }
}

function bodyErrorCode(body: string): ErrorCode | undefined {
  const parsed = parsedBody(body);
  if (typeof parsed !== 'object' || parsed === null || !('error' in parsed)) return undefined;
  const { error } = parsed;
  if (typeof error !== 'object' || error === null || !('code' in error)) return undefined;
  return isErrorCode(error.code) ? error.code : undefined;
}

function retryAfterMs(header: string | undefined): number {
  const seconds = Number(header);
  return Number.isFinite(seconds) && seconds > 0 ? seconds * 1000 : busyRetryDelayMs;
}

function isFinalStatus(status: number): boolean {
  if (status === 413 || status === 507) return true;
  return status >= 400 && status < 500 && status !== 409 && status !== 423;
}

export function classify(error: Error | DetailedError): Failure {
  if (!('originalRequest' in error)) return { status: 'failed', code: 'internal' };
  const response = error.originalResponse;
  if (response === null) return { status: 'stalled', delayMs: retryDelayMs };
  const status = response.getStatus();
  if (status === 429) {
    return { status: 'queued', delayMs: retryAfterMs(response.getHeader('Retry-After')) };
  }
  if (status === 404 || status === 410) return { status: 'canceled' };
  if (!isFinalStatus(status)) return { status: 'stalled', delayMs: retryDelayMs };
  const code = bodyErrorCode(response.getBody()) ?? codeByStatus[status] ?? 'internal';
  return { status: 'failed', code };
}
