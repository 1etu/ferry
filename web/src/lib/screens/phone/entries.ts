import { type Transfer } from '$lib/api/client';
import { displayName } from '$lib/uploads/naming';
import { type UploadItem } from '$lib/uploads/queue.svelte';

export type TransferPhase =
  'queued' | 'active' | 'stalled' | 'needsFile' | 'done' | 'failed' | 'canceled';

export type TransferEntry = {
  key: string;
  name: string;
  size: number;
  sent: number;
  isUpload: boolean;
  phase: TransferPhase;
  uploadId: string | undefined;
};

type Placed = { entry: TransferEntry; transferId: string | undefined };

function uploadPhase(upload: UploadItem, transfer: Transfer | undefined): TransferPhase {
  if (transfer?.status === 'canceled') return 'canceled';
  return upload.status === 'uploading' ? 'active' : upload.status;
}

function uploadEntry(upload: UploadItem, transfer: Transfer | undefined): TransferEntry {
  return {
    key: upload.id,
    name: transfer?.name ?? upload.name,
    size: upload.size,
    sent: upload.sent,
    isUpload: true,
    phase: uploadPhase(upload, transfer),
    uploadId: upload.id,
  };
}

function transferEntry(transfer: Transfer): TransferEntry {
  return {
    key: transfer.id,
    name: transfer.name,
    size: transfer.size,
    sent: transfer.done,
    isUpload: transfer.direction === 'in',
    phase: transfer.status,
    uploadId: undefined,
  };
}

function isNewer(queued: Placed, served: Placed): boolean {
  return queued.transferId === undefined || queued.transferId > (served.transferId ?? '');
}

function mergeNewestFirst(fromQueue: readonly Placed[], fromServer: readonly Placed[]) {
  const merged: TransferEntry[] = [];
  let queued = fromQueue[0];
  let served = fromServer[0];
  let queueIndex = 0;
  let serverIndex = 0;
  while (queued !== undefined && served !== undefined) {
    if (isNewer(queued, served)) {
      merged.push(queued.entry);
      queued = fromQueue[++queueIndex];
    } else {
      merged.push(served.entry);
      served = fromServer[++serverIndex];
    }
  }
  const rest = [...fromQueue.slice(queueIndex), ...fromServer.slice(serverIndex)];
  return [...merged, ...rest.map((placed) => placed.entry)];
}

export function mergeEntries(
  uploads: readonly UploadItem[],
  transfers: readonly Transfer[],
): TransferEntry[] {
  const transferById = new Map(transfers.map((transfer) => [transfer.id, transfer]));
  const queuedTransferIds = new Set<string>();
  const fromQueue = uploads.map(({ transferId, ...upload }): Placed => {
    if (transferId !== undefined) queuedTransferIds.add(transferId);
    const transfer = transferId === undefined ? undefined : transferById.get(transferId);
    return { entry: uploadEntry(upload, transfer), transferId };
  });
  const fromServer = transfers
    .filter((transfer) => !queuedTransferIds.has(transfer.id))
    .map((transfer): Placed => ({ entry: transferEntry(transfer), transferId: transfer.id }));
  return mergeNewestFirst(fromQueue, fromServer);
}

export class ArrivalOrder {
  #known: Set<string>;

  constructor(initial: Iterable<string>) {
    this.#known = new Set(initial);
  }

  indexes(keys: readonly string[]): ReadonlyMap<string, number> {
    const arrivals = new Map<string, number>();
    for (const key of keys) {
      if (!this.#known.has(key)) arrivals.set(key, arrivals.size);
    }
    return arrivals;
  }

  isNew(key: string): boolean {
    return !this.#known.has(key);
  }

  settle(keys: Iterable<string>): void {
    this.#known = new Set(keys);
  }
}

export function isConvertedPhoto(file: File, now: Date): boolean {
  return !file.type.startsWith('video/') && displayName(file, now) !== file.name;
}
