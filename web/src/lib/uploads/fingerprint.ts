const edgeBytes = 64 * 1024;
const fnvOffsetBasis = 0x811c9dc5;
const fnvPrime = 0x01000193;

export function fnv1a32(bytes: Uint8Array, seed: number = fnvOffsetBasis): number {
  let hash = seed;
  for (const byte of bytes) {
    hash = Math.imul(hash ^ byte, fnvPrime);
  }
  return hash >>> 0;
}

async function readBytes(blob: Blob): Promise<Uint8Array> {
  return new Uint8Array(await blob.arrayBuffer());
}

export async function fingerprintFile(file: Blob): Promise<string> {
  const head = await readBytes(file.slice(0, edgeBytes));
  const tail = await readBytes(file.slice(Math.max(0, file.size - edgeBytes)));
  const hash = fnv1a32(tail, fnv1a32(head));
  return `ferry-${String(file.size)}-${hash.toString(16).padStart(8, '0')}`;
}
