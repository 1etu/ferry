import { execFile } from 'node:child_process';
import { createHash, generateKeyPairSync, randomBytes, sign } from 'node:crypto';
import { createServer, type Server } from 'node:http';
import path from 'node:path';
import { promisify } from 'node:util';
import { fileURLToPath } from 'node:url';
import { close, listen } from './ports.js';

export const versionUnderTest = '0.0.1';

const repoRoot = fileURLToPath(new URL('../..', import.meta.url));
const manifestPath = '/releases/latest/download/manifest.json';
const signaturePath = '/releases/latest/download/manifest.sig';
const assetName = 'Ferry.exe';
const goArch: Partial<Record<string, string>> = { x64: 'amd64', arm64: 'arm64' };
const goOs: Partial<Record<string, string>> = { win32: 'windows', darwin: 'darwin' };

export type Release = { version: string; isSignatureValid: boolean };

export type ReleaseServer = {
  env: Record<string, string>;
  asset: Buffer;
  publish: (release: Release) => void;
  close: () => Promise<void>;
};

function platformKey(): string {
  return `${goOs[process.platform] ?? process.platform}-${goArch[process.arch] ?? process.arch}`;
}

function signedManifest(release: Release, asset: Buffer, signer: (body: Buffer) => Buffer) {
  const body = Buffer.from(
    JSON.stringify({
      assets: {
        [platformKey()]: {
          name: assetName,
          sha256: createHash('sha256').update(asset).digest('hex'),
          size: asset.length,
        },
      },
      version: release.version,
    }),
  );
  const signed = release.isSignatureValid ? body : Buffer.concat([body, Buffer.from(' ')]);
  return { body, signature: signer(signed).toString('base64') };
}

export async function serveReleases(): Promise<ReleaseServer> {
  const { publicKey, privateKey } = generateKeyPairSync('ed25519');
  const rawPublicKey = publicKey.export({ format: 'der', type: 'spki' }).subarray(-32);
  const asset = randomBytes(64 * 1024);
  const signer = (body: Buffer) => sign(null, body, privateKey);
  let published = signedManifest(
    { version: versionUnderTest, isSignatureValid: true },
    asset,
    signer,
  );
  let assetPath = '';
  const server: Server = createServer((request, response) => {
    const route = new URL(request.url ?? '/', 'http://127.0.0.1').pathname;
    const body = new Map([
      [manifestPath, published.body],
      [signaturePath, Buffer.from(published.signature)],
      [assetPath, asset],
    ]).get(route);
    response.writeHead(body === undefined ? 404 : 200);
    response.end(body);
  });
  const port = await listen(server);
  return {
    env: {
      FERRY_UPDATE_MANIFEST_URL: `http://127.0.0.1:${String(port)}${manifestPath}`,
      FERRY_UPDATE_PUBLIC_KEY: rawPublicKey.toString('base64'),
    },
    asset,
    publish: (release) => {
      published = signedManifest(release, asset, signer);
      assetPath = `/releases/download/v${release.version}/${assetName}`;
    },
    close: () => close(server),
  };
}

export function stagedPath(binary: string): string {
  const extension = path.extname(binary);
  return `${binary.slice(0, binary.length - extension.length)}.new${extension}`;
}

export function versionedPath(binary: string): string {
  const extension = path.extname(binary);
  const stem = binary.slice(0, binary.length - extension.length);
  return `${stem}-${versionUnderTest}${extension}`;
}

export async function buildVersioned(output: string): Promise<void> {
  await promisify(execFile)(
    'go',
    ['build', '-ldflags', `-X main.version=${versionUnderTest}`, '-o', output, './cmd/ferry'],
    { cwd: repoRoot, env: { ...process.env, CGO_ENABLED: '0' } },
  );
}
