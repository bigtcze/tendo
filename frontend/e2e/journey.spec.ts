import { expect, request, test, type ConsoleMessage, type Locator, type Page } from '@playwright/test';
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
const e2eClockNow = required('E2E_CLOCK_NOW');
const foreignHouseholdId = required('E2E_FOREIGN_HOUSEHOLD_ID');
const foreignSubjectId = required('E2E_FOREIGN_SUBJECT_ID');
const foreignItemId = required('E2E_FOREIGN_ITEM_ID');
const expectedCompletionDate = new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Prague' }).format(new Date(e2eClockNow));
const ownerLogin = 'owner_e2e';
const householdName = 'Veselí';

const problems: string[] = [];
const failedResponses = new Set<string>();
// Browser-initiated expected failures: anonymous session/setup/login attempts, deliberate stale save,
// and the foreign detail deep-link lookup. page.request calls are not reported.
const allowedFailedResponses = ['GET /api/v1/session 401', 'PUT /api/v1/auth/setup 401', 'POST /api/v1/session 401'];
const expectedFailedPaths = new Set(['/api/v1/session', '/api/v1/auth/setup']);
const expectedForeignDetailPath = new RegExp(`^/api/v1/households/[0-9a-f-]{36}/items/${foreignItemId}$`);
const wrongSetupCode = 'A'.repeat(43) + '=';

