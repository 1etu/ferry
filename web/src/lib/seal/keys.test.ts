import { x25519 } from '@noble/curves/ed25519.js';
import { bytesToHex, hexToBytes } from '@noble/hashes/utils.js';
import { describe, expect, it } from 'vitest';
import vectors from '../../../../internal/seal/testdata/vectors.json';
import { toBase64Url } from './frames';
import {
  confirmFor,
  deviceSecretFromPairing,
  firstUseDeviceSecret,
  proofFor,
  sessionKey,
  sharedSecret,
} from './keys';

const rfc7748 = {
  alicePrivate: '77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a',
  alicePublic: '8520f0098930a754748b7ddcb43ef75a0dbf3a0d26381af4eba4a98eaa9b4e6a',
  bobPrivate: '5dab087e624a8a4b79e17f8b83800ee66f3bb1292618b6fd1c2f8b27ff88e0eb',
  bobPublic: 'de9edb7d7b7dc1b4d35b61c2ece435373f8343c85b78674dadfc7e146f882b4f',
  shared: '4a5d9d5ba4ce2de1728e3bf480350f25e07e21c947d19e3376f09b3c1e161742',
};

function optionalSecret(hex: string): Uint8Array | undefined {
  return hex === '' ? undefined : hexToBytes(hex);
}

describe('x25519', () => {
  it('matches the RFC 7748 Diffie-Hellman vector from both sides', () => {
    const alice = hexToBytes(rfc7748.alicePrivate);
    const bob = hexToBytes(rfc7748.bobPrivate);
    expect(bytesToHex(x25519.getPublicKey(alice))).toBe(rfc7748.alicePublic);
    expect(bytesToHex(sharedSecret(alice, hexToBytes(rfc7748.bobPublic)))).toBe(rfc7748.shared);
    expect(bytesToHex(sharedSecret(bob, hexToBytes(rfc7748.alicePublic)))).toBe(rfc7748.shared);
  });

  it('rejects the all-zero low-order point instead of yielding a zero secret', () => {
    expect(() => sharedSecret(hexToBytes(rfc7748.alicePrivate), new Uint8Array(32))).toThrow();
  });
});

describe('internal/seal/testdata/vectors.json', () => {
  it('derives the pairing device secret', () => {
    const secret = deviceSecretFromPairing(hexToBytes(vectors.pairing.secret));
    expect(bytesToHex(secret)).toBe(vectors.pairing.deviceSecret);
  });

  it.each(vectors.handshakes)('reproduces the handshake $name byte for byte', (vector) => {
    const clientPrivate = hexToBytes(vector.clientPrivate);
    const clientPublic = hexToBytes(vector.clientPublic);
    const serverPublic = hexToBytes(vector.serverPublic);
    const deviceSecret = optionalSecret(vector.deviceSecret);

    expect(bytesToHex(x25519.getPublicKey(clientPrivate))).toBe(vector.clientPublic);
    expect(toBase64Url(clientPublic)).toBe(vector.clientKeyBase64url);
    expect(toBase64Url(serverPublic)).toBe(vector.serverKeyBase64url);

    if (deviceSecret === undefined) {
      expect(vector.proof).toBe('');
    } else {
      expect(bytesToHex(proofFor(deviceSecret, clientPublic))).toBe(vector.proof);
      expect(toBase64Url(proofFor(deviceSecret, clientPublic))).toBe(vector.proofBase64url);
    }

    const shared = sharedSecret(clientPrivate, serverPublic);
    expect(bytesToHex(shared)).toBe(vector.shared);
    expect(bytesToHex(sharedSecret(hexToBytes(vector.serverPrivate), clientPublic))).toBe(
      vector.shared,
    );

    const key = sessionKey(shared, deviceSecret, clientPublic, serverPublic);
    expect(bytesToHex(key)).toBe(vector.key);
    expect(bytesToHex(confirmFor(key, serverPublic))).toBe(vector.confirm);
    expect(toBase64Url(confirmFor(key, serverPublic))).toBe(vector.confirmBase64url);

    if (deviceSecret === undefined) {
      const derived = firstUseDeviceSecret(shared, clientPublic, serverPublic);
      expect(bytesToHex(derived)).toBe(vector.derivedDeviceSecret);
    } else {
      expect(vector.derivedDeviceSecret).toBe('');
    }
  });

  it('covers both a proven and a first-use handshake', () => {
    expect(vectors.handshakes.map((vector) => vector.deviceSecret === '')).toEqual([false, true]);
  });
});

describe('session key', () => {
  it('changes with the device secret salt', () => {
    const [proven] = vectors.handshakes;
    if (proven === undefined) throw new Error('fixture has no handshake');
    const shared = hexToBytes(proven.shared);
    const clientPublic = hexToBytes(proven.clientPublic);
    const serverPublic = hexToBytes(proven.serverPublic);
    const unsalted = sessionKey(shared, undefined, clientPublic, serverPublic);
    expect(bytesToHex(unsalted)).not.toBe(proven.key);
  });
});
