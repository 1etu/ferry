import { xchacha20poly1305 } from '@noble/ciphers/chacha.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { bytesToHex, concatBytes, hexToBytes } from '@noble/hashes/utils.js';
import { describe, expect, it } from 'vitest';
import vectors from '../../../../internal/seal/testdata/vectors.json';
import {
  frameAad,
  frameCount,
  frameNonce,
  frameOverhead,
  frameSize,
  fromBase64Url,
  openString,
  randomBytes,
  sealChunk,
  sealString,
  sealedChunkSize,
  tagSize,
  toBase64Url,
} from './frames';

const frameKey = hexToBytes(vectors.frames.key);
const uploadNonce = hexToBytes(vectors.frames.nonce);
const hello = new TextEncoder().encode('hello');
const [ascii] = vectors.strings;
if (ascii === undefined) throw new Error('fixture has no sealed string');
const stringKey = hexToBytes(ascii.key);

function mod251(length: number): Uint8Array {
  const bytes = new Uint8Array(length);
  for (let index = 0; index < length; index += 1) bytes[index] = index % 251;
  return bytes;
}

function openFrames(start: number, sealed: Uint8Array): Uint8Array[] {
  const view = new DataView(sealed.buffer, sealed.byteOffset, sealed.byteLength);
  const frames: Uint8Array[] = [];
  let offset = 0;
  let index = start / frameSize;
  while (offset < sealed.length) {
    const length = view.getUint32(offset);
    const ciphertext = sealed.subarray(offset + 4, offset + 4 + length + tagSize);
    const cipher = xchacha20poly1305(frameKey, frameNonce(uploadNonce, index), frameAad);
    frames.push(cipher.decrypt(ciphertext));
    offset += length + frameOverhead;
    index += 1;
  }
  return frames;
}

describe('base64url', () => {
  it('encodes without padding and with the url alphabet', () => {
    expect(toBase64Url(new Uint8Array([0xfb, 0xff]))).toBe('-_8');
    expect(toBase64Url(hexToBytes('000102030405060708090a0b0c0d0e0f'))).toBe(
      'AAECAwQFBgcICQoLDA0ODw',
    );
    expect(toBase64Url(new Uint8Array())).toBe('');
  });

  it('decodes what it encodes for every length', () => {
    for (let length = 0; length < 40; length += 1) {
      const bytes = randomBytes(length);
      expect(fromBase64Url(toBase64Url(bytes))).toEqual(bytes);
    }
  });

  it.each([
    { name: 'standard alphabet', text: 'AAECAwQFBgcICQoLDA0ODw+/' },
    { name: 'padding', text: 'AAECAwQFBgcICQoLDA0ODw==' },
    { name: 'impossible length', text: 'AAECA' },
    { name: 'whitespace', text: 'AAEC AwQF' },
  ])('rejects $name', ({ text }) => {
    expect(fromBase64Url(text)).toBeUndefined();
  });
});

describe('sealed strings against internal/seal/testdata/vectors.json', () => {
  it.each(vectors.strings)('seals and opens the $name name byte for byte', (vector) => {
    const key = hexToBytes(vector.key);
    expect(toBase64Url(hexToBytes(vector.sealed))).toBe(vector.sealedBase64url);
    expect(sealString(key, vector.plain, hexToBytes(vector.nonce))).toBe(vector.sealedBase64url);
    expect(openString(key, vector.sealedBase64url)).toBe(vector.plain);
  });

  it('covers ascii, unicode and empty names', () => {
    expect(vectors.strings.map((vector) => vector.name)).toEqual(['ascii', 'unicode', 'empty']);
  });
});

