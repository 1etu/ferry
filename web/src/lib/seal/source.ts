import type { SealSession } from './session.svelte';
import { frameSize, sealChunk } from './frames';

export type ChunkRequest = {
  file: Blob;
  start: number;
  end: number;
  key: Uint8Array;
  nonce: Uint8Array;
};

export type ChunkSealer = (request: ChunkRequest) => Promise<Blob>;

export type SealedSlice = { value: Blob; done: false };

export type SealedFileReader = {
  openFile(input: Blob, chunkSize: number): Promise<SealedSource>;
};

type WorkerReply =
  { id: number; sealed: Blob } | { id: number; error: { name: string; message: string } };

type Pending = { resolve: (sealed: Blob) => void; reject: (error: Error) => void };

type Prepared = { start: number; end: number; sealed: Promise<Blob> };

export async function sealRange(request: ChunkRequest): Promise<Blob> {
  const plain = new Uint8Array(await request.file.slice(request.start, request.end).arrayBuffer());
  return new Blob([sealChunk(request.key, request.nonce, request.start, plain)]);
}

function isWorkerReply(value: unknown): value is WorkerReply {
  if (typeof value !== 'object' || value === null || !('id' in value)) return false;
  return typeof value.id === 'number' && ('sealed' in value || 'error' in value);
}

function workerError(name: string, message: string): Error {
  const error = new Error(message);
  error.name = name;
  return error;
}

let worker: Worker | undefined;
let nextRequestId = 0;
const pendingById = new Map<number, Pending>();

function settleReply(reply: WorkerReply): void {
  const pending = pendingById.get(reply.id);
  if (pending === undefined) return;
  pendingById.delete(reply.id);
  if ('sealed' in reply) pending.resolve(reply.sealed);
  else pending.reject(workerError(reply.error.name, reply.error.message));
}

function failAll(error: Error): void {
  const pending = [...pendingById.values()];
  pendingById.clear();
  worker = undefined;
  for (const { reject } of pending) reject(error);
}

function startWorker(): Worker {
  const started = new Worker(new URL('./worker.ts', import.meta.url), { type: 'module' });
  started.addEventListener('message', (event: MessageEvent<unknown>) => {
    if (isWorkerReply(event.data)) settleReply(event.data);
  });
  started.addEventListener('error', () => {
    started.terminate();
    failAll(workerError('SealWorkerError', 'seal worker stopped'));
  });
  return started;
}

export function sealInWorker(request: ChunkRequest): Promise<Blob> {
  return new Promise((resolve, reject) => {
    const id = nextRequestId;
    nextRequestId += 1;
    pendingById.set(id, { resolve, reject });
    worker ??= startWorker();
    worker.postMessage({ id, ...request });
  });
}

function keepForNextSlice(): undefined {
  return undefined;
}

export class SealedSource {
  readonly size: number;
  #file: Blob;
  #chunkSize: number;
  #session: SealSession;
  #nonce: Uint8Array;
  #sealChunk: ChunkSealer;
  #ahead: Prepared | undefined;

  constructor(
    file: Blob,
    chunkSize: number,
    session: SealSession,
    nonce: Uint8Array,
    sealChunk: ChunkSealer,
  ) {
    this.size = file.size;
    this.#file = file;
    this.#chunkSize = chunkSize;
    this.#session = session;
    this.#nonce = nonce;
    this.#sealChunk = sealChunk;
  }

  slice(start: number, requestedEnd: number): Promise<SealedSlice> {
    if (start % frameSize !== 0) {
      const detail = `sealed slice must start on a frame boundary, got ${String(start)}`;
      return Promise.reject(new RangeError(detail));
    }
    const end = Math.min(requestedEnd, this.size);
    const sealed = this.#take(start, end);
    this.#prepare(end);
    return sealed.then((value) => ({ value, done: false }));
  }

  close(): void {
    this.#ahead = undefined;
  }

  #take(start: number, end: number): Promise<Blob> {
    const ahead = this.#ahead;
    this.#ahead = undefined;
    if (ahead?.start === start && ahead.end === end) return ahead.sealed;
    return this.#seal(start, end);
  }

  #prepare(start: number): void {
    if (start >= this.size) return;
    const end = Math.min(start + this.#chunkSize, this.size);
    const sealed = this.#seal(start, end);
    sealed.catch(keepForNextSlice);
    this.#ahead = { start, end, sealed };
  }

  #seal(start: number, end: number): Promise<Blob> {
    const { key } = this.#session;
    return this.#sealChunk({ file: this.#file, start, end, key, nonce: this.#nonce });
  }
}

export function sealedFileReader(
  session: SealSession,
  nonce: Uint8Array,
  sealChunk: ChunkSealer = sealInWorker,
): SealedFileReader {
  return {
    openFile: (input, chunkSize) =>
      Promise.resolve(new SealedSource(input, chunkSize, session, nonce, sealChunk)),
  };
}
