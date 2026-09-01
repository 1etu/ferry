import path from 'node:path';
import { type Page } from '@playwright/test';
import { sha256, writeRandomFile } from './files.js';
import { openSection, pairByQr } from './flows.js';
import { expect, test } from './harness.js';

const offeredName = 'Holiday cut.mov';
const offeredSize = 8 * 1024 * 1024;

async function listedOfferNames(phone: Page): Promise<string[]> {
  const reply = phone.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === '/api/files' && response.request().method() === 'GET',
  );
  await phone.reload();
  const listed = (await (await reply).json()) as { name: string }[];
  return listed.map(({ name }) => name);
}

test('a file sent with Send Files… downloads byte-exact on the phone', async ({
  pc,
  phone,
  server,
}) => {
  await pairByQr({ pc, phone, server });
  const source = path.join(server.scratchDir, offeredName);
  const sourceHash = await writeRandomFile(source, offeredSize);
  await pc.route('**/api/files/pick', async (route) => {
    await route.fulfill({ json: await server.owner.offer([source]) });
  });

  await openSection(pc, 'Activity');
  await pc.getByRole('button', { name: 'Send Files…' }).click();
  await expect(pc.getByText(offeredName)).toBeVisible();
  const link = phone.getByRole('link', { name: offeredName });
  await expect(link).toBeVisible();

  const sealedNames = await listedOfferNames(phone);
  expect(sealedNames).toHaveLength(1);
  expect(sealedNames[0]).not.toContain(offeredName);
  await expect(link).toBeVisible();

  const [download] = await Promise.all([phone.waitForEvent('download'), link.click()]);
  expect(download.suggestedFilename()).toBe(offeredName);
  const saved = path.join(server.scratchDir, 'downloaded.bin');
  await download.saveAs(saved);
  expect(await sha256(saved)).toBe(sourceHash);

  await expect
    .poll(async () => {
      const transfers = await server.owner.transfers();
      return transfers.filter(({ direction }) => direction === 'out').map(({ status }) => status);
    })
    .toEqual(['done']);
  await expect(pc.getByText('Sent')).toBeVisible();
});

test('removing an offered file on the PC takes it off the phone', async ({ pc, phone, server }) => {
  await pairByQr({ pc, phone, server });
  const source = path.join(server.scratchDir, offeredName);
  await writeRandomFile(source, 1024);
  await server.owner.offer([source]);
  await expect(phone.getByRole('link', { name: offeredName })).toBeVisible();

  await openSection(pc, 'Activity');
  await pc.getByRole('button', { name: 'Remove' }).click();
  await expect(phone.getByRole('link', { name: offeredName })).toHaveCount(0);
  await expect(pc.getByText('No Transfers')).toBeVisible();
});
