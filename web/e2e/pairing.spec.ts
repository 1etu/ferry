import { type Page, type Request } from '@playwright/test';
import {
  allow,
  anotherPhone,
  approvalSheet,
  pairByQr,
  visitQr,
  waitingText,
  codePrompt,
  expectPairScreen,
  expectPhoneHome,
  openSection,
  requestByCode,
  requestByQr,
  showPairingCode,
} from './flows.js';
import { expect, test } from './harness.js';

type PairBody = { token?: string; code?: string; name: string; hasSecret?: boolean };
type SealBody = { clientKey: string; proof?: string };

const base64UrlKey = /^[\w-]{43}$/;

function nextPost(page: Page, pathname: string): Promise<Request> {
  return page.waitForRequest(
    (request) => request.method() === 'POST' && new URL(request.url()).pathname === pathname,
  );
}

function storedSecret(phone: Page): Promise<string | null> {
  return phone.evaluate(() => localStorage.getItem('ferry.secret'));
}

test('pairs through the QR URL and proves the fragment secret', async ({ pc, phone, server }) => {
  const pairPost = nextPost(phone, '/api/pair');
  const sealPost = nextPost(phone, '/api/seal');
  const qrUrl = await requestByQr({ pc, phone, server });

  expect(qrUrl.hash).toMatch(/^#s=[\w-]{22}$/);
  expect((await pairPost).postDataJSON() as PairBody).toMatchObject({
    token: qrUrl.searchParams.get('pair') ?? '',
    hasSecret: true,
  });
  expect((await pairPost).url()).not.toContain('#');
  await allow(pc);
  await expectPhoneHome(phone);
  await expect(phone.getByText('No Transfers')).toBeVisible();
  expect(((await sealPost).postDataJSON() as SealBody).proof).toMatch(base64UrlKey);
  expect(await storedSecret(phone)).toMatch(base64UrlKey);

  await openSection(pc, 'Devices');
  await expect(pc.getByText('Approved')).toBeVisible();
});

test('the device cookie alone cannot open a sealed session', async ({ pc, phone, server }) => {
  await pairByQr({ pc, phone, server });
  const stolen = await anotherPhone(phone, server);
  try {
    await stolen.context().addCookies(await phone.context().cookies());
    const sealReply = stolen.waitForResponse((response) => response.url().endsWith('/api/seal'));
    await stolen.goto('/');
    expect((await sealReply).status()).toBe(403);
    await expectPairScreen(stolen);
  } finally {
    await stolen.context().close();
  }
});

test('the QR grace approves a second copy that kept the fragment, once', async ({
  pc,
  phone,
  server,
}) => {
  const qrUrl = await requestByQr({ pc, phone, server });
  await allow(pc);
  await expectPhoneHome(phone);

  const homeScreenCopy = await anotherPhone(phone, server);
  const latecomer = await anotherPhone(phone, server);
  try {
    await visitQr(homeScreenCopy, qrUrl);
    await expectPhoneHome(homeScreenCopy);
    await expect(approvalSheet(pc)).toHaveCount(0);

    await visitQr(latecomer, qrUrl);
    await expect(latecomer.getByText('Code expired')).toBeVisible();
  } finally {
    await homeScreenCopy.context().close();
    await latecomer.context().close();
  }
});

test('the QR grace without the fragment waits for Allow', async ({ pc, phone, server }) => {
  const qrUrl = await requestByQr({ pc, phone, server });
  await allow(pc);
  await expectPhoneHome(phone);

  const withoutFragment = await anotherPhone(phone, server);
  try {
    const pairPost = nextPost(withoutFragment, '/api/pair');
    await visitQr(withoutFragment, new URL(qrUrl.pathname + qrUrl.search, qrUrl));
    expect((await pairPost).postDataJSON() as PairBody).toMatchObject({ hasSecret: false });
    await expect(withoutFragment.getByText(waitingText)).toBeVisible();
    await expect(approvalSheet(pc)).toBeVisible();

    await allow(pc);
    await expectPhoneHome(withoutFragment);
  } finally {
    await withoutFragment.context().close();
  }
});

test('pairs by typed code and trusts the first handshake', async ({ pc, phone, server }) => {
  const pairPost = nextPost(phone, '/api/pair');
  const firstSeal = nextPost(phone, '/api/seal');
  await requestByCode({ pc, phone, server });

  expect((await pairPost).postDataJSON() as PairBody).toMatchObject({ hasSecret: false });
  await allow(pc);
  await expectPhoneHome(phone);
  expect(((await firstSeal).postDataJSON() as SealBody).proof).toBeUndefined();
  expect(await storedSecret(phone)).toMatch(base64UrlKey);

  const secondSeal = nextPost(phone, '/api/seal');
  await phone.reload();
  await expectPhoneHome(phone);
  expect(((await secondSeal).postDataJSON() as SealBody).proof).toMatch(base64UrlKey);
});

test('a wrong code is refused and the right one still pairs', async ({ pc, phone, server }) => {
  const code = await showPairingCode({ pc, phone, server });
  const wrong = code.replace(/^./, (digit) => String((Number(digit) + 1) % 10));
  await phone.goto('/');
  const field = phone.getByRole('textbox', { name: codePrompt });
  await field.fill(wrong);
  await expect(phone.getByText('Incorrect code')).toBeVisible();

  await expect(field).toHaveValue('');
  await field.fill(code);
  await allow(pc);
  await expectPhoneHome(phone);
});
