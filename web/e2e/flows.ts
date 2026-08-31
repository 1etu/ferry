import { expect, type Page } from '@playwright/test';
import { actors as actorOptions } from '../playwright.config.js';
import { type Ferry } from './ferry.js';

export const deviceName = 'iPhone';
export const codePrompt = 'Enter the code shown on your PC';
export const waitingText = 'Allow this iPhone on your PC';

const thinSpace = String.fromCodePoint(0x2009);
const ownerSideEffects = new Set([
  '/api/received/open',
  '/api/files/pick',
  '/api/settings/received-dir/pick',
  '/api/network/allow',
  '/api/update/apply',
  '/api/app/show',
  '/api/app/quit',
]);

export type Actors = { pc: Page; phone: Page; server: Ferry };

export async function guardOwnerPage(page: Page): Promise<void> {
  await page.context().route(
    (url) => ownerSideEffects.has(url.pathname),
    (route) => route.abort(),
  );
}

function isLocalOrigin(url: URL): boolean {
  return url.hostname.endsWith('.local');
}

export async function showPairingCode({ pc, server }: Actors): Promise<string> {
  await pc.goto('/');
  const { code } = await server.owner.pairing();
  await expect(pc.getByText(`${code.slice(0, 3)}${thinSpace}${code.slice(3)}`)).toBeVisible();
  return code;
}

export async function qrUrlAt(server: Ferry, origin: string): Promise<URL> {
  const qr = new URL((await server.owner.pairing()).qrUrl);
  return new URL(`${qr.pathname}${qr.search}${qr.hash}`, origin);
}

export async function visitQr(phone: Page, url: URL): Promise<void> {
  await phone.context().route(isLocalOrigin, (route) => route.abort());
  await phone.goto(url.href);
  await expect(
    phone
      .getByRole('button', { name: 'Send' })
      .or(phone.getByText(waitingText))
      .or(phone.getByRole('textbox', { name: codePrompt })),
  ).toBeVisible();
  await phone.context().unroute(isLocalOrigin);
}

export async function requestByQr(actors: Actors, origin = actors.server.url): Promise<URL> {
  await showPairingCode(actors);
  const url = await qrUrlAt(actors.server, origin);
  await visitQr(actors.phone, url);
  await expect(actors.phone.getByText(waitingText)).toBeVisible();
  return url;
}

export async function anotherPhone(phone: Page, server: Ferry): Promise<Page> {
  const browser = phone.context().browser();
  if (browser === null) throw new Error('the phone has no browser to open another context in');
  const context = await browser.newContext({
    ...actorOptions.phone.contextOptions,
    baseURL: server.url,
  });
  return context.newPage();
}

export async function requestByCode(actors: Actors, origin = actors.server.url): Promise<void> {
  const code = await showPairingCode(actors);
  const { phone } = actors;
  await phone.goto(origin);
  await expect(phone.getByText(codePrompt)).toBeVisible();
  await phone.getByRole('textbox', { name: codePrompt }).fill(code);
  await expect(phone.getByText(waitingText)).toBeVisible();
}

export function approvalSheet(pc: Page) {
  return pc.getByRole('dialog', { name: `Allow “${deviceName}”?` });
}

export async function allow(pc: Page): Promise<void> {
  await approvalSheet(pc).getByRole('button', { name: 'Allow', exact: true }).click();
}

export async function expectPhoneHome(phone: Page): Promise<void> {
  await expect(phone.getByRole('button', { name: 'Send' })).toBeVisible();
}

export async function expectPairScreen(phone: Page): Promise<void> {
  await expect(phone.getByRole('textbox', { name: codePrompt })).toBeVisible();
  await expect(phone.getByRole('button', { name: 'Send' })).toHaveCount(0);
}

export async function pairByQr(actors: Actors, origin = actors.server.url): Promise<void> {
  await requestByQr(actors, origin);
  await allow(actors.pc);
  await expectPhoneHome(actors.phone);
}

export function sendInput(phone: Page) {
  return phone.locator('input[type="file"][multiple]');
}

export function section(pc: Page, name: 'Devices' | 'Activity' | 'Settings') {
  return pc.getByRole('navigation', { name: 'Sections' }).getByRole('button', { name });
}

export async function openSection(pc: Page, name: 'Devices' | 'Activity' | 'Settings') {
  await section(pc, name).click();
  await expect(pc.getByRole('heading', { level: 1, name })).toBeVisible();
}
