import { type Row } from './row';
import { previousUploadAt, type DetailedError, type TusUpload, type UploadOptions } from './tus';

export type AttemptReport = {
  onprogress: (sent: number) => void;
  onurl: (url: string) => void;
  onsuccess: () => void;
  onerror: (error: Error | DetailedError) => void;
};

export type Ready = { file: File; fingerprint: string; nonce: string };

export type CreateUpload = (file: File, options: UploadOptions) => TusUpload;

function nextMacrotask(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve));
}

export async function startAttempt(
  row: Row,
  ready: Ready,
  create: CreateUpload,
  report: AttemptReport,
): Promise<void> {
  const upload = create(ready.file, {
    filename: row.name,
    fingerprint: ready.fingerprint,
    nonce: ready.nonce,
    onProgress: (sent) => {
      if (isCurrent()) report.onprogress(sent);
    },
    onSuccess: () => {
      if (isCurrent()) report.onsuccess();
    },
    onError: (error) => {
      if (isCurrent()) report.onerror(error);
    },
    onUploadUrlAvailable: () => {
      if (isCurrent() && upload.url !== null) report.onurl(upload.url);
    },
  });
  const isCurrent = () => row.upload === upload;
  row.upload = upload;
  row.retryAt = 0;
  row.lastProgressAt = Date.now();
  if (row.status === 'queued') row.status = 'uploading';
  if (row.uploadUrl === undefined) {
    upload.start();
    return;
  }
  const previous = await previousUploadAt(upload, row.uploadUrl, row.size);
  if (!isCurrent()) return;
  upload.resumeFromPreviousUpload(previous);
  upload.start();
}

export function restartAttempt(row: Row, upload: TusUpload): void {
  row.status = 'stalled';
  row.lastProgressAt = Date.now();
  void upload
    .abort(false)
    .then(nextMacrotask)
    .then(() => {
      if (row.upload === upload && row.status === 'stalled') upload.start();
    });
}
