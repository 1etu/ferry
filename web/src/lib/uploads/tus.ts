import { Upload, type DetailedError, type PreviousUpload } from 'tus-js-client';
import {
  fromBase64Url,
  randomBytes,
  sealString,
  toBase64Url,
  uploadNonceSize,
} from '$lib/seal/frames';
import { seal, type SealSession } from '$lib/seal/session.svelte';
import { sealedFileReader } from '$lib/seal/source';

export type { DetailedError, PreviousUpload };

export type UploadOptions = {
  filename: string;
  fingerprint: string;
  nonce: string;
  onProgress: (sent: number, total: number) => void;
  onSuccess: () => void;
  onError: (error: Error | DetailedError) => void;
  onUploadUrlAvailable?: () => void;
};

export type TusUpload = {
  readonly url: string | null;
  start(): void;
  abort(shouldTerminate?: boolean): Promise<void>;
  findPreviousUploads(): Promise<PreviousUpload[]>;
  resumeFromPreviousUpload(previous: PreviousUpload): void;
};

export const sealHeader = 'X-Ferry-Seal';

const endpoint = '/api/uploads/';
const chunkSize = 16 * 1024 * 1024;
const retryDelays = [0, 1000, 3000, 5000, 10000, 20000, 30000];
const finalStatuses = new Set([413, 507]);
const retryableClientStatuses = new Set([409, 423]);

function shouldRetry(error: DetailedError): boolean {
  const status = error.originalResponse?.getStatus() ?? 0;
  if (finalStatuses.has(status)) return false;
  const isClientError = status >= 400 && status < 500;
  return (!isClientError || retryableClientStatuses.has(status)) && navigator.onLine;
}

function sealHeaders(session: SealSession | undefined): Record<string, string> {
  return session === undefined ? {} : { [sealHeader]: session.id };
}

export function newUploadNonce(): string {
  return toBase64Url(randomBytes(uploadNonceSize));
}

export function createUpload(file: File, options: UploadOptions): TusUpload {
  const session = seal.session;
  if (session === undefined) throw new Error('upload without a sealed session');
  const nonce = fromBase64Url(options.nonce);
  if (nonce?.length !== uploadNonceSize) throw new Error('upload nonce is not 16 bytes');
  return new Upload(file, {
    endpoint,
    chunkSize,
    retryDelays,
    uploadDataDuringCreation: true,
    headers: sealHeaders(session),
    fileReader: sealedFileReader(session, nonce),
    metadata: {
      name: sealString(session.key, options.filename),
      nonce: options.nonce,
      filetype: file.type,
      lastModified: String(file.lastModified),
    },
    fingerprint: () => Promise.resolve(options.fingerprint),
    storeFingerprintForResuming: true,
    removeFingerprintOnSuccess: true,
    onProgress: (sent, total) => {
      options.onProgress(Math.min(sent, total), total);
    },
    onSuccess: options.onSuccess,
    onError: options.onError,
    onShouldRetry: shouldRetry,
    onUploadUrlAvailable: options.onUploadUrlAvailable ?? null,
  });
}

export function terminateUpload(url: string): Promise<void> {
  return Upload.terminate(url, { retryDelays, headers: sealHeaders(seal.session) });
}

export async function previousUploadAt(
  upload: TusUpload,
  uploadUrl: string,
  size: number,
): Promise<PreviousUpload> {
  let known: PreviousUpload[];
  try {
    known = await upload.findPreviousUploads();
  } catch {
    known = [];
  }
  return (
    known.find((candidate) => candidate.uploadUrl === uploadUrl) ?? {
      size,
      metadata: {},
      creationTime: '',
      urlStorageKey: '',
      uploadUrl,
      parallelUploadUrls: null,
    }
  );
}
