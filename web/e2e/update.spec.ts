import { readFile, rm, stat } from 'node:fs/promises';
import { type Page } from '@playwright/test';
import { type Ferry, type UpdateStatus } from './ferry.js';
import { hashOf } from './files.js';
import { openSection } from './flows.js';
import { expect, test as base } from './harness.js';
import {
  buildVersioned,
  serveReleases,
  stagedPath,
  versionedPath,
  versionUnderTest,
  type ReleaseServer,
} from './releases.js';

const buildTimeoutMs = 300_000;

const test = base.extend<{ releases: ReleaseServer }>({
  binary: [
    async ({ binary }, use) => {
      const versioned = process.env.FERRY_VERSIONED_BIN ?? versionedPath(binary);
      if (process.env.FERRY_VERSIONED_BIN === undefined) await buildVersioned(versioned);
      await use(versioned);
    },
    { scope: 'worker', timeout: buildTimeoutMs },
  ],
  releases: async ({ binary }, use) => {
    const releases = await serveReleases();
    try {
      await use(releases);
    } finally {
      await releases.close();
      await rm(stagedPath(binary), { force: true });
    }
  },
  serverEnv: async ({ releases }, use) => {
    await use(releases.env);
  },
});

function versionRow(pc: Page) {
  return pc.getByRole('listitem').filter({ hasText: `Ferry ${versionUnderTest}` });
}

async function isStaged(binary: string): Promise<boolean> {
  return stat(stagedPath(binary)).then(
    () => true,
    () => false,
  );
}

async function checkUntil(pc: Page, server: Ferry, state: UpdateStatus['state']) {
  await pc.goto('/');
  await openSection(pc, 'Settings');
  await versionRow(pc).getByRole('button', { name: 'Check for Updates' }).click();
  await expect.poll(async () => (await server.owner.update()).state).toBe(state);
  await pc.reload();
  await openSection(pc, 'Settings');
}

test('a signed release is staged and Settings offers Restart to Update', async ({
  pc,
  releases,
  binary,
  server,
}) => {
  releases.publish({ version: '0.0.2', isSignatureValid: true });
  await checkUntil(pc, server, 'ready');

  expect(hashOf(await readFile(stagedPath(binary)))).toBe(hashOf(releases.asset));
  await expect(versionRow(pc).getByText('Ready to install')).toBeVisible();
  await expect(versionRow(pc).getByRole('button', { name: 'Restart to Update' })).toBeVisible();
});

test('a release whose signature does not verify is refused', async ({
  pc,
  releases,
  binary,
  server,
}) => {
  releases.publish({ version: '0.0.2', isSignatureValid: false });
  await checkUntil(pc, server, 'failed');

  await expect(versionRow(pc).getByText('Could not update')).toBeVisible();
  await expect(versionRow(pc).getByRole('button', { name: 'Check for Updates' })).toBeVisible();
  expect(await isStaged(binary)).toBe(false);
});

test('the running version is up to date', async ({ pc, releases, binary, server }) => {
  releases.publish({ version: versionUnderTest, isSignatureValid: true });
  await checkUntil(pc, server, 'idle');

  await expect(versionRow(pc).getByText('Up to date')).toBeVisible();
  expect(await isStaged(binary)).toBe(false);
});

test('the 202 reply to Check for Updates never overwrites a newer ready state', async ({
  pc,
  releases,
}) => {
  await pc.goto('/');
  await openSection(pc, 'Settings');
  releases.publish({ version: '0.0.2', isSignatureValid: true });

  await versionRow(pc).getByRole('button', { name: 'Check for Updates' }).click();
  await expect(versionRow(pc).getByText('Ready to install')).toBeVisible();
  await pc.waitForTimeout(1000);
  await expect(versionRow(pc).getByRole('button', { name: 'Restart to Update' })).toBeVisible();
});
