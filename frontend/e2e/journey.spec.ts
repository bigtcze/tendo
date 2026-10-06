import { expect, request, test, type ConsoleMessage, type Page } from '@playwright/test';
import { cs } from '../src/i18n/cs';
import { en } from '../src/i18n/en';

function required(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required; run scripts/e2e-smoke.sh`);
  return value;
}

const baseURL = required('E2E_BASE_URL');
const setupToken = required('E2E_SETUP_TOKEN');
const ownerPassword = required('E2E_OWNER_PASSWORD');
const ownerLogin = 'owner_e2e';
const householdName = 'Veselí';

const problems: string[] = [];
const failedResponses = new Set<string>();
// The only HTTP errors the journey may cause: anonymous session probes and the wrong-password login.
const allowedFailedResponses = ['GET /api/v1/session 401', 'POST /api/v1/session 401'];

function watch(page: Page) {
  page.on('console', (message: ConsoleMessage) => {
    if (message.type() !== 'error') return;
    // Chromium logs a generic console error for each failed fetch; the response check below
    // asserts exactly which failures happened, so only those for the session endpoint are skipped.
    if (new URL(message.location().url || 'about:blank').pathname === '/api/v1/session') return;
    problems.push(`console: ${message.text()}`);
  });
  page.on('pageerror', (error) => problems.push(`pageerror: ${error.message}`));
  page.on('response', (response) => {
    if (response.status() >= 400) {
      failedResponses.add(`${response.request().method()} ${new URL(response.url()).pathname} ${response.status()}`);
    }
  });
}

async function signIn(page: Page, password: string) {
  await page.getByLabel(en['login.login']).fill(ownerLogin);
  await page.getByLabel(en['login.password']).fill(password);
  await page.getByRole('button', { name: en['login.submit'] }).click();
}

test.describe.configure({ mode: 'serial' });

test.describe('Tendo production journey', () => {
  let page: Page;
  let householdId = '';
  let sessionToken = '';

  test.beforeAll(async ({ browser }) => {
    const context = await browser.newContext({ baseURL, locale: 'en-US' });
    page = await context.newPage();
    watch(page);
  });

  test.afterAll(async () => {
    await page.context().close();
  });

  test('a. before setup the app says it is not set up and offers no password field', async () => {
    const response = await page.goto('/');
    expect(response?.status()).toBe(200);
    await expect(page.getByRole('heading', { name: en['setup.title'] })).toBeVisible();
    await expect(page.getByText(en['setup.body'])).toBeVisible();
    await expect(page.locator('input[type="password"]')).toHaveCount(0);
  });

  test('b. operator setup through the real API', async () => {
    const response = await page.request.put('/api/v1/auth/setup', {
      headers: { Origin: baseURL, 'X-Tendo-Setup-Token': setupToken },
      data: { login: ownerLogin, password: ownerPassword, householdName, timezone: 'Europe/Prague' },
    });
    expect(response.status()).toBe(201);
  });

  test('c. wrong password shows an alert and clears the password field', async () => {
    await page.goto('/');
    await expect(page.getByRole('heading', { name: en['login.title'] })).toBeVisible();
    await signIn(page, 'definitely the wrong password');
    await expect(page.getByRole('alert').filter({ hasText: en['login.error.invalid'] })).toBeVisible();
    await expect(page.getByLabel(en['login.password'])).toHaveValue('');
  });

  test('d. correct login opens the household home without household switching', async () => {
    await signIn(page, ownerPassword);
    await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
    await expect(page.getByText(en['home.empty.title'])).toBeVisible();
    await expect(page.getByRole('combobox')).toHaveCount(0);
    await expect(page.getByRole('listbox')).toHaveCount(0);
    await expect(page.getByText(/switch household/i)).toHaveCount(0);

    const session = await page.request.get('/api/v1/session');
    expect(session.status()).toBe(200);
    const body = (await session.json()) as { defaultHouseholdId: string };
    householdId = body.defaultHouseholdId;
    expect(householdId).toMatch(/^[0-9a-f-]{36}$/);
    expect(await page.locator('body').innerText()).not.toContain(householdId);
    expect(await page.content()).not.toContain(householdId);

    const cookies = await page.context().cookies();
    const sessionCookie = cookies.find((cookie) => cookie.name === 'tendo_session');
    expect(sessionCookie?.httpOnly).toBe(true);
    expect(sessionCookie?.sameSite).toBe('Lax');
    sessionToken = sessionCookie?.value ?? '';
    expect(sessionToken).not.toBe('');
  });

  test('e. deep link reload is served the app by the Go binary', async () => {
    const response = await page.goto('/some/deep/route');
    expect(response?.status()).toBe(200);
    await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
  });

  test('f. Czech locale applies, persists across reload, and switches back', async () => {
    await page.goto('/');
    await page.getByRole('button', { name: 'Čeština' }).click();
    await expect(page.getByText(cs['home.empty.title'])).toBeVisible();
    await expect(page.locator('html')).toHaveAttribute('lang', 'cs');
    await page.reload();
    await expect(page.getByText(cs['home.empty.title'])).toBeVisible();
    await expect(page.locator('html')).toHaveAttribute('lang', 'cs');
    await page.getByRole('button', { name: 'English' }).click();
    await expect(page.getByText(en['home.empty.title'])).toBeVisible();
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  });

  test('g. layout fits mobile and desktop viewports without horizontal overflow', async () => {
    for (const viewport of [
      { width: 360, height: 740 },
      { width: 1280, height: 800 },
    ]) {
      await page.setViewportSize(viewport);
      await page.goto('/');
      await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
      await expect(page.getByRole('button', { name: en['header.signOut'] })).toBeVisible();
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      );
      expect(overflow, `${viewport.width}px horizontal overflow`).toBeLessThanOrEqual(0);
    }
  });

  test('h. sign out returns to login and revokes the session server-side', async () => {
    await page.getByRole('button', { name: en['header.signOut'] }).click();
    await expect(page.getByRole('heading', { name: en['login.title'] })).toBeVisible();
    // Replay the old cookie from a clean client: only server-side revocation can make this fail.
    const fresh = await request.newContext({ baseURL });
    try {
      const replay = await fresh.get('/api/v1/session', { headers: { Cookie: `tendo_session=${sessionToken}` } });
      expect(replay.status()).toBe(401);
    } finally {
      await fresh.dispose();
    }
  });

  test('i. keyboard order from the top of the page is skip link, login, password, submit; Enter submits', async () => {
    await page.goto('/');
    const login = page.getByLabel(en['login.login']);
    await expect(login).toBeVisible();
    await expect(page.locator('body')).toBeFocused();
    await page.keyboard.press('Tab');
    await expect(page.getByRole('link', { name: en['app.skipToContent'] })).toBeFocused();
    await page.keyboard.press('Tab');
    await expect(login).toBeFocused();
    await page.keyboard.type(ownerLogin);
    await page.keyboard.press('Tab');
    await expect(page.getByLabel(en['login.password'])).toBeFocused();
    await page.keyboard.type(ownerPassword);
    await page.keyboard.press('Tab');
    await expect(page.getByRole('button', { name: en['login.submit'] })).toBeFocused();
    await page.keyboard.press('Shift+Tab');
    await expect(page.getByLabel(en['login.password'])).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
  });

  test('j. security headers are present and the journey produced no console errors', async () => {
    const response = await page.goto('/');
    expect(response?.headers()['content-security-policy']).toContain("default-src 'self'");
    expect(response?.headers()['x-frame-options']).toBe('DENY');
    expect(response?.headers()['x-content-type-options']).toBe('nosniff');
    const asset = await page.evaluate(
      () => document.querySelector<HTMLScriptElement>('script[src^="/assets/"]')?.src ?? '',
    );
    expect(asset).toContain('/assets/');
    const assetResponse = await page.request.get(asset);
    expect(assetResponse.headers()['cache-control']).toBe('public, max-age=31536000, immutable');
    expect([...failedResponses].sort()).toEqual([...allowedFailedResponses].sort());
    expect(problems).toEqual([]);
  });
});
