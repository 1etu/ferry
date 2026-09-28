import { type Ferry } from './ferry.js';
import { randomPayload } from './files.js';
import {
  allow,
  approvalSheet,
  expectPhoneHome,
  openSection,
  pairByQr,
  requestByQr,
  section,
  sendInput,
} from './flows.js';
import { expect, test } from './harness.js';

async function finishedUploads(server: Ferry): Promise<number> {
  const transfers = await server.owner.transfers();
  return transfers.filter(({ direction, status }) => direction === 'in' && status === 'done')
    .length;
}

test('Devices goes from the QR to the approval sheet to the device row', async ({
  pc,
  phone,
  server,
}) => {
  await pc.goto('/');
  await expect(pc.getByRole('heading', { level: 1, name: 'Devices' })).toBeVisible();
  await expect(pc.getByText("Scan with your iPhone's camera")).toBeVisible();
  const { localUrl } = await server.owner.pairing();
  await expect(pc.getByText(new URL(localUrl).host)).toBeVisible();

  await requestByQr({ pc, phone, server });
  await expect(approvalSheet(pc)).toBeVisible();
  await allow(pc);
  await expect(approvalSheet(pc)).toBeHidden();
  await expectPhoneHome(phone);
  await expect(pc.getByText('iPhone', { exact: true })).toBeVisible();
  await expect(pc.getByText('Approved')).toBeVisible();

  await pc.getByRole('button', { name: 'Pair Another iPhone' }).click();
  const sheet = pc.getByRole('dialog', { name: 'Pair' });
  await expect(sheet.getByText("Scan with your iPhone's camera")).toBeVisible();
  await sheet.getByRole('button', { name: 'Close' }).click();
  await expect(sheet).toBeHidden();
});

test("Don't Allow sends the phone away and keeps the QR up", async ({ pc, phone, server }) => {
  await requestByQr({ pc, phone, server });
  await approvalSheet(pc).getByRole('button', { name: "Don't Allow" }).click();

  await expect(phone.getByText('Not allowed')).toBeVisible();
  await expect(approvalSheet(pc)).toBeHidden();
  await expect(pc.getByText("Scan with your iPhone's camera")).toBeVisible();
  expect((await server.owner.devices()).map(({ status }) => status)).toEqual(['revoked']);
});

test('the sidebar, Ctrl+1–3 and the arrow keys switch sections', async ({ pc }) => {
  await pc.goto('/');
  const heading = pc.getByRole('heading', { level: 1 });
  await expect(heading).toHaveText('Devices');
  await expect(section(pc, 'Devices')).toHaveAttribute('aria-current', 'page');

  await openSection(pc, 'Activity');
  await expect(section(pc, 'Activity')).toHaveAttribute('aria-current', 'page');
  await expect(pc.getByText('No Transfers')).toBeVisible();
  await openSection(pc, 'Settings');

  await pc.keyboard.press('Control+1');
  await expect(heading).toHaveText('Devices');
  await pc.keyboard.press('Control+2');
  await expect(heading).toHaveText('Activity');
  await pc.keyboard.press('Control+3');
  await expect(heading).toHaveText('Settings');

  await section(pc, 'Settings').focus();
  await pc.keyboard.press('ArrowUp');
  await expect(heading).toHaveText('Activity');
  await expect(section(pc, 'Activity')).toBeFocused();
  await pc.keyboard.press('ArrowDown');
  await expect(heading).toHaveText('Settings');
});

test('Activity lists uploads as they land and clears its history', async ({
  pc,
  phone,
  server,
}) => {
  await pairByQr({ pc, phone, server });
  await openSection(pc, 'Activity');
  await expect(pc.getByText('No Transfers')).toBeVisible();

  await sendInput(phone).setInputFiles([
    randomPayload('Receipt.pdf', 120_000, 'application/pdf'),
    randomPayload('Clip.mov', 2_500_000, 'video/quicktime'),
  ]);
  await expect.poll(() => finishedUploads(server)).toBe(2);
  const transfers = pc.getByRole('list');
  await expect(transfers.getByText('Receipt.pdf')).toBeVisible();
  await expect(transfers.getByText('Clip.mov')).toBeVisible();
  await expect(transfers.getByText('120 KB')).toBeVisible();
  await expect(transfers.getByText('2.5 MB')).toBeVisible();

  await pc.getByRole('button', { name: 'Clear History' }).click();
  await expect(pc.getByText('No Transfers')).toBeVisible();
  await expect.poll(async () => (await server.owner.transfers()).length).toBe(0);
});

test('Ctrl+2 waits while the approval sheet is open', async ({ pc, phone, server }) => {
  await requestByQr({ pc, phone, server });
  await expect(approvalSheet(pc)).toBeVisible();

  await pc.keyboard.press('Control+2');
  await expect(pc.getByRole('heading', { level: 1 })).toHaveText('Devices');
  await allow(pc);
  await expectPhoneHome(phone);
  await expect(pc.getByRole('dialog')).toHaveCount(0);
  await pc.keyboard.press('Control+2');
  await expect(pc.getByRole('heading', { level: 1 })).toHaveText('Activity');
});
