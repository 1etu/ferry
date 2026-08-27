import { describe, expect, it } from 'vitest';
import { fingerprintFile, fnv1a32 } from './fingerprint';

const encoder = new TextEncoder();

function patternedBytes(length: number, salt = 0): Uint8Array<ArrayBuffer> {
  const bytes = new Uint8Array(length);
  for (let index = 0; index < length; index += 1) bytes[index] = (index * 31 + salt) % 251;
  return bytes;
}

describe('fnv1a32', () => {
  it.each([
    ['', 0x811c9dc5],
    ['a', 0xe40c292c],
    ['foobar', 0xbf9cf968],
  ])('hashes %j to the reference value', (text, expected) => {
    expect(fnv1a32(encoder.encode(text))).toBe(expected);
  });
});

describe('fingerprintFile', () => {
  it('is stable for the same bytes across File instances', async () => {
    const bytes = patternedBytes(300 * 1024);
    const first = await fingerprintFile(new File([bytes], 'a.bin'));
    const second = await fingerprintFile(new File([bytes.slice()], 'renamed.bin'));
    expect(first).toBe(second);
    expect(first).toMatch(/^ferry-307200-[0-9a-f]{8}$/);
  });

  it('changes when the head changes', async () => {
    const bytes = patternedBytes(300 * 1024);
    const changed = bytes.slice();
    changed[10] = (changed[10] ?? 0) + 1;
    expect(await fingerprintFile(new File([changed], 'b.bin'))).not.toBe(
      await fingerprintFile(new File([bytes], 'a.bin')),
    );
  });

  it('changes when the tail changes', async () => {
    const bytes = patternedBytes(300 * 1024);
    const changed = bytes.slice();
    changed[bytes.length - 5] = (changed[bytes.length - 5] ?? 0) + 1;
    expect(await fingerprintFile(new File([changed], 'b.bin'))).not.toBe(
      await fingerprintFile(new File([bytes], 'a.bin')),
    );
  });

  it('ignores the middle of large files', async () => {
    const bytes = patternedBytes(300 * 1024);
    const changed = bytes.slice();
    changed[150 * 1024] = (changed[150 * 1024] ?? 0) + 1;
    expect(await fingerprintFile(new File([changed], 'b.bin'))).toBe(
      await fingerprintFile(new File([bytes], 'a.bin')),
    );
  });

  it('includes the size', async () => {
    const small = await fingerprintFile(new File([patternedBytes(10)], 'a.bin'));
    expect(small).toMatch(/^ferry-10-/);
    expect(await fingerprintFile(new File([], 'empty.bin'))).toBe(
      `ferry-0-${fnv1a32(new Uint8Array(), fnv1a32(new Uint8Array())).toString(16)}`,
    );
  });
});
