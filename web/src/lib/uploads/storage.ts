import { isErrorCode, type ErrorCode } from '$lib/api/client';

export type StoredItem = {
  id: string;
  transferId: string | undefined;
  name: string;
  size: number;
  type: string;
  status: 'needsFile' | 'failed';
  sent: number;
  error: ErrorCode | undefined;
  fingerprint: string | undefined;
  uploadUrl: string | undefined;
  nonce: string | undefined;
};

const storageKey = 'ferry.uploads';
const persistIntervalMs = 1000;

function parsedList(raw: string): unknown[] {
  try {
    const parsed: unknown = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

function isOptionalString(value: unknown): value is string | undefined {
  return value === undefined || typeof value === 'string';
}

function isStoredItem(value: unknown): value is StoredItem {
  if (typeof value !== 'object' || value === null) return false;
  const record = value as Record<string, unknown>;
  return (
    typeof record['id'] === 'string' &&
    typeof record['name'] === 'string' &&
    typeof record['size'] === 'number' &&
    typeof record['type'] === 'string' &&
    typeof record['sent'] === 'number' &&
    (record['status'] === 'needsFile' || record['status'] === 'failed') &&
    isOptionalString(record['transferId']) &&
    isOptionalString(record['fingerprint']) &&
    isOptionalString(record['uploadUrl']) &&
    isOptionalString(record['nonce']) &&
    (record['error'] === undefined || isErrorCode(record['error']))
  );
}

export function readStoredItems(): StoredItem[] {
  let raw: string | null;
  try {
    raw = localStorage.getItem(storageKey);
  } catch {
    return [];
  }
  if (raw === null) return [];
  return parsedList(raw).filter(isStoredItem);
}

export function writeStoredItems(records: StoredItem[]): boolean {
  try {
    if (records.length === 0) localStorage.removeItem(storageKey);
    else localStorage.setItem(storageKey, JSON.stringify(records));
    return true;
  } catch {
    return false;
  }
}

export class ItemStore {
  #items: () => StoredItem[];
  #canWrite = true;
  #writtenAt = 0;
  #timer: ReturnType<typeof setTimeout> | undefined;

  constructor(items: () => StoredItem[]) {
    this.#items = items;
  }

  persist(): void {
    if (!this.#canWrite || this.#timer !== undefined) return;
    const wait = this.#writtenAt + persistIntervalMs - Date.now();
    if (wait <= 0) {
      this.#write();
      return;
    }
    this.#timer = setTimeout(() => {
      this.#timer = undefined;
      this.#write();
    }, wait);
  }

  persistNow(): void {
    clearTimeout(this.#timer);
    this.#timer = undefined;
    if (this.#canWrite) this.#write();
  }

  flush(): void {
    if (this.#timer !== undefined) this.persistNow();
  }

  #write(): void {
    this.#writtenAt = Date.now();
    this.#canWrite = writeStoredItems(this.#items());
  }
}
