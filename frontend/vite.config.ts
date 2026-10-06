import tailwindcss from '@tailwindcss/vite';
import react from '@vitejs/plugin-react';
import { writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import type { Plugin, ProxyOptions } from 'vite';
import { defineConfig } from 'vitest/config';

const backend = 'http://localhost:8080';

// The backend enforces Origin == TENDO_PUBLIC_URL, so dev requests present the backend's origin.
const proxyOptions: ProxyOptions = {
  target: backend,
  changeOrigin: true,
  configure: (proxy) => {
    proxy.on('proxyReq', (proxyReq) => {
      proxyReq.setHeader('Origin', backend);
    });
  },
};

const outDir = fileURLToPath(new URL('../backend/internal/platform/webui/dist', import.meta.url));

// emptyOutDir removes the committed placeholder that lets Go embed dist without a frontend build.
const keepEmbedPlaceholder: Plugin = {
  name: 'tendo-keep-embed-placeholder',
  apply: 'build',
  closeBundle() {
    writeFileSync(`${outDir}/.gitkeep`, '');
  },
};

export default defineConfig({
  plugins: [react(), tailwindcss(), keepEmbedPlaceholder],
  server: {
    proxy: {
      '/api': proxyOptions,
      '/health': proxyOptions,
    },
  },
  build: {
    outDir,
    emptyOutDir: true,
    assetsDir: 'assets',
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.{ts,tsx}'],
    setupFiles: ['src/test/setup.ts'],
  },
});