describe('sealed strings', () => {
  it('draws a fresh nonce per call', () => {
    expect(sealString(stringKey, 'a.jpg')).not.toBe(sealString(stringKey, 'a.jpg'));
  });

  it.each([
    { name: 'a flipped ciphertext byte', at: 30 },
    { name: 'a flipped nonce byte', at: 0 },
    { name: 'a flipped tag byte', at: -1 },
  ])('refuses $name', ({ at }) => {
    const sealed = hexToBytes(ascii.sealed);
    const position = at < 0 ? sealed.length + at : at;
    sealed[position] = (sealed[position] ?? 0) ^ 1;
    expect(openString(stringKey, toBase64Url(sealed))).toBeUndefined();
  });

  it.each([
    {
      name: 'a value shorter than nonce plus tag',
      sealed: toBase64Url(hexToBytes(ascii.sealed).subarray(0, 39)),
    },
    {
      name: 'standard base64',
      sealed: `${ascii.sealedBase64url.replaceAll('-', '+').replaceAll('_', '/')}=`,
    },
    { name: 'the empty string', sealed: '' },
  ])('refuses $name', ({ sealed }) => {
    expect(openString(stringKey, sealed)).toBeUndefined();
  });

  it('refuses another key', () => {
    expect(openString(frameKey, ascii.sealedBase64url)).toBeUndefined();
  });
});

describe('frame nonces', () => {
  it('appends the index as eight big-endian bytes', () => {
    const prefix = bytesToHex(uploadNonce);
    expect(bytesToHex(frameNonce(uploadNonce, 1))).toBe(`${prefix}0000000000000001`);
    expect(bytesToHex(frameNonce(uploadNonce, 2 ** 32 + 5))).toBe(`${prefix}0000000100000005`);
  });
});

describe('frames against internal/seal/testdata/vectors.json', () => {
  it.each(vectors.frames.small)('seals $name byte for byte', (vector) => {
    const start = vector.index * frameSize;
    const sealed = sealChunk(frameKey, uploadNonce, start, hexToBytes(vector.plain));
    expect(bytesToHex(sealed)).toBe(vector.frame);
  });

  it.each(vectors.frames.large)('seals the $name with the pinned digest', (vector) => {
    const start = vector.index * frameSize;
    const sealed = sealChunk(frameKey, uploadNonce, start, mod251(vector.plainLength));
    expect(sealed.length).toBe(vector.frameLength);
    expect(bytesToHex(sha256(sealed))).toBe(vector.frameSha256);
  });

  it('seals the stream and the same stream from the resume offset', () => {
    const { stream } = vectors.frames;
    const plain = mod251(stream.plainLength);
    expect(frameCount(plain.length)).toBe(stream.frameCount);

    const body = sealChunk(frameKey, uploadNonce, 0, plain);
    expect(body.length).toBe(stream.bodyLength);
    expect(bytesToHex(sha256(body))).toBe(stream.bodySha256);

    const rest = plain.subarray(stream.resumeOffset);
    const resumed = sealChunk(frameKey, uploadNonce, stream.resumeOffset, rest);
    expect(resumed.length).toBe(stream.resumeBodyLength);
    expect(bytesToHex(sha256(resumed))).toBe(stream.resumeBodySha256);
  });
});

describe('sealChunk', () => {
  it('lays out length-prefixed frames that open back to the plaintext', () => {
    const plain = mod251(2 * frameSize + 5);
    const sealed = sealChunk(frameKey, uploadNonce, 0, plain);
    expect(sealed.length).toBe(sealedChunkSize(plain.length));
    const frames = openFrames(0, sealed);
    expect(frames.map((frame) => frame.length)).toEqual([frameSize, frameSize, 5]);
    expect(bytesToHex(sha256(concatBytes(...frames)))).toBe(bytesToHex(sha256(plain)));
  });

  it('binds every frame to its index so a replay at another index fails', () => {
    const sealed = sealChunk(frameKey, uploadNonce, 0, hello);
    expect(() => openFrames(frameSize, sealed)).toThrow();
  });

  it('seals nothing for an empty plaintext', () => {
    expect(sealChunk(frameKey, uploadNonce, 0, new Uint8Array()).length).toBe(0);
    expect(sealedChunkSize(0)).toBe(0);
  });

  it('refuses a start that is not a frame boundary', () => {
    expect(() => sealChunk(frameKey, uploadNonce, 7, hello)).toThrow(RangeError);
  });
});
