import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { fileURLToPath, URL } from 'node:url';

const backend = 'http://127.0.0.1:8080';

export default defineConfig({
  plugins: [svelte()],
  resolve: {
    alias: { $lib: fileURLToPath(new URL('./src/lib', import.meta.url)) },
    conditions: process.env.VITEST ? ['browser'] : undefined,
  },
  build: { outDir: '../internal/webui/dist', emptyOutDir: true, target: ['safari17', 'chrome113'] },
  server: {
    host: true,
    proxy: {
      '/api': { target: backend, changeOrigin: true },
      '/manifest.webmanifest': { target: backend, changeOrigin: true },
    },
  },
  test: { environment: 'jsdom', include: ['src/**/*.test.ts', 'src/**/*.svelte.test.ts'] },
});
