import { xchacha20poly1305 } from '@noble/ciphers/chacha.js';
import { concatBytes, utf8ToBytes } from '@noble/hashes/utils.js';

export const frameSize = 1 << 20;
export const tagSize = 16;
export const frameLengthSize = 4;
export const frameOverhead = frameLengthSize + tagSize;
export const uploadNonceSize = 16;
export const stringNonceSize = 24;
export const minSealedStringSize = stringNonceSize + tagSize;
export const nameAad = utf8ToBytes('ferry-name');
export const frameAad = utf8ToBytes('ferry-frame');

const frameNonceSize = 24;
const uint32Span = 0x100000000;

export function randomBytes(length: number): Uint8Array<ArrayBuffer> {
  return crypto.getRandomValues(new Uint8Array(length));
}

export function toBase64Url(bytes: Uint8Array): string {
  let binary = '';
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replaceAll('+', '-').replaceAll('/', '_').replace(/=+$/, '');
}

export function fromBase64Url(text: string): Uint8Array<ArrayBuffer> | undefined {
  if (!/^[A-Za-z0-9_-]*$/.test(text) || text.length % 4 === 1) return undefined;
  const padded = text
    .replaceAll('-', '+')
    .replaceAll('_', '/')
    .padEnd(Math.ceil(text.length / 4) * 4, '=');
  let binary: string;
  try {
    binary = atob(padded);
  } catch {
    return undefined;
  }
  return Uint8Array.from(binary, (char) => char.charCodeAt(0));
}

export function sealString(
  key: Uint8Array,
  plain: string,
  nonce: Uint8Array = randomBytes(stringNonceSize),
): string {
  const ciphertext = xchacha20poly1305(key, nonce, nameAad).encrypt(utf8ToBytes(plain));
  return toBase64Url(concatBytes(nonce, ciphertext));
}

export function openString(key: Uint8Array, sealed: string): string | undefined {
  const bytes = fromBase64Url(sealed);
  if (bytes === undefined || bytes.length < minSealedStringSize) return undefined;
  const nonce = bytes.subarray(0, stringNonceSize);
  try {
    const plain = xchacha20poly1305(key, nonce, nameAad).decrypt(bytes.subarray(stringNonceSize));
    return new TextDecoder().decode(plain);
  } catch {
    return undefined;
  }
}

export function frameNonce(uploadNonce: Uint8Array, index: number): Uint8Array<ArrayBuffer> {
  const nonce = new Uint8Array(frameNonceSize);
  nonce.set(uploadNonce);
  const view = new DataView(nonce.buffer);
  view.setUint32(uploadNonceSize, Math.floor(index / uint32Span));
  view.setUint32(uploadNonceSize + 4, index % uint32Span);
  return nonce;
}

export function frameCount(plainLength: number): number {
  return Math.ceil(plainLength / frameSize);
}

export function sealedChunkSize(plainLength: number): number {
  return plainLength + frameCount(plainLength) * frameOverhead;
}

export function sealChunk(
  key: Uint8Array,
  uploadNonce: Uint8Array,
  start: number,
  plain: Uint8Array,
): Uint8Array<ArrayBuffer> {
  if (start % frameSize !== 0) {
    throw new RangeError(`sealed chunk must start on a frame boundary, got ${String(start)}`);
  }
  const sealed = new Uint8Array(sealedChunkSize(plain.length));
  const view = new DataView(sealed.buffer);
  let index = start / frameSize;
  let written = 0;
  for (let offset = 0; offset < plain.length; offset += frameSize) {
    const length = Math.min(frameSize, plain.length - offset);
    view.setUint32(written, length);
    const cipher = xchacha20poly1305(key, frameNonce(uploadNonce, index), frameAad);
    const ciphertextStart = written + frameLengthSize;
    cipher.encrypt(
      plain.subarray(offset, offset + length),
      sealed.subarray(ciphertextStart, ciphertextStart + length + tagSize),
    );
    written += length + frameOverhead;
    index += 1;
  }
  return sealed;
}
