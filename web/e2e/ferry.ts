import { spawn } from 'node:child_process';
import { mkdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { type components } from '../src/lib/api/schema.js';
import { freePort } from './ports.js';
import { openWifi, type Wifi, type WifiOptions } from './wifi.js';

type Schemas = components['schemas'];
export type Transfer = Schemas['Transfer'];
export type OfferedFile = Schemas['OfferedFile'];
export type Pairing = Schemas['Pairing'];
export type Settings = Schemas['Settings'];
export type SettingsPatch = Schemas['SettingsPatch'];
export type Device = Schemas['Device'];
export type UpdateStatus = Schemas['UpdateStatus'];

const startupTimeoutMs = 15_000;
const healthPollMs = 100;

export type Owner = {
  pairing: () => Promise<Pairing>;
  transfers: () => Promise<Transfer[]>;
  devices: () => Promise<Device[]>;
  offer: (paths: string[]) => Promise<OfferedFile[]>;
  settings: () => Promise<Settings>;
  patchSettings: (patch: SettingsPatch) => Promise<Settings>;
  update: () => Promise<UpdateStatus>;
};

export type Ferry = {
  url: string;
  dataDir: string;
  receivedDir: string;
  scratchDir: string;
  owner: Owner;
  openWifi: (options?: WifiOptions) => Promise<Wifi>;
  output: () => string;
  stop: () => Promise<void>;
};

function ownerApi(url: string): Owner {
  async function request<T>(method: string, route: string, body?: unknown): Promise<T> {
    const response = await fetch(url + route, {
      method,
      ...(body === undefined
        ? {}
        : { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
    });
    if (!response.ok) {
      throw new Error(`${method} ${route}: ${String(response.status)} ${await response.text()}`);
    }
    return (await response.json()) as T;
  }
  return {
    pairing: () => request<Pairing>('GET', '/api/pairing'),
    transfers: () => request<Transfer[]>('GET', '/api/transfers?limit=500'),
    devices: () => request<Device[]>('GET', '/api/devices'),
    offer: (paths) => request<OfferedFile[]>('POST', '/api/files', { paths }),
    settings: () => request<Settings>('GET', '/api/settings'),
    patchSettings: (patch) => request<Settings>('PATCH', '/api/settings', patch),
    update: () => request<UpdateStatus>('GET', '/api/update'),
  };
}

async function isHealthy(url: string): Promise<boolean> {
  try {
    const response = await fetch(`${url}/api/health`, { signal: AbortSignal.timeout(1000) });
    return response.ok && ((await response.json()) as { app?: unknown }).app === 'ferry';
  } catch {
    return false;
  }
}

export async function launchFerry(
  binary: string,
  root: string,
  extraEnv: Readonly<Record<string, string>>,
): Promise<Ferry> {
  const dataDir = path.join(root, 'data');
  const receivedDir = path.join(root, 'received');
  const scratchDir = path.join(root, 'scratch');
  await Promise.all([dataDir, receivedDir, scratchDir].map((dir) => mkdir(dir)));
  await writeFile(path.join(dataDir, 'config.json'), JSON.stringify({ receivedDir }));
  const port = await freePort();
  const url = `http://127.0.0.1:${String(port)}`;
  const child = spawn(binary, [], {
    env: {
      ...process.env,
      ...extraEnv,
      FERRY_DATA_DIR: dataDir,
      FERRY_PORT: String(port),
      FERRY_HEADLESS: '1',
      FERRY_DEV: '1',
    },
    stdio: ['ignore', 'pipe', 'pipe'],
    windowsHide: true,
  });
  let output = '';
  const collect = (chunk: string) => {
    output += chunk;
  };
  child.stdout.setEncoding('utf8').on('data', collect);
  child.stderr.setEncoding('utf8').on('data', collect);
  const exit = { hasHappened: false };
  const exited = new Promise<void>((resolve) => {
    const finish = (reason: unknown) => {
      exit.hasHappened = true;
      if (reason instanceof Error) collect(`${reason.message}\n`);
      resolve();
    };
    child.once('exit', finish);
    child.once('error', finish);
  });
  const links: (() => Promise<void>)[] = [];
  const stop = async () => {
    await Promise.all(links.map((closeLink) => closeLink()));
    if (!exit.hasHappened) child.kill();
    await exited;
  };

  const deadline = Date.now() + startupTimeoutMs;
  while (!(await isHealthy(url))) {
    if (exit.hasHappened || Date.now() > deadline) {
      await stop();
      throw new Error(`Ferry did not start at ${url} from ${binary}:\n${output}`);
    }
    await new Promise((resolve) => setTimeout(resolve, healthPollMs));
  }
  return {
    url,
    dataDir,
    receivedDir,
    scratchDir,
    owner: ownerApi(url),
    openWifi: async (options) => {
      const { close: closeLink, ...wifi } = await openWifi(port, options);
      links.push(closeLink);
      return wifi;
    },
    output: () => output,
    stop,
  };
}
