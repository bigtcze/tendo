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
// The only browser-initiated HTTP errors the journey may cause: the anonymous session probe, the
// wrong-setup-code onboarding submit, and the wrong-password login, plus the one deliberate stale subject save (412, added by that test). page.request calls are not reported.
const allowedFailedResponses = ['GET /api/v1/session 401', 'PUT /api/v1/auth/setup 401', 'POST /api/v1/session 401'];
const expectedFailedPaths = new Set(['/api/v1/session', '/api/v1/auth/setup']);
const wrongSetupCode = 'A'.repeat(43) + '=';

function watch(page: Page) {
  page.on('console', (message: ConsoleMessage) => {
    if (message.type() !== 'error') return;
    // Chromium logs a generic console error for each failed fetch; the response check below
    // asserts exactly which failures happened, so only those for the session and setup endpoints are skipped.
    if (expectedFailedPaths.has(new URL(message.location().url || 'about:blank').pathname)) return;
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
  let vehicleId = '';

  test.beforeAll(async ({ browser }) => {
    const context = await browser.newContext({ baseURL, locale: 'en-US', timezoneId: 'America/New_York' });
    page = await context.newPage();
    watch(page);
  });

  test.afterAll(async () => {
    await page.context().close();
  });

  test('a. before setup the onboarding form proposes the browser time zone', async () => {
    const response = await page.goto('/');
    expect(response?.status()).toBe(200);
    await expect(page.getByRole('heading', { level: 1, name: en['onboarding.title'] })).toBeVisible();
    await expect(page.locator('#onboarding-timezone')).toHaveValue('America/New_York');
    await expect(page.getByRole('combobox')).toHaveCount(1); // only the time zone select
    await expect(page.getByText(/switch household/i)).toHaveCount(0);

    await page.setViewportSize({ width: 360, height: 740 });
    try {
      await expect(page.getByRole('button', { name: en['onboarding.submit'] })).toBeVisible();
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      );
      expect(overflow, '360px horizontal overflow on onboarding').toBeLessThanOrEqual(0);
    } finally {
      await page.setViewportSize({ width: 1280, height: 720 });
    }
  });

  test('b. a wrong setup code shows an alert and keeps the entered fields', async () => {
    await page.getByLabel(en['onboarding.setupCode'], { exact: true }).fill(wrongSetupCode);
    await page.getByLabel(en['onboarding.householdName']).fill(householdName);
    await page.locator('#onboarding-login').fill(ownerLogin);
    await page.locator('#onboarding-password').fill(ownerPassword);
    await page.getByRole('button', { name: en['onboarding.submit'] }).click();
    await expect(page.getByRole('alert').filter({ hasText: en['onboarding.error.setupCode'] })).toBeVisible();
    await expect(page.getByLabel(en['onboarding.householdName'])).toHaveValue(householdName);
    await expect(page.locator('#onboarding-login')).toHaveValue(ownerLogin);
  });

  test('c. correcting the time zone and onboarding lands on the empty home, skipping login', async () => {
    await page.locator('#onboarding-timezone').selectOption('Europe/Prague');
    await page.getByLabel(en['onboarding.setupCode'], { exact: true }).fill(setupToken);
    await page.getByRole('button', { name: en['onboarding.submit'] }).click();

    await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
    await expect(page.getByText(en['home.empty.title'])).toBeVisible();
    await expect(page.getByRole('heading', { name: en['login.title'] })).toHaveCount(0);
    await expect(page.getByRole('combobox')).toHaveCount(0);
    await expect(page.getByRole('listbox')).toHaveCount(0);
    await expect(page.getByText(/switch household/i)).toHaveCount(0);

    const session = await page.request.get('/api/v1/session');
    expect(session.status()).toBe(200);
    const body = (await session.json()) as { defaultHouseholdId: string };
    householdId = body.defaultHouseholdId;
    expect(householdId).toMatch(/^[0-9a-f-]{36}$/);

    const household = await page.request.get(`/api/v1/households/${householdId}`);
    expect(household.status()).toBe(200);
    const details = (await household.json()) as { name: string; timezone: string };
    expect(details.timezone).toBe('Europe/Prague');
    expect(details.name).toBe(householdName);

    expect(await page.locator('body').innerText()).not.toContain(householdId);
    expect(await page.content()).not.toContain(householdId);

    const cookies = await page.context().cookies();
    const sessionCookie = cookies.find((cookie) => cookie.name === 'tendo_session');
    expect(sessionCookie?.httpOnly).toBe(true);
    expect(sessionCookie?.sameSite).toBe('Lax');
    sessionToken = sessionCookie?.value ?? '';
    expect(sessionToken).not.toBe('');
  });

  test('d. reload keeps the session and setup is closed afterwards', async () => {
    await page.reload();
    await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
    await expect(page.getByText(en['home.empty.title'])).toBeVisible();

    const again = await page.request.put('/api/v1/auth/setup', {
      headers: { Origin: baseURL, 'X-Tendo-Setup-Token': setupToken },
      data: { login: 'second_owner', password: ownerPassword, householdName: 'Second', timezone: 'Europe/Prague' },
    });
    expect(again.status()).toBe(409);
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

  const subjectKeys = ['archived', 'createdAt', 'id', 'name', 'type', 'updatedAt'];
  type ServerSubject = Record<string, unknown> & { id: string; name: string; type: string; archived: boolean };

  async function serverSubjects(archived: boolean): Promise<ServerSubject[]> {
    const response = await page.request.get(`/api/v1/households/${householdId}/subjects?archived=${archived}`);
    expect(response.status()).toBe(200);
    return ((await response.json()) as { items: ServerSubject[] }).items;
  }

  function row(name: string) {
    return page.getByRole('listitem').filter({ hasText: name });
  }

  test('h. People and things opens from home at /people and starts empty', async () => {
    await page.goto('/');
    await page.getByRole('link', { name: en['home.people'] }).click();
    await expect(page).toHaveURL(/\/people$/);
    expect(new URL(page.url()).pathname).toBe('/people');
    await expect(page.getByRole('heading', { level: 1, name: en['subjects.title'] })).toBeVisible();
    await expect(page.getByRole('heading', { name: en['subjects.empty.title'] })).toBeVisible();
    expect(await serverSubjects(false)).toEqual([]);
  });

  test('i. adding a person and a vehicle shows them and stores subjects without any account field', async () => {
    await page.getByRole('button', { name: en['subjects.add'], exact: true }).click();
    const form = page.getByRole('form', { name: en['subjects.add.formLabel'] });
    await form.getByLabel(en['subjects.name']).fill('Anička');
    await form.getByRole('radio', { name: en['subjects.type.person'] }).check();
    await form.getByRole('button', { name: en['subjects.add.submit'] }).click();

    await expect(page.getByRole('status').filter({ hasText: 'Anička' })).toHaveText(
      en['subjects.notice.added'].replace('{name}', 'Anička'),
    );
    await expect(row('Anička')).toBeVisible();
    await expect(row('Anička')).toContainText(en['subjects.type.person']);

    const afterPerson = await serverSubjects(false);
    expect(afterPerson).toHaveLength(1);
    expect(afterPerson[0]).toMatchObject({ name: 'Anička', type: 'person', archived: false });
    expect(Object.keys(afterPerson[0]).sort()).toEqual(subjectKeys);

    await page.getByRole('button', { name: en['subjects.add'], exact: true }).click();
    const second = page.getByRole('form', { name: en['subjects.add.formLabel'] });
    await second.getByLabel(en['subjects.name']).fill('Octavia');
    await second.getByRole('radio', { name: en['subjects.type.vehicle'] }).check();
    await second.getByRole('button', { name: en['subjects.add.submit'] }).click();
    await expect(row('Octavia')).toBeVisible();
    await expect(row('Octavia')).toContainText(en['subjects.type.vehicle']);

    const both = await serverSubjects(false);
    expect(both.map((s) => `${s.name}:${s.type}`).sort()).toEqual(['Anička:person', 'Octavia:vehicle']);
  });

  test('j. renaming a vehicle saves it and advances the server version', async () => {
    const before = (await serverSubjects(false)).find((s) => s.name === 'Octavia');
    expect(before).toBeDefined();
    const id = before!.id;
    vehicleId = id;
    const initial = await page.request.get(`/api/v1/households/${householdId}/subjects/${id}`);
    expect(initial.headers()['etag']).toBe('"1"');

    await page.getByRole('button', { name: en['subjects.edit.label'].replace('{name}', 'Octavia') }).click();
    const form = page.getByRole('form', { name: en['subjects.edit.label'].replace('{name}', 'Octavia') });
    await form.getByLabel(en['subjects.name']).fill('Octavia Combi');
    await form.getByRole('button', { name: en['subjects.save'] }).click();

    await expect(row('Octavia Combi')).toBeVisible();
    await expect(page.getByRole('form')).toHaveCount(0);

    const after = await page.request.get(`/api/v1/households/${householdId}/subjects/${id}`);
    expect(after.status()).toBe(200);
    expect(after.headers()['etag']).toBe('"2"');
    expect(await after.json()).toMatchObject({ id, name: 'Octavia Combi', type: 'vehicle', archived: false });
  });

  test('k. a stale save gets a conflict notice and never overwrites the newer server name', async () => {
    const subjectPath = `/api/v1/households/${householdId}/subjects/${vehicleId}`;
    // Chromium logs a console error for the failed PATCH; the response check in the last test pins the exact failure.
    expectedFailedPaths.add(subjectPath);
    const editLabel = en['subjects.edit.label'].replace('{name}', 'Octavia Combi');
    await page.getByRole('button', { name: editLabel }).click();
    const form = page.getByRole('form', { name: editLabel });
    await expect(form.getByLabel(en['subjects.name'])).toHaveValue('Octavia Combi');

    const outOfBand = await page.request.patch(subjectPath, {
      headers: { 'If-Match': '"2"', Origin: baseURL },
      data: { name: 'Octavia Wagon' },
    });
    expect(outOfBand.status()).toBe(200);
    expect(outOfBand.headers()['etag']).toBe('"3"');

    await form.getByLabel(en['subjects.name']).fill('Octavia RS');
    await form.getByRole('button', { name: en['subjects.save'] }).click();

    await expect(page.getByText(en['subjects.notice.conflict'])).toBeVisible();
    await expect(
      page.getByText(
        en['subjects.notice.yourEntry'].replace('{name}', `Octavia RS (${en['subjects.type.vehicle']})`),
      ),
    ).toBeVisible();
    const latestForm = page.getByRole('form', { name: en['subjects.edit.label'].replace('{name}', 'Octavia Wagon') });
    await expect(latestForm.getByLabel(en['subjects.name'])).toHaveValue('Octavia Wagon');

    const unchanged = await page.request.get(subjectPath);
    expect(unchanged.headers()['etag']).toBe('"3"');
    expect(await unchanged.json()).toMatchObject({ id: vehicleId, name: 'Octavia Wagon' });

    await latestForm.getByLabel(en['subjects.name']).fill('Octavia RS');
    await latestForm.getByRole('button', { name: en['subjects.save'] }).click();
    await expect(row('Octavia RS')).toBeVisible();
    await expect(page.getByRole('form')).toHaveCount(0);

    const saved = await page.request.get(subjectPath);
    expect(saved.headers()['etag']).toBe('"4"');
    expect(await saved.json()).toMatchObject({ id: vehicleId, name: 'Octavia RS', archived: false });
  });

  test('l. archiving hides a subject, and Show archived lets you restore it', async () => {
    await page.getByRole('button', { name: en['subjects.archive.label'].replace('{name}', 'Octavia RS') }).click();
    await expect(
      page.getByRole('status').filter({ hasText: en['subjects.notice.archived'].replace('{name}', 'Octavia RS') }),
    ).toBeVisible();
    await expect(page.getByRole('button', { name: en['subjects.undo'] })).toBeVisible();
    await expect(row('Octavia RS')).toHaveCount(0);
    await expect(row('Anička')).toBeVisible();

    expect((await serverSubjects(true)).map((s) => s.name)).toEqual(['Octavia RS']);
    expect((await serverSubjects(false)).map((s) => s.name)).toEqual(['Anička']);

    await page.getByRole('button', { name: en['subjects.showArchived'] }).click();
    await expect(page.getByRole('heading', { level: 2, name: en['subjects.archived.title'] })).toBeVisible();
    await expect(row('Octavia RS')).toBeVisible();
    await page.getByRole('button', { name: en['subjects.restore.label'].replace('{name}', 'Octavia RS') }).click();
    await expect(row('Octavia RS')).toHaveCount(0);

    expect(await serverSubjects(true)).toEqual([]);
    expect((await serverSubjects(false)).map((s) => s.name).sort()).toEqual(['Anička', 'Octavia RS']);

    await page.getByRole('button', { name: en['subjects.showCurrent'] }).click();
    await expect(page.getByRole('heading', { level: 2, name: en['subjects.archived.title'] })).toHaveCount(0);
    await expect(row('Octavia RS')).toBeVisible();
    await expect(row('Anička')).toBeVisible();
  });

  test('m. /people survives reload and deep links, and browser Back returns home', async () => {
    await page.reload();
    expect(new URL(page.url()).pathname).toBe('/people');
    await expect(page.getByRole('heading', { level: 1, name: en['subjects.title'] })).toBeVisible();
    await expect(row('Anička')).toBeVisible();

    await page.goBack();
    expect(new URL(page.url()).pathname).toBe('/');
    await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();

    const response = await page.goto('/people');
    expect(response?.status()).toBe(200);
    await expect(page.getByRole('heading', { level: 1, name: en['subjects.title'] })).toBeVisible();
    await expect(row('Anička')).toBeVisible();
  });

  test('n. the people screen fits 360px with the add form open and switches to Czech and back', async () => {
    await page.setViewportSize({ width: 360, height: 740 });
    try {
      await page.getByRole('button', { name: en['subjects.add'], exact: true }).click();
      await expect(page.getByRole('form', { name: en['subjects.add.formLabel'] })).toBeVisible();
      const overflow = await page.evaluate(
        () => document.documentElement.scrollWidth - window.innerWidth,
      );
      expect(overflow, '360px horizontal overflow on people screen').toBeLessThanOrEqual(0);
    } finally {
      await page.setViewportSize({ width: 1280, height: 720 });
    }

    await page.getByRole('button', { name: 'Čeština' }).click();
    await expect(page.getByRole('heading', { level: 1, name: cs['subjects.title'] })).toBeVisible();
    await expect(page.getByRole('form', { name: cs['subjects.add.formLabel'] })).toBeVisible();
    await page.getByRole('button', { name: 'English' }).click();
    await expect(page.getByRole('heading', { level: 1, name: en['subjects.title'] })).toBeVisible();

    await page.getByRole('link', { name: en['subjects.backHome'] }).click();
    await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
    expect(new URL(page.url()).pathname).toBe('/');
  });

  test('o. sign out returns to login and revokes the session server-side', async () => {
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

  test('p. wrong password shows an alert and clears the password field', async () => {
    await page.goto('/');
    await expect(page.getByRole('heading', { name: en['login.title'] })).toBeVisible();
    await signIn(page, 'definitely the wrong password');
    await expect(page.getByRole('alert').filter({ hasText: en['login.error.invalid'] })).toBeVisible();
    await expect(page.getByLabel(en['login.password'])).toHaveValue('');
  });

  test('q. keyboard order from the top of the page is skip link, login, password, submit; Enter submits', async () => {
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

  test('r. security headers are present and the journey produced no console errors', async () => {
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
    const subjectConflict = `PATCH /api/v1/households/${householdId}/subjects/${vehicleId} 412`;
    expect([...failedResponses].sort()).toEqual([...allowedFailedResponses, subjectConflict].sort());
    expect(problems).toEqual([]);
  });
});
