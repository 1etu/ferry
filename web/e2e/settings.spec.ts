import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { type Page } from '@playwright/test';
import { randomPayload, receivedFiles } from './files.js';
import { openSection, pairByQr, sendInput } from './flows.js';
import { expect, test } from './harness.js';

type StoredConfig = { name?: string; receivedDir?: string; checkUpdates?: boolean };

async function storedConfig(dataDir: string): Promise<StoredConfig> {
  return JSON.parse(await readFile(path.join(dataDir, 'config.json'), 'utf8')) as StoredConfig;
}

function nameField(pc: Page) {
  return pc.getByRole('textbox', { name: 'Name' });
}

function checkAutomatically(pc: Page) {
  return pc.getByRole('switch', { name: 'Check Automatically' });
}

test('settings round-trip through the config and rename the PC on the phone', async ({
  pc,
  phone,
  server,
}) => {
  await pairByQr({ pc, phone, server });
  await openSection(pc, 'Settings');
  const previousName = (await server.owner.settings()).name;
  await expect(nameField(pc)).toHaveValue(previousName);

  await nameField(pc).fill('Studio PC');
  await nameField(pc).press('Enter');
  await expect.poll(async () => (await server.owner.settings()).name).toBe('Studio PC');
  await expect(checkAutomatically(pc)).toHaveAttribute('aria-checked', 'true');
  await checkAutomatically(pc).click();
  await expect(checkAutomatically(pc)).toHaveAttribute('aria-checked', 'false');
  await expect
    .poll(() => storedConfig(server.dataDir))
    .toMatchObject({ name: 'Studio PC', checkUpdates: false });

  await pc.reload();
  await openSection(pc, 'Settings');
  await expect(nameField(pc)).toHaveValue('Studio PC');
  await expect(checkAutomatically(pc)).toHaveAttribute('aria-checked', 'false');
  await phone.reload();
  await expect(phone.getByRole('heading', { level: 1, name: 'Studio PC' })).toBeVisible();
});

test('an empty name is refused and the field shows the kept one', async ({ pc, server }) => {
  await pc.goto('/');
  await openSection(pc, 'Settings');
  const kept = (await server.owner.settings()).name;

  await nameField(pc).fill('   ');
  await nameField(pc).press('Enter');
  await expect(nameField(pc)).toHaveValue(kept);
  expect((await server.owner.settings()).name).toBe(kept);
});

test('a new received folder takes the next upload', async ({ pc, phone, server }) => {
  const second = path.join(path.dirname(server.dataDir), 'Second Inbox');
  await pairByQr({ pc, phone, server });
  await pc.route('**/api/settings/received-dir/pick', async (route) => {
    await route.fulfill({ json: await server.owner.patchSettings({ receivedDir: second }) });
  });
  await openSection(pc, 'Settings');
  await pc.getByRole('button', { name: 'Change…' }).click();
  await expect(pc.getByText(/Second Inbox$/)).toBeVisible();
  await expect.poll(() => storedConfig(server.dataDir)).toMatchObject({ receivedDir: second });

  await sendInput(phone).setInputFiles(randomPayload('After the move.txt', 20_000, 'text/plain'));
  await expect
    .poll(() => receivedFiles(second), { timeout: 30_000 })
    .toEqual(['After the move.txt']);
  expect(await receivedFiles(server.receivedDir)).toEqual([]);
});
