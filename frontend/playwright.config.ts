import { defineConfig } from '@playwright/test';

// Runs against a production-built Tendo with real PostgreSQL (see scripts/e2e-smoke.sh).
const baseURL = process.env.E2E_BASE_URL;
if (!baseURL) throw new Error('E2E_BASE_URL is required; run scripts/e2e-smoke.sh');

const projects = [
  { name: 'e2e', testMatch: ['journey.spec.ts', 'primitives-browser.spec.ts'] },
  { name: 'clock', testMatch: ['attention-clock.spec.ts'] },
  { name: 'readme-screenshot', testMatch: ['readme-screenshot.spec.ts'] },
  { name: 'ux-audit', testMatch: ['ux-audit.spec.ts'] },
  {
    name: 'screen-reader',
    testMatch: ['screen-reader.spec.ts'],
    use: { headless: false, launchOptions: { args: ['--force-renderer-accessibility', '--window-size=1200,850'] } },
  },
];

const webServer = process.env.E2E_APP_BINARY ? [
  ...(process.env.E2E_OIDC_PROVIDER_BINARY ? [{
    command: `"${process.env.E2E_OIDC_PROVIDER_BINARY}"`,
    url: `${process.env.E2E_OIDC_ISSUER}/healthz`,
    env: {
      LISTEN_ADDR: process.env.E2E_OIDC_LISTEN_ADDR!,
      ISSUER_URL: process.env.E2E_OIDC_ISSUER!,
      CLIENT_ID: process.env.E2E_OIDC_CLIENT_ID!,
      CLIENT_SECRET: process.env.E2E_OIDC_CLIENT_SECRET!,
      REDIRECT_URI: process.env.E2E_OIDC_REDIRECT_URI!,
      SUBJECTS: process.env.E2E_OIDC_SUBJECTS!,
    },
    reuseExistingServer: false,
    stdout: 'pipe' as const,
    stderr: 'pipe' as const,
    gracefulShutdown: { signal: 'SIGTERM' as const, timeout: 10_000 },
    timeout: 60_000,
  }] : []),
  {
    command: `"${process.env.E2E_APP_BINARY}"`,
    url: `http://127.0.0.1:${process.env.E2E_APP_PORT}/health/ready`,
    reuseExistingServer: false,
    stdout: 'pipe' as const,
    stderr: 'pipe' as const,
    gracefulShutdown: { signal: 'SIGTERM' as const, timeout: 10_000 },
    timeout: 60_000,
  },
] : undefined;

export default defineConfig({
  testDir: 'e2e',
  outputDir: process.env.PLAYWRIGHT_OUTPUT_DIR ?? 'test-results/artifacts',
  projects,
  webServer,
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
    headless: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    locale: 'en-US',
  },
});
