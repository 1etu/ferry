import { renewSeal } from '$lib/api/client';
import { seal } from '$lib/seal/session.svelte';
import { createUpload, terminateUpload, type TusUpload, type UploadOptions } from './tus';

export type UploadFactory = {
  create: (file: File, options: UploadOptions) => TusUpload;
  terminate: (url: string) => Promise<void>;
  sessionId: () => string | undefined;
  renewSession: (staleId: string | undefined) => Promise<boolean>;
};

let factory: UploadFactory = {
  create: createUpload,
  terminate: terminateUpload,
  sessionId: () => seal.session?.id,
  renewSession: renewSeal,
};

export function uploadFactory(): UploadFactory {
  return factory;
}

export function setUploadFactory(next: UploadFactory): void {
  factory = next;
}
