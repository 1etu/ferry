import { type Page, type Request } from '@playwright/test';
import { type Ferry } from './ferry.js';
import { randomPayload, receivedFiles } from './files.js';
import { pairByQr, sendInput } from './flows.js';
import { expect, test } from './harness.js';

const originalsHint = 'For originals, choose Options › Current when picking photos.';
const sealedStringMinBytes = 40;
const uploadNonceBytes = 16;

function decodeBase64Url(text: string): Buffer {
  return Buffer.from(text, 'base64url');
}

function uploadMetadata(request: Request): Map<string, string> {
  const header = request.headers()['upload-metadata'] ?? '';
  return new Map(
    header.split(',').map((pair) => {
      const [key = '', value = ''] = pair.trim().split(' ');
      return [key, Buffer.from(value, 'base64').toString('latin1')];
    }),
  );
}

function isUploadCreation(request: Request): boolean {
  return request.method() === 'POST' && new URL(request.url()).pathname === '/api/uploads/';
}

async function doneUploads(server: Ferry): Promise<number> {
  const transfers = await server.owner.transfers();
  return transfers.filter(({ direction, status }) => direction === 'in' && status === 'done')
    .length;
}

function expectSealed(sealed: string, plain: string): void {
  expect(sealed).not.toContain(plain);
  expect(decodeBase64Url(sealed).length).toBeGreaterThanOrEqual(sealedStringMinBytes);
}

async function listedNames(phone: Page): Promise<{ names: string[]; sealId: string }> {
  const reply = phone.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === '/api/transfers' &&
      response.request().method() === 'GET',
  );
  await expect.poll(() => phone.evaluate(() => localStorage.getItem('ferry.uploads'))).toBeNull();
  await phone.reload();
  const response = await reply;
  const listed = (await response.json()) as { name: string }[];
  return {
    names: listed.map(({ name }) => name),
    sealId: response.request().headers()['x-ferry-seal'] ?? '',
  };
}

test('file names travel sealed while both screens show them plain', async ({
  pc,
  phone,
  server,
}) => {
  await pairByQr({ pc, phone, server });
  const payload = randomPayload('Quarterly plan.pdf', 300_000, 'application/pdf');

  const creation = phone.waitForRequest(isUploadCreation);
  await sendInput(phone).setInputFiles(payload);
  const metadata = uploadMetadata(await creation);
  expect((await creation).headers()['x-ferry-seal']).toMatch(/^[\w-]{22}$/);
  expectSealed(metadata.get('name') ?? '', payload.name);
  expect(decodeBase64Url(metadata.get('nonce') ?? '').length).toBe(uploadNonceBytes);
  await expect.poll(() => doneUploads(server)).toBe(1);
  expect(await receivedFiles(server.receivedDir)).toEqual([payload.name]);

  const { names, sealId } = await listedNames(phone);
  expect(sealId).toMatch(/^[\w-]{22}$/);
  expect(names).toHaveLength(1);
  expectSealed(names[0] ?? '', payload.name);
  await expect(phone.getByText(payload.name)).toBeVisible();
  expect((await server.owner.transfers()).map(({ name }) => name)).toEqual([payload.name]);
});

test('the originals hint shows once, for the first converted photo only', async ({
  pc,
  phone,
  server,
}) => {
  await pairByQr({ pc, phone, server });
  const hint = phone.getByText(originalsHint);

  await sendInput(phone).setInputFiles(randomPayload('IMG_0001.HEIC', 50_000, 'image/heic'));
  await expect.poll(() => doneUploads(server)).toBe(1);
  await expect(hint).toBeHidden();

  await sendInput(phone).setInputFiles(randomPayload('image.jpg', 50_000, 'image/jpeg'));
  await expect(hint).toBeVisible();
  await expect.poll(() => doneUploads(server)).toBe(2);
  const photoName = /^Photo \d{4}-\d{2}-\d{2} \d{2}\.\d{2}\.\d{2}\.jpg$/;
  expect(
    (await receivedFiles(server.receivedDir)).filter((name) => photoName.test(name)),
  ).toHaveLength(1);

  await phone.reload();
  await expect(phone.getByRole('button', { name: 'Send' })).toBeVisible();
  await sendInput(phone).setInputFiles(randomPayload('image.jpg', 50_000, 'image/jpeg'));
  await expect.poll(() => doneUploads(server)).toBe(3);
  await expect(hint).toBeHidden();
});
