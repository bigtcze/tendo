import { defineConfig } from '@playwright/test';

// Runs against a production-built Tendo with real PostgreSQL (see scripts/e2e-smoke.sh).
const baseURL = process.env.E2E_BASE_URL;
if (!baseURL) throw new Error('E2E_BASE_URL is required; run scripts/e2e-smoke.sh');

export default defineConfig({
  testDir: 'e2e',
  outputDir: process.env.PLAYWRIGHT_OUTPUT_DIR ?? 'test-results/artifacts',
  testMatch: process.env.E2E_SR_AUDIT_DIR
    ? ['e2e/screen-reader.spec.ts']
    : process.env.E2E_AUDIT_DIR ? ['e2e/ux-audit.spec.ts'] : undefined,
  testIgnore: process.env.E2E_SR_AUDIT_DIR || process.env.E2E_AUDIT_DIR
    ? []
    : process.env.E2E_SCREENSHOT_PATH
      ? ['e2e/journey.spec.ts', 'e2e/attention-clock.spec.ts', 'e2e/ux-audit.spec.ts', 'e2e/screen-reader.spec.ts']
      : process.env.E2E_CLOCK_PHASE
        ? ['e2e/ux-audit.spec.ts', 'e2e/screen-reader.spec.ts']
        : ['e2e/attention-clock.spec.ts', 'e2e/readme-screenshot.spec.ts', 'e2e/ux-audit.spec.ts', 'e2e/screen-reader.spec.ts'],
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: true,
  reporter: [
    ['list'],
    ['html', { outputFolder: process.env.PLAYWRIGHT_HTML_OUTPUT_DIR ?? 'test-results/html-report', open: 'never' }],
    ['json', { outputFile: process.env.PLAYWRIGHT_JSON_OUTPUT_FILE ?? 'test-results/report.json' }],
  ],
  // The opt-in TENDO_E2E_READ_DELAY_MS stress mode slows every household read, so read-heavy tests get a
  // larger default budget there; required runs keep 30 s. Tests that set their own timeout keep it.
  timeout: process.env.TENDO_E2E_READ_DELAY_MS ? 90_000 : 30_000,
  expect: { timeout: 10_000 },
  use: {
    baseURL,
    headless: process.env.E2E_SR_HEADFUL ? false : true,
    launchOptions: process.env.E2E_SR_HEADFUL ? { args: ['--force-renderer-accessibility', '--window-size=1200,850'] } : {},
    trace: 'retain-on-failure',
    locale: 'en-US',
  },
});
