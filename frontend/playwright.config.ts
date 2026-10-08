import { defineConfig } from '@playwright/test';

// Runs against a production-built Tendo with real PostgreSQL (see scripts/e2e-smoke.sh).
const baseURL = process.env.E2E_BASE_URL;
if (!baseURL) throw new Error('E2E_BASE_URL is required; run scripts/e2e-smoke.sh');

export default defineConfig({
  testDir: 'e2e',
  outputDir: 'test-results/artifacts',
  testIgnore: process.env.E2E_CLOCK_PHASE ? [] : ['e2e/attention-clock.spec.ts'],
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: true,
  reporter: 'list',
  timeout: 30_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL,
    trace: 'retain-on-failure',
    locale: 'en-US',
  },
});
