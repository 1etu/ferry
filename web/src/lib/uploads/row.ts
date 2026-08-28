import { type ErrorCode, type Transfer } from '$lib/api/client';
import { displayName } from './naming';
import { type StoredItem } from './storage';
import { type TusUpload } from './tus';

export type UploadStatus =
  'queued' | 'uploading' | 'stalled' | 'needsFile' | 'done' | 'failed' | 'canceled';

export type UploadItem = {
  id: string;
  transferId?: string;
  name: string;
  size: number;
  type: string;
  status: UploadStatus;
  sent: number;
  error?: ErrorCode;
};

export type Row = Omit<StoredItem, 'status'> & {
  status: UploadStatus;
  file: File | undefined;
  upload: TusUpload | undefined;
  sealId: string | undefined;
  lastProgressAt: number;
  retryAt: number;
};

const localIdBytes = 12;

function localId(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(localIdBytes));
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
}

export function pickedRow(file: File): Row {
  return {
    id: localId(),
    transferId: undefined,
    name: displayName(file, new Date()),
    size: file.size,
    type: file.type,
    status: 'queued',
    sent: 0,
    error: undefined,
    fingerprint: undefined,
    uploadUrl: undefined,
    nonce: undefined,
    file,
    upload: undefined,
    sealId: undefined,
    lastProgressAt: 0,
    retryAt: 0,
  };
}

export function restoredRow(stored: StoredItem): Row {
  return {
    ...stored,
    file: undefined,
    upload: undefined,
    sealId: undefined,
    lastProgressAt: 0,
    retryAt: 0,
  };
}

export function replaceFile(row: Row, file: File, fingerprint: string): void {
  row.transferId = undefined;
  row.uploadUrl = undefined;
  row.nonce = undefined;
  row.name = displayName(file, new Date());
  row.size = file.size;
  row.type = file.type;
  row.sent = 0;
  row.fingerprint = fingerprint;
  row.file = file;
}

export function storedItem(row: Row): StoredItem | undefined {
  if (row.status === 'done' || row.status === 'canceled') return undefined;
  const { id, transferId, name, size, type, sent, error, fingerprint, uploadUrl, nonce } = row;
  const status = row.status === 'failed' ? 'failed' : 'needsFile';
  return { id, transferId, name, size, type, status, sent, error, fingerprint, uploadUrl, nonce };
}

export function publicItem(row: Row): UploadItem {
  const { id, name, size, type, status, sent } = row;
  const item: UploadItem = { id, name, size, type, status, sent };
  if (row.transferId !== undefined) item.transferId = row.transferId;
  if (row.error !== undefined) item.error = row.error;
  return item;
}

export function transferIdOf(url: string): string | undefined {
  return url
    .split('/')
    .filter((segment) => segment !== '')
    .at(-1);
}

export function isInFlight(status: UploadStatus): boolean {
  return status === 'uploading' || status === 'stalled';
}

export function isSettled(status: UploadStatus): boolean {
  return status === 'done' || status === 'failed' || status === 'canceled';
}

export function awaitsFile(status: UploadStatus): boolean {
  return status === 'needsFile' || status === 'failed';
}

export function isStartable(row: Row, now: number): boolean {
  return (
    row.status === 'queued' &&
    row.retryAt <= now &&
    row.file !== undefined &&
    row.fingerprint !== undefined
  );
}

export function settledByServer(
  row: Row,
  status: Transfer['status'],
): 'done' | 'canceled' | undefined {
  if (status === 'done') return row.status === 'done' ? undefined : 'done';
  if (status !== 'canceled' || row.status === 'done' || row.status === 'canceled') return undefined;
  return 'canceled';
}

export function settle(row: Row, status: 'done' | 'failed' | 'canceled', error?: ErrorCode): void {
  row.status = status;
  row.error = error;
  row.file = undefined;
  row.upload = undefined;
  if (status === 'done') row.sent = row.size;
}

export function requeue(row: Row, file: File): void {
  row.file = file;
  row.status = 'queued';
  row.error = undefined;
  row.retryAt = 0;
}

export function rowAwaiting(rows: readonly Row[], fingerprint: string, preferred: Row): Row {
  if (preferred.fingerprint === fingerprint) return preferred;
  const waiting = rows.find(
    (row) => row !== preferred && awaitsFile(row.status) && row.fingerprint === fingerprint,
  );
  return waiting ?? preferred;
}
