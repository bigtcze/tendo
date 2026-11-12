import { defineConfig, type Project } from '@playwright/test';

// Runs against a production-built Tendo with real PostgreSQL (see scripts/e2e-smoke.sh).
const baseURL = process.env.E2E_BASE_URL;
if (!baseURL) throw new Error('E2E_BASE_URL is required; run scripts/e2e-smoke.sh');

const projects: Project[] = [
  { name: 'e2e', testMatch: ['journey.spec.ts', 'primitives-browser.spec.ts', 'theme.spec.ts'] },
  { name: 'cross-browser-firefox', testMatch: ['cross-browser.spec.ts'], use: { browserName: 'firefox' } },
  { name: 'cross-browser-webkit', testMatch: ['cross-browser.spec.ts'], use: { browserName: 'webkit' } },
  { name: 'clock', testMatch: ['attention-clock.spec.ts'] },
  { name: 'readme-screenshot', testMatch: ['readme-screenshot.spec.ts'] },
  { name: 'ux-audit', testMatch: ['ux-audit.spec.ts'] },
  { name: 'design-capture', testMatch: ['design-capture.spec.ts'] },
  ...(process.env.TENDO_VISUAL_MODE ? [{ name: 'visual', testMatch: ['visual/**/*.spec.ts'], use: { browserName: 'chromium' as const } }] : []),
  {
    name: 'screen-reader',
    testMatch: ['screen-reader.spec.ts'],
    use: { headless: false, launchOptions: { args: ['--force-renderer-accessibility', '--window-size=1200,850'] } },
  },
];

// Playwright merges webServer.env over its own environment, which also carries browser-only credentials
// (owner passwords, setup token copies, fixture logins). Each server instead receives exactly the base process
// variables plus its own configuration; every other inherited key is unset (Node omits undefined values).
const BASE_KEYS = ['PATH', 'HOME', 'TMPDIR', 'LANG'];
function serverEnv(own: Record<string, string | undefined>): Record<string, string> {
  const env: Record<string, string | undefined> = Object.fromEntries(Object.keys(process.env).map((key) => [key, undefined]));
  for (const key of BASE_KEYS) env[key] = process.env[key];
  return { ...env, ...own } as Record<string, string>;
}
const APP_KEYS = [
  'DATABASE_URL', 'TENDO_LISTEN_ADDR', 'TENDO_PUBLIC_URL', 'TENDO_TRUSTED_PROXY_CIDRS', 'TENDO_DB_TIMEOUT',
  'TENDO_SHUTDOWN_TIMEOUT', 'TENDO_SETUP_TOKEN', 'TENDO_TEST_CLOCK_ACK', 'TENDO_TEST_CLOCK_NOW', 'TENDO_OIDC_ISSUER',
  'TENDO_OIDC_CLIENT_ID', 'TENDO_OIDC_CLIENT_SECRET', 'TENDO_OIDC_CLIENT_SECRET_FILE', 'TENDO_OIDC_DISPLAY_NAME',
];
const appConfig = Object.fromEntries(APP_KEYS.map((key) => [key, process.env[key]]));

const webServer = process.env.E2E_APP_BINARY ? [
  ...(process.env.E2E_OIDC_PROVIDER_BINARY ? [{
    command: `"${process.env.E2E_OIDC_PROVIDER_BINARY}"`,
    url: `${process.env.E2E_OIDC_ISSUER}/healthz`,
    env: serverEnv({
      LISTEN_ADDR: process.env.E2E_OIDC_LISTEN_ADDR,
      ISSUER_URL: process.env.E2E_OIDC_ISSUER,
      CLIENT_ID: process.env.E2E_OIDC_CLIENT_ID,
      CLIENT_SECRET: process.env.E2E_OIDC_CLIENT_SECRET,
      REDIRECT_URI: process.env.E2E_OIDC_REDIRECT_URI,
      SUBJECTS: process.env.E2E_OIDC_SUBJECTS,
      SUBJECT_EMAILS: process.env.E2E_OIDC_SUBJECT_EMAILS,
    }),
    reuseExistingServer: false,
    stdout: 'pipe' as const,
    stderr: 'pipe' as const,
    gracefulShutdown: { signal: 'SIGTERM' as const, timeout: 10_000 },
    timeout: 60_000,
  }] : []),
  {
    command: `"${process.env.E2E_APP_BINARY}"`,
    url: `http://127.0.0.1:${process.env.E2E_APP_PORT}/health/ready`,
    env: serverEnv(appConfig),
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
  snapshotPathTemplate: '{testDir}/{testFilePath}-snapshots/{arg}{ext}',
  // 'changed' rewrites only mismatching or missing baselines, so prerequisite captures stay untouched.
  updateSnapshots: process.env.TENDO_VISUAL_UPDATE === '1' ? 'changed' : 'none',
  use: {
    baseURL,
    headless: true,
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    locale: 'en-US',
  },
});
