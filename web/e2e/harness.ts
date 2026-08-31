import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import {
  test as base,
  type Page,
  type PlaywrightTestArgs,
  type PlaywrightWorkerArgs,
  type TestInfo,
} from '@playwright/test';
import { actors, type ActorName } from '../playwright.config.js';
import { launchFerry, type Ferry } from './ferry.js';
import { guardOwnerPage } from './flows.js';

const webDir = fileURLToPath(new URL('..', import.meta.url));

export const defaultBinary = path.resolve(
  process.env.FERRY_BIN ?? path.join(webDir, '../bin/Ferry.exe'),
);

type TestFixtures = {
  serverEnv: Record<string, string>;
  server: Ferry;
  pc: Page;
  phone: Page;
};
type WorkerFixtures = { binary: string };
type ActorArgs = Pick<PlaywrightTestArgs, 'page'> &
  Pick<PlaywrightWorkerArgs, 'playwright'> & { server: Ferry };

async function prepare(name: ActorName, page: Page): Promise<Page> {
  if (name === 'pc') await guardOwnerPage(page);
  return page;
}

function actor(name: ActorName) {
  return async (
    { page, playwright, server }: ActorArgs,
    use: (page: Page) => Promise<void>,
    testInfo: TestInfo,
  ) => {
    if (testInfo.project.name === name) {
      await use(await prepare(name, page));
      return;
    }
    const { browserName, contextOptions } = actors[name];
    const browser = await playwright[browserName].launch({ headless: true });
    try {
      const context = await browser.newContext({ ...contextOptions, baseURL: server.url });
      await use(await prepare(name, await context.newPage()));
    } finally {
      await browser.close();
    }
  };
}

export const test = base.extend<TestFixtures, WorkerFixtures>({
  binary: [defaultBinary, { option: true, scope: 'worker' }],
  serverEnv: [{}, { option: true }],
  server: async ({ binary, serverEnv }, use, testInfo) => {
    const root = await mkdtemp(path.join(tmpdir(), 'ferry-e2e-'));
    try {
      const running = await launchFerry(binary, root, serverEnv);
      try {
        await use(running);
      } finally {
        await running.stop();
        if (testInfo.status !== testInfo.expectedStatus) {
          await testInfo.attach('ferry.log', {
            body: running.output(),
            contentType: 'text/plain',
          });
        }
      }
    } finally {
      await rm(root, { recursive: true, force: true, maxRetries: 10, retryDelay: 200 });
    }
  },
  baseURL: async ({ server }, use) => {
    await use(server.url);
  },
  pc: actor('pc'),
  phone: actor('phone'),
});

export { expect } from '@playwright/test';
