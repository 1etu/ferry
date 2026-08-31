import { createHash, randomBytes } from 'node:crypto';
import { createReadStream } from 'node:fs';
import { open, readdir } from 'node:fs/promises';
import path from 'node:path';

export type Payload = { name: string; mimeType: string; buffer: Buffer };

export function hashOf(bytes: Buffer): string {
  return createHash('sha256').update(bytes).digest('hex');
}

export function randomPayload(name: string, size: number, mimeType = 'application/octet-stream') {
  return { name, mimeType, buffer: randomBytes(size) } satisfies Payload;
}

export async function writeRandomFile(filePath: string, size: number): Promise<string> {
  const chunkSize = 1 << 20;
  const hash = createHash('sha256');
  const file = await open(filePath, 'w');
  try {
    for (let written = 0; written < size; written += chunkSize) {
      const chunk = randomBytes(Math.min(chunkSize, size - written));
      hash.update(chunk);
      await file.write(chunk);
    }
  } finally {
    await file.close();
  }
  return hash.digest('hex');
}

export async function sha256(filePath: string): Promise<string> {
  const hash = createHash('sha256');
  for await (const chunk of createReadStream(filePath)) hash.update(chunk as Buffer);
  return hash.digest('hex');
}

export async function receivedFiles(dir: string): Promise<string[]> {
  const entries = await readdir(dir, { withFileTypes: true });
  return entries
    .filter((entry) => entry.isFile())
    .map((entry) => entry.name)
    .sort();
}

export async function hashesIn(dir: string): Promise<Map<string, string>> {
  const names = await receivedFiles(dir);
  const hashes = await Promise.all(names.map((name) => sha256(path.join(dir, name))));
  return new Map(names.map((name, index) => [name, hashes[index] ?? '']));
}
