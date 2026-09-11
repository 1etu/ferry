import { sealRange, type ChunkRequest } from './source';

type WorkerRequest = ChunkRequest & { id: number };

function isWorkerRequest(value: unknown): value is WorkerRequest {
  if (typeof value !== 'object' || value === null) return false;
  const record = value as Record<string, unknown>;
  return (
    typeof record['id'] === 'number' &&
    record['file'] instanceof Blob &&
    typeof record['start'] === 'number' &&
    typeof record['end'] === 'number' &&
    record['key'] instanceof Uint8Array &&
    record['nonce'] instanceof Uint8Array
  );
}

function errorFields(error: unknown): { name: string; message: string } {
  if (error instanceof Error) return { name: error.name, message: error.message };
  return { name: 'Error', message: String(error) };
}

async function answer(request: WorkerRequest): Promise<void> {
  try {
    const sealed = await sealRange(request);
    postMessage({ id: request.id, sealed });
  } catch (error: unknown) {
    postMessage({ id: request.id, error: errorFields(error) });
  }
}

self.addEventListener('message', (event: MessageEvent<unknown>) => {
  if (isWorkerRequest(event.data)) void answer(event.data);
});
