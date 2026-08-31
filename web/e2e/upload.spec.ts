import path from 'node:path';
import { type Page } from '@playwright/test';
import { type Ferry } from './ferry.js';
import { hashesIn, hashOf, randomPayload, sha256, type Payload } from './files.js';
import { pairByQr, sendInput, type Actors } from './flows.js';
import { expect, test } from './harness.js';
import { type Wifi } from './wifi.js';

const largeSize = 50_000_000;
const wifiBytesPerSecond = 8 * 1024 * 1024;
const offlineMs = 3000;
const creationChunkSize = 16 * 1024 * 1024;
const burstCount = 200;
const burstSize = 100_000;

async function uploads(server: Ferry) {
  const transfers = await server.owner.transfers();
  return transfers.filter((transfer) => transfer.direction === 'in');
}

async function bytesReceived(server: Ferry): Promise<number> {
  return (await uploads(server)).at(0)?.done ?? 0;
}

async function setOffline(phone: Page, wifi: Wifi, isOffline: boolean): Promise<void> {
  wifi.setOffline(isOffline);
  await phone.context().setOffline(isOffline);
}

test('a 50 MB sealed upload survives 3 s offline and lands byte-exact', async ({
  pc,
  phone,
  server,
}) => {
  const wifi = await server.openWifi({ bytesPerSecond: wifiBytesPerSecond });
  await pairByQr({ pc, phone, server }, wifi.url);
  const payload = randomPayload('payload.bin', largeSize);

  await sendInput(phone).setInputFiles(payload);
  await expect
    .poll(() => bytesReceived(server), { intervals: [50] })
    .toBeGreaterThan(creationChunkSize);
  await setOffline(phone, wifi, true);
  await phone.waitForTimeout(offlineMs);
  const interrupted = (await uploads(server)).at(0);
  expect(interrupted?.status).toBe('active');
  expect(interrupted?.done).toBeLessThan(largeSize);
  await setOffline(phone, wifi, false);

  await expect
    .poll(async () => (await uploads(server)).map(({ status }) => status), { timeout: 60_000 })
    .toEqual(['done']);
  const received = (await uploads(server)).at(0);
  expect(received?.done).toBe(largeSize);
  expect(await sha256(path.join(server.receivedDir, received?.name ?? payload.name))).toBe(
    hashOf(payload.buffer),
  );
  await expect(phone.getByText(payload.name)).toBeVisible();
});

test('a burst of 200 small files all land with equal hashes', async ({ pc, phone, server }) => {
  await pairByQr({ pc, phone, server });
  const payloads = [...Array(burstCount).keys()].map((index) =>
    randomPayload(`burst-${String(index).padStart(3, '0')}.bin`, burstSize),
  );

  await sendInput(phone).setInputFiles(payloads);
  await expect
    .poll(async () => (await uploads(server)).filter(({ status }) => status === 'done').length, {
      timeout: 90_000,
    })
    .toBe(burstCount);

  const expected = new Map(payloads.map(({ name, buffer }) => [name, hashOf(buffer)]));
  expect(await hashesIn(server.receivedDir)).toEqual(expected);
  await expect(phone.getByText(/^burst-\d{3}\.bin$/)).toHaveCount(burstCount);
});

async function cutCreationRequest({ pc, phone, server }: Actors): Promise<Payload> {
  const wifi = await server.openWifi({ bytesPerSecond: wifiBytesPerSecond });
  await pairByQr({ pc, phone, server }, wifi.url);
  const payload = randomPayload('restart.bin', creationChunkSize + 4_000_000);

  await sendInput(phone).setInputFiles(payload);
  await expect.poll(() => bytesReceived(server), { intervals: [50] }).toBeGreaterThan(0);
  const cut = (await uploads(server)).at(0);
  await setOffline(phone, wifi, true);
  await phone.waitForTimeout(offlineMs);
  await setOffline(phone, wifi, false);
  await expect
    .poll(async () => (await uploads(server)).map(({ id, status }) => `${id}:${status}`), {
      timeout: 60_000,
    })
    .toEqual([expect.stringMatching(/:done$/)]);
  expect((await uploads(server)).at(0)?.id).not.toBe(cut?.id);
  return payload;
}

test('a creation request cut offline starts over and still lands byte-exact', async ({
  pc,
  phone,
  server,
}) => {
  const payload = await cutCreationRequest({ pc, phone, server });
  const landed = (await uploads(server)).find(({ status }) => status === 'done');
  expect(await sha256(path.join(server.receivedDir, landed?.name ?? payload.name))).toBe(
    hashOf(payload.buffer),
  );
});

test('a creation request cut offline leaves no ghost row on the phone', async ({
  pc,
  phone,
  server,
}) => {
  const payload = await cutCreationRequest({ pc, phone, server });
  await expect(phone.getByText(payload.name)).toHaveCount(1);
});

test('reloading right after an upload finishes shows no ghost Tap to resume row', async ({
  pc,
  phone,
  server,
}) => {
  await pairByQr({ pc, phone, server });
  const payload = randomPayload('finished.bin', 300_000);
  await sendInput(phone).setInputFiles(payload);
  await expect
    .poll(async () => (await uploads(server)).map(({ status }) => status))
    .toEqual(['done']);

  await phone.reload();
  await expect(phone.getByRole('button', { name: 'Send' })).toBeVisible();
  await expect(phone.getByText(payload.name)).toHaveCount(1);
  await expect(phone.getByText('Tap to resume')).toHaveCount(0);
});
