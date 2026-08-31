import { defineConfig, devices } from '@playwright/test';

const { defaultBrowserType: pcBrowser, ...desktop } = devices['Desktop Chrome'];
const { defaultBrowserType: phoneBrowser, ...iPhone } = devices['iPhone 15'];

export const actors = {
  pc: { browserName: pcBrowser, contextOptions: { ...desktop, extraHTTPHeaders: {} } },
  phone: {
    browserName: phoneBrowser,
    contextOptions: { ...iPhone, extraHTTPHeaders: { 'X-Forwarded-For': '192.168.77.7' } },
  },
} as const;

export type ActorName = keyof typeof actors;

export default defineConfig({
  testDir: 'e2e',
  timeout: 120_000,
  expect: { timeout: 15_000 },
  forbidOnly: true,
  reporter: 'list',
  use: { headless: true, trace: 'retain-on-failure' },
  projects: [
    {
      name: 'pc',
      testMatch: [
        'desktop.spec.ts',
        'settings.spec.ts',
        'update.spec.ts',
        'download.spec.ts',
        'revoke.spec.ts',
      ],
      use: { browserName: actors.pc.browserName, ...actors.pc.contextOptions },
    },
    {
      name: 'phone',
      testMatch: ['pairing.spec.ts', 'upload.spec.ts', 'names.spec.ts', 'tamper.spec.ts'],
      use: { browserName: actors.phone.browserName, ...actors.phone.contextOptions },
    },
  ],
});
