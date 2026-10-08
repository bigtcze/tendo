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
const expectedCompletionDate = new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Prague' }).format(new Date(e2eClockNow));
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

  function group(name: string) {
    return page.locator('section').filter({ has: page.getByRole('heading', { level: 2, name, exact: true }) });
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
    await page.setViewportSize({ width: 360, height: 740 });
    try {
      await page.getByRole('button', { name: en['items.add'], exact: true }).click();
      await expect(page.getByRole('form', { name: en['items.add.formLabel'] })).toBeVisible();
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
      expect(overflow, '360px horizontal overflow with the item form open').toBeLessThanOrEqual(0);
      await page.getByRole('button', { name: en['subjects.cancel'] }).click();
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
      await page.getByRole('link', { name: en['subjects.backHome'] }).click();
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

    for (const viewport of [{ width: 360, height: 740 }, { width: 1280, height: 800 }]) {
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

    await page.getByRole('link', { name: en['subjects.backHome'] }).click();
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

    await page.getByRole('link', { name: en['subjects.backHome'] }).click();
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
    await page.getByRole('link', { name: en['home.members'] }).click();
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

      await memberPage.getByRole('link', { name: en['home.members'] }).click();
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
      await memberPage.getByRole('button', { name: en['login.submit'] }).click();
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
