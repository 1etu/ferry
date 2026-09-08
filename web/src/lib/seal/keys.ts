import { x25519 } from '@noble/curves/ed25519.js';
import { hkdf } from '@noble/hashes/hkdf.js';
import { hmac } from '@noble/hashes/hmac.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { concatBytes, utf8ToBytes } from '@noble/hashes/utils.js';

export type Keypair = { secretKey: Uint8Array; publicKey: Uint8Array };

export const keySize = 32;
export const pairingSecretSize = 16;

const deviceLabel = utf8ToBytes('ferry-seal-device');
const proofLabel = utf8ToBytes('ferry-seal-proof');
const sessionLabel = utf8ToBytes('ferry-seal-v1');
const confirmLabel = utf8ToBytes('ferry-seal-confirm');

export function clientKeypair(): Keypair {
  return x25519.keygen();
}

export function sharedSecret(secretKey: Uint8Array, publicKey: Uint8Array): Uint8Array {
  return x25519.getSharedSecret(secretKey, publicKey);
}

export function deviceSecretFromPairing(pairingSecret: Uint8Array): Uint8Array {
  return hkdf(sha256, pairingSecret, undefined, deviceLabel, keySize);
}

export function proofFor(deviceSecret: Uint8Array, clientKey: Uint8Array): Uint8Array {
  return hmac(sha256, deviceSecret, concatBytes(proofLabel, clientKey));
}

export function sessionKey(
  shared: Uint8Array,
  deviceSecret: Uint8Array | undefined,
  clientKey: Uint8Array,
  serverKey: Uint8Array,
): Uint8Array {
  const info = concatBytes(sessionLabel, clientKey, serverKey);
  return hkdf(sha256, shared, deviceSecret, info, keySize);
}

export function confirmFor(key: Uint8Array, serverKey: Uint8Array): Uint8Array {
  return hmac(sha256, key, concatBytes(confirmLabel, serverKey));
}

export function firstUseDeviceSecret(
  shared: Uint8Array,
  clientKey: Uint8Array,
  serverKey: Uint8Array,
): Uint8Array {
  return hkdf(sha256, shared, undefined, concatBytes(deviceLabel, clientKey, serverKey), keySize);
}