function watch(page: Page) {
  page.on('console', (message: ConsoleMessage) => {
    if (message.type() !== 'error') return;
    // Chromium logs a generic console error for each failed fetch; the response check below
    // asserts exactly which failures happened, so only the session/setup endpoints and the foreign item deep link are skipped.
    const path = new URL(message.location().url || 'about:blank').pathname;
    if (expectedFailedPaths.has(path)) return;
    if (expectedForeignDetailPath.test(path) && /status of 404/.test(message.text())) return;
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
  await page.getByRole('button', { name: en['login.submit'], exact: true }).click();
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
      { width: 768, height: 1024 },
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

  const subjectKeys = ['archived', 'createdAt', 'id', 'linkedUserId', 'name', 'type', 'updatedAt'];
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
    await page.getByRole('link', { name: en['home.people'], exact: true }).click();
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
    const createRequestPromise = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith(`/api/v1/households/${householdId}/subjects`));
    await form.getByRole('button', { name: en['subjects.add.submit'] }).click();
    const createRequest = await createRequestPromise;
    const createBody = createRequest.postDataJSON() as { name: string; type: string };
    const createKey = createRequest.headers()['idempotency-key'];
    expect(createKey).toMatch(/^[0-9a-f]{32}$/);

    await expect(page.getByRole('status').filter({ hasText: 'Anička' })).toHaveText(
      en['subjects.notice.added'].replace('{name}', 'Anička'),
    );
    await expect(row('Anička')).toBeVisible();
    await expect(row('Anička')).toContainText(en['subjects.type.person']);

    const afterPerson = await serverSubjects(false);
    expect(afterPerson).toHaveLength(1);
    expect(afterPerson[0]).toMatchObject({ name: 'Anička', type: 'person', archived: false });
    expect(Object.keys(afterPerson[0]).sort()).toEqual(subjectKeys);
    expect(createBody).toEqual({ name: 'Anička', type: 'person' });
    const replay = await page.request.post(`/api/v1/households/${householdId}/subjects`, {
      headers: { 'Idempotency-Key': createKey, Origin: new URL(page.url()).origin },
      data: createBody,
    });
    expect(replay.status()).toBe(201);
    expect((await replay.json() as { id: string }).id).toBe(afterPerson[0]!.id);
    const afterReplay = await serverSubjects(false);
    expect(afterReplay.filter((entry) => entry.name === 'Anička')).toHaveLength(1);

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

    await page.getByRole('link', { name: en['nav.home'], exact: true }).click();
    await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
    expect(new URL(page.url()).pathname).toBe('/');
  });

  type ServerItem = {
    id: string; title: string; attentionOn: string | null; attention: string; done: boolean; lastCompletedOn: string | null; responsibleUserId: string | null;
    recurrence: { intervalValue: number; intervalUnit: string; mode: string } | null;
  };
  type ServerCompletion = { id: string; completedOn: string; cycleAttentionOn: string | null; nextAttentionOn: string | null; undoneAt: string | null; recurrence: unknown };

  async function serverItem(title: string, done = false): Promise<ServerItem> {
    const response = await page.request.get(`/api/v1/households/${householdId}/items?done=${done}`);
    expect(response.status()).toBe(200);
    const found = ((await response.json()) as { items: ServerItem[] }).items.find((i) => i.title === title);
    expect(found, `${title} on the server (done=${done})`).toBeDefined();
    return found!;
  }

  async function serverCompletions(itemId: string): Promise<ServerCompletion[]> {
    const response = await page.request.get(`/api/v1/households/${householdId}/items/${itemId}/completions`);
    expect(response.status()).toBe(200);
    return ((await response.json()) as { items: ServerCompletion[] }).items;
  }

  // Household today in Europe/Prague derived from the server's deterministic test business clock.
  function pragueToday(): string {
    return expectedCompletionDate;
  }

  // Anchors are chosen relative to the deterministic test date so fixed and fluid outcomes always differ.
  // Both anchors are well over a year/week in the past, so the completion is late. Date.UTC rolls invalid
  // days over, and 29 February is skipped so the yearly anchor's month-day exists in every year.
  function yearlyAnchor(): string {
    const [y, m, d] = pragueToday().split('-').map(Number);
    const anchor = new Date(Date.UTC(y - 2, m - 1, d + 15)).toISOString().slice(0, 10);
    return anchor.endsWith('-02-29') ? addDays(anchor, 1) : anchor;
  }
  function weeklyAnchor(): string {
    return addDays(pragueToday(), -(7 * 60 + 3));
  }

  function addYearsClamped(isoDate: string, years: number): string {
    const [y, m, d] = isoDate.split('-').map(Number);
    const last = new Date(Date.UTC(y + years, m, 0)).getUTCDate();
    return `${y + years}-${String(m).padStart(2, '0')}-${String(Math.min(d, last)).padStart(2, '0')}`;
  }

  function addDays(isoDate: string, days: number): string {
    const date = new Date(`${isoDate}T00:00:00Z`);
    date.setUTCDate(date.getUTCDate() + days);
    return date.toISOString().slice(0, 10);
  }

  function escapeRegExp(value: string) { return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'); }
  function group(name: string) {
    const matches = page.getByRole('heading', { level: 2, name: new RegExp(`^${escapeRegExp(name)}(\\s|$)`) });
    return matches.first().locator('xpath=ancestor::section[1]');
  }

  async function addItem(options: { title: string; subject: string; attentionOn?: string; historicalCompletedOn?: string; repeat?: { value: string; unit: string; fluid: boolean } }) {
    await page.getByRole('button', { name: en['items.add'], exact: true }).click();
    const form = page.getByRole('form', { name: en['items.add.formLabel'] });
    await expect(form.getByLabel(en['items.title'])).toBeFocused();
    const repeat = form.getByRole('switch', { name: en['items.repeat'], exact: true });
    await expect(repeat).not.toBeChecked();
    await expect(form.getByRole('switch', { name: en['items.fluid'] })).toHaveCount(0);
    await form.getByLabel(en['items.title']).fill(options.title);
    await form.getByLabel(en['items.subject'], { exact: true }).selectOption({ label: options.subject });
    if (options.attentionOn) await form.getByLabel(en['items.attentionDate']).fill(options.attentionOn);
    if (options.repeat) {
      await repeat.check();
      const fluid = form.getByRole('switch', { name: en['items.fluid'] });
      await expect(fluid).not.toBeChecked();
      await form.getByRole('spinbutton').fill(options.repeat.value);
      await form.getByRole('combobox', { name: en['items.intervalUnit'] }).selectOption(options.repeat.unit);
      if (options.repeat.fluid) await fluid.check();
      if (options.historicalCompletedOn) {
        await form.getByRole('button', { name: en['items.historical.show'] }).click();
        await form.getByLabel(en['items.historical.label']).fill(options.historicalCompletedOn);
      }
    }
    await form.getByRole('button', { name: en['items.add.submit'] }).click();
    await expect(page.getByRole('form', { name: en['items.add.formLabel'] })).toHaveCount(0);
  }

  test('n1. a one-off item with Repeat off completes, leaves the active list, keeps history, and undo brings it back', async () => {
    await page.goto('/');
    try {
      for (const viewport of [
        { width: 360, height: 740 },
        { width: 768, height: 1024 },
        { width: 1280, height: 800 },
      ]) {
        await page.setViewportSize(viewport);
        await page.getByRole('button', { name: en['items.add'], exact: true }).click();
        await expect(page.getByRole('form', { name: en['items.add.formLabel'] })).toBeVisible();
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
        expect(overflow, `${viewport.width}px horizontal overflow with the item form open`).toBeLessThanOrEqual(0);
        await page.getByRole('button', { name: en['subjects.cancel'], exact: true }).click();
      }
    } finally {
      await page.setViewportSize({ width: 1280, height: 720 });
    }

    await addItem({ title: 'Book dental check', subject: 'Anička' });
    await expect(group(en['items.group.needs']).getByRole('listitem').filter({ hasText: 'Book dental check' })).toBeVisible();
    const created = await serverItem('Book dental check');
    expect(created).toMatchObject({ recurrence: null, attentionOn: null, attention: 'needs_attention', done: false });

    await row('Book dental check').getByRole('button', { name: en['items.done'] }).click();
    await expect(page.getByRole('status').filter({ hasText: en['items.notice.done'].replace('{title}', 'Book dental check') })).toBeVisible();
    await expect(row('Book dental check')).toHaveCount(0);
    const done = await serverItem('Book dental check', true);
    expect(done.done).toBe(true);
    const history = await serverCompletions(done.id);
    expect(history).toHaveLength(1);
    expect(history[0]).toMatchObject({ nextAttentionOn: null, recurrence: null, undoneAt: null, completedOn: expectedCompletionDate });
    expect(done.lastCompletedOn).toBe(expectedCompletionDate);

    await page.getByRole('button', { name: en['items.undo'] }).click();
    await expect(row('Book dental check')).toBeVisible();
    const restored = await serverItem('Book dental check');
    expect(restored).toMatchObject({ done: false, lastCompletedOn: null, attentionOn: null });
    const afterUndo = await serverCompletions(done.id);
    expect(afterUndo).toHaveLength(1);
    expect(afterUndo[0].undoneAt).not.toBeNull();

    await row('Book dental check').getByRole('button', { name: en['items.done'] }).click();
    await expect(row('Book dental check')).toHaveCount(0);
    const completedAgain = await serverItem('Book dental check', true);
    expect(completedAgain).toMatchObject({ done: true, lastCompletedOn: expectedCompletionDate });
    const secondHistory = await serverCompletions(done.id);
    expect(secondHistory).toHaveLength(2);
    expect(secondHistory[1]).toMatchObject({ completedOn: expectedCompletionDate });
  });

  test('n2. a fixed yearly item (Repeat on, Fluid off) completed late keeps its planned date', async () => {
    const anchor = yearlyAnchor();
    await addItem({ title: 'Service the boiler', subject: 'Anička', attentionOn: anchor, repeat: { value: '1', unit: 'year', fluid: false } });
    await expect(group(en['items.group.needs']).getByRole('listitem').filter({ hasText: 'Service the boiler' })).toBeVisible();
    const before = await serverItem('Service the boiler');
    expect(before).toMatchObject({ attentionOn: anchor, recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' } });

    await row('Service the boiler').getByRole('button', { name: en['items.done'] }).click();
    await expect(page.getByRole('status').filter({ hasText: 'Service the boiler' })).toContainText('Next time:');
    const [receipt] = await serverCompletions(before.id);
    // First anchor-aligned date strictly after the completion day, never completion + 1 year.
    let expected = anchor;
    while (expected <= receipt.completedOn) expected = addYearsClamped(anchor, Number(expected.slice(0, 4)) - Number(anchor.slice(0, 4)) + 1);
    expect(expected.slice(5)).toBe(anchor.slice(5));
    expect(expected).not.toBe(addYearsClamped(receipt.completedOn, 1));
    expect(receipt).toMatchObject({ cycleAttentionOn: anchor, nextAttentionOn: expected, completedOn: expectedCompletionDate });
    const after = await serverItem('Service the boiler');
    expect(after).toMatchObject({ attentionOn: expected, attention: 'upcoming', done: false, lastCompletedOn: expectedCompletionDate });
    await expect(group(en['items.group.upcoming']).getByRole('listitem').filter({ hasText: 'Service the boiler' })).toBeVisible();
  });

  test('n3. a fluid weekly item (Repeat on, Fluid on) completed late counts from the completion day; undo restores it', async () => {
    const anchor = weeklyAnchor();
    await addItem({ title: 'Water the plants', subject: 'Octavia RS', attentionOn: anchor, repeat: { value: '1', unit: 'week', fluid: true } });
    const before = await serverItem('Water the plants');
    expect(before.recurrence).toEqual({ intervalValue: 1, intervalUnit: 'week', mode: 'after_completion' });

    await row('Water the plants').getByRole('button', { name: en['items.done'] }).click();
    await expect(page.getByRole('status').filter({ hasText: 'Water the plants' })).toContainText('Next time:');
    const [receipt] = await serverCompletions(before.id);
    const expected = addDays(receipt.completedOn, 7);
    // A fixed weekly cadence would land on the anchor's weekday, which is never the completion's weekday here.
    const fixedDays = (Date.parse(expected) - Date.parse(anchor)) / 86_400_000;
    expect(fixedDays % 7).not.toBe(0);
    expect(receipt).toMatchObject({ cycleAttentionOn: anchor, nextAttentionOn: expected, completedOn: expectedCompletionDate });
    expect(await serverItem('Water the plants')).toMatchObject({ attentionOn: expected, attention: 'upcoming' });
    await expect(group(en['items.group.upcoming']).getByRole('listitem').filter({ hasText: 'Water the plants' })).toBeVisible();

    await page.getByRole('button', { name: en['items.undo'] }).click();
    await expect(group(en['items.group.needs']).getByRole('listitem').filter({ hasText: 'Water the plants' })).toBeVisible();
    expect(await serverItem('Water the plants')).toMatchObject({ attentionOn: anchor, attention: 'needs_attention', lastCompletedOn: null });
    const history = await serverCompletions(before.id);
    expect(history).toHaveLength(1);
    expect(history[0].undoneAt).not.toBeNull();
  });

  test('n4. item detail workflow preserves a fixed monthly schedule and history through archive/restore', async () => {
    const anchor = addDays(pragueToday(), -420);
    const title = 'Inspect the roof';
    await addItem({ title, subject: 'Anička', attentionOn: anchor, repeat: { value: '1', unit: 'month', fluid: false } });
    await expect(group(en['items.group.needs']).getByRole('listitem').filter({ hasText: title })).toBeVisible();
    const created = await serverItem(title);
    const itemId = created.id;
    expect(created).toMatchObject({ attentionOn: anchor, lastCompletedOn: null, recurrence: { intervalValue: 1, intervalUnit: 'month', mode: 'fixed' } });

    async function openDetail() {
      await page.getByRole('link', { name: en['itemDetail.open'].replace('{title}', title) }).click();
      await expect(page).toHaveURL(new RegExp(`/items/${itemId}$`));
      await expect(page.getByRole('heading', { level: 1, name: title })).toBeVisible();
    }
    async function detailItem() {
      const response = await page.request.get(`/api/v1/households/${householdId}/items/${itemId}`);
      expect(response.status()).toBe(200);
      return (await response.json()) as ServerItem & { workflowState: string; archived: boolean };
    }
    async function backHome() {
      await page.getByRole('link', { name: en['nav.home'], exact: true }).click();
      await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
    }

    await openDetail();
    await page.getByRole('button', { name: en['itemDetail.start'] }).click();
    await expect.poll(async () => (await detailItem()).workflowState).toBe('in_progress');
    expect(await detailItem()).toMatchObject({ attentionOn: anchor, lastCompletedOn: null });
    await backHome();
    await expect(group(en['items.group.inProgress']).getByRole('listitem').filter({ hasText: title })).toBeVisible();

    await openDetail();
    await page.getByRole('button', { name: en['itemDetail.waitingAction'] }).click();
    await expect.poll(async () => (await detailItem()).workflowState).toBe('waiting');
    expect(await detailItem()).toMatchObject({ attentionOn: anchor, lastCompletedOn: null });
    expect(await serverCompletions(itemId)).toEqual([]);

    await backHome();
    await row(title).getByRole('button', { name: en['items.done'] }).click();
    await expect.poll(async () => (await serverCompletions(itemId)).length).toBe(1);
    const [completion] = await serverCompletions(itemId);
    const [anchorYear, anchorMonth, anchorDay] = anchor.split('-').map(Number);
    const monthFromAnchor = (offset: number) => {
      const target = new Date(Date.UTC(anchorYear, anchorMonth - 1 + offset, 1));
      const year = target.getUTCFullYear();
      const month = target.getUTCMonth() + 1;
      const lastDay = new Date(Date.UTC(year, month, 0)).getUTCDate();
      return `${year}-${String(month).padStart(2, '0')}-${String(Math.min(anchorDay, lastDay)).padStart(2, '0')}`;
    };
    let monthOffset = 1;
    let expected = monthFromAnchor(monthOffset);
    while (expected <= completion.completedOn) expected = monthFromAnchor(++monthOffset);
    expect(completion).toMatchObject({ cycleAttentionOn: anchor, nextAttentionOn: expected, undoneAt: null, completedOn: expectedCompletionDate });
    expect(await serverCompletions(itemId)).toHaveLength(1);
    expect(await detailItem()).toMatchObject({ attentionOn: expected, workflowState: 'open', lastCompletedOn: completion.completedOn });

    await openDetail();
    const displayDate = (value: string) => new Intl.DateTimeFormat('en-US', { dateStyle: 'medium', timeZone: 'UTC' }).format(new Date(`${value}T00:00:00Z`));
    await expect(page.getByRole('heading', { level: 2, name: en['itemDetail.history'] })).toBeVisible();
    const history = page.locator('ol').getByRole('listitem');
    await expect(history).toHaveCount(1);
    await expect(history).toContainText(en['itemDetail.historyDone'].replace('{date}', displayDate(completion.completedOn)));
    await expect(history).toContainText(en['itemDetail.historyNext'].replace('{date}', displayDate(expected)));

    await page.getByRole('button', { name: en['itemDetail.pause'] }).click();
    await expect.poll(async () => (await detailItem()).workflowState).toBe('paused');
    await page.getByRole('button', { name: en['itemDetail.resume'] }).click();
    await expect.poll(async () => (await detailItem()).workflowState).toBe('open');
    await page.getByRole('button', { name: en['subjects.archive'] }).click();
    await expect.poll(async () => (await detailItem()).archived).toBe(true);
    await expect(history).toHaveCount(1);
    await expect(history).toContainText(en['itemDetail.historyDone'].replace('{date}', displayDate(completion.completedOn)));
    await page.getByRole('button', { name: en['subjects.restore'] }).click();
    await expect.poll(async () => (await detailItem()).archived).toBe(false);

    for (const viewport of [{ width: 360, height: 740 }, { width: 768, height: 1024 }, { width: 1280, height: 800 }]) {
      await page.setViewportSize(viewport);
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
      expect(overflow, `${viewport.width}px horizontal overflow on item detail`).toBeLessThanOrEqual(0);
    }
    await page.setViewportSize({ width: 1280, height: 720 });
  });

  test('n5. editing Repeat and Fluid on the detail screen keeps the attention date and completion history', async () => {
    const anchor = addDays(pragueToday(), -30);
    const title = 'Descale the kettle';
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
    await addItem({ title, subject: 'Anička', attentionOn: anchor, repeat: { value: '1', unit: 'year', fluid: false } });
    const itemId = (await serverItem(title)).id;
    await row(title).getByRole('button', { name: en['items.done'] }).click();
    await expect.poll(async () => (await serverCompletions(itemId)).length).toBe(1);
    const history = await serverCompletions(itemId);
    const next = addYearsClamped(anchor, 1);
    expect(history[0]).toMatchObject({ cycleAttentionOn: anchor, nextAttentionOn: next, undoneAt: null, completedOn: expectedCompletionDate });

    async function detailItem() {
      const response = await page.request.get(`/api/v1/households/${householdId}/items/${itemId}`);
      expect(response.status()).toBe(200);
      return (await response.json()) as ServerItem & { notes: string | null };
    }
    async function saveEdit(change: (form: Locator) => Promise<void>) {
      await page.getByRole('button', { name: en['itemDetail.edit.button'], exact: true }).click();
      const form = page.getByRole('form', { name: en['itemDetail.edit.formLabel'] });
      await expect(form.getByLabel(en['items.title'])).toHaveValue(title);
      await change(form);
      await form.getByRole('button', { name: en['itemDetail.save'] }).click();
      await expect(page.getByRole('form', { name: en['itemDetail.edit.formLabel'] })).toHaveCount(0);
      await expect(page.getByRole('status').filter({ hasText: en['itemDetail.updated'] })).toBeVisible();
    }

    await page.getByRole('link', { name: en['itemDetail.open'].replace('{title}', title) }).click();
    await expect(page.getByRole('heading', { level: 1, name: title })).toBeVisible();

    await saveEdit(async (form) => {
      await expect(form.getByRole('switch', { name: en['items.repeat'], exact: true })).toBeChecked();
      const fluid = form.getByRole('switch', { name: en['items.fluid'] });
      await expect(fluid).not.toBeChecked();
      await fluid.check();
      await form.getByRole('button', { name: en['items.notes.show'] }).click();
      await form.getByLabel(en['items.notes']).fill('Call the technician first');
    });
    expect(await detailItem()).toMatchObject({ attentionOn: next, notes: 'Call the technician first', recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'after_completion' } });
    expect(await serverCompletions(itemId)).toEqual(history);

    await saveEdit(async (form) => {
      await form.getByRole('switch', { name: en['items.repeat'], exact: true }).uncheck();
      await expect(form.getByRole('switch', { name: en['items.fluid'] })).toHaveCount(0);
    });
    expect(await detailItem()).toMatchObject({ attentionOn: next, recurrence: null, done: false });
    expect(await serverCompletions(itemId)).toEqual(history);
    await expect(page.locator('ol').getByRole('listitem')).toHaveCount(1);

    await page.getByRole('link', { name: en['nav.home'], exact: true }).click();
    await row(title).getByRole('button', { name: en['items.done'] }).click();
    await expect.poll(async () => (await serverCompletions(itemId)).length).toBe(2);
    const after = await serverCompletions(itemId);
    expect(after[0]).toEqual(history[0]);
    expect(after[1]).toMatchObject({ cycleAttentionOn: next, nextAttentionOn: null, recurrence: null, completedOn: expectedCompletionDate });
    expect(await detailItem()).toMatchObject({ done: true });
  });

  test('n6. historical completion initializes recurring cycles, history, undo, and upcoming state', async () => {
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();

    // Exercise the disclosure at the requested narrow width, then create using the same dates.
    await page.setViewportSize({ width: 360, height: 740 });
    try {
      await page.getByRole('button', { name: en['items.add'], exact: true }).click();
      const form = page.getByRole('form', { name: en['items.add.formLabel'] });
      await form.getByLabel(en['items.title']).fill('Historical fixed monthly January');
      await form.getByLabel(en['items.subject'], { exact: true }).selectOption({ label: 'Anička' });
      await form.getByRole('switch', { name: en['items.repeat'], exact: true }).check();
      await form.getByRole('spinbutton').fill('1');
      await form.getByRole('combobox', { name: en['items.intervalUnit'] }).selectOption('month');
      await form.getByRole('button', { name: en['items.historical.show'] }).click();
      await expect(form.getByLabel(en['items.historical.label'])).toBeVisible();
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
      expect(overflow, '360px horizontal overflow with previous-completion disclosure open').toBeLessThanOrEqual(0);
      await form.getByRole('button', { name: en['items.cancel'] }).click();
    } finally {
      await page.setViewportSize({ width: 1280, height: 720 });
    }

    const fixedTitle = 'Historical fixed monthly January';
    await addItem({ title: fixedTitle, subject: 'Anička', historicalCompletedOn: '2025-10-31', repeat: { value: '1', unit: 'month', fluid: false } });
    const fixed = await serverItem(fixedTitle);
    expect(fixed).toMatchObject({ attentionOn: '2025-11-30', lastCompletedOn: '2025-10-31', attention: 'needs_attention', recurrence: { intervalValue: 1, intervalUnit: 'month', mode: 'fixed' } });
    expect(await serverCompletions(fixed.id)).toHaveLength(1);
    expect(await serverCompletions(fixed.id)).toMatchObject([{ completedOn: '2025-10-31', cycleAttentionOn: null, nextAttentionOn: '2025-11-30', recurrence: { intervalValue: 1, intervalUnit: 'month', mode: 'fixed' }, undoneAt: null }]);
    await expect(group(en['items.group.needs']).getByRole('listitem').filter({ hasText: fixedTitle })).toBeVisible();

    await page.getByRole('link', { name: en['itemDetail.open'].replace('{title}', fixedTitle) }).click();
    await expect(page.getByRole('heading', { level: 1, name: fixedTitle })).toBeVisible();
    const dateLabel = (value: string) => new Intl.DateTimeFormat('en-US', { dateStyle: 'medium', timeZone: 'UTC' }).format(new Date(`${value}T00:00:00Z`));
    await expect(page.getByText(en['itemDetail.lastDone'].replace('{date}', dateLabel('2025-10-31')))).toBeVisible();
    await page.reload();
    await expect(page.getByRole('heading', { level: 1, name: fixedTitle })).toBeVisible();
    await expect(page.locator('ol').getByRole('listitem')).toHaveCount(1);
    await expect(page.locator('ol').getByRole('listitem')).toContainText(en['itemDetail.historyDone'].replace('{date}', dateLabel('2025-10-31')));

    await page.getByRole('link', { name: en['nav.home'], exact: true }).click();
    await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
    await row(fixedTitle).getByRole('button', { name: en['items.done'] }).click();
    await expect.poll(async () => (await serverCompletions(fixed.id)).length).toBe(2);
    expect(await serverItem(fixedTitle)).toMatchObject({ attentionOn: '2026-01-30', lastCompletedOn: expectedCompletionDate });
    await page.getByRole('button', { name: en['items.undo'] }).click();
    await expect.poll(async () => (await serverItem(fixedTitle)).attentionOn).toBe('2025-11-30');
    expect(await serverItem(fixedTitle)).toMatchObject({ attentionOn: '2025-11-30', lastCompletedOn: '2025-10-31' });

    const fluidTitle = 'Historical fluid monthly February';
    await addItem({ title: fluidTitle, subject: 'Anička', historicalCompletedOn: '2025-10-31', repeat: { value: '1', unit: 'month', fluid: true } });
    const fluid = await serverItem(fluidTitle);
    expect(fluid).toMatchObject({ attentionOn: '2025-11-30', lastCompletedOn: '2025-10-31', recurrence: { intervalValue: 1, intervalUnit: 'month', mode: 'after_completion' } });
    expect(await serverCompletions(fluid.id)).toHaveLength(1);
    await row(fluidTitle).getByRole('button', { name: en['items.done'] }).click();
    await expect.poll(async () => (await serverCompletions(fluid.id)).length).toBe(2);
    expect(await serverItem(fluidTitle)).toMatchObject({ attentionOn: '2026-02-15', lastCompletedOn: expectedCompletionDate });

    const upcomingTitle = 'Historical upcoming monthly February';
    await addItem({ title: upcomingTitle, subject: 'Anička', historicalCompletedOn: '2025-12-31', repeat: { value: '1', unit: 'month', fluid: false } });
    const upcoming = await serverItem(upcomingTitle);
    expect(upcoming).toMatchObject({ attentionOn: '2026-01-31', lastCompletedOn: '2025-12-31', attention: 'upcoming' });
    const upcomingRow = group(en['items.group.upcoming']).getByRole('listitem').filter({ hasText: upcomingTitle });
    await expect(upcomingRow).toBeVisible();
    await expect(upcomingRow).toContainText('Jan 31, 2026');

    const ordinaryTitle = 'Ordinary repeating no history';
    await addItem({ title: ordinaryTitle, subject: 'Anička', repeat: { value: '1', unit: 'month', fluid: false } });
    const ordinary = await serverItem(ordinaryTitle);
    expect(ordinary).toMatchObject({ lastCompletedOn: null, attentionOn: null });
    expect(await serverCompletions(ordinary.id)).toEqual([]);
  });

  test('n7. an owner invites a second person who joins as a member with member-only access', async ({ browser }) => {
    test.setTimeout(60_000);
    await page.goto('/');
    await page.getByRole('link', { name: en['home.members'], exact: true }).click();
    await expect(page).toHaveURL(/\/members$/);
    await expect(page.getByRole('heading', { level: 1, name: en['members.title'] })).toBeVisible();
    const ownerRow = page.getByRole('listitem').filter({ hasText: ownerLogin });
    await expect(ownerRow).toContainText(en['members.role.owner']);
    await expect(ownerRow).toContainText(en['members.you']);

    await page.getByRole('button', { name: en['members.invite.action'] }).click();
    const inviteField = page.getByRole('textbox', { name: en['members.invite.link'] });
    const firstLink = await inviteField.inputValue();
    expect(firstLink.startsWith(`${baseURL}/invite#`)).toBe(true);
    expect(new URL(firstLink).hash).toMatch(/^#[A-Za-z0-9_-]{43}$/);
    const invitationList = page.getByRole('heading', { name: en['members.invitations.title'] }).locator('..');
    await expect(invitationList).toContainText(en['members.invitation.status.pending']);
    const invitationPath = `/api/v1/households/${householdId}/invitations`;
    const beforeSecondInvite = (await (await page.request.get(invitationPath)).json()).items as Array<{ id: string }>;

    await page.getByRole('button', { name: en['members.invite.action'] }).click();
    const secondLink = await inviteField.inputValue();
    expect(secondLink).not.toBe(firstLink);
    const afterSecondInvite = (await (await page.request.get(invitationPath)).json()).items as Array<{ id: string }>;
    const secondInviteId = afterSecondInvite.find((item) => !beforeSecondInvite.some((existing) => existing.id === item.id))?.id;
    expect(secondInviteId).toBeTruthy();
    const secondInviteIndex = afterSecondInvite.findIndex((item) => item.id === secondInviteId);
    const invitationRows = invitationList.locator('ul').getByRole('listitem');
    await invitationRows.nth(secondInviteIndex).getByRole('button', { name: en['members.invitation.revoke'] }).click();
    await invitationRows.nth(secondInviteIndex).getByRole('button', { name: en['members.invitation.yes'] }).click();
    await expect(invitationList).toContainText(en['members.invitation.status.revoked']);

    const memberContext = await browser.newContext({ baseURL, locale: 'en-US', timezoneId: 'Europe/Prague' });
    const memberPage = await memberContext.newPage();
    const memberProblems: string[] = [];
    const memberFailedResponses = new Set<string>();
    const expectedMemberConsoleFailurePaths = new Set(['/api/v1/session', '/api/v1/auth/invitations/accept', '/api/v1/invitations/accept']);
    memberPage.on('console', (message) => {
      if (message.type() === 'error' && !expectedMemberConsoleFailurePaths.has(new URL(message.location().url || 'about:blank').pathname)) memberProblems.push(`console: ${message.text()}`);
    });
    memberPage.on('pageerror', (error) => memberProblems.push(`pageerror: ${error.message}`));
    memberPage.on('response', (response) => {
      const path = new URL(response.url()).pathname;
      // The initial signed-out session probe is routine; collect unexpected failures and the explicit invite failures below.
      if (response.status() >= 400 && !(response.request().method() === 'GET' && path === '/api/v1/session' && response.status() === 401)) memberFailedResponses.add(`${response.request().method()} ${path} ${response.status()}`);
    });
    try {
      await memberPage.goto(firstLink);
      expect(new URL(memberPage.url()).pathname).toBe('/invite');
      await expect(memberPage.getByRole('heading', { level: 1, name: en['invite.title'] })).toBeVisible();
      await memberPage.locator('#invite-login').fill('member_e2e');
      await memberPage.locator('#invite-password').fill(`${ownerPassword}-member`);
      await memberPage.getByRole('button', { name: en['invite.create'] }).click();
      await expect(memberPage.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
      await expect(memberPage.locator('body')).not.toContainText(householdId);

      await memberPage.getByRole('link', { name: en['home.members'], exact: true }).click();
      await expect(memberPage.getByRole('heading', { level: 1, name: en['members.title'] })).toBeVisible();
      const memberRows = memberPage.getByRole('listitem');
      await expect(memberRows.filter({ hasText: ownerLogin })).toContainText(en['members.role.owner']);
      await expect(memberRows.filter({ hasText: 'member_e2e' })).toContainText(en['members.role.member']);
      await expect(memberPage.getByRole('button', { name: en['members.invite.action'] })).toHaveCount(0);
      await expect(memberPage.getByText(en['members.ownerOnly'])).toBeVisible();

      await memberPage.getByRole('button', { name: en['header.signOut'] }).click();
      await expect(memberPage.getByRole('heading', { name: en['login.title'] })).toBeVisible();
      await memberPage.goto(secondLink);
      await memberPage.locator('#invite-login').fill('member_e2e_two');
      await memberPage.locator('#invite-password').fill(`${ownerPassword}-member-two`);
      await memberPage.getByRole('button', { name: en['invite.create'] }).click();
      await expect(memberPage.getByRole('status')).toContainText(en['invite.error.invalid']);
      await expect(memberPage.locator('#invite-login')).toHaveCount(0);
      await memberPage.goto('/');
      await memberPage.locator('#login').fill('member_e2e');
      await memberPage.locator('#password').fill(`${ownerPassword}-member`);
      await memberPage.getByRole('button', { name: en['login.submit'], exact: true }).click();
      await expect(memberPage.getByRole('heading', { level: 1, name: householdName })).toBeVisible();

      const invitationPath = `/api/v1/households/${householdId}/invitations`;
      const forbiddenList = await memberPage.request.get(invitationPath);
      expect(forbiddenList.status()).toBe(403);
      expect((await forbiddenList.json()).code).toBe('owner_required');
      const forbiddenCreate = await memberPage.request.post(invitationPath, { headers: { Origin: baseURL, 'Idempotency-Key': 'e2e-member-try' }, data: {} });
      expect(forbiddenCreate.status()).toBe(403);
      expect((await forbiddenCreate.json()).code).toBe('owner_required');
      const memberSessionResponse = await memberPage.request.get('/api/v1/session');
      expect((await memberSessionResponse.json()).defaultHouseholdId).toBe(householdId);
      expect((await memberPage.request.get(`/api/v1/households/${householdId}/items`)).status()).toBe(200);

      await page.goto('/members');
      await expect(page.getByRole('heading', { level: 1, name: en['members.title'] })).toBeVisible();
      const refreshedInvitations = page.getByRole('heading', { name: en['members.invitations.title'] }).locator('..');
      await expect(refreshedInvitations).toContainText(en['members.invitation.status.accepted']);
      await expect(page.getByRole('listitem').filter({ hasText: 'member_e2e' })).toContainText(en['members.role.member']);

      await memberPage.goto(firstLink);
      await memberPage.getByRole('button', { name: en['invite.join'] }).click();
      const usedTokenNotice = memberPage.getByRole('status');
      await expect(usedTokenNotice).toContainText(en['invite.error.invalid']);
      const expectedMemberFailures = [
        `POST /api/v1/auth/invitations/accept 404`,
        `POST /api/v1/invitations/accept 404`,
      ];
      expect([...memberFailedResponses].sort()).toEqual(expectedMemberFailures.sort());
      expect(memberProblems).toEqual([]);
    } finally {
      await memberContext.close();
    }

    await page.getByRole('button', { name: en['members.invite.action'] }).click();
    const mobileInviteField = page.getByRole('textbox', { name: en['members.invite.link'] });
    await expect(mobileInviteField).toBeVisible();
    await page.setViewportSize({ width: 360, height: 740 });
    try {
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
      expect(overflow, '360px horizontal overflow on members with an invite link').toBeLessThanOrEqual(0);
    } finally {
      await page.setViewportSize({ width: 1280, height: 720 });
    }
  });

  test('n8. an owner assigns a member to an item and can clear the assignment', async () => {
    await page.goto('/');
    const memberResponse = await page.request.get(`/api/v1/households/${householdId}/members`);
    expect(memberResponse.status()).toBe(200);
    const member = ((await memberResponse.json()) as { items: Array<{ userId: string; login: string }> }).items.find((entry) => entry.login === 'member_e2e');
    expect(member).toBeDefined();
    const title = 'Responsible member journey item';
    await page.getByRole('button', { name: en['items.add'], exact: true }).click();
    const form = page.getByRole('form', { name: en['items.add.formLabel'] });
    await form.getByLabel(en['items.title']).fill(title);
    await form.getByLabel(en['items.subject'], { exact: true }).selectOption({ label: 'Anička' });
    await form.getByLabel(en['items.responsible.label']).selectOption(member!.userId);
    await form.getByRole('button', { name: en['items.add.submit'] }).click();
    await expect(page.getByRole('form', { name: en['items.add.formLabel'] })).toHaveCount(0);
    // V2 slice 6b: the home row names who looks after the item and keeps Done secondary to Add item.
    await expect(row(title)).toContainText(en['items.responsible.read'].replace('{name}', member!.login));
    const rowDone = row(title).getByRole('button', { name: en['items.done.label'].replace('{title}', title), exact: true });
    await expect(rowDone).toHaveAttribute('data-variant', 'secondary');
    await expect(page.getByRole('button', { name: en['items.add'], exact: true })).toHaveAttribute('data-variant', 'primary');
    await page.getByRole('link', { name: en['itemDetail.open'].replace('{title}', title) }).click();
    await expect(page.getByText(en['items.responsible.read'].replace('{name}', member!.login))).toBeVisible();
    const created = await serverItem(title);
    expect(created.responsibleUserId).toBe(member!.userId);
    await page.getByRole('button', { name: en['itemDetail.edit.button'] }).click();
    const edit = page.getByRole('form', { name: en['itemDetail.edit.formLabel'] });
    await edit.getByLabel(en['items.responsible.label']).selectOption('');
    await edit.getByRole('button', { name: en['itemDetail.save'] }).click();
    await expect(page.getByText(en['items.responsible.read'].replace('{name}', member!.login))).toHaveCount(0);
    expect((await serverItem(title)).responsibleUserId).toBeNull();
    await page.setViewportSize({ width: 360, height: 740 });
    try {
      expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
    } finally { await page.setViewportSize({ width: 1280, height: 720 }); }
  });

  test('n8b. a person can be linked to a member account, edited, and unlinked', async () => {
    await page.goto('/people');
    await expect(page.getByRole('heading', { level: 1, name: en['subjects.title'] })).toBeVisible();
    const memberResponse = await page.request.get(`/api/v1/households/${householdId}/members`);
    expect(memberResponse.status()).toBe(200);
    const member = ((await memberResponse.json()) as { items: Array<{ userId: string; login: string }> }).items.find((entry) => entry.login === 'member_e2e');
    expect(member).toBeDefined();
    const title = 'Linked member person e2e';
    await page.getByRole('button', { name: en['subjects.add'], exact: true }).click();
    const form = page.getByRole('form', { name: en['subjects.add.formLabel'] });
    await form.getByLabel(en['subjects.name']).fill(title);
    await form.getByRole('combobox', { name: en['subjects.link.label'] }).selectOption(member!.userId);
    await form.getByRole('button', { name: en['subjects.add.submit'] }).click();
    await expect(row(title)).toContainText(en['subjects.link.row'].replace('{login}', member!.login));
    const created = (await serverSubjects(false)).find((entry) => entry.name === title);
    expect(created?.linkedUserId).toBe(member!.userId);
    const subjectPath = `/api/v1/households/${householdId}/subjects/${created!.id}`;
    const savePatch = async (form: ReturnType<typeof page.getByRole>) => {
      const responsePromise = page.waitForResponse((response) => response.request().method() === 'PATCH' && response.url().endsWith(subjectPath));
      await form.getByRole('button', { name: en['subjects.save'] }).click();
      const response = await responsePromise;
      expect(response.status()).toBe(200);
      await expect(form).toHaveCount(0);
      return response.request().postDataJSON() as Record<string, unknown>;
    };

    await page.getByRole('button', { name: en['subjects.edit.label'].replace('{name}', title) }).click();
    const edit = page.getByRole('form', { name: en['subjects.edit.label'].replace('{name}', title) });
    await edit.getByRole('combobox', { name: en['subjects.link.label'] }).selectOption('');
    await page.setViewportSize({ width: 360, height: 740 });
    try {
      await expect(edit.getByRole('combobox', { name: en['subjects.link.label'] })).toBeVisible();
      expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
    } finally { await page.setViewportSize({ width: 1280, height: 720 }); }
    expect(await savePatch(edit)).toEqual({ linkedUserId: null });
    await expect(row(title)).not.toContainText(en['subjects.link.row'].replace('{login}', member!.login));
    expect((await (await page.request.get(subjectPath)).json() as { linkedUserId: string | null }).linkedUserId).toBeNull();

    await page.getByRole('button', { name: en['subjects.edit.label'].replace('{name}', title) }).click();
    const relink = page.getByRole('form', { name: en['subjects.edit.label'].replace('{name}', title) });
    await relink.getByRole('combobox', { name: en['subjects.link.label'] }).selectOption(member!.userId);
    expect(await savePatch(relink)).toEqual({ linkedUserId: member!.userId });
    await expect(row(title)).toContainText(en['subjects.link.row'].replace('{login}', member!.login));
    await page.getByRole('button', { name: en['subjects.edit.label'].replace('{name}', title) }).click();
    const changeType = page.getByRole('form', { name: en['subjects.edit.label'].replace('{name}', title) });
    await changeType.getByRole('radio', { name: en['subjects.type.vehicle'] }).check();
    await expect(changeType.getByText(en['subjects.link.clearedByType'])).toBeVisible();
    expect(await savePatch(changeType)).toEqual({ type: 'vehicle', linkedUserId: null });
    expect(await (await page.request.get(subjectPath)).json()).toMatchObject({ type: 'vehicle', linkedUserId: null });
    await page.setViewportSize({ width: 360, height: 740 });
    try {
      expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
    } finally { await page.setViewportSize({ width: 1280, height: 720 }); }
  });

  test('n9. the owner connects the test identity provider from Account, then signs in with it; refusals are calm', async () => {
    const provider = 'OIDC Smoke Provider';
    await page.goto('/');
    await page.getByRole('link', { name: en['home.account'], exact: true }).click();
    await expect(page).toHaveURL(/\/account$/);
    await expect(page.getByRole('heading', { level: 1, name: en['account.title'] })).toBeVisible();
    const before = await page.request.get('/api/v1/auth/oidc/identity');
    expect(await before.json()).toEqual({ linked: false });
    await page.getByLabel(en['account.oidc.password']).fill(ownerPassword);
    await page.getByRole('button', { name: en['account.oidc.connect'].replace('{provider}', provider) }).click();
    // Real redirect chain: Tendo start -> provider authorize -> Tendo callback -> /account#oidc=connected.
    await expect(page.getByText(en['account.oidc.connected'])).toBeVisible();
    expect(new URL(page.url()).pathname).toBe('/account');
    expect(new URL(page.url()).hash).toBe('');
    await expect(page.getByText(en['account.oidc.linked'].replace('{provider}', provider))).toBeVisible();
    expect(await (await page.request.get('/api/v1/auth/oidc/identity')).json()).toEqual({ linked: true });
    // Linking replaces the session: the earlier cookie is revoked server-side.
    const fresh = await request.newContext({ baseURL });
    try {
      expect((await fresh.get('/api/v1/session', { headers: { Cookie: `tendo_session=${sessionToken}` } })).status()).toBe(401);
    } finally {
      await fresh.dispose();
    }
    sessionToken = (await page.context().cookies()).find((cookie) => cookie.name === 'tendo_session')?.value ?? '';
    expect(sessionToken).not.toBe('');
    await page.setViewportSize({ width: 360, height: 740 });
    try {
      expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
    } finally { await page.setViewportSize({ width: 1280, height: 720 }); }

    await page.getByRole('button', { name: en['header.signOut'] }).click();
    await expect(page.getByRole('heading', { name: en['login.title'] })).toBeVisible();
    const oidcButton = page.getByRole('button', { name: en['login.oidc.submit'].replace('{provider}', provider) });

    // An identity nobody connected is refused without creating anything.
    const issuer = process.env.E2E_OIDC_ISSUER;
    if (!issuer) throw new Error('E2E_OIDC_ISSUER is required; run scripts/e2e-smoke.sh');
    const authorize = `${issuer}/authorize**`;
    await page.route(authorize, (route) => route.continue({ url: `${route.request().url()}&sub=unknown-subject` }));
    await oidcButton.click();
    await expect(page.getByText(en['oidc.error.identity_not_linked'])).toBeVisible();
    expect(new URL(page.url()).pathname).toBe('/login');
    expect(new URL(page.url()).hash).toBe('');
    await page.unroute(authorize);

    // Cancelling at the provider returns to the login screen with a calm notice.
    await page.route(authorize, (route) => route.continue({ url: `${route.request().url()}&deny=1` }));
    await oidcButton.click();
    await expect(page.getByText(en['oidc.error.cancelled'])).toBeVisible();
    await page.unroute(authorize);

    await oidcButton.click();
    await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
    expect(new URL(page.url()).pathname).toBe('/');
    const signedIn = (await (await page.request.get('/api/v1/session')).json()) as { login: string; defaultHouseholdId: string };
    expect(signedIn.login).toBe(ownerLogin);
    expect(signedIn.defaultHouseholdId).toBe(householdId);
    sessionToken = (await page.context().cookies()).find((cookie) => cookie.name === 'tendo_session')?.value ?? '';
    expect(sessionToken).not.toBe('');
  });

  test('n10. foreign household and resource access is indistinguishable from missing data', async () => {
    await page.goto('/');
    const deepLink = await page.goto(`/items/${foreignItemId}`);
    expect(deepLink?.status()).toBe(200);
    await expect(page.getByRole('heading', { level: 1, name: en['itemDetail.gone'], exact: true })).toBeVisible();
    await expect(page.getByText(en['itemDetail.goneBody'], { exact: true })).toBeVisible();
    const browserText = await page.locator('body').innerText();
    expect(browserText).not.toContain('Foreign gutter check');
    expect(browserText).not.toContain('Foreign cottage');

    await page.goto('/');
    const activeItems = (await (await page.request.get(`/api/v1/households/${householdId}/items`)).json()).items as Array<{ title: string }>;
    expect(activeItems.length).toBeGreaterThan(0);
    await expect(page.getByRole('listitem').filter({ hasText: activeItems[0].title }).first()).toBeVisible();
    await expect(page.locator('body')).not.toContainText('Foreign gutter check');
    await expect(page.locator('body')).not.toContainText('Foreign cottage');
    await page.goto('/people');
    await expect(page.getByRole('heading', { level: 1, name: en['subjects.title'] })).toBeVisible();
    await expect(row('Anička')).toBeVisible();
    await expect(page.locator('body')).not.toContainText('Foreign gutter check');
    await expect(page.locator('body')).not.toContainText('Foreign cottage');

    const missingId = '01890000-0000-7000-8000-000000000000';
    const missing = await page.request.get(`/api/v1/households/${missingId}`);
    expect(missing.status()).toBe(404);
    expect(missing.headers()['content-type']).toBe('application/problem+json');
    const baseline = await missing.json() as Record<string, unknown>;
    expect(baseline).toEqual({ type: 'about:blank', title: 'Not Found', status: 404, code: 'not_found' });
    const mutationHeaders = { Origin: baseURL, 'If-Match': '"1"' };
    const attempts = [
      await page.request.get(`/api/v1/households/${foreignHouseholdId}`),
      await page.request.get(`/api/v1/households/${foreignHouseholdId}/subjects`),
      await page.request.get(`/api/v1/households/${foreignHouseholdId}/items`),
      await page.request.get(`/api/v1/households/${foreignHouseholdId}/members`),
      await page.request.get(`/api/v1/households/${foreignHouseholdId}/items/${foreignItemId}`),
      await page.request.get(`/api/v1/households/${foreignHouseholdId}/items/${foreignItemId}/completions`),
      await page.request.get(`/api/v1/households/${householdId}/items/${foreignItemId}`),
      await page.request.get(`/api/v1/households/${householdId}/subjects/${foreignSubjectId}`),
      await page.request.patch(`/api/v1/households/${householdId}/items/${foreignItemId}`, { headers: mutationHeaders, data: { title: 'Hijack' } }),
      await page.request.patch(`/api/v1/households/${foreignHouseholdId}/items/${foreignItemId}`, { headers: mutationHeaders, data: { title: 'Hijack' } }),
      await page.request.post(`/api/v1/households/${foreignHouseholdId}/items/${foreignItemId}/completions`, { headers: { ...mutationHeaders, 'Idempotency-Key': 'e2e-foreign-completion' }, data: {} }),
      await page.request.post(`/api/v1/households/${foreignHouseholdId}/subjects`, { headers: { Origin: baseURL, 'Idempotency-Key': 'e2e-foreign-subject' }, data: { name: 'Hijack subject', type: 'home' } }),
      await page.request.patch(`/api/v1/households/${foreignHouseholdId}/subjects/${foreignSubjectId}`, { headers: mutationHeaders, data: { archived: true } }),
    ];
    for (const response of attempts) {
      expect(response.status(), response.url()).toBe(404);
      expect(response.headers()['etag'], response.url()).toBeUndefined();
      expect(response.headers()['content-type'], response.url()).toBe('application/problem+json');
      expect(await response.json(), response.url()).toEqual(baseline);
    }

    const listPaths = [
      `/api/v1/households/${householdId}/items?archived=false`,
      `/api/v1/households/${householdId}/items?archived=true`,
      `/api/v1/households/${householdId}/items?done=true`,
      `/api/v1/households/${householdId}/subjects?archived=false`,
      `/api/v1/households/${householdId}/subjects?archived=true`,
    ];
    for (const path of listPaths) {
      const response = await page.request.get(path);
      expect(response.status(), path).toBe(200);
      const body = JSON.stringify(await response.json());
      expect(body).not.toContain(foreignItemId);
      expect(body).not.toContain(foreignSubjectId);
    }
  });

  test('n11. home groups use complete server counts and per-group pagination', async () => {
    const seededIds: string[] = [];
    let primaryFailure: unknown;
    let cleanupFailure: unknown;
    const homeRequests: string[] = [];
    const requestListener = (req: import('@playwright/test').Request) => {
      if (req.url().includes(`/api/v1/households/${householdId}/items`)) homeRequests.push(`${req.method()} ${new URL(req.url()).pathname}`);
    };
    try {
      await page.goto('/');
      await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
      const subjectResponse = await page.request.post(`/api/v1/households/${householdId}/subjects`, { headers: { Origin: baseURL, 'Idempotency-Key': 'e2e-n11-subject' }, data: { name: 'Slice six subject', type: 'custom' } });
      expect(subjectResponse.status()).toBe(201);
      const subjectId = (await subjectResponse.json() as { id: string }).id;
      for (let index = 0; index < 120; index++) {
        const response = await page.request.post(`/api/v1/households/${householdId}/items`, { headers: { Origin: baseURL, 'Idempotency-Key': `e2e-n11-future-${index}` }, data: { title: `Slice six future ${index}`, subjectId, attentionOn: '2027-01-01', recurrence: null } });
        expect(response.status()).toBe(201);
        seededIds.push((await response.json() as { id: string }).id);
      }
      for (let index = 0; index < 3; index++) {
        const response = await page.request.post(`/api/v1/households/${householdId}/items`, { headers: { Origin: baseURL, 'Idempotency-Key': `e2e-n11-needs-${index}` }, data: { title: `Slice six needs ${index}`, subjectId, recurrence: null } });
        expect(response.status()).toBe(201);
        seededIds.push((await response.json() as { id: string }).id);
      }
      const summaryResponse = await page.request.get(`/api/v1/households/${householdId}/items/summary`);
      expect(summaryResponse.status()).toBe(200);
      const counts = await summaryResponse.json() as { upcoming: number };
      page.on('request', requestListener);
      await page.goto('/');
      const needs = group(en['items.group.needs']);
      for (let index = 0; index < 3; index++) await expect(needs.getByRole('listitem').filter({ hasText: `Slice six needs ${index}` })).toBeVisible();
      const upcoming = group(en['items.group.upcoming']);
      const heading = upcoming.getByRole('heading', { level: 2 });
      await expect(heading).toContainText(String(counts.upcoming));
      const initialItems = await upcoming.getByRole('listitem').count();
      expect(initialItems).toBe(Math.min(25, counts.upcoming));
      expect(homeRequests.length).toBeLessThanOrEqual(6);
      await page.setViewportSize({ width: 360, height: 740 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)).toBeLessThanOrEqual(0);
      const more = upcoming.locator('[data-group-more="upcoming"]');
      await more.focus();
      const beforePageRequestCount = homeRequests.filter((entry) => entry.startsWith('GET ') && entry.endsWith('/items')).length;
      await page.keyboard.press('Enter');
      await expect(upcoming.getByRole('listitem')).toHaveCount(Math.min(50, counts.upcoming));
      await expect(more).toHaveAttribute('aria-disabled', 'false');
      await expect(more).toBeFocused();
      expect(homeRequests.filter((entry) => entry.startsWith('GET ') && entry.endsWith('/items')).length - beforePageRequestCount).toBe(1);
      if (counts.upcoming > 50) {
        await page.keyboard.press('Enter');
        await expect(upcoming.getByRole('listitem')).toHaveCount(Math.min(75, counts.upcoming));
        await expect(more).toHaveAttribute('aria-disabled', 'false');
        await expect(more).toBeFocused();
      }
      const stableMore = upcoming.locator('[data-group-more="upcoming"]');
      while (await stableMore.count()) {
        const before = await upcoming.getByRole('listitem').count();
        await page.keyboard.press('Enter');
        const expected = Math.min(counts.upcoming, before + 25);
        await expect(upcoming.getByRole('listitem')).toHaveCount(expected);
        if (await stableMore.count()) await expect(stableMore).toHaveAttribute('aria-disabled', 'false');
      }
      await expect.poll(() => page.evaluate(() => (document.activeElement as HTMLElement | null)?.dataset.groupHeading ?? '')).toBe('upcoming');
    } catch (error) { primaryFailure = error; }
    finally {
      page.off('request', requestListener);
      await page.setViewportSize({ width: 1280, height: 720 });
      for (const itemId of seededIds) {
        try {
          const response = await page.request.get(`/api/v1/households/${householdId}/items/${itemId}`);
          if (response.status() !== 200) { cleanupFailure ??= new Error(`Could not read seeded item ${itemId}: ${response.status()}`); continue; }
          const archived = await page.request.patch(`/api/v1/households/${householdId}/items/${itemId}`, { headers: { Origin: baseURL, 'If-Match': response.headers()['etag']! }, data: { archived: true } });
          if (archived.status() !== 200) cleanupFailure ??= new Error(`Could not archive seeded item ${itemId}: ${archived.status()}`);
        } catch (error) { cleanupFailure ??= error; }
      }
    }
    if (primaryFailure) throw primaryFailure;
    if (cleanupFailure) throw cleanupFailure;
  });

  test('n12. all items finds and manages items among 1000+ seeded items with bounded pages', async () => {
    test.setTimeout(240_000);
    type SeedItem = { id: string; title: string; recurrence: null | { intervalUnit: 'day' | 'week' | 'month' | 'year'; intervalValue: number; mode: 'fixed' | 'after_completion' }; attentionOn?: string; subjectId: string };
    type ListBody = { items: Array<{ id: string; title: string }>; nextCursor: string | null };
    type Summary = { evaluatedOn: string; total: number; needsAttention: number; inProgress: number; waiting: number; upcoming: number; paused: number; done: number; archived: number };
    const seeded: SeedItem[] = [];
    let primaryFailure: unknown;
    let cleanupFailure: unknown;
    const requests: string[] = [];
    const onRequest = (req: import('@playwright/test').Request) => {
      const path = new URL(req.url()).pathname;
      if (path === `/api/v1/households/${householdId}/items` || path === `/api/v1/households/${householdId}/items/summary`) requests.push(`${req.method()} ${req.url()}`);
    };
    const apiWalk = async (query: Record<string, string> = {}) => {
      const results: Array<{ id: string; title: string }> = [];
      let cursor: string | undefined;
      do {
        const params = new URLSearchParams({ limit: '100', ...query, ...(cursor ? { cursor } : {}) });
        const response = await page.request.get(`/api/v1/households/${householdId}/items?${params}`);
        expect(response.status()).toBe(200);
        const body = await response.json() as ListBody;
        results.push(...body.items);
        cursor = body.nextCursor ?? undefined;
      } while (cursor);
      return results;
    };
    const getSummary = async (query = '') => {
      const response = await page.request.get(`/api/v1/households/${householdId}/items/summary${query ? `?${query}` : ''}`);
      expect(response.status()).toBe(200);
      return await response.json() as Summary;
    };
    const updateItem = async (id: string, body: Record<string, unknown>) => {
      const current = await page.request.get(`/api/v1/households/${householdId}/items/${id}`);
      expect(current.status()).toBe(200);
      const response = await page.request.patch(`/api/v1/households/${householdId}/items/${id}`, {
        headers: { Origin: baseURL, 'If-Match': current.headers()['etag']! }, data: body,
      });
      expect(response.status()).toBe(200);
      return response;
    };
    try {
      await page.setViewportSize({ width: 1280, height: 800 });
      await page.goto('/');
      await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
      const subjectsResponse = await page.request.get(`/api/v1/households/${householdId}/subjects?archived=false`);
      expect(subjectsResponse.status()).toBe(200);
      const subjectEntries = (await subjectsResponse.json() as { items: Array<{ id: string; name: string }> }).items;
      const subjects = subjectEntries.slice(0, 2);
      expect(subjects.length).toBe(2);
      const labelSubjectResponse = await page.request.post(`/api/v1/households/${householdId}/subjects`, { headers: { Origin: baseURL, 'Idempotency-Key': 'e2e-n12-home-archive-subject' }, data: { name: 'N12 archived home subject', type: 'home' } });
      expect(labelSubjectResponse.status()).toBe(201);
      const archivedHomeSubject = await labelSubjectResponse.json() as { id: string };
      const sessionResponse = await page.request.get('/api/v1/session');
      const ownerId = (await sessionResponse.json() as { userId: string }).userId;

      const seedStarted = Date.now();
      const batches = Array.from({ length: 42 }, (_, batch) => Array.from({ length: 25 }, (_, offset) => batch * 25 + offset));
      for (const batch of batches) {
        const responses = await Promise.all(batch.map(async (index) => {
          const repeated = index % 3 !== 0;
          const recurrence = repeated ? { intervalUnit: (['day', 'week', 'month', 'year'] as const)[index % 4], intervalValue: (index % 4) + 1, mode: (index % 2 ? 'fixed' : 'after_completion') as 'fixed' | 'after_completion' } : null;
          const payload = {
            title: index % 131 === 0 ? `Výměna filtru Škoda ${String(index).padStart(4, '0')}` : index === 17 ? `LongUnbreakableHouseholdItemTitle${'x'.repeat(150)}` : `All items seed ${String(index).padStart(4, '0')}`,
            subjectId: index % 100 === 0 ? archivedHomeSubject.id : subjects[index % 2]!.id,
            ...(index % 5 === 0 ? { responsibleUserId: ownerId } : {}),
            ...(index % 4 === 0 ? { attentionOn: '2025-01-10' } : index % 4 === 1 ? { attentionOn: '2027-01-10' } : {}),
            recurrence,
          };
          const response = await page.request.post(`/api/v1/households/${householdId}/items`, {
            headers: { Origin: baseURL, 'Idempotency-Key': `e2e-n12-create-${index}` }, data: payload,
          });
          expect(response.status(), `seed item ${index}`).toBe(201);
          return { ...(await response.json() as SeedItem), recurrence, subjectId: payload.subjectId };
        }));
        seeded.push(...responses);
      }
      const seedDuration = Date.now() - seedStarted;
      console.log(`n12 seeded ${seeded.length} items in ${seedDuration}ms`);
      for (let i = 0; i < 12; i++) await updateItem(seeded[100 + i]!.id, { workflowState: i % 2 ? 'paused' : 'in_progress' });
      for (let i = 0; i < 10; i++) await updateItem(seeded[200 + i]!.id, { archived: true });
      const archiveSubjectCurrent = await page.request.get(`/api/v1/households/${householdId}/subjects/${archivedHomeSubject.id}`);
      expect(archiveSubjectCurrent.status()).toBe(200);
      const archiveSubjectResponse = await page.request.patch(`/api/v1/households/${householdId}/subjects/${archivedHomeSubject.id}`, { headers: { Origin: baseURL, 'If-Match': archiveSubjectCurrent.headers()['etag']! }, data: { archived: true } });
      expect(archiveSubjectResponse.status()).toBe(200);
      const oneOffEntries = seeded.filter((item) => item.recurrence === null && !item.title.startsWith('Výměna')).slice(0, 10);
      for (let i = 0; i < oneOffEntries.length; i++) {
        const entry = oneOffEntries[i]!;
        expect(entry.recurrence).toBeNull();
        const current = await page.request.get(`/api/v1/households/${householdId}/items/${entry.id}`);
        expect(current.status()).toBe(200);
        const completed = await page.request.post(`/api/v1/households/${householdId}/items/${entry.id}/completions`, {
          headers: { Origin: baseURL, 'If-Match': current.headers()['etag']!, 'Idempotency-Key': `e2e-n12-done-${i}` }, data: {},
        });
        expect(completed.status()).toBe(201);
      }

      page.on('request', onRequest);
      await page.goto('/');
      await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
      const beforeNavigation = requests.length;
      await page.getByRole('link', { name: en['home.allItems'], exact: true }).click();
      await expect(page.getByRole('heading', { level: 1, name: en['allItems.title'] })).toBeVisible();
      const summaryResponse = await page.request.get(`/api/v1/households/${householdId}/items/summary`);
      const summary = await summaryResponse.json() as Summary;
      const activeCount = summary.needsAttention + summary.inProgress + summary.waiting + summary.upcoming + summary.paused;
      const statusSelect = page.getByLabel(en['allItems.status.label']);
      await expect(statusSelect.locator('option').filter({ hasText: `Active (${activeCount})` })).toHaveCount(1);
      await expect(page.getByRole('heading', { level: 2, name: `Showing 1–25 of ${activeCount}` })).toBeVisible();
      const openingCalls = requests.slice(beforeNavigation).filter((entry) => entry.startsWith('GET '));
      expect(openingCalls.filter((entry) => entry.includes('/items/summary')).length).toBe(1);
      expect(openingCalls.filter((entry) => new URL(entry.replace(/^GET /, '')).pathname === `/api/v1/households/${householdId}/items`).length).toBe(1);
      expect(openingCalls.length).toBeLessThanOrEqual(6); // summary + first items page + subjects (active/archive) + members, with a small pagination allowance

      const searchInput = page.getByRole('searchbox');
      await searchInput.fill('vymena filtru skoda');
      await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
      const accented = seeded.filter((item) => item.title.startsWith('Výměna filtru Škoda'));
      await expect(page.getByRole('main').getByRole('listitem')).toHaveCount(accented.length);
      await expect(page).toHaveURL(/q=vymena\+filtru\+skoda/);
      await expect(page.getByRole('heading', { level: 2, name: `Showing 1–${accented.length} of ${accented.length}` })).toBeVisible();

      await searchInput.fill('All items seed');
      await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
      await statusSelect.selectOption('done');
      await expect(page.getByRole('main').getByRole('listitem')).toHaveCount(10);
      await statusSelect.selectOption('archived');
      await expect(page.getByRole('main').getByRole('listitem')).toHaveCount(10);
      await statusSelect.selectOption('active');
      await searchInput.fill('');
      await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();

      const subjectSelect = page.getByLabel(en['items.subject']);
      await subjectSelect.selectOption(subjects[0]!.id);
      await page.getByLabel(en['allItems.repeat']).selectOption('false');
      const filterQuery = `subjectId=${encodeURIComponent(subjects[0]!.id)}&recurring=false`;
      const filteredSummary = await getSummary(filterQuery);
      const expectedWalk = await apiWalk({ subjectId: subjects[0]!.id, recurring: 'false', archived: 'false', done: 'false', sort: 'attention_on', attentionDate: (await getSummary(filterQuery)).evaluatedOn });
      const filterTotal = filteredSummary.needsAttention + filteredSummary.inProgress + filteredSummary.waiting + filteredSummary.upcoming + filteredSummary.paused;
      expect(expectedWalk.length).toBe(filterTotal);
      await expect(page.getByRole('main').getByRole('listitem')).toHaveCount(Math.min(25, filterTotal));
      const walkIds: string[] = [];
      const pageCount = Math.ceil(filterTotal / 25);
      for (let i = 0; i < pageCount; i++) {
        const visibleIds = await page.locator('[data-item-id]').evaluateAll((nodes) => nodes.map((node) => node.getAttribute('data-item-id')!));
        expect(visibleIds.length).toBeLessThanOrEqual(25);
        expect(new Set(visibleIds).size).toBe(visibleIds.length);
        walkIds.push(...visibleIds);
        if (i < pageCount - 1) {
          const before = requests.length;
          await page.getByRole('button', { name: en['allItems.next'] }).click();
          await expect(page.locator('[data-item-id]')).toHaveCount(Math.min(25, filterTotal - (i + 1) * 25));
          await expect.poll(() => requests.length - before).toBe(2); // one summary freshness/count request, then one list-page request
        }
      }
      expect(walkIds.length).toBe(expectedWalk.length);
      expect(new Set(walkIds).size).toBe(walkIds.length);
      expect([...walkIds].sort()).toEqual(expectedWalk.map((item) => item.id).sort());
      const domElements = await page.locator('*').count();
      console.log(`n12 filtered list DOM element count=${domElements}`);
      expect(domElements).toBeLessThan(1500); // measured bound allows headroom above the 25-row results and filter controls

      // Search context can be paged independently. Open a page-three row and verify exact return restoration.
      await page.getByLabel(en['allItems.repeat']).selectOption('');
      await subjectSelect.selectOption('');
      await searchInput.fill('All items seed');
      await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
      const searchSummary = await getSummary('q=All+items+seed');
      const searchTotal = searchSummary.needsAttention + searchSummary.inProgress + searchSummary.waiting + searchSummary.upcoming + searchSummary.paused;
      await expect(page.getByRole('heading', { level: 2, name: `Showing 1–25 of ${searchTotal}` })).toBeVisible();
      expect(searchTotal).toBeGreaterThan(50);
      await expect.poll(() => page.getByRole('button', { name: en['allItems.next'] }).count()).toBe(1);
      await page.getByRole('button', { name: en['allItems.next'] }).click();
      await expect(page.getByRole('heading', { level: 2, name: /Showing 26–50 of/ })).toBeVisible();
      await page.getByRole('button', { name: en['allItems.next'] }).click();
      await expect(page.getByRole('heading', { level: 2, name: /Showing 51–75 of/ })).toBeVisible();
      const beforeDetailUrl = new URL(page.url());
      const beforeDetailRows = await page.locator('[data-item-id]').evaluateAll((nodes) => nodes.map((node) => node.getAttribute('data-item-id')));
      await page.locator('[data-item-id]').first().getByRole('link').click();
      await page.getByRole('link', { name: en['itemDetail.backAllItems'], exact: true }).click();
      await expect(page).toHaveURL(beforeDetailUrl.toString());
      await expect.poll(() => page.locator('[data-item-id]').evaluateAll((nodes) => nodes.map((node) => node.getAttribute('data-item-id')))).toEqual(beforeDetailRows);

      // Archive one uniquely searched result through its detail screen, then verify the result/count update.
      await searchInput.fill('vymena filtru skoda 0000');
      await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
      await expect(page.getByRole('main').getByRole('listitem')).toHaveCount(1);
      const archivedSearchItem = page.getByRole('link', { name: 'Výměna filtru Škoda 0000' });
      await archivedSearchItem.click();
      await page.getByRole('button', { name: en['subjects.archive'], exact: true }).click();
      await page.getByRole('link', { name: en['itemDetail.backAllItems'], exact: true }).click();
      await expect(page.getByText(en['allItems.noMatch'])).toBeVisible();
      await statusSelect.selectOption('archived');
      await expect(page.getByRole('main').getByRole('listitem')).toHaveCount(1);

      for (const width of [360, 768]) {
        await page.setViewportSize({ width, height: 800 });
        await page.goto('/items?q=LongUnbreakableHouseholdItemTitle');
        await expect(page.getByText(/LongUnbreakableHouseholdItemTitle/)).toBeVisible();
        expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth), `${width}px All items overflow`).toBeLessThanOrEqual(0);
      }
      await page.getByRole('button', { name: 'Čeština' }).click();
      await expect(page.getByRole('heading', { level: 1, name: cs['allItems.title'] })).toBeVisible();
      await expect(page.getByLabel(cs['allItems.status.label']).getByRole('option', { name: new RegExp(cs['allItems.status.active']) })).toHaveCount(1);
      await page.getByRole('button', { name: 'English' }).click();
      await expect(page.getByRole('heading', { level: 1, name: en['allItems.title'] })).toBeVisible();
    } catch (error) { primaryFailure = error; }
    finally {
      page.off('request', onRequest);
      await page.setViewportSize({ width: 1280, height: 800 });
      const batches = Array.from({ length: Math.ceil(seeded.length / 25) }, (_, batch) => seeded.slice(batch * 25, batch * 25 + 25));
      for (const batch of batches) {
        try {
          await Promise.all(batch.map(async (item) => {
            const current = await page.request.get(`/api/v1/households/${householdId}/items/${item.id}`);
            if (current.status() !== 200) { cleanupFailure ??= new Error(`Could not read seeded item ${item.id}: ${current.status()}`); return; }
            const body = await current.json() as { archived: boolean };
            if (body.archived) return;
            const archived = await page.request.patch(`/api/v1/households/${householdId}/items/${item.id}`, {
              headers: { Origin: baseURL, 'If-Match': current.headers()['etag']! }, data: { archived: true },
            });
            if (archived.status() !== 200) cleanupFailure ??= new Error(`Could not archive seeded item ${item.id}: ${archived.status()}`);
          }));
        } catch (error) { cleanupFailure ??= error; }
      }
    }
    if (primaryFailure) throw primaryFailure;
    if (cleanupFailure) throw cleanupFailure;
  });

  test('n13. home batch-loads labels only for rendered subjects, including archived references', async () => {
    type HomeItem = { id: string; title: string; subjectId: string };
    const itemIDs: string[] = [];
    let archivedSubjectId = '';
    let extraSubjectId = '';
    let primaryFailure: unknown;
    let cleanupFailure: unknown;
    const subjectRequests: URL[] = [];
    const onSubjectRequest = (req: import('@playwright/test').Request) => {
      const url = new URL(req.url());
      if (req.method() === 'GET' && url.pathname === `/api/v1/households/${householdId}/subjects`) subjectRequests.push(url);
    };
    try {
      await page.goto('/');
      await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
      const activeResponse = await page.request.get(`/api/v1/households/${householdId}/subjects?archived=false`);
      expect(activeResponse.status()).toBe(200);
      const activeSubjects = (await activeResponse.json() as { items: Array<{ id: string; name: string }> }).items;
      const subjects = activeSubjects.slice(0, 3);
      expect(subjects).toHaveLength(3);
      const extraSubjectResponse = await page.request.post(`/api/v1/households/${householdId}/subjects`, { headers: { Origin: baseURL, 'Idempotency-Key': 'e2e-n13-extra-subject' }, data: { name: 'N13 extra active subject', type: 'vehicle' } });
      expect(extraSubjectResponse.status()).toBe(201);
      const extraSubject = await extraSubjectResponse.json() as { id: string };
      extraSubjectId = extraSubject.id;
      const archiveResponse = await page.request.post(`/api/v1/households/${householdId}/subjects`, { headers: { Origin: baseURL, 'Idempotency-Key': 'e2e-n13-archived-subject' }, data: { name: 'N13 archived reference', type: 'home' } });
      expect(archiveResponse.status()).toBe(201);
      const archivedSubject = await archiveResponse.json() as { id: string; name: string };
      archivedSubjectId = archivedSubject.id;

      const sessionResponse = await page.request.get('/api/v1/session');
      expect(sessionResponse.status()).toBe(200);
      const ownerId = (await sessionResponse.json() as { userId: string }).userId;
      const visibleTitles: string[] = [];
      for (let index = 0; index < 30; index++) {
        const subjectId = index === 0 ? archivedSubject.id : index === 25 ? extraSubject.id : subjects[index % subjects.length]!.id;
        const title = `N13 rendered ${String(index).padStart(2, '0')}`;
        const created = await page.request.post(`/api/v1/households/${householdId}/items`, { headers: { Origin: baseURL, 'Idempotency-Key': `e2e-n13-create-${index}` }, data: { title, subjectId, ...(index % 9 === 0 ? { responsibleUserId: ownerId } : {}) } });
        expect(created.status()).toBe(201);
        const createdItem = await created.json() as HomeItem;
        itemIDs.push(createdItem.id);
        visibleTitles.push(title);
        const currentItem = await page.request.get(`/api/v1/households/${householdId}/items/${createdItem.id}`);
        expect(currentItem.status()).toBe(200);
        const moved = await page.request.patch(`/api/v1/households/${householdId}/items/${createdItem.id}`, { headers: { Origin: baseURL, 'If-Match': currentItem.headers()['etag']! }, data: { workflowState: 'in_progress' } });
        expect(moved.status()).toBe(200);
      }
      const archiveSubjectCurrent = await page.request.get(`/api/v1/households/${householdId}/subjects/${archivedSubject.id}`);
      expect(archiveSubjectCurrent.status()).toBe(200);
      const archived = await page.request.patch(`/api/v1/households/${householdId}/subjects/${archivedSubject.id}`, { headers: { Origin: baseURL, 'If-Match': archiveSubjectCurrent.headers()['etag']! }, data: { archived: true } });
      expect(archived.status()).toBe(200);

      page.on('request', onSubjectRequest);
      await page.goto('/');
      await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
      const main = page.getByRole('main');
      await expect(main.getByRole('listitem').filter({ hasText: visibleTitles[0]! })).toBeVisible();
      const renderedSubjectIDs = await main.locator('[data-item-id]').evaluateAll((nodes) => nodes.map((node) => node.getAttribute('data-item-id')!));
      const renderedItems = await Promise.all(renderedSubjectIDs.map(async (id) => {
        const response = await page.request.get(`/api/v1/households/${householdId}/items/${id}`);
        expect(response.status()).toBe(200);
        return await response.json() as HomeItem;
      }));
      const initialUniqueSubjects = new Set(renderedItems.map((item) => item.subjectId));
      expect(initialUniqueSubjects.size).toBe(4);
      await expect(main.getByText(archivedSubject.name, { exact: true })).toBeVisible();
      await expect.poll(() => subjectRequests.length).toBe(2 * Math.ceil(initialUniqueSubjects.size / 100));
      expect(subjectRequests.every((url) => url.searchParams.has('id'))).toBe(true);
      expect(subjectRequests.filter((url) => url.searchParams.get('archived') === 'true')).toHaveLength(Math.ceil(initialUniqueSubjects.size / 100));
      expect(subjectRequests.filter((url) => url.searchParams.get('archived') === 'false')).toHaveLength(Math.ceil(initialUniqueSubjects.size / 100));
      for (const url of subjectRequests) for (const id of url.searchParams.getAll('id')) expect(initialUniqueSubjects.has(id)).toBe(true);

      const beforeMore = subjectRequests.length;
      const more = main.locator('[data-group-more="inProgress"]');
      if (await more.count()) {
        await more.click();
        await expect(main.getByRole('listitem').filter({ hasText: visibleTitles[25]! })).toBeVisible();
      }
      const allRenderedIDs = await main.locator('[data-item-id]').evaluateAll((nodes) => nodes.map((node) => node.getAttribute('data-item-id')!));
      const allRenderedItems = await Promise.all(allRenderedIDs.map(async (id) => {
        const response = await page.request.get(`/api/v1/households/${householdId}/items/${id}`);
        expect(response.status()).toBe(200);
        return await response.json() as HomeItem;
      }));
      const newlySeen = [...new Set(allRenderedItems.map((item) => item.subjectId))].filter((id) => !initialUniqueSubjects.has(id));
      await expect.poll(() => subjectRequests.length).toBe(beforeMore + (newlySeen.length ? 2 : 0));
      for (const url of subjectRequests.slice(beforeMore)) {
        expect(url.searchParams.getAll('id').sort()).toEqual(newlySeen.sort());
        expect(url.searchParams.get('archived')).toMatch(/^(true|false)$/);
      }

      const activeCatalog = await page.request.get(`/api/v1/households/${householdId}/subjects?archived=false`);
      expect(activeCatalog.status()).toBe(200);
      const expectedActiveCount = (await activeCatalog.json() as { items: Array<{ id: string }> }).items.length;
      const beforeAdd = subjectRequests.length;
      await main.getByRole('button', { name: en['items.add'], exact: true }).click();
      await expect(page.getByRole('form', { name: en['items.add.formLabel'] })).toBeVisible();
      await expect.poll(() => subjectRequests.length).toBe(beforeAdd + Math.ceil(expectedActiveCount / 100));
      const activeCatalogueRequests = subjectRequests.slice(beforeAdd);
      expect(activeCatalogueRequests).toHaveLength(Math.ceil(expectedActiveCount / 100));
      expect(activeCatalogueRequests.every((url) => !url.searchParams.has('id') && url.searchParams.get('archived') === 'false')).toBe(true);
      expect(subjectRequests.filter((url) => !url.searchParams.has('id') && url.searchParams.get('archived') === 'true')).toHaveLength(0);
      await page.getByRole('button', { name: en['items.cancel'], exact: true }).click();
    } catch (error) { primaryFailure = error; }
    finally {
      page.off('request', onSubjectRequest);
      for (const subjectId of [archivedSubjectId, extraSubjectId].filter(Boolean)) {
        try {
          const current = await page.request.get(`/api/v1/households/${householdId}/subjects/${subjectId}`);
          if (current.status() === 200 && (await current.json() as { archived: boolean }).archived) {
            const restored = await page.request.patch(`/api/v1/households/${householdId}/subjects/${subjectId}`, { headers: { Origin: baseURL, 'If-Match': current.headers()['etag']! }, data: { archived: false } });
            if (restored.status() !== 200) cleanupFailure ??= new Error(`Could not restore n13 subject ${subjectId}: ${restored.status()}`);
          }
        } catch (error) { cleanupFailure ??= error; }
      }
      for (const itemId of itemIDs) {
        try {
          const current = await page.request.get(`/api/v1/households/${householdId}/items/${itemId}`);
          if (current.status() !== 200) { cleanupFailure ??= new Error(`Could not read n13 item ${itemId}: ${current.status()}`); continue; }
          const data = await current.json() as { archived: boolean };
          if (data.archived) continue;
          const archived = await page.request.patch(`/api/v1/households/${householdId}/items/${itemId}`, { headers: { Origin: baseURL, 'If-Match': current.headers()['etag']! }, data: { archived: true } });
          if (archived.status() !== 200) cleanupFailure ??= new Error(`Could not archive n13 item ${itemId}: ${archived.status()}`);
        } catch (error) { cleanupFailure ??= error; }
      }
    }
    if (primaryFailure) throw primaryFailure;
    if (cleanupFailure) throw cleanupFailure;
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
    await expect(page.getByRole('button', { name: en['login.submit'], exact: true })).toBeFocused();
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
    const foreignDetailNotFound = `GET /api/v1/households/${householdId}/items/${foreignItemId} 404`;
    expect([...failedResponses].sort()).toEqual([...allowedFailedResponses, subjectConflict, foreignDetailNotFound].sort());
    expect(problems).toEqual([]);
  });
});
