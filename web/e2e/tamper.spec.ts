import { stat } from 'node:fs/promises';
import path from 'node:path';
import { randomPayload, receivedFiles } from './files.js';
import { pairByQr, sendInput } from './flows.js';
import { expect, test } from './harness.js';

const frameSize = 1024 * 1024;
const creationChunkSize = 16 * frameSize;

test('a byte flipped in a sealed PATCH fails the upload and lands nothing', async ({
  pc,
  phone,
  server,
}) => {
  const wifi = await server.openWifi();
  await pairByQr({ pc, phone, server }, wifi.url);
  const payload = randomPayload('tampered.bin', creationChunkSize + 4_000_000);

  const tampered = wifi.tamperNextPatch();
  await sendInput(phone).setInputFiles(payload);
  await tampered;
  await expect(phone.getByText('Failed')).toBeVisible();

  const upload = (await server.owner.transfers()).find(({ direction }) => direction === 'in');
  if (upload === undefined) throw new Error('the tampered upload never reached the PC');
  expect(upload.done).toBeLessThan(payload.buffer.length);
  const partial = await stat(path.join(server.receivedDir, '.incoming', upload.id));
  expect(partial.size).toBeGreaterThanOrEqual(creationChunkSize);
  expect(partial.size % frameSize).toBe(0);
  expect(await receivedFiles(server.receivedDir)).toEqual([]);
});
