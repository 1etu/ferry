import { xchacha20poly1305 } from '@noble/ciphers/chacha.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { bytesToHex, concatBytes, hexToBytes } from '@noble/hashes/utils.js';
import { describe, expect, it } from 'vitest';
import {
  frameAad,
  frameNonce,
  frameOverhead,
  frameSize,
  sealChunk,
  sealedChunkSize,
  tagSize,
} from './frames';
import { sealRange, sealedFileReader, type ChunkRequest, type ChunkSealer } from './source';

const session = {
  id: 'c2Vzc2lvbi1pZA',
  key: hexToBytes('000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f'),
};
const nonce = hexToBytes('e0e1e2e3e4e5e6e7e8e9eaebecedeeef');
const chunkSize = 2 * frameSize;
const mebibyte = frameSize;

function mod251(length: number): Uint8Array<ArrayBuffer> {
  const bytes = new Uint8Array(length);
  for (let index = 0; index < length; index += 1) bytes[index] = index % 251;
  return bytes;
}

function digest(bytes: Uint8Array): string {
  return bytesToHex(sha256(bytes));
}

function openChunk(start: number, sealed: Uint8Array): Uint8Array {
  const view = new DataView(sealed.buffer, sealed.byteOffset, sealed.byteLength);
  const frames: Uint8Array[] = [];
  let offset = 0;
  let index = start / frameSize;
  while (offset < sealed.length) {
    const length = view.getUint32(offset);
    const ciphertext = sealed.subarray(offset + 4, offset + 4 + length + tagSize);
    const cipher = xchacha20poly1305(session.key, frameNonce(nonce, index), frameAad);
    frames.push(cipher.decrypt(ciphertext));
    offset += length + frameOverhead;
    index += 1;
  }
  return concatBytes(...frames);
}

function recordingSealer(fail?: (request: ChunkRequest) => boolean) {
  const ranges: [number, number][] = [];
  const sealedByBlob = new Map<Blob, Uint8Array>();
  const sealer: ChunkSealer = async (request) => {
    ranges.push([request.start, request.end]);
    if (fail?.(request) === true) throw new Error('unreadable');
    const plain = new Uint8Array(
      await request.file.slice(request.start, request.end).arrayBuffer(),
    );
    const sealed = sealChunk(request.key, request.nonce, request.start, plain);
    const blob = new Blob([sealed]);
    sealedByBlob.set(blob, sealed);
    return blob;
  };
  const bytesOf = (blob: Blob): Uint8Array => {
    const sealed = sealedByBlob.get(blob);
    if (sealed === undefined) throw new Error('blob did not come from the sealer');
    return sealed;
  };
  return { ranges, sealer, bytesOf };
}

async function openSource(bytes: Uint8Array<ArrayBuffer>, sealer: ChunkSealer) {
  const file = new File([bytes], 'clip.mov');
  return sealedFileReader(session, nonce, sealer).openFile(file, chunkSize);
}

describe('sealRange', () => {
  it('reads the range from the file and seals it into a blob', async () => {
    const bytes = mod251(40);
    const file = new File([bytes], 'note.txt');
    const sealed = await sealRange({ file, start: 0, end: 40, key: session.key, nonce });
    expect(sealed.size).toBe(sealedChunkSize(40));
    expect(openChunk(0, new Uint8Array(await sealed.arrayBuffer()))).toEqual(bytes);
  });
});

describe('sealed source', () => {
  it('reports the plaintext size and seals whole chunks that open back to the file', async () => {
    const bytes = mod251(4 * mebibyte + mebibyte / 2);
    const { sealer, bytesOf } = recordingSealer();
    const source = await openSource(bytes, sealer);
    expect(source.size).toBe(bytes.length);

    const first = await source.slice(0, chunkSize);
    const second = await source.slice(chunkSize, 2 * chunkSize);
    const last = await source.slice(2 * chunkSize, 3 * chunkSize);

    expect(first.value.size).toBe(sealedChunkSize(chunkSize));
    expect(last.value.size).toBe(sealedChunkSize(mebibyte / 2));
    expect(digest(openChunk(0, bytesOf(first.value)))).toBe(digest(bytes.subarray(0, chunkSize)));
    expect(digest(openChunk(chunkSize, bytesOf(second.value)))).toBe(
      digest(bytes.subarray(chunkSize, 2 * chunkSize)),
    );
    expect(digest(openChunk(2 * chunkSize, bytesOf(last.value)))).toBe(
      digest(bytes.subarray(2 * chunkSize)),
    );
  });

  it('reports done false so tus ends on the server offset, not on the ciphertext size', async () => {
    const bytes = mod251(mebibyte);
    const source = await openSource(bytes, recordingSealer().sealer);
    expect((await source.slice(0, chunkSize)).done).toBe(false);
  });

  it('seals the next chunk while the current one is in flight and serves it without sealing again', async () => {
    const bytes = mod251(4 * mebibyte + 3);
    const { ranges, sealer } = recordingSealer();
    const source = await openSource(bytes, sealer);

    const pending = source.slice(0, chunkSize);
    expect(ranges).toEqual([
      [0, chunkSize],
      [chunkSize, 2 * chunkSize],
    ]);
    await pending;
    await source.slice(chunkSize, 2 * chunkSize);
    expect(ranges).toHaveLength(3);
    expect(ranges[2]).toEqual([2 * chunkSize, bytes.length]);
    await source.slice(2 * chunkSize, 3 * chunkSize);
    expect(ranges).toHaveLength(3);
  });

  it('discards the sealed-ahead chunk when a resume asks for another range', async () => {
    const bytes = mod251(4 * mebibyte);
    const { ranges, sealer, bytesOf } = recordingSealer();
    const source = await openSource(bytes, sealer);

    await source.slice(0, chunkSize);
    const resumed = await source.slice(mebibyte, mebibyte + chunkSize);

    expect(ranges.slice(2)).toEqual([
      [mebibyte, mebibyte + chunkSize],
      [mebibyte + chunkSize, bytes.length],
    ]);
    expect(digest(openChunk(mebibyte, bytesOf(resumed.value)))).toBe(
      digest(bytes.subarray(mebibyte, mebibyte + chunkSize)),
    );
  });

  it('drops the sealed-ahead chunk on close', async () => {
    const bytes = mod251(4 * mebibyte);
    const { ranges, sealer } = recordingSealer();
    const source = await openSource(bytes, sealer);

    await source.slice(0, chunkSize);
    source.close();
    await source.slice(chunkSize, 2 * chunkSize);

    expect(ranges.slice(1)).toEqual([
      [chunkSize, 2 * chunkSize],
      [chunkSize, 2 * chunkSize],
    ]);
  });

  it('sends an empty body for an empty file', async () => {
    const source = await openSource(new Uint8Array(), recordingSealer().sealer);
    const { value, done } = await source.slice(0, chunkSize);
    expect(value.size).toBe(0);
    expect(done).toBe(false);
  });

  it('surfaces a sealing failure on the slice that needs it, not as an unhandled rejection', async () => {
    const bytes = mod251(3 * mebibyte);
    const { sealer } = recordingSealer((request) => request.start === chunkSize);
    const source = await openSource(bytes, sealer);

    await expect(source.slice(0, chunkSize)).resolves.toBeDefined();
    await expect(source.slice(chunkSize, 2 * chunkSize)).rejects.toThrow('unreadable');
  });

  it('refuses a start that is not a frame boundary', async () => {
    const source = await openSource(mod251(mebibyte), recordingSealer().sealer);
    await expect(source.slice(1, chunkSize)).rejects.toThrow(RangeError);
  });
});
