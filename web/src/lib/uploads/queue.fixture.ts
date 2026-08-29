import { afterEach, beforeEach, vi } from 'vitest';
import { type Transfer } from '$lib/api/client';
import { setUploadFactory, UploadQueue, type UploadItem } from './queue.svelte';
import { type DetailedError, type PreviousUpload, type TusUpload, type UploadOptions } from './tus';

export type FakeResponse = { status: number; body?: string; retryAfter?: string };

export const storageKey = 'ferry.uploads';
export const pickedAt = new Date(2026, 9, 2, 14, 3, 7);
export const sealExpiredBody = '{"error":{"code":"seal_expired","message":"x"}}';

const frameMs = 16;
const fakedTimers = [
  'setTimeout',
  'clearTimeout',
  'setInterval',
  'clearInterval',
  'setImmediate',
  'clearImmediate',
  'Date',
  'requestAnimationFrame',
  'cancelAnimationFrame',
] as const;

export class FakeUpload implements TusUpload {
  url: string | null = null;
  starts = 0;
  aborts: boolean[] = [];
  resumedFrom: PreviousUpload | undefined;
  previous: PreviousUpload[] = [];
  readonly file: File;
  readonly options: UploadOptions;

  constructor(file: File, options: UploadOptions) {
    this.file = file;
    this.options = options;
  }

  start(): void {
    this.starts += 1;
  }

  abort(shouldTerminate = false): Promise<void> {
    this.aborts.push(shouldTerminate);
    return Promise.resolve();
  }

  findPreviousUploads(): Promise<PreviousUpload[]> {
    return Promise.resolve(this.previous);
  }

  resumeFromPreviousUpload(previous: PreviousUpload): void {
    this.resumedFrom = previous;
    this.url = previous.uploadUrl;
  }

  create(id: string): void {
    this.url = `/api/uploads/${id}`;
    this.options.onUploadUrlAvailable?.();
  }

  progress(sent: number): void {
    this.options.onProgress(sent, this.file.size);
  }

  succeed(): void {
    this.options.onSuccess();
  }

  fail(response: FakeResponse | null): void {
    this.options.onError(detailedError(response));
  }
}

export function detailedError(response: FakeResponse | null): DetailedError {
  const error = new Error('tus: request failed') as DetailedError;
  error.originalRequest = {} as DetailedError['originalRequest'];
  error.causingError = null;
  error.originalResponse =
    response === null
      ? null
      : {
          getStatus: () => response.status,
          getHeader: (name: string) => (name === 'Retry-After' ? response.retryAfter : undefined),
          getBody: () => response.body ?? '',
          getUnderlyingObject: () => undefined,
        };
  return error;
}

export function previousUpload(url: string): PreviousUpload {
  return {
    size: null,
    metadata: {},
    creationTime: '',
    urlStorageKey: 'tus::key',
    uploadUrl: url,
    parallelUploadUrls: null,
  };
}

export function serverTransfer(id: string, status: Transfer['status']): Transfer {
  return {
    id,
    deviceId: 'D1',
    direction: 'in',
    name: 'a.jpg',
    size: 1000,
    done: 400,
    status,
    createdAt: '2026-10-02T14:03:07.000Z',
    updatedAt: '2026-10-02T14:03:09.000Z',
  };
}

export function fileOf(name: string, seed: number, type = 'image/jpeg', size = 1000): File {
  return new File([new Uint8Array(size).fill(seed)], name, { type });
}

export async function settle(): Promise<void> {
  await vi.advanceTimersByTimeAsync(0);
}

export async function frame(): Promise<void> {
  await vi.advanceTimersByTimeAsync(2 * frameMs);
}

export function only(queue: UploadQueue): UploadItem {
  const [first] = queue.items;
  if (first === undefined || queue.items.length !== 1) throw new Error('expected one item');
  return first;
}

export function stored(): unknown[] {
  return JSON.parse(localStorage.getItem(storageKey) ?? '[]') as unknown[];
}

export class QueueHarness {
  uploads: FakeUpload[] = [];
  terminated: string[] = [];
  previousByFingerprint = new Map<string, PreviousUpload[]>();
  isOnline = true;
  visibility: DocumentVisibilityState = 'visible';
  sessionId: string | undefined = 'S0';
  renewals: (string | undefined)[] = [];
  handshakes = 0;
  canHandshake = true;
  #queues: UploadQueue[] = [];

  newQueue(): UploadQueue {
    const queue = new UploadQueue();
    this.#queues.push(queue);
    return queue;
  }

  async queueWith(...files: File[]): Promise<{ queue: UploadQueue; items: UploadItem[] }> {
    const queue = this.newQueue();
    await queue.add(files);
    await settle();
    return { queue, items: queue.items };
  }

  setVisibility(visibility: DocumentVisibilityState): void {
    this.visibility = visibility;
    document.dispatchEvent(new Event('visibilitychange'));
  }

  createUpload(file: File, options: UploadOptions): FakeUpload {
    const fake = new FakeUpload(file, options);
    fake.previous = this.previousByFingerprint.get(options.fingerprint) ?? [];
    this.uploads.push(fake);
    return fake;
  }

  renewSession(staleId: string | undefined): Promise<boolean> {
    this.renewals.push(staleId);
    if (this.sessionId !== undefined && this.sessionId !== staleId) return Promise.resolve(true);
    if (!this.canHandshake) return Promise.resolve(false);
    this.handshakes += 1;
    this.sessionId = `S${String(this.handshakes)}`;
    return Promise.resolve(true);
  }

  installFactory(): void {
    setUploadFactory({
      create: (file, options) => this.createUpload(file, options),
      terminate: (url) => {
        this.terminated.push(url);
        return Promise.resolve();
      },
      sessionId: () => this.sessionId,
      renewSession: (staleId) => this.renewSession(staleId),
    });
  }

  clear(): void {
    for (const queue of this.#queues) for (const item of queue.items) queue.remove(item.id);
    this.#queues = [];
  }
}

export function useQueueHarness(): QueueHarness {
  const harness = new QueueHarness();
  beforeEach(() => {
    vi.useFakeTimers({ toFake: [...fakedTimers] });
    vi.setSystemTime(pickedAt);
    localStorage.clear();
    harness.uploads = [];
    harness.terminated = [];
    harness.previousByFingerprint = new Map();
    harness.isOnline = true;
    harness.visibility = 'visible';
    harness.sessionId = 'S0';
    harness.renewals = [];
    harness.handshakes = 0;
    harness.canHandshake = true;
    Object.defineProperty(navigator, 'onLine', { configurable: true, get: () => harness.isOnline });
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => harness.visibility,
    });
    harness.installFactory();
  });
  afterEach(() => {
    harness.clear();
    vi.restoreAllMocks();
    vi.useRealTimers();
  });
  return harness;
}
