import { type Page, type Response } from '@playwright/test';
import {
  allow,
  codePrompt,
  expectPairScreen,
  expectPhoneHome,
  openSection,
  pairByQr,
  requestByQr,
} from './flows.js';
import { expect, test } from './harness.js';

function isSealedEventStream(response: Response): boolean {
  const url = new URL(response.url());
  return url.pathname === '/api/events' && url.searchParams.has('seal') && response.ok();
}

async function removePhone(pc: Page): Promise<void> {
  await openSection(pc, 'Devices');
  const remove = pc.getByRole('button', { name: 'Remove' });
  await remove.click();
  await expect(remove).toHaveCount(0);
}

test('removing the phone on the PC sends it back to Pair', async ({ pc, phone, server }) => {
  const sealedStream = phone.waitForResponse(isSealedEventStream);
  await pairByQr({ pc, phone, server });
  await sealedStream;

  await removePhone(pc);
  await expectPairScreen(phone);
  expect((await server.owner.devices()).map(({ status }) => status)).toEqual(['revoked']);
});

test('removing the phone while its event stream reconnects sends it back to Pair', async ({
  pc,
  phone,
  server,
}) => {
  await pairByQr({ pc, phone, server });

  await removePhone(pc);
  await expectPairScreen(phone);
});

test('removing the phone during its first handshake sends it back to Pair', async ({
  pc,
  phone,
  server,
}) => {
  await requestByQr({ pc, phone, server });
  await allow(pc);
  await removePhone(pc);

  await expectPairScreen(phone);
  await phone.reload();
  await expectPairScreen(phone);
});

test('a phone removed while offline returns to Pair when it reconnects', async ({
  pc,
  phone,
  server,
}) => {
  const wifi = await server.openWifi();
  await pairByQr({ pc, phone, server }, wifi.url);
  await expectPhoneHome(phone);

  wifi.setOffline(true);
  await expect(phone.getByText('Not Connected')).toBeVisible();
  await removePhone(pc);
  wifi.setOffline(false);

  await expectPairScreen(phone);
});

test('a phone removed after QR pairing never redeems its stale token again', async ({
  pc,
  phone,
  server,
}) => {
  await pairByQr({ pc, phone, server });
  const pairPosts: string[] = [];
  phone.on('request', (request) => {
    if (new URL(request.url()).pathname === '/api/pair') pairPosts.push(request.url());
  });

  await removePhone(pc);
  await expectPairScreen(phone);
  await expect(phone.getByText(codePrompt)).toBeVisible();
  expect(pairPosts).toEqual([]);
});
