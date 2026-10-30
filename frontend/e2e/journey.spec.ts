import { chromium, expect, request, test, type BrowserContext, type ConsoleMessage, type Locator, type Page } from '@playwright/test';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { cs } from '../src/i18n/cs';
import { en } from '../src/i18n/en';
import { expectNoAxeViolations } from './a11y';
import { measureMotion } from './motion';
import { colorString, composite, contrastRatio, parseColor, passesContrastThreshold, type Color } from './contrast';
import { auditContrast, contrastShortfalls, type ContrastAuditRow } from './contrastAudit';

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
const existingAccountPassword = required('E2E_EXISTING_PASSWORD');
const conflictHouseholdId = required('E2E_CONFLICT_HOUSEHOLD_ID');
const largeHouseholdId = required('E2E_LARGE_HOUSEHOLD_ID');
const largeOwnerLogin = required('E2E_LARGE_OWNER_LOGIN');
const expectedCompletionDate = new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Prague' }).format(new Date(e2eClockNow));
const ownerLogin = 'owner_e2e';
const householdName = 'Veselí';

const problems: string[] = [];
const problem503 = { type: 'about:blank', title: 'Unavailable', status: 503, code: 'unavailable' };
const failedResponses = new Set<string>();
let n17OfflineActive = false;
let n17FailurePhaseActive = false;
let n19PhaseActive = false;
let n20OidcFailureActive = false;
type InjectedFailure = { key: string; url: string };
type FailureWindow = { injected: InjectedFailure[]; declared: string[]; observed: string[] };
const failureWindows = new WeakMap<Page, FailureWindow>();
const n19FailedResponses: string[] = [];
const n20FailedResponses: string[] = [];
const n17OfflineExpectedResources: RegExp[] = [];
const n17ExpectedFailurePaths = new Set<string>();
// Browser-initiated expected failures: anonymous session/setup/login attempts, deliberate stale save,
// and the foreign detail deep-link lookup. page.request calls are not reported.
const allowedFailedResponses = ['GET /api/v1/session 401', 'PUT /api/v1/auth/setup 401', 'POST /api/v1/session 401'];
const expectedFailedPaths = new Set(['/api/v1/session', '/api/v1/auth/setup']);
const expectedForeignDetailPath = new RegExp(`^/api/v1/households/[0-9a-f-]{36}/items/${foreignItemId}$`);
let expectedSessionEndedPaths: Array<(path: string) => boolean> = [];
const wrongSetupCode = 'A'.repeat(43) + '=';

// Counts the current document's in-flight API fetches. The counter lives in the page, so a full navigation,
// which abandons the previous document's fetches without a Playwright requestfinished/requestfailed event,
// starts again from zero. The app resolves globalThis.fetch at call time (src/lib/api.ts).
const trackedPages = new WeakSet<Page>();

async function trackInFlightApiFetches(page: Page) {
  if (trackedPages.has(page)) return;
  trackedPages.add(page);
  await page.addInitScript(() => {
    const state = window as unknown as { __tendoInFlightApi: number };
    state.__tendoInFlightApi = 0;
    const originalFetch = window.fetch.bind(window);
    window.fetch = async (...args: Parameters<typeof fetch>) => {
      const input = args[0];
      const url = new URL(input instanceof Request ? input.url : String(input), window.location.href);
      if (!url.pathname.startsWith('/api/')) return originalFetch(...args);
      state.__tendoInFlightApi += 1;
      let response: Response;
      try {
        response = await originalFetch(...args);
      } catch (error) {
        state.__tendoInFlightApi -= 1;
        throw error;
      }
      // fetch resolves at response headers; stay in flight until a clone's body has fully arrived so the
      // caller's body read (and any request it chains) is not mistaken for idleness. The caller's body is untouched.
      void response.clone().arrayBuffer().catch(() => undefined).finally(() => { state.__tendoInFlightApi -= 1; });
      return response;
    };
  });
}

// fullChromium: pages of the full Chromium build (n23) also fetch /favicon.ico on their own; Tendo ships
// no favicon, so exactly that browser-initiated 404 is expected there and nowhere else.
function watch(page: Page, options: { fullChromium?: boolean } = {}) {
  page.on('console', (message: ConsoleMessage) => {
    if (message.type() !== 'error') return;
    const path = new URL(message.location().url || 'about:blank').pathname;
    if (options.fullChromium && path === '/favicon.ico' && message.text().includes('status of 404')) return;
    if (n17OfflineActive && message.text().includes('Failed to load resource') && message.text().includes('ERR_INTERNET_DISCONNECTED') && n17OfflineExpectedResources.some((expected) => expected.test(message.location().url))) return;
    const failureWindow = failureWindows.get(page);
    if (failureWindow && message.text().includes('Failed to load resource')) {
      if (failureWindow.injected.some((failure) => failure.url === message.location().url)) return;
      const match = message.text().match(/status of (\d{3})/);
      if (match && failureWindow.declared.some((entry) => entry.endsWith(` ${path} ${match[1]}`))) return;
    }
    if (n17FailurePhaseActive && n17ExpectedFailurePaths.has(path) && message.text().includes('Failed to load resource')) return;
    // The response check below asserts the exact expected HTTP failures.
    if (expectedFailedPaths.has(path)) return;
    if (expectedForeignDetailPath.test(path) && /status of 404/.test(message.text())) return;
    if (n19PhaseActive && expectedSessionEndedPaths.some((expected) => expected(path)) && message.text().includes('status of 401')) return;
    if (n20OidcFailureActive && path === '/api/v1/auth/oidc' && message.text().includes('status of 503')) return;
    problems.push(`console: ${message.text()}`);
  });
  page.on('pageerror', (error) => problems.push(`pageerror: ${error.message}`));
  page.on('requestfailed', (request) => {
    const window = failureWindows.get(page);
    const path = new URL(request.url()).pathname;
    if (window && path.startsWith('/api/') && request.failure()?.errorText !== 'net::ERR_ABORTED') {
      window.observed.push(`${request.method()} ${path} abort`);
    }
  });
  page.on('response', (response) => {
    if (response.status() >= 400) {
      const failure = `${response.request().method()} ${new URL(response.url()).pathname} ${response.status()}`;
      const window = failureWindows.get(page);
      if (window) { window.observed.push(failure); return; }
      if (n19PhaseActive) n19FailedResponses.push(failure);
      else if (n20OidcFailureActive && failure === 'GET /api/v1/auth/oidc 503') n20FailedResponses.push(failure);
      else failedResponses.add(failure);
    }
  });
}

async function waitForSettledHouseholdRequests(page: Page) {
  // Waits until the app has no API fetch in flight for three consecutive polls; networkidle misses later SPA reads.
  if (!trackedPages.has(page)) throw new Error('trackInFlightApiFetches(page) must run before waiting for API reads');
  let consecutiveIdlePolls = 0;
  await expect.poll(async () => {
    const inFlight = await page.evaluate(() => (window as unknown as { __tendoInFlightApi?: number }).__tendoInFlightApi);
    if (inFlight === undefined) throw new Error('in-flight API counter missing: trackInFlightApiFetches(page) must run before the first navigation');
    consecutiveIdlePolls = inFlight === 0 ? consecutiveIdlePolls + 1 : 0;
    return consecutiveIdlePolls;
  }).toBe(3);
}

// Home's loading statuses in both locales: the item list, the add-form catalogue and the deferred subject labels.
const escapeRegExp = (text: string) => text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
const homeLoadingTexts = [...new Set([en, cs].flatMap((messages) => [messages['items.loading'], messages['items.subjects.loading'], messages['items.subjectLabels.loading']]))];
const homeLoadingStatus = new RegExp(`^(?:${homeLoadingTexts.map(escapeRegExp).join('|')})$`);

async function waitForSettledHome(page: Page, name: string) {
  await expect(page.getByRole('heading', { level: 1, name })).toBeVisible();
  // Only loading statuses mean unsettled; a completion notice from an earlier step is legitimate settled state.
  // Home chains reads (summary, then groups, then subject labels), so settle the network after the UI is ready
  // and confirm no later stage started loading meanwhile.
  const loading = page.getByRole('main').getByRole('status').filter({ hasText: homeLoadingStatus });
  await expect(loading).toHaveCount(0);
  await waitForSettledHouseholdRequests(page);
  await expect(loading).toHaveCount(0);
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
    const delayValue = process.env.TENDO_E2E_READ_DELAY_MS;
    if (delayValue !== undefined) {
      if (!/^[1-9]\d*$/.test(delayValue)) throw new Error('TENDO_E2E_READ_DELAY_MS must be a positive integer');
      const delayMs = Number(delayValue);
      if (!Number.isSafeInteger(delayMs)) throw new Error('TENDO_E2E_READ_DELAY_MS must be a positive integer');
      await context.route('**/api/v1/households/**', async (route) => {
        if (route.request().method() === 'GET') await new Promise((resolve) => setTimeout(resolve, delayMs));
        await route.continue();
      });
    }
    page = await context.newPage();
    await trackInFlightApiFetches(page);
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
    await expect(page.locator('#onboarding-timezone')).toHaveValue('America/New_York');
    await expectNoAxeViolations(page, 'onboarding-error');
    const contrastRows = await auditContrast(page, 'onboarding-error');
    expect(contrastRows.some((row) => (row.kind === 'text' || row.kind === 'placeholder') && !row.exempt && row.ratio !== null), 'onboarding-error has measured non-exempt text').toBe(true);
    expect(contrastRows.some((row) => row.kind === 'boundary' && row.ratio !== null), 'onboarding-error has a measured control boundary').toBe(true);
    // describe() names elements tag#id plus up to two classes, so match the id prefix and the [value] suffix.
    const measuredValue = (prefix: string) => contrastRows.some((row) => row.kind === 'text' && !row.exempt && row.ratio !== null && row.name.startsWith(prefix) && row.name.endsWith('[value]'));
    expect(measuredValue('input#household-name'), 'household name value is measured').toBe(true);
    expect(measuredValue('select#onboarding-timezone'), 'time zone selected value is measured').toBe(true);
    expect(contrastShortfalls(contrastRows), 'onboarding-error contrast failures').toEqual([]);
    console.log(`contrast-summary onboarding-error ${JSON.stringify({ measured: contrastRows.length, shortfalls: contrastShortfalls(contrastRows).length })}`);
    await test.info().attach('contrast-evidence-onboarding.json', { body: JSON.stringify(contrastRows, null, 2), contentType: 'application/json' });
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
    await expectNoAxeViolations(page, 'home-empty');

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
    await waitForSettledHome(page, householdName);
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
    await expectNoAxeViolations(page, 'people-add-form');

    await page.getByRole('button', { name: 'Čeština' }).click();
    await expect(page.getByRole('heading', { level: 1, name: cs['subjects.title'] })).toBeVisible();
    await expect(page.getByRole('form', { name: cs['subjects.add.formLabel'] })).toBeVisible();
    await page.getByRole('button', { name: 'English' }).click();
    await expect(page.getByRole('heading', { level: 1, name: en['subjects.title'] })).toBeVisible();

    await page.getByRole('link', { name: en['nav.home'], exact: true }).click();
    await waitForSettledHome(page, householdName);
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

  test('n0. item history pages 25 receipts at a time, newest first, and every receipt stays reachable', async () => {
    const displayDate = (value: string) => new Intl.DateTimeFormat('en-US', { dateStyle: 'medium', timeZone: 'UTC' }).format(new Date(`${value}T00:00:00Z`));
    const doneRow = (value: string) => en['itemDetail.historyDone'].replace('{date}', displayDate(value));
    const title = 'Water the balcony plants';
    const subjectsResponse = await page.request.get(`/api/v1/households/${householdId}/subjects?archived=false`);
    expect(subjectsResponse.status()).toBe(200);
    const subjectId = ((await subjectsResponse.json()) as { items: Array<{ id: string }> }).items[0]!.id;
    const created = await page.request.post(`/api/v1/households/${householdId}/items`, { headers: { Origin: baseURL, 'Idempotency-Key': 'e2e-history-pagination-item' }, data: { title, subjectId, recurrence: { intervalValue: 1, intervalUnit: 'day', mode: 'fixed' } } });
    expect(created.status()).toBe(201);
    const seeded = await created.json() as { id: string };
    const completionsPath = `/api/v1/households/${householdId}/items/${seeded.id}/completions`;
    // 60 receipts recorded in date order (one per day, the last today), so newest-recorded is also the latest date.
    const seededDates = Array.from({ length: 60 }, (_, index) => addDays(pragueToday(), index - 59));
    for (const [index, completedOn] of seededDates.entries()) {
      const current = await page.request.get(`/api/v1/households/${householdId}/items/${seeded.id}`);
      expect(current.status()).toBe(200);
      const result = await page.request.post(completionsPath, { headers: { Origin: baseURL, 'If-Match': current.headers()['etag']!, 'Idempotency-Key': `e2e-history-pagination-${index}` }, data: { completedOn } });
      expect(result.status(), `seed completion ${index}`).toBe(201);
    }
    const newestFirst = [...seededDates].reverse();
    const detailRequests: URL[] = [];
    const onRequest = (req: import('@playwright/test').Request) => { const url = new URL(req.url()); if (url.pathname === completionsPath && req.method() === 'GET') detailRequests.push(url); };
    try {
      page.on('request', onRequest);
      await page.goto(`/items/${seeded.id}`);
      const main = page.getByRole('main');
      const history = main.getByRole('heading', { level: 2, name: en['itemDetail.history'], exact: true }).locator('xpath=ancestor::section[1]');
      const rows = history.getByRole('listitem');
      const button = (key: 'itemDetail.historyOlder' | 'itemDetail.historyNewer' | 'itemDetail.historyLatest') => history.getByRole('button', { name: en[key], exact: true });
      // Every page row, in order: a receipt's first line is its done date (planned/next dates follow).
      const expectPage = async (from: number, to: number) => {
        const expected = newestFirst.slice(from, to).map((value) => new RegExp(`^${escapeRegExp(doneRow(value))}`));
        await expect(rows).toHaveText(expected);
      };
      await expectPage(0, 25);
      await expect(button('itemDetail.historyNewer')).toHaveCount(0);
      await expect(button('itemDetail.historyLatest')).toHaveCount(0);

      await button('itemDetail.historyOlder').click();
      await expectPage(25, 50);
      await expect(main.getByRole('heading', { level: 2, name: en['itemDetail.history'], exact: true })).toBeFocused();

      await button('itemDetail.historyOlder').click();
      await expectPage(50, 60);
      await expect(rows.last()).toHaveText(new RegExp(`^${escapeRegExp(doneRow(seededDates[0]!))}`));
      await expect(button('itemDetail.historyOlder')).toHaveCount(0);

      await button('itemDetail.historyNewer').click();
      await expectPage(25, 50);
      await button('itemDetail.historyNewer').click();
      await expectPage(0, 25);
      await expect(button('itemDetail.historyNewer')).toHaveCount(0);
      await expect(button('itemDetail.historyLatest')).toHaveCount(0);

      await button('itemDetail.historyOlder').click();
      await expectPage(25, 50);
      await button('itemDetail.historyLatest').click();
      await expectPage(0, 25);
      await expect(button('itemDetail.historyLatest')).toHaveCount(0);

      expect(detailRequests.length).toBe(7);
      for (const url of detailRequests) {
        expect(url.searchParams.get('limit'), url.search).toBe('25');
        expect(url.searchParams.get('order'), url.search).toBe('newest');
      }
      expect(detailRequests.map((url) => url.searchParams.has('cursor'))).toEqual([false, true, true, true, false, true, false]);
    } finally {
      page.off('request', onRequest);
      const current = await page.request.get(`/api/v1/households/${householdId}/items/${seeded.id}`);
      if (current.status() === 200) await page.request.patch(`/api/v1/households/${householdId}/items/${seeded.id}`, { headers: { Origin: baseURL, 'If-Match': current.headers()['etag']! }, data: { archived: true } });
    }
  });

  test('n26. 999, 1000 and 1001 receipt histories stay reachable through Latest/Newer/Older', async () => {
    test.setTimeout(600_000);
    const subjectResponse = await page.request.get(`/api/v1/households/${householdId}/subjects?archived=false`);
    expect(subjectResponse.status()).toBe(200);
    const subjectId = ((await subjectResponse.json()) as { items: Array<{ id: string }> }).items[0]!.id;
    for (const count of [999, 1000, 1001]) {
      const title = `History boundary ${count}`;
      const create = await page.request.post(`/api/v1/households/${householdId}/items`, {
        headers: { Origin: baseURL, 'Idempotency-Key': `e2e-history-boundary-${count}` },
        data: { title, subjectId, recurrence: { intervalValue: 1, intervalUnit: 'day', mode: 'fixed' } },
      });
      expect(create.status()).toBe(201);
      const { id } = await create.json() as { id: string };
      const completionsPath = `/api/v1/households/${householdId}/items/${id}/completions`;
      const initialDate = new Date('2020-01-01T00:00:00Z');
      for (let index = 0; index < count; index++) {
        const current = await page.request.get(`/api/v1/households/${householdId}/items/${id}`);
        expect(current.status()).toBe(200);
        const date = new Date(initialDate);
        date.setUTCDate(date.getUTCDate() + index);
        const response = await page.request.post(completionsPath, {
          headers: { Origin: baseURL, 'If-Match': current.headers()['etag']!, 'Idempotency-Key': `boundary-${count}-${index}` },
          data: { completedOn: date.toISOString().slice(0, 10) },
        });
        expect(response.status(), `receipt ${index + 1}/${count} for boundary ${count}`).toBe(201);
      }
      const allIds: string[] = [];
      let cursor: string | undefined;
      do {
        const url = new URL(`${baseURL}${completionsPath}`);
        url.searchParams.set('limit', '25');
        url.searchParams.set('order', 'newest');
        if (cursor) url.searchParams.set('cursor', cursor);
        const response = await page.request.get(url.toString());
        expect(response.status()).toBe(200);
        const body = await response.json() as { items: ServerCompletion[]; nextCursor?: string };
        allIds.push(...body.items.map((receipt) => receipt.id));
        cursor = body.nextCursor;
      } while (cursor);
      expect(allIds).toHaveLength(count);
      expect(new Set(allIds).size).toBe(count);

      const historyRequests: URL[] = [];
      const onHistoryRequest = (request: import('@playwright/test').Request) => {
        const url = new URL(request.url());
        if (request.method() === 'GET' && url.pathname === completionsPath) historyRequests.push(url);
      };
      page.on('request', onHistoryRequest);
      await page.goto(`/items/${id}`);
      const history = page.getByRole('main').getByRole('heading', { level: 2, name: en['itemDetail.history'], exact: true }).locator('xpath=ancestor::section[1]');
      const rows = history.getByRole('listitem');
      const button = (key: 'itemDetail.historyOlder' | 'itemDetail.historyNewer' | 'itemDetail.historyLatest') => history.getByRole('button', { name: en[key], exact: true });
      const visitedDates: string[] = [];
      const historyDoneRow = (value: string) => en['itemDetail.historyDone'].replace('{date}', new Intl.DateTimeFormat('en-US', { dateStyle: 'medium', timeZone: 'UTC' }).format(new Date(`${value}T00:00:00Z`)));
      const newestDates = Array.from({ length: count }, (_, index) => {
        const date = new Date(initialDate);
        date.setUTCDate(date.getUTCDate() + index);
        return date.toISOString().slice(0, 10);
      }).reverse();
      const assertPage = async (offset: number) => {
        const dates = newestDates.slice(offset, offset + 25);
        await expect(rows).toHaveText(dates.map((date) => new RegExp(`^${escapeRegExp(historyDoneRow(date))}`)));
        visitedDates.push(...dates);
      };
      let offset = 0;
      await assertPage(offset);
      expect(await page.getByText(/partial history/i).count()).toBe(0);
      await expect(button('itemDetail.historyNewer')).toHaveCount(0);
      while (offset + 25 < count) {
        await button('itemDetail.historyOlder').click();
        offset += 25;
        await assertPage(offset);
      }
      expect(visitedDates).toEqual(newestDates);
      await expect(button('itemDetail.historyOlder')).toHaveCount(0);
      expect(visitedDates.at(-1)).toBe('2020-01-01');
      visitedDates.length = 0;
      await button('itemDetail.historyNewer').click();
      offset -= 25;
      await assertPage(offset);
      visitedDates.length = 0;
      await button('itemDetail.historyLatest').click();
      await assertPage(0);
      await expect(button('itemDetail.historyLatest')).toHaveCount(0);
      try {
        expect(historyRequests.length).toBe(Math.ceil(count / 25) + 2);
        for (const url of historyRequests) {
          expect(url.searchParams.get('limit'), url.search).toBe('25');
          expect(url.searchParams.get('order'), url.search).toBe('newest');
        }
        expect(historyRequests[0]!.searchParams.has('cursor')).toBe(false);
        expect(historyRequests.slice(1, Math.ceil(count / 25) + 1).every((url) => url.searchParams.has('cursor'))).toBe(true);
        expect(historyRequests.at(-2)!.searchParams.has('cursor')).toBe(true);
        expect(historyRequests.at(-1)!.searchParams.has('cursor')).toBe(false);
      } finally {
        page.off('request', onHistoryRequest);
      }
      const current = await page.request.get(`/api/v1/households/${householdId}/items/${id}`);
      expect(current.status()).toBe(200);
      const archived = await page.request.patch(`/api/v1/households/${householdId}/items/${id}`, {
        headers: { Origin: baseURL, 'If-Match': current.headers()['etag']! }, data: { archived: true },
      });
      expect(archived.status()).toBe(200);
    }
  });

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

  test('n3b. early completion preserves fixed cadence and shifts fluid cadence; undo restores the fluid anchor', async () => {
    // Three days ahead (not one full interval) so fixed next, fluid next and the undone anchor are all distinct dates.
    const anchor = addDays(pragueToday(), 3);
    const fixedTitle = 'Check the emergency kit';
    const fluidTitle = 'Water the seedlings early';

    await addItem({ title: fixedTitle, subject: 'Anička', attentionOn: anchor, repeat: { value: '1', unit: 'week', fluid: false } });
    await expect(group(en['items.group.upcoming']).getByRole('listitem').filter({ hasText: fixedTitle })).toBeVisible();
    const fixedBefore = await serverItem(fixedTitle);
    expect(fixedBefore).toMatchObject({ attentionOn: anchor, attention: 'upcoming', recurrence: { intervalValue: 1, intervalUnit: 'week', mode: 'fixed' } });

    // Upcoming rows expose the same Done action, allowing an explicitly early completion.
    const fixedRow = group(en['items.group.upcoming']).getByRole('listitem').filter({ hasText: fixedTitle });
    await expect(fixedRow.getByRole('button', { name: en['items.done'] })).toBeVisible();
    await fixedRow.getByRole('button', { name: en['items.done'] }).click();
    await expect(page.getByRole('status').filter({ hasText: fixedTitle })).toContainText('Next time:');
    const fixedReceipt = (await serverCompletions(fixedBefore.id))[0]!;
    const fixedNext = addDays(anchor, 7);
    expect(fixedReceipt).toMatchObject({ cycleAttentionOn: anchor, nextAttentionOn: fixedNext, completedOn: expectedCompletionDate, undoneAt: null });
    expect(fixedNext > expectedCompletionDate).toBe(true);
    expect(await serverItem(fixedTitle)).toMatchObject({ attentionOn: fixedNext, attention: 'upcoming', lastCompletedOn: expectedCompletionDate });
    await expect(group(en['items.group.upcoming']).getByRole('listitem').filter({ hasText: fixedTitle })).toBeVisible();

    await addItem({ title: fluidTitle, subject: 'Anička', attentionOn: anchor, repeat: { value: '1', unit: 'week', fluid: true } });
    await expect(group(en['items.group.upcoming']).getByRole('listitem').filter({ hasText: fluidTitle })).toBeVisible();
    const fluidBefore = await serverItem(fluidTitle);
    expect(fluidBefore).toMatchObject({ attentionOn: anchor, attention: 'upcoming', recurrence: { intervalValue: 1, intervalUnit: 'week', mode: 'after_completion' } });
    const fluidRow = group(en['items.group.upcoming']).getByRole('listitem').filter({ hasText: fluidTitle });
    await expect(fluidRow.getByRole('button', { name: en['items.done'] })).toBeVisible();
    await fluidRow.getByRole('button', { name: en['items.done'] }).click();
    await expect(page.getByRole('status').filter({ hasText: fluidTitle })).toContainText('Next time:');

    const fluidReceipt = (await serverCompletions(fluidBefore.id))[0]!;
    const fluidNext = addDays(expectedCompletionDate, 7);
    expect(fluidNext < fixedNext && fluidNext !== anchor).toBe(true);
    expect(fluidReceipt).toMatchObject({ cycleAttentionOn: anchor, nextAttentionOn: fluidNext, completedOn: expectedCompletionDate, undoneAt: null });
    expect(await serverItem(fluidTitle)).toMatchObject({ attentionOn: fluidNext, attention: 'upcoming', lastCompletedOn: expectedCompletionDate });
    await expect(group(en['items.group.upcoming']).getByRole('listitem').filter({ hasText: fluidTitle })).toBeVisible();

    await page.getByRole('button', { name: en['items.undo'] }).click();
    await expect.poll(async () => (await serverCompletions(fluidBefore.id))[0]?.undoneAt ?? null).not.toBeNull();
    expect(await serverItem(fluidTitle)).toMatchObject({ attentionOn: anchor, attention: 'upcoming', lastCompletedOn: null, done: false });
    await expect(group(en['items.group.upcoming']).getByRole('listitem').filter({ hasText: fluidTitle })).toBeVisible();
    const fluidHistory = await serverCompletions(fluidBefore.id);
    expect(fluidHistory).toHaveLength(1);
    expect(fluidHistory[0]).toMatchObject({ cycleAttentionOn: anchor, nextAttentionOn: fluidNext, undoneAt: expect.any(String) });
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
    await expect(page.getByRole('main').getByRole('listitem').filter({ hasText: fixedTitle })).toContainText(`${en['items.every.month.one'].replace('{count}', '1')} · ${en['items.repeatMode.fixed']}`);

    await page.getByRole('link', { name: en['itemDetail.open'].replace('{title}', fixedTitle) }).click();
    await expect(page.getByRole('heading', { level: 1, name: fixedTitle })).toBeVisible();
    const dateLabel = (value: string) => new Intl.DateTimeFormat('en-US', { dateStyle: 'medium', timeZone: 'UTC' }).format(new Date(`${value}T00:00:00Z`));
    await expect(page.getByText(en['itemDetail.lastDone'].replace('{date}', dateLabel('2025-10-31')))).toBeVisible();
    await page.reload();
    await expect(page.getByRole('heading', { level: 1, name: fixedTitle })).toBeVisible();
    await expect(page.locator('ol').getByRole('listitem')).toHaveCount(1);
    await expect(page.locator('ol').getByRole('listitem')).toContainText(en['itemDetail.historyDone'].replace('{date}', dateLabel('2025-10-31')));

    await page.getByRole('link', { name: en['nav.home'], exact: true }).click();
    await waitForSettledHome(page, householdName);
    await row(fixedTitle).getByRole('button', { name: en['items.done'] }).click();
    await expect.poll(async () => (await serverCompletions(fixed.id)).length).toBe(2);
    expect(await serverItem(fixedTitle)).toMatchObject({ attentionOn: '2026-01-30', lastCompletedOn: expectedCompletionDate });
    await page.getByRole('button', { name: en['items.undo'] }).click();
    await expect(row(fixedTitle).getByRole('button', { name: en['items.done'] })).toBeFocused();
    await expect.poll(async () => (await serverItem(fixedTitle)).attentionOn).toBe('2025-11-30');
    expect(await serverItem(fixedTitle)).toMatchObject({ attentionOn: '2025-11-30', lastCompletedOn: '2025-10-31' });

    const fluidTitle = 'Historical fluid monthly February';
    await addItem({ title: fluidTitle, subject: 'Anička', historicalCompletedOn: '2025-10-31', repeat: { value: '1', unit: 'month', fluid: true } });
    const fluid = await serverItem(fluidTitle);
    expect(fluid).toMatchObject({ attentionOn: '2025-11-30', lastCompletedOn: '2025-10-31', recurrence: { intervalValue: 1, intervalUnit: 'month', mode: 'after_completion' } });
    expect(await serverCompletions(fluid.id)).toHaveLength(1);
    await expect(page.getByRole('main').getByRole('listitem').filter({ hasText: fluidTitle })).toContainText(`${en['items.every.month.one'].replace('{count}', '1')} · ${en['items.repeatMode.fluid']}`);
    await expect(page.getByRole('main').getByRole('listitem').filter({ hasText: fixedTitle })).not.toContainText(en['items.repeatMode.fluid']);
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
    await expectNoAxeViolations(page, 'members-owner-invite-link');
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
    await trackInFlightApiFetches(memberPage);
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
      await expect(memberPage.locator('#invite-login')).toBeVisible();
      await expectNoAxeViolations(memberPage, 'invite-accept-new-account');
      await memberPage.locator('#invite-login').fill('member_e2e');
      await memberPage.locator('#invite-password').fill(`${ownerPassword}-member`);
      await memberPage.getByRole('button', { name: en['invite.create'] }).click();
      await expect(memberPage.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
      await expect(memberPage.locator('body')).not.toContainText(householdId);

      await memberPage.getByRole('link', { name: en['home.members'], exact: true }).click();
      await expect(memberPage.getByRole('heading', { level: 1, name: en['members.title'] })).toBeVisible();
      await waitForSettledHouseholdRequests(memberPage);
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
    await waitForSettledHome(page, householdName);
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
    await waitForSettledHouseholdRequests(page);
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
      await waitForSettledHome(page, householdName);
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
      const counts = await summaryResponse.json() as { needsAttention: number; inProgress: number; waiting: number; upcoming: number; paused: number };
      page.on('request', requestListener);
      await page.goto('/');
      await waitForSettledHome(page, householdName);
      const needs = group(en['items.group.needs']);
      for (let index = 0; index < 3; index++) await expect(needs.getByRole('listitem').filter({ hasText: `Slice six needs ${index}` })).toBeVisible();
      const upcoming = group(en['items.group.upcoming']);
      const heading = upcoming.getByRole('heading', { level: 2 });
      await expect(heading).toContainText(String(counts.upcoming));
      const overview = page.getByRole('main').getByRole('navigation', { name: en['items.group.overview'] });
      await expect(overview.getByRole('link')).toHaveCount([counts.needsAttention, counts.inProgress, counts.waiting, counts.upcoming, counts.paused].filter(count => count > 0).length);
      await expect(overview.getByRole('link', { name: `Coming up ${counts.upcoming} items` })).toBeVisible();
      await expectNoAxeViolations(page, 'home-group-overview');
      await overview.getByRole('link', { name: `Coming up ${counts.upcoming} items` }).click();
      await expect(heading).toBeFocused();
      const initialItems = await upcoming.getByRole('listitem').count();
      expect(initialItems).toBe(Math.min(25, counts.upcoming));
      expect(homeRequests.length).toBeLessThanOrEqual(6);
      for (const width of [320, 390]) {
        await page.setViewportSize({ width, height: 740 });
        expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth), `no horizontal overflow at ${width}px`).toBeLessThanOrEqual(0);
      }
      const more = upcoming.locator('[data-group-more="upcoming"]');
      await more.focus();
      const beforePageRequestCount = homeRequests.filter((entry) => entry.startsWith('GET ') && entry.endsWith('/items')).length;
      await page.keyboard.press('Enter');
      await expect(upcoming.getByRole('listitem')).toHaveCount(Math.min(50, counts.upcoming));
      await expect(more).toHaveAttribute('aria-disabled', 'false');
      await expect(more).toBeFocused();
      expect(homeRequests.filter((entry) => entry.startsWith('GET ') && entry.endsWith('/items')).length - beforePageRequestCount).toBe(1);
      while (await more.count()) {
        const before = await upcoming.getByRole('listitem').count();
        await page.keyboard.press('Enter');
        await expect(upcoming.getByRole('listitem')).toHaveCount(Math.min(100, before + 25));
        if (await more.count()) await expect(more).toHaveAttribute('aria-disabled', 'false');
      }
      await expect(upcoming.getByRole('listitem')).toHaveCount(100);
      const seeAll = upcoming.locator('[data-group-all="upcoming"]');
      await expect(seeAll).toHaveText(`See all ${counts.upcoming} in All items`);
      await expect(seeAll).toBeFocused();
      await seeAll.click();
      await expect(page).toHaveURL('/items?status=upcoming');
      await expect(page.getByRole('main').getByLabel('Show')).toHaveValue('upcoming');
      await expect(page.getByRole('main')).toContainText(`Showing 1–25 of ${counts.upcoming}`);
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
      await waitForSettledHome(page, householdName);
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
      await waitForSettledHome(page, householdName);
      const homeMain = page.getByRole('main');
      await expect(homeMain.getByRole('navigation', { name: en['items.group.overview'] })).toBeVisible();
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
      await expect(page.getByRole('status').filter({ hasText: `Showing 1–${accented.length} of ${accented.length}` })).toHaveAttribute('aria-atomic', 'true');

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
      await expect(page.getByRole('main').locator('p.font-display').filter({ hasText: en['allItems.noMatch'] })).toBeVisible();
      await expect(page.getByRole('main').getByRole('status').filter({ hasText: en['allItems.noMatch'] })).toHaveAttribute('aria-atomic', 'true');
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
      await waitForSettledHome(page, householdName);
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
      await waitForSettledHome(page, householdName);
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
      await expect(more).toHaveCount(1);
      await more.click();
      await expect(main.getByRole('listitem').filter({ hasText: visibleTitles[25]! })).toBeVisible();
      const allRenderedIDs = await main.locator('[data-item-id]').evaluateAll((nodes) => nodes.map((node) => node.getAttribute('data-item-id')!));
      const allRenderedItems = await Promise.all(allRenderedIDs.map(async (id) => {
        const response = await page.request.get(`/api/v1/households/${householdId}/items/${id}`);
        expect(response.status()).toBe(200);
        return await response.json() as HomeItem;
      }));
      const newlySeen = [...new Set(allRenderedItems.map((item) => item.subjectId))].filter((id) => !initialUniqueSubjects.has(id));
      expect(newlySeen).toContain(extraSubjectId);
      await expect.poll(() => subjectRequests.length).toBe(beforeMore + (newlySeen.length ? 2 : 0));
      for (const url of subjectRequests.slice(beforeMore)) {
        expect(url.searchParams.getAll('id').sort()).toEqual(newlySeen.sort());
        expect(url.searchParams.get('archived')).toMatch(/^(true|false)$/);
      }

      const expectedActiveIDs: string[] = [];
      for (let cursor: string | null = null, pages = 0; ; pages++) {
        expect(pages).toBeLessThan(50);
        const walk = await page.request.get(`/api/v1/households/${householdId}/subjects?archived=false&limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`);
        expect(walk.status()).toBe(200);
        const body = await walk.json() as { items: Array<{ id: string }>; nextCursor: string | null };
        expectedActiveIDs.push(...body.items.map((entry) => entry.id));
        if (!body.nextCursor) break;
        cursor = body.nextCursor;
      }
      expect(expectedActiveIDs).toContain(extraSubjectId);
      expect(expectedActiveIDs).not.toContain(archivedSubjectId);
      const expectedActiveCount = expectedActiveIDs.length;
      const beforeAdd = subjectRequests.length;
      await main.getByRole('button', { name: en['items.add'], exact: true }).click();
      await expect(page.getByRole('form', { name: en['items.add.formLabel'] })).toBeVisible();
      await expect.poll(() => subjectRequests.length).toBe(beforeAdd + Math.ceil(expectedActiveCount / 100));
      const activeCatalogueRequests = subjectRequests.slice(beforeAdd);
      expect(activeCatalogueRequests).toHaveLength(Math.ceil(expectedActiveCount / 100));
      expect(activeCatalogueRequests.every((url) => !url.searchParams.has('id') && url.searchParams.get('archived') === 'false')).toBe(true);
      expect(subjectRequests.filter((url) => !url.searchParams.has('id') && url.searchParams.get('archived') === 'true')).toHaveLength(0);
      const chooser = page.getByRole('form', { name: en['items.add.formLabel'] }).getByLabel(en['items.subject'], { exact: true });
      const chooserIDs = await chooser.locator('option').evaluateAll((nodes) => nodes.map((node) => (node as HTMLOptionElement).value));
      expect([...chooserIDs].sort()).toEqual([...expectedActiveIDs].sort());
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

  test('n14. accessibility: WCAG scans, 200% zoom reflow and reduced motion across household screens', async ({ browser }) => {
    test.setTimeout(120_000);
    const title = 'Accessibility check: water the balcony plants';
    const noMatch = 'zz no item matches this accessibility search';
    const expectNoOverflow = async (label: string) => {
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
      expect(overflow, `${label} horizontal overflow`).toBeLessThanOrEqual(0);
    };
    const setWidth = async (width: number, height: number) => {
      await page.setViewportSize({ width, height });
      expect(await page.evaluate(() => window.innerWidth)).toBe(width);
    };
    const zoomMeasurements: Array<{ route: string; percent: number; cssWidth: number; dpr: number; bodyFontPx: number; bodyFontDevicePx: number; overflow: number }> = [];
    // Emulate Chrome page zoom on the live page, so the measured state is the one the test opened
    // (form, edit, filters). 1280 device px at 200% gives a 640 CSS px layout viewport and DPR 2;
    // at 400% 320 CSS px and DPR 4. A 16px body font therefore occupies 32/64 device px.
    // `check` runs while zoomed and must assert the state's own controls.
    const atPageZoom = async (route: string, check: (percent: 200 | 400) => Promise<void>) => {
      const zoomCDP = await page.context().newCDPSession(page);
      try {
        for (const percent of [200, 400] as const) {
          const width = 1280 / (percent / 100);
          await zoomCDP.send('Emulation.setDeviceMetricsOverride', { width, height: 400, deviceScaleFactor: percent / 100, mobile: false });
          const measured = await page.evaluate(() => ({ width: window.innerWidth, dpr: window.devicePixelRatio, fontSize: parseFloat(getComputedStyle(document.body).fontSize), overflow: document.documentElement.scrollWidth - window.innerWidth }));
          expect(measured.width, `${route} ${percent}% layout width`).toBe(width);
          expect(measured.dpr, `${route} ${percent}% DPR`).toBe(percent / 100);
          expect(measured.fontSize * measured.dpr).toBeGreaterThanOrEqual(32);
          expect(measured.overflow, `${route} ${percent}% overflow`).toBeLessThanOrEqual(0);
          zoomMeasurements.push({ route, percent, cssWidth: measured.width, dpr: measured.dpr, bodyFontPx: measured.fontSize, bodyFontDevicePx: measured.fontSize * measured.dpr, overflow: measured.overflow });
          await check(percent);
          await expectNoOverflow(`${route} ${percent}% after checking its controls`);
        }
      } finally {
        // Drop this session's override, then make Playwright re-send its own viewport emulation
        // (setViewportSize to an unchanged size is a no-op, so change it first).
        await zoomCDP.send('Emulation.clearDeviceMetricsOverride');
        await zoomCDP.detach();
        await page.setViewportSize({ width: 1281, height: 800 });
        await page.setViewportSize({ width: 1280, height: 800 });
        expect(await page.evaluate(() => [window.innerWidth, window.devicePixelRatio]), `${route} zoom restored`).toEqual([1280, 1]);
      }
    };
    // A real 320 CSS px viewport (not emulated zoom): overflow and axe on the current state.
    const at320 = async (label: string, check: () => Promise<void>) => {
      await setWidth(320, 640);
      try {
        await expectNoOverflow(`320px ${label}`);
        await check();
        await expectNoAxeViolations(page, `${label}-320`);
      } finally {
        await setWidth(1280, 800);
      }
    };
    const snapshot = async (label: string) => {
      const value = await page.locator('body').ariaSnapshot();
      await test.info().attach(`aria-${label}.txt`, { body: value, contentType: 'text/plain' });
      return value;
    };
    // Targets are compared with a 1/64 px tolerance: Chrome lays out in 1/64 px units, so a 24 px
    // min-height can measure 23.99998 px at some vertical offsets.
    const checkTargets = async (label: string) => {
      const small = await page.locator('a[href],button,input,select,textarea,[role="switch"],[role="radio"]').evaluateAll((elements) => elements.filter((element) => {
        if (element.matches('.sr-only') && !element.matches(':focus')) return false; // revealed only while focused
        const box = element.getBoundingClientRect();
        const label = element.closest('label');
        const hitBox = label ? label.getBoundingClientRect() : box;
        return box.width > 0 && box.height > 0 && (hitBox.width < 24 - 1 / 64 || hitBox.height < 24 - 1 / 64);
      }).map((element) => { const target = element.closest('label') ?? element; return { tag: element.tagName, name: element.getAttribute('aria-label') || element.textContent?.trim() || element.getAttribute('type'), width: Math.round(target.getBoundingClientRect().width), height: Math.round(target.getBoundingClientRect().height) }; }));
      await test.info().attach(`targets-${label}.json`, { body: JSON.stringify(small, null, 2), contentType: 'application/json' });
      expect(small, `${label} interactive targets >=24 CSS px`).toEqual([]);
    };
    let itemId = '';
    try {
      await setWidth(1280, 800);
      await page.goto('/');
      await waitForSettledHome(page, householdName);
      await addItem({ title, subject: 'Anička', attentionOn: addDays(pragueToday(), -2), repeat: { value: '2', unit: 'week', fluid: true } });
      itemId = (await serverItem(title)).id;
      await expect(group(en['items.group.needs']).getByRole('listitem').filter({ hasText: title })).toBeVisible();
      const homeSnapshot = await snapshot('home-populated-en');
      expect(homeSnapshot).toContain('navigation');
      expect(homeSnapshot).toContain('main');
      await checkTargets('home-populated-en');
      await expectNoAxeViolations(page, 'home-populated-en');
      await atPageZoom('home populated', async (percent) => {
        await expect(page.getByRole('button', { name: en['items.add'], exact: true }), `Add item at ${percent}%`).toBeVisible();
        const homeRow = group(en['items.group.needs']).getByRole('listitem').filter({ hasText: title });
        await homeRow.getByRole('button', { name: en['items.done'] }).scrollIntoViewIfNeeded();
        await expect(homeRow.getByRole('button', { name: en['items.done'] }), `Done at ${percent}%`).toBeInViewport();
      });
      await at320('home-populated', async () => {
        await expect(page.getByRole('button', { name: en['items.add'], exact: true })).toBeVisible();
      });
      // Keyboard: the skip link moves focus past the navigation, and each of the first sampled tab stops shows a visible focus indicator.
      await page.setViewportSize({ width: 1280, height: 800 });
      await page.goto('/');
      await expect(group(en['items.group.needs']).getByRole('listitem').filter({ hasText: title })).toBeVisible();
      await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
      await page.keyboard.press('Tab');
      await expect(page.getByRole('link', { name: en['app.skipToContent'] })).toBeFocused();
      await page.keyboard.press('Enter');
      await expect(page.locator('#main')).toBeFocused();
      const stops: string[] = [];
      for (let index = 0; index < 12; index += 1) {
        await page.keyboard.press('Tab');
        // A focus indicator is real only if focusing changes the outline or shadow; a permanent shadow does not count.
        const stop = await page.evaluate(() => {
          const element = document.activeElement as HTMLElement | null;
          if (!element || element === document.body) return null;
          // Zero-width or fully transparent outlines are not visible indicators.
          const ring = (style: CSSStyleDeclaration) => `${style.outlineStyle === 'none' || parseFloat(style.outlineWidth) <= 0 || /rgba\([^)]*,\s*0\)|transparent/.test(style.outlineColor) ? '' : `${style.outlineStyle} ${style.outlineWidth} ${style.outlineColor}`}|${style.boxShadow}`;
          const focused = ring(getComputedStyle(element));
          return { name: element.getAttribute('aria-label') ?? element.textContent?.trim().slice(0, 40) ?? element.tagName, focused, inMain: !!element.closest('main') };
        });
        if (!stop) break;
        const unfocused = await page.evaluate(() => {
          const element = document.activeElement as HTMLElement;
          element.blur();
          const style = getComputedStyle(element);
          const value = `${style.outlineStyle === 'none' || parseFloat(style.outlineWidth) <= 0 || /rgba\([^)]*,\s*0\)|transparent/.test(style.outlineColor) ? '' : `${style.outlineStyle} ${style.outlineWidth} ${style.outlineColor}`}|${style.boxShadow}`;
          element.focus({ preventScroll: true });
          return value;
        });
        const visible = stop.focused !== unfocused && stop.focused !== '|none';
        expect(visible, `focus indicator on ${stop.name}`).toBe(true);
        stops.push(stop.inMain ? `main:${stop.name}` : stop.name);
      }
      expect(stops.length, 'sampled tab stops').toBeGreaterThanOrEqual(6);
      expect(stops[0], 'first tab stop after skipping lands in main content').toMatch(/^main:/);

      await page.getByRole('button', { name: en['items.add'], exact: true }).click();
      const addForm = page.getByRole('form', { name: en['items.add.formLabel'] });
      await addForm.getByRole('switch', { name: en['items.repeat'], exact: true }).check();
      await addForm.getByRole('button', { name: en['items.historical.show'] }).click();
      await expect(addForm.getByLabel(en['items.historical.label'])).toBeVisible();
      const addSnapshot = await snapshot('add-item-repeat-and-previous-completion');
      expect(addSnapshot).toContain(en['items.repeat']);
      expect(addSnapshot).toContain(en['items.fluid']);
      await checkTargets('add-item-repeat-and-previous-completion');
      await expectNoAxeViolations(page, 'add-item-repeat-and-previous-completion');
      const addFormControls = async (where: string) => {
        await expect(addForm, `add form open ${where}`).toBeVisible();
        await expect(addForm.getByLabel(en['items.historical.label']), `previous completion ${where}`).toBeVisible();
        for (const name of [en['items.add.submit'], en['items.cancel']]) {
          const control = addForm.getByRole('button', { name, exact: true });
          await control.scrollIntoViewIfNeeded();
          await expect(control, `${name} in add form ${where}`).toBeInViewport();
        }
      };
      await atPageZoom('add form', (percent) => addFormControls(`at ${percent}%`));
      await at320('add-item-form', () => addFormControls('at 320px'));
      await addForm.getByRole('button', { name: en['items.cancel'], exact: true }).click();
      await expect(addForm).toHaveCount(0);

      await row(title).getByRole('button', { name: en['items.done'] }).click();
      await expect.poll(async () => (await serverCompletions(itemId)).length).toBe(1);
      await page.getByRole('link', { name: en['itemDetail.open'].replace('{title}', title) }).click();
      await expect(page.getByRole('heading', { level: 1, name: title })).toBeVisible();
      await expect(page.locator('ol').getByRole('listitem')).toHaveCount(1);
      const detailSnapshot = await snapshot('item-detail-with-history');
      expect(detailSnapshot).toContain('heading');
      await checkTargets('item-detail-with-history');
      await expectNoAxeViolations(page, 'item-detail-with-history');
      const detailControls = async (where: string) => {
        await expect(page.getByRole('heading', { level: 1, name: title }), `detail heading ${where}`).toBeVisible();
        const edit = page.getByRole('button', { name: en['itemDetail.edit.button'], exact: true });
        await edit.scrollIntoViewIfNeeded();
        await expect(edit, `Edit ${where}`).toBeInViewport();
      };
      await atPageZoom('item detail', (percent) => detailControls(`at ${percent}%`));
      await at320('item-detail', () => detailControls('at 320px'));
      await page.getByRole('button', { name: en['itemDetail.edit.button'], exact: true }).click();
      const editForm = page.getByRole('form', { name: en['itemDetail.edit.formLabel'] });
      await expect(editForm.getByLabel(en['items.title'])).toHaveValue(title);
      const editSnapshot = await snapshot('item-edit-form');
      expect(editSnapshot).toContain(en['items.title']);
      await checkTargets('item-edit-form');
      await expectNoAxeViolations(page, 'item-edit-form');
      const editControls = async (where: string) => {
        await expect(editForm.getByLabel(en['items.title']), `edit title ${where}`).toHaveValue(title);
        for (const name of [en['itemDetail.save'], en['items.cancel']]) {
          const control = editForm.getByRole('button', { name, exact: true });
          await control.scrollIntoViewIfNeeded();
          await expect(control, `${name} in edit form ${where}`).toBeInViewport();
        }
      };
      await atPageZoom('item edit', (percent) => editControls(`at ${percent}%`));
      await at320('item-edit-form', () => editControls('at 320px'));
      await editForm.getByRole('button', { name: en['items.cancel'], exact: true }).scrollIntoViewIfNeeded();
      await editForm.getByRole('button', { name: en['items.cancel'], exact: true }).click();
      await expect(editForm).toHaveCount(0);

      await page.getByRole('link', { name: en['home.allItems'], exact: true }).click();
      await expect(page.getByRole('heading', { level: 1, name: en['allItems.title'] })).toBeVisible();
      await page.getByRole('searchbox').fill('balcony plants');
      await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
      await page.getByLabel(en['allItems.repeat']).selectOption('true');
      await expect(page.getByRole('main').getByRole('listitem').filter({ hasText: title })).toBeVisible();
      const allItemsSnapshot = await snapshot('all-items-search-and-filters');
      expect(allItemsSnapshot).toContain('searchbox');
      expect(allItemsSnapshot).toContain(en['allItems.repeat']);
      await checkTargets('all-items-search-and-filters');
      await expectNoAxeViolations(page, 'all-items-search-and-filters');
      const allItemsControls = async (where: string) => {
        await expect(page.getByRole('searchbox'), `search kept ${where}`).toHaveValue('balcony plants');
        await expect(page.getByLabel(en['allItems.repeat']), `repeat filter kept ${where}`).toHaveValue('true');
        for (const control of [page.getByRole('button', { name: en['allItems.searchButton'], exact: true }), page.getByRole('main').getByRole('link', { name: title, exact: true })]) {
          await control.scrollIntoViewIfNeeded();
          await expect(control, `All items control ${where}`).toBeInViewport();
        }
      };
      await atPageZoom('All items', (percent) => allItemsControls(`at ${percent}%`));
      await at320('all-items-search-and-filters', () => allItemsControls('at 320px'));
      await page.getByRole('searchbox').fill(noMatch);
      await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
      await expect(page.getByRole('main').locator('p.font-display').filter({ hasText: en['allItems.noMatch'] })).toBeVisible();
      await expect(page.getByRole('main').getByRole('status').filter({ hasText: en['allItems.noMatch'] })).toHaveAttribute('aria-atomic', 'true');
      await expectNoAxeViolations(page, 'all-items-no-match');

      await page.getByRole('link', { name: en['home.people'], exact: true }).click();
      await expect(page.getByRole('heading', { level: 1, name: en['subjects.title'] })).toBeVisible();
      await page.getByRole('button', { name: en['subjects.edit.label'].replace('{name}', 'Anička') }).click();
      await expect(page.getByRole('form', { name: en['subjects.edit.label'].replace('{name}', 'Anička') })).toBeVisible();
      const peopleSnapshot = await snapshot('people-edit-form');
      expect(peopleSnapshot).toContain(en['subjects.name']);
      await checkTargets('people-edit-form');
      await expectNoAxeViolations(page, 'people-edit-form');
      const peopleForm = page.getByRole('form', { name: en['subjects.edit.label'].replace('{name}', 'Anička') });
      const peopleControls = async (where: string) => {
        await expect(peopleForm.getByLabel(en['subjects.name']), `name field ${where}`).toBeVisible();
        for (const name of [en['subjects.save'], en['subjects.cancel']]) {
          const control = peopleForm.getByRole('button', { name, exact: true });
          await control.scrollIntoViewIfNeeded();
          await expect(control, `${name} in People edit ${where}`).toBeInViewport();
        }
      };
      await atPageZoom('People and things edit', (percent) => peopleControls(`at ${percent}%`));
      await at320('people-edit-form', () => peopleControls('at 320px'));
      await page.getByRole('form', { name: en['subjects.edit.label'].replace('{name}', 'Anička') }).getByRole('button', { name: en['subjects.cancel'], exact: true }).click();

      await page.getByRole('link', { name: en['home.account'], exact: true }).click();
      await expect(page.getByRole('heading', { level: 1, name: en['account.title'] })).toBeVisible();
      await waitForSettledHouseholdRequests(page);
      const accountSnapshot = await snapshot('account');
      expect(accountSnapshot).toContain(en['account.title']);
      await checkTargets('account');
      await expectNoAxeViolations(page, 'account');
      const accountControls = async (where: string) => {
        await expect(page.getByRole('heading', { level: 1, name: en['account.title'] }), `Account heading ${where}`).toBeVisible();
        const czech = page.getByRole('button', { name: 'Čeština' });
        await czech.scrollIntoViewIfNeeded();
        await expect(czech, `language switch ${where}`).toBeInViewport();
      };
      await atPageZoom('Account', (percent) => accountControls(`at ${percent}%`));
      await at320('account', () => accountControls('at 320px'));
      await page.getByRole('button', { name: 'Čeština' }).click();
      try {
        await page.getByRole('link', { name: cs['nav.home'], exact: true }).click();
        await expect(group(cs['items.group.needs'])).toBeVisible();
        await expectNoAxeViolations(page, 'home-populated-cs');
        await page.getByRole('link', { name: cs['home.allItems'], exact: true }).click();
        await expect(page.getByRole('heading', { level: 1, name: cs['allItems.title'] })).toBeVisible();
        await expect(page.getByRole('main').getByRole('listitem').first()).toBeVisible();
        await expectNoAxeViolations(page, 'all-items-cs');
        await at320('all-items-cs', async () => {
          await expect(page.getByRole('button', { name: cs['allItems.searchButton'], exact: true })).toBeVisible();
        });
      } finally {
        await setWidth(1280, 800);
        await page.getByRole('button', { name: 'English' }).click();
        await expect(page.locator('html')).toHaveAttribute('lang', 'en');
      }

      // Confirm the helper detects the known 400ms Home settle animation without reduced motion.
      const normal = await browser.newContext({ baseURL, locale: 'en-US', reducedMotion: 'no-preference', storageState: await page.context().storageState() });
      try {
        const normalPage = await normal.newPage();
        await normalPage.goto('/');
        await expect(normalPage.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
        const normalMeasurement = await measureMotion(normalPage);
        expect(normalMeasurement.elements).toBeGreaterThan(0);
        expect(normalMeasurement.longestMs, 'normal-motion Home detects .settle animation').toBeGreaterThanOrEqual(300);
      } finally {
        await normal.close();
      }

      const reduced = await browser.newContext({ baseURL, locale: 'en-US', reducedMotion: 'reduce', storageState: await page.context().storageState() });
      const reducedMeasurements: Array<{ route: string; measurement: Awaited<ReturnType<typeof measureMotion>> }> = [];
      try {
        const reducedPage = await reduced.newPage();
        await trackInFlightApiFetches(reducedPage);
        const measureState = async (route: string) => {
          const measurement = await measureMotion(reducedPage);
          expect(measurement.elements, `${route} has measured document elements`).toBeGreaterThan(0);
          expect(measurement.longestMs, `${route} longest motion (ms): ${JSON.stringify(measurement.offender)}`).toBeLessThanOrEqual(1);
          expect(measurement.rootScrollBehavior, `${route} root scroll behavior`).not.toBe('smooth');
          for (const [index, animation] of measurement.animationTimings.entries()) {
            expect(Number.isFinite(animation.iterations), `${route} animation ${index} has finite iterations`).toBe(true);
            expect(animation.durationMs + animation.delayMs + animation.endDelayMs, `${route} animation ${index} duration+delay`).toBeLessThanOrEqual(1);
          }
          reducedMeasurements.push({ route, measurement });
        };
        const openHome = async () => {
          await reducedPage.goto('/');
          await waitForSettledHome(reducedPage, householdName);
        };
        await openHome();
        await measureState('Home');
        await reducedPage.getByRole('button', { name: en['items.add'], exact: true }).click();
        const reducedAddForm = reducedPage.getByRole('form', { name: en['items.add.formLabel'] });
        await reducedAddForm.getByRole('switch', { name: en['items.repeat'], exact: true }).check();
        await expect(reducedAddForm).toBeVisible();
        await measureState('Home with Add item form and Repeat on');
        await reducedAddForm.getByRole('button', { name: en['items.cancel'], exact: true }).click();

        await openHome();
        await reducedPage.getByRole('link', { name: en['itemDetail.open'].replace('{title}', title) }).click();
        await expect(reducedPage.getByRole('heading', { level: 1, name: title })).toBeVisible();
        await waitForSettledHouseholdRequests(reducedPage);
        await measureState('Item detail');
        await reducedPage.getByRole('button', { name: en['itemDetail.edit.button'], exact: true }).click();
        const reducedEditForm = reducedPage.getByRole('form', { name: en['itemDetail.edit.formLabel'] });
        await expect(reducedEditForm.getByLabel(en['items.title'])).toHaveValue(title);
        await measureState('Item edit form');
        await reducedEditForm.getByRole('button', { name: en['items.cancel'], exact: true }).click();

        await openHome();
        await reducedPage.getByRole('link', { name: en['home.allItems'], exact: true }).click();
        await expect(reducedPage.getByRole('heading', { level: 1, name: en['allItems.title'] })).toBeVisible();
        await waitForSettledHouseholdRequests(reducedPage);
        await measureState('All items');
        await reducedPage.getByRole('link', { name: en['home.people'], exact: true }).click();
        await expect(reducedPage.getByRole('heading', { level: 1, name: en['subjects.title'] })).toBeVisible();
        await waitForSettledHouseholdRequests(reducedPage);
        await measureState('People and things');
        await reducedPage.getByRole('button', { name: en['subjects.edit.label'].replace('{name}', 'Anička') }).click();
        const reducedPeopleForm = reducedPage.getByRole('form', { name: en['subjects.edit.label'].replace('{name}', 'Anička') });
        await expect(reducedPeopleForm).toBeVisible();
        await measureState('People and things edit form');
        await reducedPeopleForm.getByRole('button', { name: en['subjects.cancel'], exact: true }).click();

        await openHome();
        await reducedPage.getByRole('link', { name: en['home.members'], exact: true }).click();
        await expect(reducedPage.getByRole('heading', { level: 1, name: en['members.title'] })).toBeVisible();
        await waitForSettledHouseholdRequests(reducedPage);
        await measureState('Members');
        await openHome();
        await reducedPage.getByRole('link', { name: en['home.account'], exact: true }).click();
        await expect(reducedPage.getByRole('heading', { level: 1, name: en['account.title'] })).toBeVisible();
        await waitForSettledHouseholdRequests(reducedPage);
        await measureState('Account');
      } finally {
        console.log(`reduced-motion-n14 ${JSON.stringify(reducedMeasurements)}`);
        await test.info().attach('reduced-motion-n14.json', { body: JSON.stringify(reducedMeasurements, null, 2), contentType: 'application/json' });
        await reduced.close();
      }
    } finally {
      console.log(`zoom-measurements-n14 ${JSON.stringify(zoomMeasurements)}`);
      await test.info().attach('zoom-measurements-n14.json', { body: JSON.stringify(zoomMeasurements, null, 2), contentType: 'application/json' });
      await page.setViewportSize({ width: 1280, height: 720 });
      if (itemId) {
        const current = await page.request.get(`/api/v1/households/${householdId}/items/${itemId}`);
        if (current.status() === 200 && !(await current.json() as { archived: boolean }).archived) {
          const archived = await page.request.patch(`/api/v1/households/${householdId}/items/${itemId}`, { headers: { Origin: baseURL, 'If-Match': current.headers()['etag']! }, data: { archived: true } });
          expect(archived.status()).toBe(200);
        }
      }
    }
  });

  test('n15. responsive: every household route fits 320, 360, 390, 768 and 1440 px without horizontal overflow or clipped primary actions', async () => {
    const title = 'Responsive route evidence item';
    const widths = [320, 360, 390, 768, 1440];
    await page.goto('/');
    await addItem({ title, subject: 'Anička' });
    const created = await serverItem(title);
    const main = page.getByRole('main');
    const expectInViewport = async (locator: Locator, label: string) => {
      await expect(locator, label).toBeVisible();
      const box = await locator.boundingBox();
      expect(box, `${label} has a bounding box`).not.toBeNull();
      expect(box!.x, `${label} left edge`).toBeGreaterThanOrEqual(0);
      expect(box!.x + box!.width, `${label} right edge`).toBeLessThanOrEqual(await page.evaluate(() => window.innerWidth));
    };
    const expectNoHorizontalOverflow = async (label: string) => {
      const state = await page.evaluate(() => ({ width: window.innerWidth, scrollWidth: document.scrollingElement!.scrollWidth }));
      expect(state.scrollWidth, `${label} scrollWidth ${state.scrollWidth} <= innerWidth ${state.width}`).toBeLessThanOrEqual(state.width);
    };
    // Readiness first: each route's loaded content/actions must be visible before overflow is measured,
    // so rows or sections rendered after the heading cannot escape the overflow assertion.
    const check = async (label: string, primary: Locator[], ready: Locator[] = []) => {
      for (const [index, content] of ready.entries()) await expect(content, `${label} loaded content ${index + 1}`).toBeVisible();
      for (const [index, action] of primary.entries()) await expectInViewport(action, `${label} primary action ${index + 1}`);
      await expectNoHorizontalOverflow(label);
    };

    try {
      for (const width of widths) {
        await page.setViewportSize({ width, height: 800 });
        expect(await page.evaluate(() => window.innerWidth), `requested viewport ${width}`).toBe(width);
        await page.goto('/');
        await waitForSettledHome(page, householdName);
        await check(`Home ${width}`, [main.getByRole('button', { name: en['items.add'], exact: true })], [main.getByRole('listitem').filter({ hasText: title }).first()]);

        await page.getByRole('link', { name: en['home.allItems'], exact: true }).click();
        await expect(page.getByRole('heading', { level: 1, name: en['allItems.title'] })).toBeVisible();
        const search = main.getByRole('searchbox');
        await search.fill('Responsive route evidence');
        await main.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
        await expect(main.getByRole('listitem').filter({ hasText: title })).toBeVisible();
        await check(`All items with search ${width}`, [main.getByRole('button', { name: en['allItems.searchButton'], exact: true }), main.getByRole('link', { name: title, exact: true })]);

        await main.getByRole('link', { name: title, exact: true }).click();
        await expect(page).toHaveURL(new RegExp(`/items/${created.id}`));
        await check(`Item detail ${width}`, [main.getByRole('button', { name: en['itemDetail.edit.button'], exact: true })]);
        await main.getByRole('button', { name: en['itemDetail.edit.button'], exact: true }).click();
        const editForm = main.getByRole('form', { name: en['itemDetail.edit.formLabel'] });
        await check(`Item edit ${width}`, [editForm.getByRole('button', { name: en['itemDetail.save'], exact: true })]);

        await page.goto('/people');
        await expect(page.getByRole('heading', { level: 1, name: en['subjects.title'] })).toBeVisible();
        await check(`People and things ${width}`, [main.getByRole('button', { name: en['subjects.add'], exact: true })], [main.getByRole('listitem').filter({ hasText: 'Anička' }).first()]);
        await page.goto('/members');
        await expect(page.getByRole('heading', { level: 1, name: en['members.title'] })).toBeVisible();
        await check(`Members ${width}`, [main.getByRole('button', { name: en['members.invite.action'], exact: true })], [main.getByRole('listitem').filter({ hasText: ownerLogin }).first()]);
        await page.goto('/account');
        await expect(page.getByRole('heading', { level: 1, name: en['account.title'] })).toBeVisible();
        // n9 already linked the owner, so Account has no primary action: assert the linked section content instead.
        await check(`Account ${width}`, [], [main.getByRole('heading', { level: 2, name: en['account.oidc.title'].replace('{provider}', 'OIDC Smoke Provider') }), main.getByText(en['account.oidc.linked'].replace('{provider}', 'OIDC Smoke Provider'))]);

        // SPA routes must also work as direct deep links after a reload and remain traversable
        // through the browser history. Every implemented signed-in route is sampled at each width;
        // invite/login/setup lifecycle behavior is covered by n7/n25, a–d, and n19.
        const routeChecks = [
          { path: '/', heading: householdName },
          { path: '/items', heading: en['allItems.title'] },
          { path: `/items/${created.id}`, heading: title },
          { path: '/people', heading: en['subjects.title'] },
          { path: '/members', heading: en['members.title'] },
          { path: '/account', heading: en['account.title'] },
        ];
        for (const route of routeChecks) {
          await page.goto(route.path);
          await expect(page.getByRole('heading', { level: 1, name: route.heading })).toBeVisible();
          await page.reload();
          await expect(page.getByRole('heading', { level: 1, name: route.heading })).toBeVisible();
        }
        await page.goto('/');
        await page.getByRole('navigation', { name: en['nav.label'] }).getByRole('link', { name: en['home.people'], exact: true }).click();
        await expect(page.getByRole('heading', { level: 1, name: en['subjects.title'] })).toBeVisible();
        await page.goBack();
        await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
        await page.goForward();
        await expect(page.getByRole('heading', { level: 1, name: en['subjects.title'] })).toBeVisible();
        await expectNoHorizontalOverflow(`route history ${width}`);
      }

      await page.setViewportSize({ width: 320, height: 800 });
      await page.getByRole('button', { name: 'Čeština' }).click();
      await expect(page.locator('html')).toHaveAttribute('lang', 'cs');
      await page.goto('/');
      await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
      await check('Home 320 Czech', [page.getByRole('main').getByRole('button', { name: cs['items.add'], exact: true })], [page.getByRole('main').getByRole('listitem').filter({ hasText: title }).first()]);
      await page.goto(`/items/${created.id}`);
      await expect(page.getByRole('heading', { level: 1, name: title })).toBeVisible();
      await check('Item detail 320 Czech', [page.getByRole('main').getByRole('button', { name: cs['itemDetail.edit.button'], exact: true })]);
      await page.goto('/items');
      await expect(page.getByRole('heading', { level: 1, name: cs['allItems.title'] })).toBeVisible();
      const czechMain = page.getByRole('main');
      await czechMain.getByRole('searchbox').fill('Responsive route evidence');
      await czechMain.getByRole('button', { name: cs['allItems.searchButton'], exact: true }).click();
      await expect(czechMain.getByRole('listitem').filter({ hasText: title })).toBeVisible();
      await check('All items 320 Czech', [czechMain.getByRole('button', { name: cs['allItems.searchButton'], exact: true }), czechMain.getByRole('link', { name: title, exact: true })]);
    } finally {
      await page.setViewportSize({ width: 1280, height: 720 });
      if (await page.locator('html').getAttribute('lang') === 'cs') {
        await page.goto('/');
        await page.getByRole('button', { name: 'English' }).click();
        await expect(page.locator('html')).toHaveAttribute('lang', 'en');
      }
      await page.setViewportSize({ width: 1280, height: 720 });
    }
  });

  test('n16. keyboard only: add, complete, undo, open, edit and save an item', async () => {
    const originalTitle = 'Keyboard journey unique item';
    const savedTitle = 'Keyboard journey renamed item';
    await page.setViewportSize({ width: 1280, height: 720 });
    await page.goto('/');
    await waitForSettledHome(page, householdName);
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
    const pressTabUntil = async (predicate: (name: string) => boolean, description: string) => {
      for (let i = 0; i < 200; i += 1) {
        const active = await page.evaluate(() => {
          const element = document.activeElement as HTMLElement | null;
          return element ? (element.getAttribute('aria-label') || element.innerText || element.getAttribute('title') || element.tagName).trim() : '';
        });
        if (predicate(active)) return;
        await page.keyboard.press('Tab');
      }
      throw new Error(`Could not tab to ${description} within 200 stops`);
    };

    await pressTabUntil((name) => name === en['items.add'], 'Add item');
    await page.keyboard.press('Enter');
    const main0 = () => page.getByRole('main');
    const form = main0().getByRole('form', { name: en['items.add.formLabel'] });
    await expect(form.getByLabel(en['items.title'])).toBeFocused();
    await page.keyboard.type(originalTitle);
    await page.keyboard.press('Tab'); // subject select
    await page.keyboard.press('Home');
    await page.keyboard.press('Tab'); // next form control: responsible select or submit
    // Focus the submit button by tabbing the remaining controls, without DOM focus helpers.
    for (let i = 0; i < 12; i += 1) {
      const active = await page.evaluate(() => document.activeElement?.getAttribute('aria-label') || (document.activeElement as HTMLElement | null)?.innerText?.trim() || '');
      if (active === en['items.add.submit']) break;
      await page.keyboard.press('Tab');
    }
    const activeSubmit = await page.evaluate(() => document.activeElement?.getAttribute('aria-label') || (document.activeElement as HTMLElement | null)?.innerText?.trim() || '');
    expect(activeSubmit).toContain(en['items.add.submit']);
    await page.keyboard.press('Enter');
    await expect(form).toHaveCount(0);
    await expect(main0().getByRole('button', { name: en['items.add'], exact: true })).toBeFocused();
    await expect(page.getByRole('main').getByRole('listitem').filter({ hasText: originalTitle })).toBeVisible();

    const main = page.getByRole('main');
    const itemRow = main.getByRole('listitem').filter({ hasText: originalTitle });
    const tabToRowButton = async (name: string) => {
      for (let i = 0; i < 200; i += 1) {
        const activeName = await page.evaluate(() => {
          const element = document.activeElement as HTMLElement | null;
          return element ? (element.getAttribute('aria-label') || element.innerText || element.getAttribute('title') || '').trim() : '';
        });
        if (activeName === name || activeName.includes(name)) return;
        await page.keyboard.press('Tab');
      }
      throw new Error(`Could not tab to ${name}`);
    };
    await tabToRowButton(en['items.done.label'].replace('{title}', originalTitle));
    await page.keyboard.press('Enter');
    await expect(page.getByRole('status').filter({ hasText: originalTitle })).toContainText(en['items.notice.done'].replace('{title}', originalTitle));
    // The notice container (tabindex -1 wrapper around the status) receives focus so Undo is the next Tab stop.
    await expect.poll(() => page.evaluate(() => document.activeElement?.querySelector('[role="status"]')?.textContent ?? '')).toContain(en['items.notice.done'].replace('{title}', originalTitle));
    await pressTabUntil((name) => name === en['items.undo'], 'Undo');
    await page.keyboard.press('Enter');
    await expect(itemRow).toBeVisible();
    await expect(itemRow.getByRole('button', { name: en['items.done.label'].replace('{title}', originalTitle) })).toBeFocused();

    const titleLink = main.getByRole('link', { name: en['itemDetail.open'].replace('{title}', originalTitle), exact: true });
    for (let i = 0; i < 200 && !(await titleLink.evaluate((element) => element === document.activeElement)); i += 1) await page.keyboard.press('Tab');
    await expect(titleLink).toBeFocused();
    await page.keyboard.press('Enter');
    await expect(page.getByRole('heading', { level: 1, name: originalTitle })).toBeFocused();
    await pressTabUntil((name) => name === en['itemDetail.edit.button'], 'Edit');
    await page.keyboard.press('Enter');
    const editForm = page.getByRole('main').getByRole('form', { name: en['itemDetail.edit.formLabel'] });
    const titleInput = editForm.getByLabel(en['items.title']);
    await expect(titleInput).toBeFocused();
    await page.keyboard.press('ControlOrMeta+A');
    await page.keyboard.type(savedTitle);
    await expect(titleInput).toBeFocused();
    await expect(titleInput).toHaveValue(savedTitle);
    // Save is reached through the form's normal tab order; submitting with Enter is equivalent to activating the focused submit.
    await page.keyboard.press('Tab');
    for (let i = 0; i < 16; i += 1) {
      const name = await page.evaluate(() => document.activeElement?.getAttribute('aria-label') || (document.activeElement as HTMLElement | null)?.innerText?.trim() || '');
      if (name === en['itemDetail.save']) break;
      await page.keyboard.press('Tab');
    }
    expect(await page.evaluate(() => document.activeElement?.getAttribute('aria-label') || (document.activeElement as HTMLElement | null)?.innerText?.trim() || '')).toContain(en['itemDetail.save']);
    await page.keyboard.press('Enter');
    await expect(page.getByRole('heading', { level: 1, name: savedTitle })).toBeVisible();
    await expect(page.getByRole('main').getByRole('status').filter({ hasText: en['itemDetail.updated'] })).toBeFocused();

    const saved = await serverItem(savedTitle);
    expect(saved.done).toBe(false);
    const completions = await serverCompletions(saved.id);
    expect(completions).toHaveLength(1);
    expect(completions[0]!.undoneAt).not.toBeNull();
  });

  // G6 keyboard pairings beyond n16's item flow: each control is reached with Tab and activated with
  // Enter/arrow keys only; the visible outcome, focus destination and persisted server state are asserted.
  test('n16b. keyboard only: People add/type/edit/archive/undo, Members invite/revoke cancel/confirm, All items search/open/back', async () => {
    await page.setViewportSize({ width: 1280, height: 720 });
    const subjectsPath = `/api/v1/households/${householdId}/subjects`;
    const invitationsPath = `/api/v1/households/${householdId}/invitations`;
    const subjectName = 'Keyboard pairing shed';
    const renamedSubject = 'Keyboard pairing garden';
    const itemTitle = 'Keyboard pairing search target';
    const main = page.getByRole('main');
    const tabTo = async (target: import('@playwright/test').Locator, description: string) => {
      await expect(target, `${description} rendered`).toBeVisible();
      for (let i = 0; i < 200; i += 1) {
        if (await target.evaluate((element) => element === document.activeElement)) return;
        await page.keyboard.press('Tab');
      }
      throw new Error(`Could not tab to ${description} within 200 stops`);
    };
    const startFromTop = async () => page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
    const activeSubjects = async () => {
      const response = await page.request.get(subjectsPath, { params: { archived: 'false', limit: '100' } });
      expect(response.status()).toBe(200);
      return (await response.json()) as { items: Array<{ id: string; name: string; type: string; archived: boolean }>; nextCursor: string | null };
    };
    const invitationList = async () => {
      const response = await page.request.get(invitationsPath, { params: { limit: '100' } });
      expect(response.status()).toBe(200);
      return (await response.json()) as { items: Array<{ id: string; status: string }>; nextCursor: string | null };
    };
    let subjectId: string | undefined;
    let itemId: string | undefined;
    let primaryFailure: unknown;
    try {
      // People: Add → name → type radio by arrow key → submit; Archive → Undo restores focus to the row; Edit → Save.
      const before = await activeSubjects();
      expect(before.items.length, 'n16b People fixture fits on the first UI page').toBeLessThan(50);
      await page.goto('/people');
      await expect(page.getByRole('heading', { level: 1, name: en['subjects.title'] })).toBeVisible();
      await startFromTop();
      await tabTo(main.getByRole('button', { name: en['subjects.add'], exact: true }), 'People Add');
      await page.keyboard.press('Enter');
      const addForm = main.getByRole('form', { name: en['subjects.add.formLabel'] });
      await expect(addForm.getByLabel(en['subjects.name'])).toBeFocused();
      await page.keyboard.type(subjectName);
      await tabTo(addForm.getByRole('radio', { name: en['subjects.type.person'] }), 'checked Person type');
      await page.keyboard.press('ArrowRight');
      await expect(addForm.getByRole('radio', { name: en['subjects.type.home'] })).toBeChecked();
      await tabTo(addForm.getByRole('button', { name: en['subjects.add.submit'], exact: true }), 'People submit');
      await page.keyboard.press('Enter');
      await expect(main.getByRole('status')).toContainText(en['subjects.notice.added'].replace('{name}', subjectName));
      const created = (await activeSubjects()).items.find((subject) => subject.name === subjectName);
      expect(created, 'n16b keyboard-added subject persisted').toMatchObject({ type: 'home', archived: false });
      subjectId = created!.id;

      await tabTo(main.getByRole('button', { name: en['subjects.archive.label'].replace('{name}', subjectName) }), 'Archive subject');
      await page.keyboard.press('Enter');
      await expect(main.getByRole('status')).toContainText(en['subjects.notice.archived'].replace('{name}', subjectName));
      expect((await activeSubjects()).items.some((subject) => subject.id === subjectId), 'n16b archived subject leaves active list').toBe(false);
      await tabTo(main.getByRole('button', { name: en['subjects.undo'], exact: true }), 'Undo archive');
      await page.keyboard.press('Enter');
      const editButton = main.getByRole('button', { name: en['subjects.edit.label'].replace('{name}', subjectName) });
      await expect(editButton, 'n16b undo returns focus to the restored row').toBeFocused();
      expect((await activeSubjects()).items.some((subject) => subject.id === subjectId), 'n16b undo restores subject').toBe(true);

      await page.keyboard.press('Enter');
      const editName = main.getByLabel(en['subjects.name']);
      await expect(editName).toBeFocused();
      await page.keyboard.press('ControlOrMeta+A');
      await page.keyboard.type(renamedSubject);
      await tabTo(main.getByRole('button', { name: en['subjects.save'], exact: true }), 'People Save');
      await page.keyboard.press('Enter');
      await expect(main.getByRole('button', { name: en['subjects.edit.label'].replace('{name}', renamedSubject) }), 'n16b save returns focus to the row').toBeFocused();
      expect((await activeSubjects()).items.find((subject) => subject.id === subjectId), 'n16b keyboard rename persisted').toMatchObject({ name: renamedSubject, type: 'home' });

      // Members: Create invite link focuses the link; Revoke → Cancel restores focus; Revoke → Yes revokes.
      const invitationsBefore = await invitationList();
      expect(invitationsBefore.nextCursor, 'n16b invitations fit on one API page').toBeNull();
      expect(invitationsBefore.items.length, 'n16b invitations fit on the first UI page').toBeLessThan(49);
      await page.goto('/members');
      await expect(page.getByRole('heading', { level: 1, name: en['members.title'] })).toBeVisible();
      await startFromTop();
      await tabTo(main.getByRole('button', { name: en['members.invite.action'], exact: true }), 'Create invite link');
      await page.keyboard.press('Enter');
      const link = main.getByLabel(en['members.invite.link']);
      await expect(link, 'n16b new invite link takes focus').toBeFocused();
      await expect(link).toHaveValue(/\/invite#/);
      const added = (await invitationList()).items.filter((invitation) => !invitationsBefore.items.some((old) => old.id === invitation.id));
      expect(added, 'n16b exactly one invitation created').toHaveLength(1);
      expect(added[0]!.status).toBe('pending');
      const invitationRows = main.getByRole('heading', { name: en['members.invitations.title'], exact: true }).locator('xpath=following-sibling::ul[1]/li');
      const newestRow = invitationRows.last(); // invitations are listed oldest first by ID
      const revoke = newestRow.getByRole('button', { name: en['members.invitation.revoke'], exact: true });
      await tabTo(revoke, 'Revoke newest invitation');
      await page.keyboard.press('Enter');
      await expect(newestRow.getByRole('button', { name: en['members.invitation.yes'], exact: true }), 'n16b confirm takes focus').toBeFocused();
      await page.keyboard.press('Tab');
      await expect(newestRow.getByRole('button', { name: en['members.invitation.cancel'], exact: true })).toBeFocused();
      await page.keyboard.press('Enter');
      await expect(revoke, 'n16b cancel restores focus to Revoke').toBeFocused();
      expect((await invitationList()).items.find((invitation) => invitation.id === added[0]!.id)?.status, 'n16b cancel keeps invitation pending').toBe('pending');
      await page.keyboard.press('Enter');
      await expect(newestRow.getByRole('button', { name: en['members.invitation.yes'], exact: true })).toBeFocused();
      await page.keyboard.press('Enter');
      // The Members notice container (role=status, tabIndex=-1) receives focus, not <body>.
      const revokedNotice = main.getByRole('status').filter({ hasText: en['members.invite.revoked'] });
      await expect(revokedNotice).toHaveCount(1);
      await expect(revokedNotice, 'n16b revoke notice takes focus').toBeFocused();
      expect((await invitationList()).items.find((invitation) => invitation.id === added[0]!.id)?.status, 'n16b keyboard revoke persisted').toBe('revoked');

      // All items: search field → Enter applies q; result link → detail heading; Back link restores the filtered URL.
      const create = await page.request.post(`/api/v1/households/${householdId}/items`, {
        headers: { Origin: baseURL, 'Idempotency-Key': `e2e-keyboard-pairing-${crypto.randomUUID()}` },
        data: { title: itemTitle, subjectId },
      });
      expect(create.status()).toBe(201);
      itemId = ((await create.json()) as { id: string }).id;
      await page.goto('/items');
      await expect(page.getByRole('heading', { level: 1, name: en['allItems.title'] })).toBeVisible();
      await startFromTop();
      const search = main.getByRole('searchbox', { name: en['allItems.search'], exact: true });
      await tabTo(search, 'All items search');
      await page.keyboard.type(itemTitle);
      await page.keyboard.press('Enter');
      await expect(page).toHaveURL(/[?&]q=Keyboard\+pairing\+search\+target/);
      const result = main.getByRole('link', { name: itemTitle, exact: true });
      await expect(main.getByRole('region', { name: en['allItems.results'] }).getByRole('link')).toHaveCount(1);
      await tabTo(result, 'search result');
      await page.keyboard.press('Enter');
      await expect(page.getByRole('heading', { level: 1, name: itemTitle })).toBeFocused();
      await expect(page).toHaveURL(new RegExp(`/items/${itemId}$`));
      await startFromTop();
      await tabTo(page.getByRole('link', { name: en['itemDetail.backAllItems'], exact: true }), 'Back to all items');
      await page.keyboard.press('Enter');
      await expect(page).toHaveURL(/\/items\?q=Keyboard\+pairing\+search\+target$/);
      await expect(main.getByRole('searchbox', { name: en['allItems.search'], exact: true })).toHaveValue(itemTitle);
      await expect(main.getByRole('link', { name: itemTitle, exact: true })).toBeVisible();
    } catch (error) {
      primaryFailure = error;
    }
    // Attempt every cleanup independently; a cleanup failure is reported only when the test body passed.
    const cleanupFailures: unknown[] = [];
    for (const path of [itemId ? `/api/v1/households/${householdId}/items/${itemId}` : null, subjectId ? `${subjectsPath}/${subjectId}` : null]) {
      if (!path) continue;
      try {
        const current = await page.request.get(path);
        expect(current.status(), `n16b cleanup reads ${path}`).toBe(200);
        const archived = await page.request.patch(path, { headers: { Origin: baseURL, 'If-Match': current.headers()['etag']! }, data: { archived: true } });
        expect(archived.status(), `n16b cleanup archives ${path}`).toBe(200);
      } catch (error) { cleanupFailures.push(error); }
    }
    if (primaryFailure) throw primaryFailure;
    if (cleanupFailures.length) throw cleanupFailures[0];
  });

  test('n17. network failure and server errors show recoverable states', async () => {
    const main = page.getByRole('main');
    const homeLink = page.getByRole('link', { name: en['nav.home'], exact: true });
    const allItemsPath = '/api/v1/households/' + householdId + '/items/summary';
    const membersRoute = `**/api/v1/households/${householdId}/members*`;
    const ownedRoutes: Array<{ url: string; handler: (route: import('@playwright/test').Route) => void | Promise<void> }> = [];
    const ownRoute = async (url: string, handler: (route: import('@playwright/test').Route) => void | Promise<void>) => {
      ownedRoutes.push({ url, handler });
      await page.route(url, handler);
    };
    let switchedToCzech = false;
    let primaryFailure: unknown;
    let cleanupFailure: unknown;
    try {
      await page.setViewportSize({ width: 1280, height: 720 });
      await page.goto('/');
      await waitForSettledHome(page, householdName);

    // Offline failures are induced by in-app navigation, keeping the production SPA shell loaded.
    n17OfflineExpectedResources.length = 0;
    const offlineHouseholdResource = new URL(`/api/v1/households/${householdId}/`, baseURL).href;
    n17OfflineExpectedResources.push(
      new RegExp(`^${escapeRegExp(offlineHouseholdResource)}items/summary$`),
      new RegExp(`^${escapeRegExp(offlineHouseholdResource)}subjects\\?limit=100&archived=(?:false|true)(?:&id=[0-9a-f-]{36})*$`),
      new RegExp(`^${escapeRegExp(offlineHouseholdResource)}members\\?limit=50$`),
    );
    n17OfflineActive = true;
    await page.context().setOffline(true);
    try {
      await page.getByRole('link', { name: en['nav.allItems'], exact: true }).click();
      await expect(main.locator('p:not(.sr-only)').filter({ hasText: en['app.error.body'] })).toBeVisible();
    await expect(main.getByRole('status').filter({ hasText: en['app.error.body'] })).toHaveAttribute('aria-atomic', 'true');
      await expect(main.getByRole('button', { name: en['app.error.retry'], exact: true })).toBeVisible();
      await page.setViewportSize({ width: 320, height: 720 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth), '320px offline horizontal overflow').toBeLessThanOrEqual(0);
      await expectNoAxeViolations(page, 'n17-offline-error');
    } finally {
      await page.context().setOffline(false);
      n17OfflineActive = false;
      await page.setViewportSize({ width: 1280, height: 720 });
    }
    await main.getByRole('button', { name: en['app.error.retry'], exact: true }).click();
    await expect(main.getByText(en['app.error.body'], { exact: true })).toHaveCount(0);
    await expect(main.getByRole('button', { name: en['app.error.retry'], exact: true })).toHaveCount(0);
    await expect(main.getByRole('link', { name: 'Water the plants', exact: true })).toBeVisible();

    // A real API problem response produces the same recoverable list state.
    await homeLink.click();
    await waitForSettledHome(page, householdName);
    const summaryRoute = async (route: import('@playwright/test').Route) => route.fulfill({ status: 503, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Unavailable', status: 503, code: 'unavailable' }) });
    n17ExpectedFailurePaths.add(allItemsPath);
    n17FailurePhaseActive = true;
    await ownRoute(`**${allItemsPath}`, summaryRoute);
    await page.getByRole('link', { name: en['nav.allItems'], exact: true }).click();
    await expect(main.locator('p:not(.sr-only)').filter({ hasText: en['app.error.body'] })).toBeVisible();
    await expect(main.getByRole('status').filter({ hasText: en['app.error.body'] })).toHaveAttribute('aria-atomic', 'true');
    await expect(main.getByRole('button', { name: en['app.error.retry'], exact: true })).toBeVisible();
    await page.unroute(`**${allItemsPath}`, summaryRoute);
    ownedRoutes.splice(ownedRoutes.findIndex((entry) => entry.handler === summaryRoute), 1);
    n17ExpectedFailurePaths.delete(allItemsPath);
    n17FailurePhaseActive = false;
    await main.getByRole('button', { name: en['app.error.retry'], exact: true }).click();
    await expect(main.getByText(en['app.error.body'], { exact: true })).toHaveCount(0);
    await expect(main.getByRole('button', { name: en['app.error.retry'], exact: true })).toHaveCount(0);
    await expect(main.getByRole('link', { name: 'Water the plants', exact: true })).toBeVisible();

    // A lost completion response keeps the same completion key and does not advance the item until retry.
    const failureTitle = 'N17 recoverable completion unique';
    await homeLink.click();
    await waitForSettledHome(page, householdName);
    await addItem({ title: failureTitle, subject: 'Anička' });
    const itemBeforeFailure = await serverItem(failureTitle);
    const completionPath = `/api/v1/households/${householdId}/items/${itemBeforeFailure.id}/completions`;
    n17ExpectedFailurePaths.add(completionPath);
    n17FailurePhaseActive = true;
    const abortCompletion = (route: import('@playwright/test').Route) => route.abort();
    await ownRoute(`**${completionPath}`, abortCompletion);
    const failureRow = main.getByRole('listitem').filter({ hasText: failureTitle });
    await failureRow.getByRole('button', { name: en['items.done.label'].replace('{title}', failureTitle), exact: true }).click();
    await expect(main.getByRole('status').getByText(en['items.notice.retry'], { exact: true })).toBeVisible();
    const itemAfterFailure = await page.request.get(`/api/v1/households/${householdId}/items/${itemBeforeFailure.id}`);
    expect(itemAfterFailure.status()).toBe(200);
    expect(await itemAfterFailure.json()).toMatchObject({ attentionOn: itemBeforeFailure.attentionOn, done: false });
    await expect(failureRow).toBeVisible();
    await page.unroute(`**${completionPath}`, abortCompletion);
    ownedRoutes.splice(ownedRoutes.findIndex((entry) => entry.handler === abortCompletion), 1);
    await main.getByRole('status').getByRole('button', { name: en['items.retry'], exact: true }).click();
    await expect(main.getByRole('status').getByText(en['items.notice.done'].replace('{title}', failureTitle), { exact: true })).toBeVisible();
    const completedFailureItem = await page.request.get(`/api/v1/households/${householdId}/items/${itemBeforeFailure.id}`);
    expect(completedFailureItem.status()).toBe(200);
    expect(await completedFailureItem.json()).toMatchObject({ done: true });
    expect(await serverCompletions(itemBeforeFailure.id)).toHaveLength(1);
    n17ExpectedFailurePaths.delete(completionPath);
    n17FailurePhaseActive = false;

    // The server commits this second completion but the response is lost. Retrying must replay
    // the same idempotency key and return the existing receipt without creating another one.
    const lostResponseTitle = 'N17 lost completion response unique';
    await homeLink.click();
    await waitForSettledHome(page, householdName);
    await addItem({ title: lostResponseTitle, subject: 'Anička' });
    const itemBeforeLostResponse = await serverItem(lostResponseTitle);
    const lostResponsePath = `/api/v1/households/${householdId}/items/${itemBeforeLostResponse.id}/completions`;
    n17ExpectedFailurePaths.add(lostResponsePath);
    n17FailurePhaseActive = true;
    const completionKeys: string[] = [];
    let loseFirstResponse = true;
    const loseCompletionResponse = async (route: import('@playwright/test').Route) => {
      completionKeys.push((await route.request().allHeaders())['idempotency-key'] ?? '');
      if (loseFirstResponse) {
        loseFirstResponse = false;
        await route.fetch();
        await route.abort();
      } else {
        expect((await route.request().allHeaders())['idempotency-key']).toBe(completionKeys[0]);
        await route.continue();
      }
    };
    await ownRoute(`**${lostResponsePath}`, loseCompletionResponse);
    const lostResponseRow = main.getByRole('listitem').filter({ hasText: lostResponseTitle });
    await lostResponseRow.getByRole('button', { name: en['items.done.label'].replace('{title}', lostResponseTitle), exact: true }).click();
    await expect(main.getByRole('status').getByText(en['items.notice.retry'], { exact: true })).toBeVisible();
    const itemAfterLostResponse = await page.request.get(`/api/v1/households/${householdId}/items/${itemBeforeLostResponse.id}`);
    expect(itemAfterLostResponse.status()).toBe(200);
    expect(await itemAfterLostResponse.json()).toMatchObject({ done: true });
    const receiptBeforeRetry = await serverCompletions(itemBeforeLostResponse.id);
    expect(receiptBeforeRetry).toHaveLength(1);
    const receiptIdBeforeRetry = receiptBeforeRetry[0]!.id;
    expect(completionKeys).toHaveLength(1);
    expect(completionKeys[0]).toMatch(/^[0-9a-f-]{32,}$/i);
    await main.getByRole('status').getByRole('button', { name: en['items.retry'], exact: true }).click();
    await expect(main.getByRole('status').getByText(en['items.notice.done'].replace('{title}', lostResponseTitle), { exact: true })).toBeVisible();
    expect(completionKeys).toHaveLength(2);
    expect(completionKeys[1]).toBe(completionKeys[0]);
    const receiptAfterRetry = await serverCompletions(itemBeforeLostResponse.id);
    expect(receiptAfterRetry).toHaveLength(1);
    expect(receiptAfterRetry[0]!.id).toBe(receiptIdBeforeRetry);
    await page.unroute(`**${lostResponsePath}`, loseCompletionResponse);
    ownedRoutes.splice(ownedRoutes.findIndex((entry) => entry.handler === loseCompletionResponse), 1);
    n17ExpectedFailurePaths.delete(lostResponsePath);
    n17FailurePhaseActive = false;

    // Member list 403 is a refused request, regardless of the problem code, and retries in place.
    const membersPath = `/api/v1/households/${householdId}/members`;
    const membersForbidden = (route: import('@playwright/test').Route) => route.fulfill({ status: 403, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Forbidden', status: 403, code: 'owner_required' }) });
    await page.getByRole('link', { name: en['nav.home'], exact: true }).click();
    await waitForSettledHome(page, householdName);
    n17ExpectedFailurePaths.add(membersPath);
    n17FailurePhaseActive = true;
    await ownRoute(membersRoute, membersForbidden);
    await page.getByRole('link', { name: en['members.title'], exact: true }).click();
    await expect(main.getByText(en['members.error.requestRefused'], { exact: true })).toBeVisible();
    await page.unroute(membersRoute);
    ownedRoutes.splice(ownedRoutes.findIndex((entry) => entry.handler === membersForbidden), 1);
    n17ExpectedFailurePaths.delete(membersPath);
    n17FailurePhaseActive = false;
    await main.getByRole('button', { name: en['app.error.retry'], exact: true }).click();
    await expect(main.getByText(en['members.error.requestRefused'], { exact: true })).toHaveCount(0);
    await expect(main.getByRole('heading', { level: 1, name: en['members.title'] })).toBeVisible();
    await expect(main.getByRole('listitem').filter({ hasText: ownerLogin })).toBeVisible();

    // Repeat the list failure in Czech to verify localized recovery copy, then restore English.
    await page.getByRole('button', { name: 'Čeština', exact: true }).click();
    switchedToCzech = true;
    await page.goto('/');
    await waitForSettledHome(page, householdName);
    n17OfflineExpectedResources.length = 0;
    const czechOfflineHouseholdResource = new URL(`/api/v1/households/${householdId}/`, baseURL).href;
    n17OfflineExpectedResources.push(
      new RegExp(`^${escapeRegExp(czechOfflineHouseholdResource)}items/summary$`),
      new RegExp(`^${escapeRegExp(czechOfflineHouseholdResource)}subjects\\?limit=100&archived=(?:false|true)(?:&id=[0-9a-f-]{36})*$`),
      new RegExp(`^${escapeRegExp(czechOfflineHouseholdResource)}members\\?limit=50$`),
    );
    n17OfflineActive = true;
    await page.context().setOffline(true);
    try {
      await page.getByRole('link', { name: cs['nav.allItems'], exact: true }).click();
      await expect(page.getByRole('main').locator('p:not(.sr-only)').filter({ hasText: cs['app.error.body'] })).toBeVisible();
      await expect(page.getByRole('main').getByRole('button', { name: cs['app.error.retry'], exact: true })).toBeVisible();
    } finally {
      await page.context().setOffline(false);
      n17OfflineActive = false;
    }
    await page.getByRole('button', { name: 'English', exact: true }).click();
    switchedToCzech = false;
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    await page.goto('/');
    } catch (error) {
      primaryFailure = error;
    } finally {
      n17OfflineActive = false;
      n17OfflineExpectedResources.length = 0;
      n17FailurePhaseActive = false;
      n17ExpectedFailurePaths.clear();
      const cleanup = async (operation: () => Promise<unknown>) => {
        try { await operation(); } catch (error) { cleanupFailure ??= error; }
      };
      for (const route of ownedRoutes) await cleanup(() => page.unroute(route.url, route.handler));
      ownedRoutes.length = 0;
      await cleanup(() => page.context().setOffline(false));
      await cleanup(() => page.setViewportSize({ width: 1280, height: 720 }));
      await cleanup(async () => {
        if (switchedToCzech && await page.getByRole('button', { name: 'English', exact: true }).count()) {
          await page.getByRole('button', { name: 'English', exact: true }).click();
        }
      });
    }
    if (primaryFailure) throw primaryFailure;
    if (cleanupFailure) throw cleanupFailure;
  });

  test('n18. loading states, no-match recovery, Account language and Czech phone labels on household routes', async () => {
    test.setTimeout(60_000);
    const main = page.getByRole('main');
    const ownedRoutes: Array<{ url: string | RegExp; handler: (route: import('@playwright/test').Route) => void | Promise<void> }> = [];
    const releases: Array<() => void> = [];
    let switchedToCzech = false;
    let primaryFailure: unknown;
    let cleanupFailure: unknown;
    const ownRoute = async (url: string | RegExp, handler: (route: import('@playwright/test').Route) => void | Promise<void>) => {
      ownedRoutes.push({ url, handler });
      await page.route(url, handler);
    };
    const holdRequest = async (url: string | RegExp) => {
      let markHit!: () => void;
      let release!: () => void;
      const hit = new Promise<void>((resolve) => { markHit = resolve; });
      const held = new Promise<void>((resolve) => { release = resolve; });
      releases.push(release);
      const handler = async (route: import('@playwright/test').Route) => {
        markHit();
        await held;
        await route.continue();
      };
      await ownRoute(url, handler);
      return { url, hit, release, handler };
    };
    const expectLoading = async (text: string) => {
      await expect(main.getByRole('status').filter({ hasText: text })).toBeVisible();
    };
    const expectNoHorizontalOverflow = async (label: string) => {
      const state = await page.evaluate(() => ({ width: window.innerWidth, scrollWidth: document.scrollingElement!.scrollWidth }));
      expect(state.scrollWidth, `${label} scrollWidth ${state.scrollWidth} <= innerWidth ${state.width}`).toBeLessThanOrEqual(state.width);
    };

    try {
      await page.setViewportSize({ width: 1280, height: 720 });
      await page.goto('/account');
      await expect(main.getByRole('heading', { level: 2, name: 'Language', exact: true })).toBeVisible();
      await waitForSettledHouseholdRequests(page);
      await expect(main.getByText('Switch between English and Czech at the bottom of any page.', { exact: true })).toBeVisible();
      await page.getByRole('button', { name: 'Čeština', exact: true }).click();
      switchedToCzech = true;
      await expect(main.getByRole('heading', { level: 2, name: 'Jazyk', exact: true })).toBeVisible();
      await expect(main.getByText('Mezi češtinou a angličtinou přepnete dole na každé stránce.', { exact: true })).toBeVisible();
      await page.getByRole('button', { name: 'English', exact: true }).click();
      switchedToCzech = false;

      const waterItem = await serverItem('Water the plants');
      const itemPath = `/api/v1/households/${householdId}/items/${waterItem.id}`;
      await page.goto(`/items/${waterItem.id}`);
      await expect(main.getByRole('heading', { level: 1, name: 'Water the plants' })).toBeVisible();
      await waitForSettledHouseholdRequests(page);
      const editHold = await holdRequest(`**${itemPath}`);
      await main.getByRole('button', { name: en['itemDetail.edit.button'], exact: true }).click();
      await editHold.hit;
      await expectLoading(en['itemDetail.edit.loading']);
      await expect(main.getByRole('button', { name: en['itemDetail.edit.button'], exact: true })).toBeDisabled();
      editHold.release();
      await expect(main.getByRole('form', { name: en['itemDetail.edit.formLabel'] })).toBeVisible();
      await main.getByRole('form', { name: en['itemDetail.edit.formLabel'] }).getByRole('button', { name: en['items.cancel'], exact: true }).click();
      await expect(main.getByRole('form', { name: en['itemDetail.edit.formLabel'] })).toHaveCount(0);
      await page.unroute(`**${itemPath}`, editHold.handler);
      ownedRoutes.splice(ownedRoutes.findIndex((entry) => entry.handler === editHold.handler), 1);

      const itemsListPath = `/api/v1/households/${householdId}/items`;
      const allItemsHold = await holdRequest(new RegExp(`^${escapeRegExp(new URL(itemsListPath, baseURL).href)}\\?[^#]*$`));
      await page.goto('/items');
      await allItemsHold.hit;
      await expectLoading(en['items.loading']);
      allItemsHold.release();
      await expect(main.getByRole('listitem').filter({ hasText: 'Water the plants' })).toBeVisible();
      await page.unroute(allItemsHold.url, allItemsHold.handler);
      ownedRoutes.splice(ownedRoutes.findIndex((entry) => entry.handler === allItemsHold.handler), 1);

      await main.getByRole('searchbox').fill('N18 no matching household item query');
      await main.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
      await expect(main.locator('p.font-display').filter({ hasText: en['allItems.noMatch'] })).toBeVisible();
      await main.getByRole('button', { name: en['allItems.clear'], exact: true }).click();
      await expect(main.getByRole('listitem').filter({ hasText: 'Water the plants' })).toBeVisible();

      const peopleListUrl = new URL(`/api/v1/households/${householdId}/subjects`, baseURL).href;
      const peopleHold = await holdRequest(new RegExp(`^${escapeRegExp(peopleListUrl)}\\?[^#]*archived=false(?:&|$)`));
      await waitForSettledHouseholdRequests(page);
      await page.goto('/people');
      await peopleHold.hit;
      await expectLoading(en['subjects.loading']);
      peopleHold.release();
      await expect(main.getByRole('listitem').filter({ hasText: 'Anička' })).toBeVisible();
      await page.unroute(peopleHold.url, peopleHold.handler);
      ownedRoutes.splice(ownedRoutes.findIndex((entry) => entry.handler === peopleHold.handler), 1);

      const membersListUrl = new URL(`/api/v1/households/${householdId}/members`, baseURL).href;
      const membersHold = await holdRequest(new RegExp(`^${escapeRegExp(membersListUrl)}\\?limit=50$`));
      await page.goto('/members');
      await membersHold.hit;
      await expectLoading(en['members.loading']);
      membersHold.release();
      await expect(main.getByRole('listitem').filter({ hasText: ownerLogin })).toBeVisible();
      await page.unroute(membersHold.url, membersHold.handler);
      ownedRoutes.splice(ownedRoutes.findIndex((entry) => entry.handler === membersHold.handler), 1);

      await page.setViewportSize({ width: 320, height: 740 });
      await page.getByRole('button', { name: 'Čeština', exact: true }).click();
      switchedToCzech = true;
      const inviteAction = main.getByRole('button', { name: cs['members.invite.action'], exact: true });
      await expect(inviteAction).toBeVisible();
      await expectNoHorizontalOverflow('Czech Members invite action at 320px');
      const inviteBox = await inviteAction.boundingBox();
      expect(inviteBox, 'Czech invite action has a box').not.toBeNull();
      expect(inviteBox!.x + inviteBox!.width, 'Czech invite action stays inside the 320px viewport').toBeLessThanOrEqual(320);
      await page.goto('/items');
      await expect(main.getByRole('heading', { level: 1, name: cs['allItems.title'] })).toBeVisible();
      await main.getByRole('link', { name: 'Water the plants', exact: true }).click();
      const itemDetailBack = main.getByRole('link', { name: cs['itemDetail.backAllItems'], exact: true });
      await expect(itemDetailBack).toBeVisible();
      await expectNoHorizontalOverflow('Czech item detail back link at 320px');
      const backBox = await itemDetailBack.boundingBox();
      expect(backBox!.x + backBox!.width, 'Czech back link stays inside the 320px viewport').toBeLessThanOrEqual(320);
      await page.getByRole('button', { name: 'English', exact: true }).click();
      switchedToCzech = false;
    } catch (error) {
      primaryFailure = error;
    } finally {
      for (const release of releases) release();
      for (const route of ownedRoutes) {
        try { await page.unroute(route.url, route.handler); } catch (error) { cleanupFailure ??= error; }
      }
      await page.setViewportSize({ width: 1280, height: 720 }).catch((error) => { cleanupFailure ??= error; });
      try {
        if (switchedToCzech && await page.getByRole('button', { name: 'English', exact: true }).count()) {
          await page.getByRole('button', { name: 'English', exact: true }).click();
        }
      } catch (error) { cleanupFailure ??= error; }
    }
    if (primaryFailure) throw primaryFailure;
    if (cleanupFailure) throw cleanupFailure;
  });

  test('n19. an ended session explains the sign-out on the login screen', async () => {
    // Home chains summary -> group pages -> subject labels; revoke only after that chain has settled, otherwise a late
    // background 401 (CI run 40081332586: GET /subjects) ends the session before the navigation under test.
    await page.goto('/');
    await waitForSettledHome(page, householdName);
    const cookies = await page.context().cookies();
    const sessionCookie = cookies.find((cookie) => cookie.name === 'tendo_session' || cookie.name === '__Host-tendo_session');
    expect(sessionCookie).toBeDefined();

    const fresh = await request.newContext({ baseURL });
    n19FailedResponses.length = 0;
    expectedSessionEndedPaths = ['/items/summary', '/subjects', '/members'].map((suffix) => (path) => path === `/api/v1/households/${householdId}${suffix}`);
    n19PhaseActive = true;
    try {
      const revoked = await fresh.delete('/api/v1/session', {
        headers: { Cookie: `${sessionCookie!.name}=${sessionCookie!.value}`, Origin: baseURL },
      });
      expect(revoked.status()).toBe(204);
      const allItemsLink = page.getByRole('navigation', { name: en['nav.label'] }).getByRole('link', { name: en['nav.allItems'], exact: true });
      await expect(allItemsLink).toBeVisible();
      await page.evaluate(() => {
        const link = [...document.querySelectorAll<HTMLAnchorElement>('nav a')].find((candidate) => candidate.getAttribute('href') === '/items');
        if (!link) throw new Error('All items navigation link is missing');
        link.click();
      });
      await expect(page.getByRole('heading', { level: 1, name: en['login.title'] })).toBeVisible();
      await expect(page.getByRole('heading', { level: 1, name: en['login.title'] })).toBeFocused();
      await expect(page.getByRole('status')).toHaveText(en['login.sessionEnded']);
      await expectNoAxeViolations(page, 'login-session-ended');
      const loginZoom = await page.context().newCDPSession(page);
      for (const [percent, width] of [[200, 640], [400, 320]] as const) {
        await loginZoom.send('Emulation.setDeviceMetricsOverride', { width, height: 400, deviceScaleFactor: percent / 100, mobile: false });
        const measured = await page.evaluate(() => ({ width: window.innerWidth, dpr: window.devicePixelRatio, overflow: document.documentElement.scrollWidth - window.innerWidth }));
        expect(measured.width).toBe(width);
        expect(measured.dpr).toBe(percent / 100);
        expect(measured.overflow, `${percent}% zoom login`).toBeLessThanOrEqual(0);
      }
      await loginZoom.send('Emulation.clearDeviceMetricsOverride');
      await loginZoom.detach();
      await signIn(page, ownerPassword);
      await waitForSettledHome(page, householdName);
    } finally {
      n19PhaseActive = false;
      await fresh.dispose();
    }
    // Checked after the phase ends so every failure recorded during it is examined.
    const expectedN19Failures = [`GET /api/v1/households/${householdId}/items/summary 401`, `GET /api/v1/households/${householdId}/subjects 401`, `GET /api/v1/households/${householdId}/members 401`];
    // Which of the three household reads reach the server before sign-in replaces the screen is timing-dependent.
    expect(n19FailedResponses).toContain(expectedN19Failures[0]);
    expect(n19FailedResponses.filter((entry) => !expectedN19Failures.includes(entry)), 'n19 failures must be only session-ended household 401s').toEqual([]);
    sessionToken = (await page.context().cookies()).find((cookie) => cookie.name === 'tendo_session' || cookie.name === '__Host-tendo_session')?.value ?? '';
    expect(sessionToken).not.toBe('');
  });

  test('o. sign out returns to login and revokes the session server-side', async () => {
    await waitForSettledHome(page, householdName);
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
    await expectNoAxeViolations(page, 'login-error');
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

  test('n20. login recovers from a temporary sign-in options outage', async ({ browser }) => {
    const context = await browser.newContext({ baseURL, locale: 'en-US' });
    const loginPage = await context.newPage();
    watch(loginPage);
    let oidcRequests = 0;
    await loginPage.route(`${baseURL}/api/v1/auth/oidc`, async (route) => {
      if (route.request().method() !== 'GET') return route.continue();
      oidcRequests += 1;
      if (oidcRequests === 1) {
        n20OidcFailureActive = true;
        await route.fulfill({ status: 503, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'unavailable', status: 503 }) });
      } else {
        await route.continue();
      }
    });
    try {
      await loginPage.goto('/');
      await expect(loginPage.getByRole('heading', { level: 1, name: en['login.title'] })).toBeVisible();
      await expect(loginPage.getByRole('status')).toHaveText(en['login.oidc.loadError']);
      await expect(loginPage.getByRole('button', { name: en['login.oidc.submit'].replace('{provider}', 'OIDC Smoke Provider'), exact: true })).toHaveCount(0);
      await expectNoAxeViolations(loginPage, 'login-oidc-recovery');
      await loginPage.getByRole('button', { name: 'Čeština', exact: true }).click();
      await expect(loginPage.getByRole('heading', { level: 1, name: cs['login.title'] })).toBeVisible();
      await expect(loginPage.getByRole('status')).toHaveText(cs['login.oidc.loadError']);
      const cdp = await context.newCDPSession(loginPage);
      for (const [percent, width] of [[200, 640], [400, 320]] as const) {
        await cdp.send('Emulation.setDeviceMetricsOverride', { width, height: 400, deviceScaleFactor: percent / 100, mobile: false });
        const measured = await loginPage.evaluate(() => ({ cssWidth: window.innerWidth, dpr: window.devicePixelRatio, overflow: document.documentElement.scrollWidth - window.innerWidth }));
        expect(measured.cssWidth).toBe(width);
        expect(measured.dpr).toBe(percent / 100);
        expect(measured.overflow, `login ${percent}% overflow`).toBeLessThanOrEqual(0);
      }
      await cdp.detach();
      await loginPage.setViewportSize({ width: 320, height: 740 });
      const overflow = await loginPage.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
      expect(overflow, '320px horizontal overflow on Czech login').toBeLessThanOrEqual(0);

      await loginPage.getByRole('button', { name: cs['app.error.retry'], exact: true }).click();
      const providerButton = loginPage.getByRole('button', { name: cs['login.oidc.submit'].replace('{provider}', 'OIDC Smoke Provider'), exact: true });
      await expect(providerButton).toBeVisible();
      await expect(providerButton).toBeFocused();
      expect(oidcRequests).toBe(2);
    } finally {
      n20OidcFailureActive = false;
      await context.close();
    }
    expect(n20FailedResponses).toEqual(['GET /api/v1/auth/oidc 503']);
  });

  test('n21. exploratory accessibility evidence: route semantics, target sizing, and zoom layouts', async ({ browser }) => {
    test.setTimeout(90_000);
    const routes: Array<{ path: string; name: string; heading: string; content: (target: Page) => Locator }> = [
      { path: '/', name: 'home', heading: householdName, content: (target) => target.getByRole('main').getByRole('button', { name: en['items.add'], exact: true }) },
      { path: '/items', name: 'all-items', heading: en['allItems.title'], content: (target) => target.getByRole('region', { name: en['allItems.results'] }).getByRole('listitem').first() },
      { path: '/people', name: 'people', heading: en['subjects.title'], content: (target) => target.getByRole('main').getByRole('listitem').first() },
      { path: '/members', name: 'members', heading: en['members.title'], content: (target) => target.getByRole('main').getByRole('listitem').first() },
      { path: '/account', name: 'account', heading: en['account.title'], content: (target) => target.getByRole('main').getByRole('heading', { level: 2, name: en['account.preferences.title'] }) },
    ];
    // A route is ready when its h1 and route-specific content are shown and no loading status remains.
    const loadingTexts = [...new Set([en['app.loading'], en['home.loading'], en['items.loading'], en['members.loading'], en['subjects.loading'], en['account.oidc.loading']])];
    const expectReady = async (target: Page, route: { name: string; heading: string; content: (target: Page) => Locator }) => {
      await expect(target.getByRole('heading', { level: 1, name: route.heading }), `${route.name} heading`).toBeVisible();
      await expect(route.content(target), `${route.name} content`).toBeVisible();
      for (const text of loadingTexts) await expect(target.getByText(text, { exact: true }), `${route.name} still loading`).toHaveCount(0);
    };
    const measured: Array<{ route: string; zoom: number; cssWidth: number; dpr: number; bodyFont: number; overflow: number }> = [];
    const contrastEvidence: ContrastAuditRow[] = [];
    const focusEvidence: Array<{ route: string; stop: string; color: string; background: string; ratio: number | null; unobscured: boolean }> = [];
    const measureContrast = async (target: Page, route: string, expectBoundary = false): Promise<ContrastAuditRow[]> => {
      const rows = await auditContrast(target, route);
      const measuredText = rows.filter((row) => (row.kind === 'text' || row.kind === 'placeholder') && !row.exempt && row.ratio !== null);
      expect(measuredText.length, `${route} has a measured non-exempt text candidate`).toBeGreaterThan(0);
      if (expectBoundary) {
        const measuredBoundaries = rows.filter((row) => row.kind === 'boundary' && row.ratio !== null);
        expect(measuredBoundaries.length, `${route} has a measured control boundary candidate`).toBeGreaterThan(0);
      }
      contrastEvidence.push(...rows);
      return rows;
    };
    const context = await browser.newContext({ baseURL, locale: 'en-US', storageState: await page.context().storageState() });
    const auditPage = await context.newPage();
    const signedOutContext = await browser.newContext({ baseURL, locale: 'en-US' });
    const signedOut = await signedOutContext.newPage();
    const runAxe = async (target: Page, label: string) => {
      await expectNoAxeViolations(target, label);
    };
    const auditZoom = async (route: (typeof routes)[number], percent: 200 | 400) => {
      const width = 1280 / (percent / 100);
      const zoomContext = await browser.newContext({ baseURL, locale: 'en-US', viewport: { width: 1280, height: 800 }, storageState: await context.storageState() });
      const zoomPage = await zoomContext.newPage();
      const cdp = await zoomContext.newCDPSession(zoomPage);
      try {
        await zoomPage.goto(route.path);
        await expectReady(zoomPage, route);
        await cdp.send('Emulation.setDeviceMetricsOverride', { width, height: 400, deviceScaleFactor: percent / 100, mobile: false });
        const metrics = await zoomPage.evaluate(() => ({ cssWidth: window.innerWidth, dpr: window.devicePixelRatio, bodyFont: parseFloat(getComputedStyle(document.body).fontSize), overflow: document.documentElement.scrollWidth - window.innerWidth }));
        expect(metrics.cssWidth).toBe(width);
        expect(metrics.dpr).toBe(percent / 100);
        expect(metrics.overflow, `${route.name} ${percent}% overflow`).toBeLessThanOrEqual(0);
        measured.push({ route: route.name, zoom: percent, ...metrics });
      } finally {
        await zoomContext.close();
      }
    };
    // Keyboard sweep: Tab through the whole page from the top. Each stop is identified by a
    // stable per-element id, so reachability compares identities (not counts) and a revisit
    // before the footer is reported as a loop. A focus indicator counts only if the element's
    // outline or box shadow differs from its unfocused style, so a permanent shadow does not
    // pass. Each stop must be unobscured at its centre (2.4.11 proxy), every visible enabled
    // control in main must be reached, and the sweep must end on the footer language switch
    // (no keyboard trap, 2.1.2).
    const keyboardSweep = async (target: Page, label: string, mainControlsExpected = true) => {
      const expectedMain = await target.evaluate(() => {
        let next = 0;
        const ids: string[] = [];
        for (const element of document.querySelectorAll<HTMLElement>('main a[href], main button:not([disabled]), main input:not([disabled]), main select:not([disabled]), main textarea:not([disabled]), main [tabindex]:not([tabindex="-1"])')) {
          const box = element.getBoundingClientRect();
          if (box.width === 0 || box.height === 0 || element.closest('[inert],[hidden]') || getComputedStyle(element).visibility === 'hidden') continue;
          if (element.matches('input[type="radio"]') && !(element as HTMLInputElement).checked && document.querySelector(`input[type="radio"][name="${CSS.escape((element as HTMLInputElement).name)}"]:checked`)) continue; // one tab stop per radio group
          element.dataset.sweepId = `main-${next += 1}`;
          ids.push(element.dataset.sweepId);
        }
        return ids;
      });
      await target.evaluate(() => { (document.activeElement as HTMLElement | null)?.blur(); window.scrollTo(0, 0); });
      const stops: Array<{ id: string; name: string; region: string; focused: string; unfocused: string; unobscured: boolean; ringColor: string; ringBackgrounds: string[] }> = [];
      let loop = '';
      for (let index = 0; index < 150; index += 1) {
        await target.keyboard.press('Tab');
        // Focus styles transition (transition-colors includes outline-color); measure the settled ring, not a mid-transition colour.
        await target.waitForFunction(() => document.getAnimations().every((animation) => animation.playState !== 'running'));
        const stop = await target.evaluate((position) => {
          const element = document.activeElement as HTMLElement | null;
          if (!element || element === document.body) return null;
          element.dataset.sweepId ??= `other-${position}`;
          const indicator = () => {
            const style = getComputedStyle(element);
            const outline = style.outlineStyle === 'none' || parseFloat(style.outlineWidth) <= 0 || /rgba\([^)]*,\s*0\)|transparent/.test(style.outlineColor) ? 'none' : `${style.outlineStyle} ${style.outlineWidth} ${style.outlineColor}`;
            return `${outline}|${style.boxShadow}`;
          };
          const focused = indicator();
          const focusedStyle = getComputedStyle(element);
          let ringColor = focusedStyle.outlineColor;
          if (focusedStyle.outlineStyle === 'none' || parseFloat(focusedStyle.outlineWidth) <= 0) {
            const shadowColor = focusedStyle.boxShadow.match(/rgba?\([^)]*\)|#[0-9a-fA-F]{3,8}/);
            ringColor = shadowColor?.[0] ?? 'transparent';
          }
          // Normalize every colour (Chromium serializes color-mix() as oklab()) to rgba via canvas; fail closed on unknown values.
          const canvas = document.createElement('canvas');
          canvas.width = 1;
          canvas.height = 1;
          const context = canvas.getContext('2d', { willReadFrequently: true })!;
          const toRgba = (value: string) => {
            if (!CSS.supports('color', value)) return `unsupported ${value}`;
            context.clearRect(0, 0, 1, 1);
            context.fillStyle = value;
            context.fillRect(0, 0, 1, 1);
            const [r, g, b, alpha] = context.getImageData(0, 0, 1, 1).data;
            return `rgba(${r}, ${g}, ${b}, ${alpha / 255})`;
          };
          // Ancestor backgrounds root-first, composited in Node with the shared contrast module.
          const ringBackgrounds: string[] = [];
          for (let ancestor: Element | null = element.parentElement; ancestor; ancestor = ancestor.parentElement) ringBackgrounds.unshift(toRgba(getComputedStyle(ancestor).backgroundColor));
          const box = element.getBoundingClientRect();
          const hit = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2);
          const unobscured = !!hit && (hit === element || element.contains(hit) || hit.contains(element));
          element.blur();
          const unfocused = indicator();
          element.focus({ preventScroll: true });
          const region = element.closest('main') ? 'main' : element.closest('nav') ? 'nav' : element.closest('footer') ? 'footer' : element.closest('header') ? 'header' : 'other';
          return { id: element.dataset.sweepId, name: (element.getAttribute('aria-label') ?? element.textContent ?? element.tagName).trim().slice(0, 40), region, focused, unfocused, unobscured, ringColor: toRgba(ringColor), ringBackgrounds };
        }, index);
        if (!stop) break;
        if (stops.some((entry) => entry.id === stop.id)) { loop = `${stop.region}:${stop.name}`; break; }
        stops.push(stop);
        if (stop.region === 'footer' && stops.filter((entry) => entry.region === 'footer').length >= 2) break;
      }
      const focusRatios = stops.map((stop) => {
        const foreground = parseColor(stop.ringColor);
        const layers = stop.ringBackgrounds.map(parseColor);
        const background = layers.every((layer) => layer !== null) ? layers.reduce<Color>((backdrop, layer) => composite(layer!, backdrop), { r: 250, g: 247, b: 242, a: 1 }) : null;
        const ratio = foreground && background ? contrastRatio(composite(foreground, background), background) : null;
        const evidence = { route: label, stop: `${stop.region}:${stop.name}`, color: stop.ringColor, background: background ? colorString(background) : `unparsed ${stop.ringBackgrounds.join(' > ')}`, ratio, unobscured: stop.unobscured };
        focusEvidence.push(evidence);
        return evidence;
      });
      console.log(`focus-contrast ${label} ${JSON.stringify(focusRatios)}`);
      console.log(`keyboard-sweep ${label} ${JSON.stringify(stops.map((stop) => `${stop.region}:${stop.name}`))}`);
      expect(loop, `${label} focus revisited a stop before the footer (loop)`).toBe('');
      expect(stops.filter((stop) => stop.focused === stop.unfocused || stop.focused === 'none|none').map((stop) => `${stop.name} [${stop.focused}]`), `${label} stops whose focus does not change the outline or shadow`).toEqual([]);
      expect(stops.filter((stop) => !stop.unobscured).map((stop) => stop.name), `${label} obscured focus`).toEqual([]);
      const reachedMain = stops.filter((stop) => stop.region === 'main').map((stop) => stop.id);
      // Guards against an empty identity set making reachability vacuous.
      if (mainControlsExpected) expect(expectedMain.length, `${label} has controls in main`).toBeGreaterThan(0);
      else expect(expectedMain, `${label} has no controls in main`).toEqual([]);
      expect([...reachedMain].sort(), `${label} reaches every visible control in main, each once`).toEqual([...expectedMain].sort());
      expect(stops.filter((stop) => stop.region === 'footer').length, `${label} reaches the footer (no trap)`).toBe(2);
      const firstFooter = stops.findIndex((stop) => stop.region === 'footer');
      expect(stops.slice(firstFooter).every((stop) => stop.region === 'footer'), `${label} footer is last in order`).toBe(true);
    };
    try {
      const fixturePage = await context.newPage();
      try {
        await fixturePage.setContent(`
          <html>
            <body style="margin: 0; background: #faf7f2; color: #25302b">
              <input id="fx-transparent" style="background: transparent; border: 1px solid #d8d0c3">
              <input id="fx-value" value="Low contrast value" style="color: #c8c8c8; background: #fff; border: 1px solid #857c6e">
              <textarea id="fx-notes" style="color: #c8c8c8; background: #fff">Low contrast notes</textarea>
              <select id="fx-subject" style="color: #c8c8c8; background: #fff"><option>Low contrast option</option></select>
              <input id="fx-passing" value="Passing value" style="color: #25302b; background: #fff; border: 1px solid #857c6e">
            </body>
          </html>
        `);
        const fixtureRows = await auditContrast(fixturePage, 'auditor-self-test');
        const transparentBoundary = fixtureRows.find((row) => row.kind === 'boundary' && row.name === 'input#fx-transparent');
        expect(transparentBoundary, 'transparent fixture boundary is collected').toBeDefined();
        expect(transparentBoundary?.exempt, 'transparent fill cannot exempt its low-contrast border').toBe(false);
        expect(transparentBoundary?.ratio, 'transparent fixture border is below 3:1').toBeLessThan(3);

        const expectFailingText = (name: string) => {
          const row = fixtureRows.find((candidate) => candidate.kind === 'text' && candidate.name === name);
          expect(row, `${name} text is collected`).toBeDefined();
          expect(row?.exempt, `${name} is not exempt`).toBe(false);
          expect(row?.ratio, `${name} fails its text threshold`).toBeLessThan(row?.threshold ?? 0);
        };
        expectFailingText('input#fx-value[value]');
        expectFailingText('textarea#fx-notes[value]');
        expectFailingText('select#fx-subject[value]');

        const passingText = fixtureRows.find((row) => row.kind === 'text' && row.name === 'input#fx-passing[value]');
        const passingBoundary = fixtureRows.find((row) => row.kind === 'boundary' && row.name === 'input#fx-passing');
        expect(passingText, 'passing control text is collected').toBeDefined();
        expect(passingText?.exempt, 'passing control text is not exempt').toBe(false);
        expect(passingText?.ratio, 'passing control text meets its threshold').toBeGreaterThanOrEqual(passingText?.threshold ?? Infinity);
        expect(passingBoundary, 'passing control boundary is collected').toBeDefined();
        expect(passingBoundary?.ratio, 'passing control border meets its threshold').toBeGreaterThanOrEqual(passingBoundary?.threshold ?? Infinity);
      } finally {
        await fixturePage.close();
      }

      for (const route of routes) {
        await auditPage.setViewportSize({ width: 1280, height: 800 });
        await auditPage.goto(route.path);
        await expectReady(auditPage, route);
        await expect(auditPage).toHaveTitle(`${route.heading} — Tendo`);
        await measureContrast(auditPage, route.name, route.name === 'all-items');
        const aria = await auditPage.locator('body').ariaSnapshot();
        console.log(`aria-snapshot ${route.name}\n${aria}`);
        await test.info().attach(`aria-${route.name}.txt`, { body: aria, contentType: 'text/plain' });
        expect(aria, `${route.name} has main landmark`).toContain('main');
        const tooSmall = await auditPage.locator('a[href],button,input,select,textarea,[role="switch"],[role="radio"]').evaluateAll((nodes) => nodes.filter((node) => {
          if (node.matches('.sr-only')) return false;
          const rect = node.getBoundingClientRect();
          const hit = node.closest('label')?.getBoundingClientRect() ?? rect;
          return rect.width > 0 && rect.height > 0 && (hit.width < 24 - 1 / 64 || hit.height < 24 - 1 / 64);
        }).map((node) => ({ text: node.textContent?.trim(), label: node.getAttribute('aria-label'), rect: (node.closest('label') ?? node).getBoundingClientRect().toJSON() })));
        await test.info().attach(`targets-${route.name}.json`, { body: JSON.stringify(tooSmall, null, 2), contentType: 'application/json' });
        expect(tooSmall, `${route.name} WCAG 2.5.8 target-size review`).toEqual([]);
        await runAxe(auditPage, `${route.name}-default`);
        // Account's main is read-only for a password-only owner; its language switch lives in the footer.
        await keyboardSweep(auditPage, route.name, route.name !== 'account');
        for (const zoom of [200, 400] as const) await auditZoom(route, zoom);
      }
      // Measure form states separately from route-entry evidence, including populated control values.
      await auditPage.goto(routes[0].path);
      await expectReady(auditPage, routes[0]);
      await auditPage.getByRole('button', { name: en['items.add'], exact: true }).click();
      const addForm = auditPage.getByRole('form', { name: en['items.add.formLabel'] });
      await expect(addForm).toBeVisible();
      await addForm.getByLabel(en['items.title'], { exact: true }).fill('Contrast evidence item');
      await addForm.getByRole('switch', { name: en['items.repeat'], exact: true }).check();
      const addRows = await measureContrast(auditPage, 'home-add-form', true);
      expect(addRows.some((row) => row.kind === 'text' && row.name.endsWith('[value]')), 'home-add-form has a measured [value] text row').toBe(true);
      await addForm.getByRole('button', { name: en['items.cancel'], exact: true }).click();
      await expect(addForm).toHaveCount(0);

      await auditPage.goto(routes[1].path);
      await expectReady(auditPage, routes[1]);
      await auditPage.getByRole('region', { name: en['allItems.results'] }).getByRole('link').first().click();
      await expect(auditPage.getByRole('heading', { level: 1 })).toBeVisible();
      await auditPage.getByRole('button', { name: en['itemDetail.edit.button'], exact: true }).click();
      const editForm = auditPage.getByRole('form', { name: en['itemDetail.edit.formLabel'] });
      await expect(editForm).toBeVisible();
      const editRows = await measureContrast(auditPage, 'item-edit-form', true);
      expect(editRows.some((row) => row.kind === 'text' && row.name.endsWith('[value]')), 'item-edit-form has a measured [value] text row').toBe(true);
      await editForm.getByRole('button', { name: en['items.cancel'], exact: true }).click();
      await expect(editForm).toHaveCount(0);

      // An undone receipt is readable history, not an inactive control: it must meet 1.4.3 like any text.
      // n1 left "Book dental check" done with one undone and one active receipt.
      const undoneHistoryItem = await serverItem('Book dental check', true);
      const undoneReceipts = (await serverCompletions(undoneHistoryItem.id)).filter((completion) => completion.undoneAt !== null);
      expect(undoneReceipts.length, 'item detail fixture has an undone receipt').toBeGreaterThan(0);
      await auditPage.goto(`/items/${undoneHistoryItem.id}`);
      await expect(auditPage).toHaveTitle('Book dental check — Tendo');
      const historySection = auditPage.getByRole('main').getByRole('heading', { level: 2, name: en['itemDetail.history'], exact: true }).locator('xpath=ancestor::section[1]');
      const undoneDate = new Intl.DateTimeFormat('en-US', { dateStyle: 'medium', timeZone: 'UTC' }).format(new Date(`${undoneReceipts[0]!.completedOn}T00:00:00Z`));
      const undoneText = en['itemDetail.historyUndone'].replace('{date}', undoneDate);
      await expect(historySection.getByText(undoneText, { exact: true }).first()).toBeVisible();
      const historyRows = await measureContrast(auditPage, 'item-detail-undone-history');
      expect(historyRows.filter((row) => row.kind === 'text' && !row.exempt && row.ratio !== null).length, 'undone history text measured').toBeGreaterThan(0);

      // Readiness includes asynchronously loaded content: login's OIDC provider button.
      const signedOutRoutes: Array<{ path: string; name: string; findTitle: string; content: (target: Page) => Locator }> = [
        { path: '/', name: 'login', findTitle: en['login.title'], content: (target) => target.getByRole('button', { name: /^Sign in with / }) },
        { path: '/invite#' + 'A'.repeat(43), name: 'invitation-acceptance', findTitle: en['invite.title'], content: (target) => target.getByRole('button', { name: en['invite.create'], exact: true }) },
      ];
      for (const route of signedOutRoutes) {
        await signedOut.goto(route.path);
        await expect(signedOut.getByRole('main')).toBeVisible();
        await expect(signedOut.getByRole('heading', { level: 1, name: route.findTitle })).toBeVisible();
        await expect(route.content(signedOut), `${route.name} content`).toBeVisible();
        await measureContrast(signedOut, route.name, route.name === 'login' || route.name === 'invitation-acceptance');
        const aria = await signedOut.locator('body').ariaSnapshot();
        console.log(`aria-snapshot ${route.name}\n${aria}`);
        await test.info().attach(`aria-${route.name}.txt`, { body: aria, contentType: 'text/plain' });
        const targets = await signedOut.locator('a[href],button,input,select,textarea,[role="switch"],[role="radio"]').evaluateAll((nodes) => nodes.filter((node) => {
          if (node.matches('.sr-only')) return false;
          const box = node.getBoundingClientRect();
          return box.width > 0 && box.height > 0 && (box.width < 24 - 1 / 64 || box.height < 24 - 1 / 64);
        }).map((node) => ({ name: node.getAttribute('aria-label') || node.textContent?.trim(), rect: node.getBoundingClientRect().toJSON() })));
        await test.info().attach(`targets-${route.name}.json`, { body: JSON.stringify(targets, null, 2), contentType: 'application/json' });
        expect(targets, `${route.name} target size >=24`).toEqual([]);
        await runAxe(signedOut, `${route.name}-default`);
        await keyboardSweep(signedOut, route.name);
        for (const percent of [200, 400] as const) {
          const width = 1280 / (percent / 100);
          const zoomContext = await browser.newContext({ baseURL, viewport: { width: 1280, height: 800 } });
          const zoomPage = await zoomContext.newPage();
          const zoomCDP = await zoomContext.newCDPSession(zoomPage);
          try {
            await zoomPage.goto(route.path);
            await expect(zoomPage.getByRole('heading', { level: 1, name: route.findTitle })).toBeVisible();
            await zoomCDP.send('Emulation.setDeviceMetricsOverride', { width, height: 400, deviceScaleFactor: percent / 100, mobile: false });
            const metrics = await zoomPage.evaluate(() => ({ cssWidth: window.innerWidth, dpr: window.devicePixelRatio, bodyFont: parseFloat(getComputedStyle(document.body).fontSize), overflow: document.documentElement.scrollWidth - window.innerWidth }));
            expect(metrics.cssWidth).toBe(width);
            expect(metrics.dpr).toBe(percent / 100);
            expect(metrics.overflow, `${route.name} ${percent}% overflow`).toBeLessThanOrEqual(0);
            measured.push({ route: route.name, zoom: percent, ...metrics });
          } finally { await zoomContext.close(); }
        }
      }
      const contrastPairs = [...new Set(contrastEvidence.map((row) => `${row.foreground} on ${row.background}`))];
      const contrastSummary = contrastPairs.map((pair) => {
        const matchingRows = contrastEvidence.filter((row) => `${row.foreground} on ${row.background}` === pair);
        const measuredRatios = matchingRows.flatMap((row) => row.ratio === null ? [] : [row.ratio]);
        return {
          pair,
          minRatio: measuredRatios.length > 0 ? Math.min(...measuredRatios) : null,
          measured: matchingRows.length,
        };
      });
      console.log(`contrast-summary ${JSON.stringify(contrastSummary)}`);
      const contrastFailures = contrastEvidence.filter((row) => !row.exempt && row.ratio !== null && !passesContrastThreshold(row.ratio, row.threshold));
      console.log(`contrast-failures ${JSON.stringify(contrastFailures.map((row) => ({ route: row.route, element: row.name, foreground: row.foreground, background: row.background, ratio: row.ratio, threshold: row.threshold })))}`);
      expect(contrastShortfalls(contrastEvidence), 'contrast failures').toEqual([]);
      const focusFailures = focusEvidence.filter((row) => row.ratio === null || row.ratio < 3);
      console.log(`focus-contrast-failures ${JSON.stringify(focusFailures)}`);
      expect(focusFailures, 'focus ring contrast').toEqual([]);
      await test.info().attach('contrast-evidence-n21.json',{body:JSON.stringify({summary:contrastSummary,measurements:contrastEvidence},null,2),contentType:'application/json'});
      console.log(`zoom-measurements ${JSON.stringify(measured)}`);
      await test.info().attach('all-route-zoom-measurements.json', { body: JSON.stringify(measured, null, 2), contentType: 'application/json' });
    } finally {
      await signedOutContext.close();
      await context.close();
      await page.setViewportSize({ width: 1280, height: 720 });
    }
  });

  // Seeds 130 subjects and 55 invitations; the two-member household exercises only the Members
  // page shell, while member paging itself is covered by MembersScreen component tests.
  test('n22. People and things and Members page large collections one page at a time', async () => {
    test.setTimeout(180_000);
    const subjectsPath = `/api/v1/households/${householdId}/subjects`;
    const invitationsPath = `/api/v1/households/${householdId}/invitations`;
    const seededSubjects: Array<{ id: string; name: string }> = [];
    const createdInvitations: string[] = [];
    let primaryFailure: unknown;
    let cleanupFailure: unknown;
    let subjectListGets = 0;
    const onRequest = (request: import('@playwright/test').Request) => {
      const url = new URL(request.url());
      if (request.method() === 'GET' && url.pathname === subjectsPath && !url.searchParams.has('id')) subjectListGets += 1;
    };
    const walkSubjects = async () => {
      const result: Array<{ id: string; name: string }> = [];
      let cursor: string | undefined;
      do {
        const response = await page.request.get(subjectsPath, { params: { archived: 'false', limit: '100', ...(cursor ? { cursor } : {}) } });
        expect(response.status()).toBe(200);
        const body = await response.json() as { items: Array<{ id: string; name: string }>; nextCursor: string | null };
        result.push(...body.items);
        cursor = body.nextCursor ?? undefined;
      } while (cursor);
      return result;
    };
    const walkInvitations = async () => {
      const result: Array<{ id: string; status: string }> = []; 
      let cursor: string | undefined;
      do {
        const response = await page.request.get(invitationsPath, { params: { limit: '100', ...(cursor ? { cursor } : {}) } });
        expect(response.status()).toBe(200);
        const body = await response.json() as { items: Array<{ id: string; status: string }>; nextCursor: string | null };
        result.push(...body.items);
        cursor = body.nextCursor ?? undefined;
      } while (cursor);
      return result;
    };
    try {
      await page.goto('/');
      await waitForSettledHome(page, householdName);
      const preexistingCount = (await walkSubjects()).length;
      for (let start = 0; start < 130; start += 25) {
        const batch = Array.from({ length: Math.min(25, 130 - start) }, (_, offset) => start + offset);
        const responses = await Promise.all(batch.map((index) => page.request.post(subjectsPath, {
          headers: { Origin: baseURL, 'Idempotency-Key': `e2e-n22-subject-${index}` },
          data: { name: `N22 subject ${String(index).padStart(3, '0')}`, type: 'custom' },
        })));
        for (let index = 0; index < responses.length; index += 1) {
          expect(responses[index]!.status(), `seed subject ${start + index}`).toBe(201);
          const created = await responses[index]!.json() as { id: string; name: string };
          seededSubjects.push({ id: created.id, name: created.name });
        }
      }
      const serverPageWalk = await walkSubjects();
      expect(serverPageWalk).toHaveLength(preexistingCount + 130);
      const serverIds = serverPageWalk.map((subject) => subject.id);
      const expectedIds = new Set(serverIds);
      expect(expectedIds.size).toBe(serverIds.length);

      page.on('request', onRequest);
      subjectListGets = 0;
      await page.getByRole('link', { name: en['home.people'], exact: true }).click();
      const main = page.getByRole('main');
      await expect(page.getByRole('heading', { level: 1, name: en['subjects.title'] })).toBeVisible();
      const subjectRows = main.locator('[data-focus^="row:"]');
      const pageOneIds = await subjectRows.evaluateAll((rows) => rows.map((row) => row.getAttribute('data-focus')!.slice('row:'.length)));
      expect(pageOneIds.length).toBeLessThanOrEqual(50);
      await expect(main.getByText(en['subjects.page.number'].replace('{n}', '1'), { exact: true })).toBeVisible();
      await expect(main.getByRole('button', { name: en['subjects.page.next'], exact: true })).toBeVisible();
      await expect(main.getByRole('button', { name: en['subjects.page.first'], exact: true })).toHaveCount(0);
      expect(subjectListGets).toBe(1);

      const apiPages: Array<{ ids: string[]; nextCursor: string | null }> = [];
      let apiCursor: string | undefined;
      do {
        const response = await page.request.get(subjectsPath, { params: { archived: 'false', limit: '50', ...(apiCursor ? { cursor: apiCursor } : {}) } });
        expect(response.status()).toBe(200);
        const body = await response.json() as { items: Array<{ id: string }>; nextCursor: string | null };
        apiPages.push({ ids: body.items.map((subject) => subject.id), nextCursor: body.nextCursor });
        apiCursor = body.nextCursor ?? undefined;
      } while (apiCursor);
      const seenIds = new Set<string>();
      let walkedCount = 0;
      let pageNumber = 1;
      let lastPageIds = pageOneIds;
      while (true) {
        const ids = await subjectRows.evaluateAll((rows) => rows.map((row) => row.getAttribute('data-focus')!.slice('row:'.length)));
        const expectedPageIds = apiPages[pageNumber - 1]?.ids ?? [];
        expect(ids.length).toBe(apiPages[pageNumber - 1]?.ids.length ?? 0);
        expect(ids).toEqual(expectedPageIds);
        expect(ids.length).toBeLessThanOrEqual(50);
        for (const id of ids) { expect(seenIds.has(id), `subject ${id} is not repeated`).toBe(false); seenIds.add(id); }
        walkedCount += ids.length;
        lastPageIds = ids;
        const next = main.getByRole('button', { name: en['subjects.page.next'], exact: true });
        if (!(await next.isVisible().catch(() => false))) {
          expect(apiPages[pageNumber - 1]?.nextCursor).toBeNull();
          break;
        }
        expect(apiPages[pageNumber - 1]?.nextCursor).not.toBeNull();
        pageNumber += 1;
        const before = subjectListGets;
        await next.click();
        await expect(main.getByText(en['subjects.page.number'].replace('{n}', String(pageNumber)), { exact: true })).toBeVisible();
        await expect.poll(() => subjectListGets).toBe(before + 1);
        expect(subjectListGets).toBe(pageNumber);
      }
      expect(walkedCount).toBe(serverPageWalk.length);
      expect(seenIds).toEqual(expectedIds);
      const domCount = await page.locator('*').count();
      console.log(`n22 last-page DOM elements=${domCount} subjects=${lastPageIds.length}`);
      expect(domCount).toBeLessThan(1500);

      if (pageNumber > 1) {
        while (pageNumber > 1) {
          const previousLabel = pageNumber === 2 ? en['subjects.page.first'] : en['subjects.page.previous'];
          await main.getByRole('button', { name: previousLabel, exact: true }).click();
          pageNumber -= 1;
          const expectedPreviousIds = apiPages[pageNumber - 1]?.ids ?? [];
          await expect.poll(async () => subjectRows.evaluateAll((rows) => rows.map((row) => row.getAttribute('data-focus')!.slice('row:'.length)))).toEqual(expectedPreviousIds);
          if (pageNumber > 1) await expect(main.getByText(en['subjects.page.number'].replace('{n}', String(pageNumber)), { exact: true })).toBeVisible();
        }
        const restoredIds = await subjectRows.evaluateAll((rows) => rows.map((row) => row.getAttribute('data-focus')!.slice('row:'.length)));
        expect(restoredIds).toEqual(apiPages[0]!.ids);
      }

      if (serverPageWalk.length > 50) {
        const next = main.getByRole('button', { name: en['subjects.page.next'], exact: true });
        await next.focus();
        await page.keyboard.press('Enter');
        const secondRows = main.locator('[data-focus^="row:"]');
        await expect.poll(async () => secondRows.first().evaluate((element) => document.activeElement === element)).toBe(true);
      }

      const invitationIdsBefore = new Set((await walkInvitations()).map((invitation) => invitation.id));
      for (let start = 0; start < 55; start += 25) {
        const batch = Array.from({ length: Math.min(25, 55 - start) }, (_, offset) => start + offset);
        const responses = await Promise.all(batch.map((index) => page.request.post(invitationsPath, {
          headers: { Origin: baseURL, 'Idempotency-Key': `e2e-n22-invite-${index}` }, data: {},
        })));
        for (let offset = 0; offset < responses.length; offset += 1) {
          expect(responses[offset]!.status(), `seed invitation ${start + offset}`).toBe(201);
          const invitation = await responses[offset]!.json() as { id: string };
          createdInvitations.push(invitation.id);
        }
      }
      const invitationWalk = await walkInvitations();
      const newInvitationIds = invitationWalk.map((invitation) => invitation.id).filter((id) => !invitationIdsBefore.has(id));
      expect(newInvitationIds).toHaveLength(55);
      expect(new Set(newInvitationIds).size).toBe(55);
      expect(invitationWalk.filter((invitation) => newInvitationIds.includes(invitation.id)).every((invitation) => invitation.status === 'pending')).toBe(true);
      const expectedInvitationPageTwoSize = invitationWalk.length - 50;

      await page.getByRole('link', { name: en['home.members'], exact: true }).click();
      const memberSection = page.getByRole('heading', { name: en['members.list.title'] }).locator('..');
      const memberRows = memberSection.locator('ul').first().getByRole('listitem');
      await expect.poll(async () => memberRows.count()).toBe(2);
      const memberPageOne = await memberRows.allTextContents();
      const invitationHeading = page.getByRole('heading', { name: en['members.invitations.title'] });
      const invitationSection = invitationHeading.locator('..');
      const invitationRows = invitationSection.locator('ul').getByRole('listitem');
      await expect.poll(async () => invitationRows.count()).toBe(50);
      const firstInvitationRows = await invitationRows.allTextContents();
      expect(firstInvitationRows.length).toBe(50);
      await page.getByRole('button', { name: en['members.page.next'], exact: true }).click();
      await expect.poll(async () => invitationRows.count()).toBe(expectedInvitationPageTwoSize);
      const secondInvitationPage = await invitationRows.allTextContents();
      expect(secondInvitationPage).toHaveLength(expectedInvitationPageTwoSize);
      expect(await memberRows.allTextContents()).toEqual(memberPageOne);
      expect(firstInvitationRows.length + secondInvitationPage.length).toBe(invitationWalk.length);
      const secondStatuses = secondInvitationPage.map((row) => row.slice(0, row.indexOf('Created')));
      const statusText: Record<string, string> = { pending: en['members.invitation.status.pending'], accepted: en['members.invitation.status.accepted'], revoked: en['members.invitation.status.revoked'], expired: en['members.invitation.status.expired'] };
      expect(secondStatuses).toEqual(invitationWalk.slice(50).map((invitation) => statusText[invitation.status]));

      await page.getByRole('link', { name: en['home.people'], exact: true }).click();
      const peopleMain = page.getByRole('main');
      await peopleMain.getByRole('button', { name: en['subjects.page.next'], exact: true }).click();
      await expect(peopleMain.getByRole('button', { name: en['subjects.page.first'], exact: true })).toBeVisible();
      await page.getByRole('button', { name: 'Čeština', exact: true }).click();
      await expect(peopleMain.getByText(cs['subjects.page.number'].replace('{n}', '2'), { exact: true })).toBeVisible();
      await expect(peopleMain.getByRole('button', { name: cs['subjects.page.first'], exact: true })).toBeVisible();
      await page.getByRole('button', { name: 'English', exact: true }).click();
      await page.setViewportSize({ width: 320, height: 740 });
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
      expect(overflow, '320px People and things overflow on page two').toBeLessThanOrEqual(0);
      const pagingButtons = peopleMain.locator('[data-subject-page]');
      for (const button of await pagingButtons.all()) {
        await button.scrollIntoViewIfNeeded();
        const box = await button.boundingBox();
        expect(box).not.toBeNull();
        expect(box!.x).toBeGreaterThanOrEqual(0);
        expect(box!.x + box!.width).toBeLessThanOrEqual(320);
        expect(box!.y).toBeGreaterThanOrEqual(0);
        expect(box!.y + box!.height).toBeLessThanOrEqual(740);
      }
    } catch (error) {
      primaryFailure = error;
    } finally {
      page.off('request', onRequest);
      for (const subject of seededSubjects) {
        try {
          const current = await page.request.get(`${subjectsPath}/${subject.id}`, { headers: { Origin: baseURL } });
          if (current.status() !== 200) { cleanupFailure ??= new Error(`Could not read n22 subject ${subject.id}: ${current.status()}`); continue; }
          const etag = current.headers()['etag'];
          const archived = await page.request.patch(`${subjectsPath}/${subject.id}`, { headers: { Origin: baseURL, 'If-Match': etag }, data: { archived: true } });
          if (archived.status() !== 200) cleanupFailure ??= new Error(`Could not archive n22 subject ${subject.id}: ${archived.status()}`);
        } catch (error) { cleanupFailure ??= error; }
      }
      for (const invitationId of createdInvitations) {
        try {
          const revoked = await page.request.delete(`${invitationsPath}/${invitationId}`, { headers: { Origin: baseURL } });
          if (revoked.status() !== 204) cleanupFailure ??= new Error(`Could not revoke n22 invitation ${invitationId}: ${revoked.status()}`);
        } catch (error) { cleanupFailure ??= error; }
      }
      await page.setViewportSize({ width: 1280, height: 800 }).catch((error) => { cleanupFailure ??= error; });
    }
    if (primaryFailure) throw primaryFailure;
    if (cleanupFailure) throw cleanupFailure;
  });

  // G7 browser evidence: a runner-seeded, isolated household (scripts/e2e_db_fixtures.py) holds 1001 people,
  // 1002 members and 1001 pending invitations; every UI page must equal the matching authenticated API page.
  test('n28. People, Members and invitations walk 1000+ rows with bounded pages and stable pager history', async ({ browser }) => {
    test.setTimeout(120_000);
    const large = `/api/v1/households/${largeHouseholdId}`;
    const context = await browser.newContext({ baseURL, locale: 'en-US', timezoneId: 'UTC' });
    const actor = await context.newPage();
    const errors: string[] = [];
    actor.on('pageerror', (error) => errors.push(error.message));
    // Seeded invitations were created on distinct days, so each row's "Created" date identifies it.
    const createdDate = new Intl.DateTimeFormat('en-US', { dateStyle: 'medium', timeZone: 'UTC' });
    type Row = { id: string; label: string };
    const apiPages = async (path: string, query: Record<string, string> = {}) => {
      const pages: Row[][] = [];
      let cursor: string | undefined;
      do {
        const response = await actor.request.get(path, { params: { limit: '50', ...query, ...(cursor ? { cursor } : {}) } });
        expect(response.status(), `n28 API ${path}`).toBe(200);
        const body = await response.json() as { items: Array<{ id?: string; userId?: string; login?: string; name?: string; createdAt?: string }>; nextCursor: string | null };
        pages.push(body.items.map((item) => ({
          id: item.id ?? item.userId!,
          label: item.name ?? item.login ?? en['members.invitation.created'].replace('{date}', createdDate.format(new Date(item.createdAt!))),
        })));
        cursor = body.nextCursor ?? undefined;
      } while (cursor);
      return pages;
    };
    const walk = async (name: 'subjects' | 'members' | 'invitations', expected: Row[][]) => {
      const kind = name === 'subjects' ? 'subject' : name === 'members' ? 'member' : 'invitation';
      const prefix = name === 'subjects' ? 'subjects' : 'members';
      const responsePath = `${large}/${name}`;
      const responses: string[][] = [];
      const onResponse = async (response: import('@playwright/test').Response) => {
        if (new URL(response.url()).pathname !== responsePath || response.request().method() !== 'GET') return;
        const body = await response.json().catch(() => ({ items: [] })) as { items: Array<{ id?: string; userId?: string }> };
        responses.push(body.items.map((item) => item.id ?? item.userId!));
      };
      actor.on('response', onResponse);
      try {
        await actor.goto(name === 'subjects' ? '/people' : '/members');
        const main = actor.getByRole('main');
        const scope = name === 'subjects' ? main
          : main.getByRole('heading', { name: en[name === 'members' ? 'members.list.title' : 'members.invitations.title'], exact: true }).locator('xpath=ancestor::section[1]');
        const rows = name === 'subjects' ? scope.locator('[data-focus^="row:"]') : scope.locator('ul').last().locator(':scope > li');
        const pager = scope.locator(`button[data-${kind}-page]`);
        const next = pager.filter({ hasText: en[`${prefix}.page.next`] });
        const visible = async (): Promise<string[]> => name === 'subjects'
          ? rows.evaluateAll((els) => els.map((el) => el.getAttribute('data-focus')!.slice('row:'.length)))
          : rows.evaluateAll((els) => els.map((el) => (el.textContent ?? '').trim()));
        const expectPage = async (index: number, label: string) => {
          const page = expected[index]!;
          if (name === 'subjects') await expect.poll(visible, { message: `n28 ${label} subject IDs` }).toEqual(page.map((row) => row.id));
          else if (name === 'members') {
            // The first span of each member row is the login, with " (you)" for the signed-in owner.
            const logins = () => rows.evaluateAll((els) => els.map((el) => el.querySelector(':scope > span')?.textContent ?? ''));
            await expect.poll(logins, { message: `n28 ${label} member logins in API order` })
              .toEqual(page.map((row) => (row.label === largeOwnerLogin ? `${row.label} (${en['members.you']})` : row.label)));
          } else {
            // Each row shows its status, then "Created {date} · Expires {date}".
            const created = () => rows.evaluateAll((els) => els.map((el) => (el.querySelector('p + p')?.textContent ?? '').split(' · ')[0]));
            await expect.poll(created, { message: `n28 ${label} invitation created dates in API order` }).toEqual(page.map((row) => row.label));
            expect((await visible()).every((text) => text.startsWith(en['members.invitation.status.pending'])), `n28 ${label} invitations pending`).toBe(true);
          }
          await expect(scope.getByText(en[`${prefix}.page.number`].replace('{n}', String(index + 1)), { exact: true })).toBeVisible();
        };
        for (let index = 0; index < expected.length; index += 1) {
          await expectPage(index, `${name} page ${index + 1}`);
          await expect.poll(() => responses.length, { message: `n28 ${name} one request per page` }).toBe(index + 1);
          expect(responses[index], `n28 ${name} page ${index + 1} response equals API walk`).toEqual(expected[index]!.map((row) => row.id));
          expect(await actor.locator('*').count(), `n28 ${name} page ${index + 1} DOM bounded`).toBeLessThan(1500);
          if (index < expected.length - 1) await next.click();
        }
        await expect(next, `n28 ${name} last page has no Next`).toHaveCount(0);
        const seen = responses.flat();
        expect(seen.length, `n28 ${name} walk length`).toBe(expected.flat().length);
        expect(new Set(seen).size, `n28 ${name} no duplicates`).toBe(seen.length);
        // Previous restores the exact earlier page from the cursor stack, then Next returns to the last page.
        const last = expected.length - 1;
        const revisit = async (index: number, count: number, label: string) => {
          await expectPage(index, `${name} ${label}`);
          await expect.poll(() => responses.length, { message: `n28 ${name} ${label} fetched once` }).toBe(count);
          expect(responses[count - 1], `n28 ${name} ${label} response equals API walk`).toEqual(expected[index]!.map((row) => row.id));
        };
        await pager.filter({ hasText: en[`${prefix}.page.previous`] }).click();
        await revisit(last - 1, expected.length + 1, 'previous');
        await next.click();
        await revisit(last, expected.length + 2, 'next again');
      } finally {
        actor.off('response', onResponse);
      }
    };
    try {
      const login = await actor.request.post('/api/v1/session', { headers: { Origin: baseURL }, data: { login: largeOwnerLogin, password: existingAccountPassword } });
      expect(login.status(), 'n28 large-household owner signs in').toBe(201);
      const subjects = await apiPages(`${large}/subjects`, { archived: 'false' });
      const members = await apiPages(`${large}/members`);
      const invitations = await apiPages(`${large}/invitations`);
      for (const [name, pages, total] of [['subjects', subjects, 1001], ['members', members, 1002], ['invitations', invitations, 1001]] as const) {
        expect(pages.flat().length, `n28 ${name} fixture size`).toBe(total);
        expect(new Set(pages.flat().map((row) => row.id)).size, `n28 ${name} fixture IDs unique`).toBe(total);
        expect(pages.slice(0, -1).every((page) => page.length === 50), `n28 ${name} full 50-row pages`).toBe(true);
        expect(pages.at(-1)!.length, `n28 ${name} final page past the 1000-row boundary`).toBe(total - 1000);
      }
      await walk('subjects', subjects);
      await walk('members', members);
      await walk('invitations', invitations);
      expect(errors, 'n28 no page errors').toEqual([]);
    } finally {
      await actor.request.delete('/api/v1/session', { headers: { Origin: baseURL } }).catch(() => undefined);
      await context.close();
    }
  });

  // n14/n21 emulate zoom with CDP device metrics. This test uses Chromium's own page zoom instead: the
  // full Chromium build (channel "chromium", not the headless shell) launched with the profile preference
  // that chrome://settings "Page zoom" writes (partition.default_zoom_level, log base 1.2 of the factor),
  // with no viewport emulation. The browser itself then lays out a 1280 px window at 640 CSS px and
  // DPR 2 (200 %) or 320 CSS px and DPR 4 (400 %), as when a user picks that zoom level.
  test('n23. real browser page zoom: sign in and manage an item at 200 %, every household route at 400 %', async () => {
    test.setTimeout(180_000);
    const title = 'Native zoom: clean the cooker hood';
    const renamed = 'Native zoom: clean the cooker hood filter';
    const anchor = addDays(pragueToday(), -3);
    const measured: Array<{ zoom: number; state: string; cssWidth: number; dpr: number; outerWidth: number; bodyFont: number; overflow: number }> = [];
    const profiles: string[] = [];
    const contexts: BrowserContext[] = [];
    const launchAtZoom = async (percent: 200 | 400) => {
      const profile = mkdtempSync(join(tmpdir(), `tendo-zoom-${percent}-`));
      profiles.push(profile);
      mkdirSync(join(profile, 'Default'));
      writeFileSync(join(profile, 'Default', 'Preferences'), JSON.stringify({ partition: { default_zoom_level: { x: Math.log(percent / 100) / Math.log(1.2) } } }));
      const context = await chromium.launchPersistentContext(profile, { channel: 'chromium', headless: true, viewport: null, locale: 'en-US', baseURL, args: ['--window-size=1280,900'] });
      contexts.push(context);
      const zoomed = context.pages()[0] ?? await context.newPage();
      await trackInFlightApiFetches(zoomed);
      watch(zoomed, { fullChromium: true });
      return { context, zoomed };
    };
    const measure = async (target: Page, percent: 200 | 400, state: string) => {
      const metrics = await target.evaluate(() => ({ cssWidth: window.innerWidth, dpr: window.devicePixelRatio, outerWidth: window.outerWidth, bodyFont: parseFloat(getComputedStyle(document.body).fontSize), overflow: document.documentElement.scrollWidth - window.innerWidth }));
      measured.push({ zoom: percent, state, ...metrics });
      // The window stays 1280 px wide; only the browser's zoom shrinks the CSS layout width.
      expect(metrics.outerWidth, `${state} window width`).toBe(1280);
      expect(metrics.cssWidth, `${state} layout width at ${percent}%`).toBe(1280 / (percent / 100));
      expect(metrics.dpr, `${state} device pixel ratio at ${percent}%`).toBe(percent / 100);
      expect(metrics.bodyFont, `${state} body text`).toBe(16);
      expect(metrics.overflow, `${state} horizontal overflow at ${percent}%`).toBeLessThanOrEqual(0);
    };
    const inView = async (control: Locator, label: string) => {
      await control.scrollIntoViewIfNeeded();
      // ratio 1: the whole control must lie inside the viewport, not merely touch it.
      await expect(control, label).toBeInViewport({ ratio: 1 });
      const box = await control.boundingBox();
      expect(box, `${label} has a box`).not.toBeNull();
      expect(box!.x, `${label} left edge`).toBeGreaterThanOrEqual(0);
      expect(box!.x + box!.width, `${label} right edge`).toBeLessThanOrEqual(await control.page().evaluate(() => window.innerWidth));
    };
    const currentItem = async (id: string) => {
      const response = await page.request.get(`/api/v1/households/${householdId}/items/${id}`);
      expect(response.status()).toBe(200);
      return (await response.json()) as ServerItem & { workflowState: string; archived: boolean };
    };
    let itemId = '';
    let primaryFailure: unknown;
    try {
      // 200 %: sign in, then add, complete, undo, pause, resume, edit, archive and restore one item.
      const { context, zoomed } = await launchAtZoom(200);
      const main = zoomed.getByRole('main');
      await zoomed.goto('/');
      await expect(zoomed.getByRole('heading', { level: 1, name: en['login.title'] })).toBeVisible();
      await measure(zoomed, 200, 'login');
      await inView(zoomed.getByRole('button', { name: en['login.submit'], exact: true }), 'Sign in at 200%');
      await signIn(zoomed, ownerPassword);
      await waitForSettledHome(zoomed, householdName);
      await measure(zoomed, 200, 'home');

      await main.getByRole('button', { name: en['items.add'], exact: true }).click();
      const addForm = main.getByRole('form', { name: en['items.add.formLabel'] });
      await expect(addForm.getByLabel(en['items.title'])).toBeFocused();
      await addForm.getByLabel(en['items.title']).fill(title);
      await addForm.getByLabel(en['items.subject'], { exact: true }).selectOption({ label: 'Anička' });
      await addForm.getByLabel(en['items.attentionDate']).fill(anchor);
      await addForm.getByRole('switch', { name: en['items.repeat'], exact: true }).check();
      await addForm.getByRole('spinbutton').fill('3');
      await addForm.getByRole('combobox', { name: en['items.intervalUnit'] }).selectOption('week');
      await addForm.getByRole('switch', { name: en['items.fluid'] }).check();
      await measure(zoomed, 200, 'home add form with Repeat and Fluid on');
      await inView(addForm.getByRole('button', { name: en['items.add.submit'], exact: true }), 'Add item submit at 200%');
      await inView(addForm.getByRole('button', { name: en['items.cancel'], exact: true }), 'Cancel at 200%');
      await addForm.getByRole('button', { name: en['items.add.submit'], exact: true }).click();
      await expect(addForm).toHaveCount(0);
      const created = await serverItem(title);
      itemId = created.id;
      expect(created).toMatchObject({ attentionOn: anchor, attention: 'needs_attention', recurrence: { intervalValue: 3, intervalUnit: 'week', mode: 'after_completion' } });

      const homeRow = main.getByRole('listitem').filter({ hasText: title });
      await expect(homeRow).toBeVisible();
      await measure(zoomed, 200, 'home with the new row');
      const done = homeRow.getByRole('button', { name: en['items.done.label'].replace('{title}', title), exact: true });
      await inView(done, 'Done at 200%');
      await done.click();
      await expect(zoomed.getByRole('status').filter({ hasText: title })).toContainText('Next time:');
      await measure(zoomed, 200, 'home with the completion notice');
      await expect.poll(async () => (await serverCompletions(itemId)).length).toBe(1);
      const [receipt] = await serverCompletions(itemId);
      expect(receipt).toMatchObject({ cycleAttentionOn: anchor, nextAttentionOn: addDays(receipt.completedOn, 21), undoneAt: null });
      const undo = zoomed.getByRole('button', { name: en['items.undo'], exact: true });
      await inView(undo, 'Undo at 200%');
      await undo.click();
      await expect.poll(async () => (await serverCompletions(itemId))[0]?.undoneAt ?? null).not.toBeNull();
      expect(await currentItem(itemId)).toMatchObject({ attentionOn: anchor, lastCompletedOn: null, done: false });
      await expect(homeRow).toBeVisible();

      await zoomed.getByRole('link', { name: en['itemDetail.open'].replace('{title}', title) }).click();
      await expect(zoomed.getByRole('heading', { level: 1, name: title })).toBeVisible();
      await waitForSettledHouseholdRequests(zoomed);
      await measure(zoomed, 200, 'item detail');
      for (const [action, state] of [[en['itemDetail.pause'], 'paused'], [en['itemDetail.resume'], 'open']] as const) {
        const button = main.getByRole('button', { name: action, exact: true });
        await inView(button, `${action} at 200%`);
        await button.click();
        await expect.poll(async () => (await currentItem(itemId)).workflowState).toBe(state);
        await waitForSettledHouseholdRequests(zoomed);
        await measure(zoomed, 200, `item detail ${state}`);
      }
      const edit = main.getByRole('button', { name: en['itemDetail.edit.button'], exact: true });
      await inView(edit, 'Edit at 200%');
      await edit.click();
      const editForm = main.getByRole('form', { name: en['itemDetail.edit.formLabel'] });
      await expect(editForm.getByLabel(en['items.title'])).toHaveValue(title);
      await editForm.getByLabel(en['items.title']).fill(renamed);
      await measure(zoomed, 200, 'item edit form');
      const save = editForm.getByRole('button', { name: en['itemDetail.save'], exact: true });
      await inView(save, 'Save changes at 200%');
      await save.click();
      await expect(editForm).toHaveCount(0);
      await expect(zoomed.getByRole('heading', { level: 1, name: renamed })).toBeVisible();
      expect(await currentItem(itemId)).toMatchObject({ title: renamed, attentionOn: anchor, recurrence: { intervalValue: 3, intervalUnit: 'week', mode: 'after_completion' } });
      const archive = main.getByRole('button', { name: en['subjects.archive'], exact: true });
      await inView(archive, 'Archive at 200%');
      await archive.click();
      await expect.poll(async () => (await currentItem(itemId)).archived).toBe(true);
      const restore = main.getByRole('button', { name: en['subjects.restore'], exact: true });
      await expect(restore).toBeVisible();
      await waitForSettledHouseholdRequests(zoomed);
      await measure(zoomed, 200, 'item detail archived');
      await inView(restore, 'Restore at 200%');
      await restore.click();
      await expect.poll(async () => (await currentItem(itemId)).archived).toBe(false);
      await measure(zoomed, 200, 'item detail after restore');

      // All items finds the renamed item by search, in English and then Czech.
      await zoomed.getByRole('navigation', { name: en['nav.label'] }).getByRole('link', { name: en['home.allItems'], exact: true }).click();
      await expect(zoomed.getByRole('heading', { level: 1, name: en['allItems.title'] })).toBeVisible();
      await main.getByRole('searchbox').fill('cooker hood filter');
      await main.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
      const result = zoomed.getByRole('region', { name: en['allItems.results'] }).getByRole('link', { name: renamed, exact: true });
      await expect(result).toBeVisible();
      await measure(zoomed, 200, 'All items with a search');
      await inView(result, 'search result at 200%');
      const czech = zoomed.getByRole('button', { name: 'Čeština', exact: true });
      await inView(czech, 'language switch at 200%');
      await czech.click();
      await expect(zoomed.getByRole('heading', { level: 1, name: cs['allItems.title'] })).toBeVisible();
      await expect(zoomed.getByRole('region', { name: cs['allItems.results'] }).getByRole('link', { name: renamed, exact: true })).toBeVisible();
      await measure(zoomed, 200, 'All items with a search in Czech');
      await zoomed.getByRole('button', { name: 'English', exact: true }).click();
      await expect(zoomed.getByRole('heading', { level: 1, name: en['allItems.title'] })).toBeVisible();
      const cookies = await context.cookies();

      // 400 %: every household route and the item detail in a 320 CSS px layout, with its primary action reachable.
      const { zoomed: deep } = await launchAtZoom(400);
      await deep.context().addCookies(cookies);
      const deepMain = deep.getByRole('main');
      const routes: Array<{ path: string; name: string; heading: string; ready: Locator; action?: Locator }> = [
        { path: '/', name: 'home', heading: householdName, ready: deepMain.getByRole('listitem').first(), action: deepMain.getByRole('button', { name: en['items.add'], exact: true }) },
        { path: '/items', name: 'All items', heading: en['allItems.title'], ready: deep.getByRole('region', { name: en['allItems.results'] }).getByRole('listitem').first(), action: deepMain.getByRole('button', { name: en['allItems.searchButton'], exact: true }) },
        { path: `/items/${itemId}`, name: 'item detail', heading: renamed, ready: deepMain.getByRole('heading', { level: 2, name: en['itemDetail.history'] }), action: deepMain.getByRole('button', { name: en['itemDetail.edit.button'], exact: true }) },
        { path: '/people', name: 'People and things', heading: en['subjects.title'], ready: deepMain.getByRole('listitem').first(), action: deepMain.getByRole('button', { name: en['subjects.add'], exact: true }) },
        { path: '/members', name: 'Members', heading: en['members.title'], ready: deepMain.getByRole('listitem').filter({ hasText: ownerLogin }).first(), action: deepMain.getByRole('button', { name: en['members.invite.action'], exact: true }) },
        { path: '/account', name: 'Account', heading: en['account.title'], ready: deepMain.getByRole('heading', { level: 2, name: en['account.preferences.title'] }) },
      ];
      for (const route of routes) {
        await deep.goto(route.path);
        await expect(deep.getByRole('heading', { level: 1, name: route.heading }), `${route.name} heading at 400%`).toBeVisible();
        await expect(route.ready, `${route.name} content at 400%`).toBeVisible();
        await waitForSettledHouseholdRequests(deep);
        await measure(deep, 400, route.name);
        if (route.action) await inView(route.action, `${route.name} primary action at 400%`);
        await inView(deep.getByRole('navigation', { name: en['nav.label'] }).getByRole('link', { name: en['nav.home'], exact: true }), `${route.name} navigation at 400%`);
        await measure(deep, 400, `${route.name} after scrolling to its controls`);
      }
    } catch (error) {
      primaryFailure = error;
    }
    // Every cleanup step runs even if an earlier one fails; the journey's own failure is reported first.
    let cleanupFailure: unknown;
    console.log(`native-zoom-measurements ${JSON.stringify(measured)}`);
    await test.info().attach('native-zoom-measurements.json', { body: JSON.stringify(measured, null, 2), contentType: 'application/json' }).catch((error) => { cleanupFailure ??= error; });
    for (const context of contexts) await context.close().catch((error) => { cleanupFailure ??= error; });
    for (const profile of profiles) {
      try { rmSync(profile, { recursive: true, force: true }); } catch (error) { cleanupFailure ??= error; }
    }
    if (primaryFailure) throw primaryFailure;
    if (cleanupFailure) throw cleanupFailure;
  });

  test('n24. a lost create response survives reload and Check again stores exactly one item and one person', async () => {
    const main = page.getByRole('main');
    const ownedRoutes: Array<{ url: string; handler: (route: import('@playwright/test').Route) => void | Promise<void> }> = [];
    const failedCreatePaths = new Set<string>();
    let primaryFailure: unknown;
    let cleanupFailure: unknown;
    const ownRoute = async (url: string, handler: (route: import('@playwright/test').Route) => void | Promise<void>) => {
      ownedRoutes.push({ url, handler });
      await page.route(url, handler);
    };
    const removeRoute = async (url: string, handler: (route: import('@playwright/test').Route) => void | Promise<void>) => {
      await page.unroute(url, handler);
      const index = ownedRoutes.findIndex((entry) => entry.url === url && entry.handler === handler);
      if (index >= 0) ownedRoutes.splice(index, 1);
    };
    const walkAll = async <T,>(path: string, query: Record<string, string>): Promise<T[]> => {
      const results: T[] = [];
      let cursor: string | undefined;
      do {
        const params = new URLSearchParams({ limit: '100', ...query, ...(cursor ? { cursor } : {}) });
        const response = await page.request.get(`/api/v1/households/${householdId}/${path}?${params}`);
        expect(response.status()).toBe(200);
        const body = await response.json() as { items: T[]; nextCursor: string | null };
        results.push(...body.items);
        cursor = body.nextCursor ?? undefined;
      } while (cursor);
      return results;
    };
    const archiveCreated = async (path: 'items' | 'subjects', id: string) => {
      const detail = await page.request.get(`/api/v1/households/${householdId}/${path}/${id}`);
      expect(detail.status()).toBe(200);
      const archive = await page.request.patch(`/api/v1/households/${householdId}/${path}/${id}`, {
        headers: { Origin: baseURL, 'If-Match': detail.headers()['etag']! }, data: { archived: true },
      });
      expect(archive.status()).toBe(200);
      expect(((await archive.json()) as { archived: boolean }).archived).toBe(true);
    };
    let createdItemId: string | undefined;
    let createdSubjectId: string | undefined;
    try {
      const itemTitle = 'N24 lost create item unique';
      const itemPath = `/api/v1/households/${householdId}/items`;
      const itemRequests: Array<{ key: string; body: string }> = [];
      let itemFirst = true;
      const loseItemResponse = async (route: import('@playwright/test').Route) => {
        const request = route.request();
        itemRequests.push({ key: (await request.allHeaders())['idempotency-key'] ?? '', body: request.postData() ?? '' });
        if (itemFirst) {
          itemFirst = false;
          await route.fetch();
          await route.abort();
        } else {
          await route.continue();
        }
      };
      await page.goto('/');
      await waitForSettledHome(page, householdName);
      await main.getByRole('button', { name: en['items.add'], exact: true }).click();
      const itemForm = main.getByRole('form', { name: en['items.add.formLabel'] });
      await itemForm.getByLabel(en['items.title']).fill(itemTitle);
      await itemForm.getByLabel(en['items.subject'], { exact: true }).selectOption({ label: 'Anička' });
      await itemForm.getByRole('switch', { name: en['items.repeat'], exact: true }).check();
      await itemForm.getByRole('spinbutton').fill('1');
      await itemForm.getByRole('combobox', { name: en['items.intervalUnit'] }).selectOption('month');
      const historicalDate = addDays(pragueToday(), -10);
      await itemForm.getByRole('button', { name: en['items.historical.show'] }).click();
      await itemForm.getByLabel(en['items.historical.label']).fill(historicalDate);
      await ownRoute(`**${itemPath}`, loseItemResponse);
      n17ExpectedFailurePaths.add(itemPath);
      failedCreatePaths.add(itemPath);
      n17FailurePhaseActive = true;
      await itemForm.getByRole('button', { name: en['items.add.submit'], exact: true }).click();
      await expect(itemForm.getByRole('status').getByText(en['items.create.uncertain'], { exact: true })).toBeVisible();
      await expect(itemForm.getByRole('button', { name: en['items.create.checkAgain'], exact: true })).toBeVisible();
      expect(itemRequests).toHaveLength(1);
      const itemBeforeReload = await serverItem(itemTitle);
      createdItemId = itemBeforeReload.id;
      expect((await walkAll<ServerItem>('items', { done: 'false' })).filter((item) => item.title === itemTitle)).toHaveLength(1);
      const initialItemCompletions = await walkAll<ServerCompletion>(`items/${itemBeforeReload.id}/completions`, {});
      expect(initialItemCompletions).toHaveLength(1);
      expect(initialItemCompletions[0]!.completedOn).toBe(historicalDate);
      await removeRoute(`**${itemPath}`, loseItemResponse);
      n17ExpectedFailurePaths.delete(itemPath);
      n17FailurePhaseActive = false;
      await page.reload();
      const recoveredItemForm = main.getByRole('form', { name: en['items.add.formLabel'] });
      await expect(recoveredItemForm).toBeVisible();
      await expect(recoveredItemForm.getByLabel(en['items.title'])).toHaveValue(itemTitle);
      await expect(recoveredItemForm.getByLabel(en['items.title'])).toBeDisabled();
      await expect(recoveredItemForm.getByRole('status').getByText(en['items.create.uncertain'], { exact: true })).toBeVisible();
      await expect(recoveredItemForm.getByRole('button', { name: en['items.create.checkAgain'], exact: true })).toBeVisible();
      const itemPendingBeforeRetry = await page.evaluate(() => Object.keys(sessionStorage).filter((key) => key.startsWith('tendo.pendingItemCreate.v1:')).map((key) => JSON.parse(sessionStorage.getItem(key) ?? 'null')));
      expect(itemPendingBeforeRetry).toContainEqual({ version: 1, key: itemRequests[0]!.key, body: JSON.parse(itemRequests[0]!.body) });
      const itemRetryRequest = page.waitForRequest((request) => request.method() === 'POST' && new URL(request.url()).pathname === itemPath);
      await recoveredItemForm.getByRole('button', { name: en['items.create.checkAgain'], exact: true }).click();
      const itemRetry = await itemRetryRequest;
      expect(itemRetry.headers()['idempotency-key']).toBe(itemRequests[0]!.key);
      expect(itemRetry.postData()).toBe(itemRequests[0]!.body);
      await expect(main.getByRole('form', { name: en['items.add.formLabel'] })).toHaveCount(0);
      await expect(main.getByRole('listitem').filter({ hasText: itemTitle })).toBeVisible();
      const itemsAfterRetry = (await walkAll<ServerItem>('items', { done: 'false' })).filter((item) => item.title === itemTitle);
      expect(itemsAfterRetry).toHaveLength(1);
      expect(itemsAfterRetry[0]!.id).toBe(itemBeforeReload.id);
      const completionsAfterRetry = await walkAll<ServerCompletion>(`items/${itemBeforeReload.id}/completions`, {});
      expect(completionsAfterRetry).toHaveLength(1);
      expect(completionsAfterRetry[0]).toMatchObject({ id: initialItemCompletions[0]!.id, completedOn: historicalDate });
      expect(await page.evaluate(() => Object.keys(sessionStorage).filter((key) => key.startsWith('tendo.pendingItemCreate.v1:')))).toEqual([]);

      const subjectName = 'N24 lost create person unique';
      const subjectPath = `/api/v1/households/${householdId}/subjects`;
      const subjectRequests: Array<{ key: string; body: string }> = [];
      let subjectFirst = true;
      const loseSubjectResponse = async (route: import('@playwright/test').Route) => {
        const request = route.request();
        subjectRequests.push({ key: (await request.allHeaders())['idempotency-key'] ?? '', body: request.postData() ?? '' });
        if (subjectFirst) {
          subjectFirst = false;
          await route.fetch();
          await route.abort();
        } else {
          await route.continue();
        }
      };
      await page.goto('/people');
      await expect(main.getByRole('heading', { level: 1, name: en['subjects.title'] })).toBeVisible();
      await main.getByRole('button', { name: en['subjects.add'], exact: true }).click();
      const subjectForm = main.getByRole('form', { name: en['subjects.add.formLabel'] });
      await subjectForm.getByLabel(en['subjects.name']).fill(subjectName);
      await subjectForm.getByRole('radio', { name: en['subjects.type.person'], exact: true }).check();
      await ownRoute(`**${subjectPath}`, loseSubjectResponse);
      n17ExpectedFailurePaths.add(subjectPath);
      failedCreatePaths.add(subjectPath);
      n17FailurePhaseActive = true;
      await subjectForm.getByRole('button', { name: en['subjects.add.submit'], exact: true }).click();
      await expect(subjectForm.getByText(en['subjects.notice.uncertain'], { exact: true })).toBeVisible();
      await expect(subjectForm.getByRole('button', { name: en['subjects.create.checkAgain'], exact: true })).toBeVisible();
      expect(subjectRequests).toHaveLength(1);
      const subjectsBeforeReload = await walkAll<ServerSubject>('subjects', { archived: 'false' });
      const subjectBeforeReload = subjectsBeforeReload.find((subject) => subject.name === subjectName);
      expect(subjectBeforeReload).toBeDefined();
      createdSubjectId = subjectBeforeReload!.id;
      expect(subjectsBeforeReload.filter((subject) => subject.name === subjectName)).toHaveLength(1);
      await removeRoute(`**${subjectPath}`, loseSubjectResponse);
      n17ExpectedFailurePaths.delete(subjectPath);
      n17FailurePhaseActive = false;
      await page.reload();
      const recoveredSubjectForm = main.getByRole('form', { name: en['subjects.add.formLabel'] });
      await expect(recoveredSubjectForm).toBeVisible();
      await expect(recoveredSubjectForm.getByLabel(en['subjects.name'])).toHaveValue(subjectName);
      await expect(recoveredSubjectForm.getByLabel(en['subjects.name'])).toBeDisabled();
      await expect(recoveredSubjectForm.getByText(en['subjects.notice.uncertain'], { exact: true })).toBeVisible();
      await expect(recoveredSubjectForm.getByRole('button', { name: en['subjects.create.checkAgain'], exact: true })).toBeVisible();
      const subjectPendingBeforeRetry = await page.evaluate(() => Object.keys(sessionStorage).filter((key) => key.startsWith('tendo.pendingSubjectCreate.v1:')).map((key) => JSON.parse(sessionStorage.getItem(key) ?? 'null')));
      expect(subjectPendingBeforeRetry).toContainEqual({ version: 1, key: subjectRequests[0]!.key, body: JSON.parse(subjectRequests[0]!.body) });
      const subjectRetryRequest = page.waitForRequest((request) => request.method() === 'POST' && new URL(request.url()).pathname === subjectPath);
      await recoveredSubjectForm.getByRole('button', { name: en['subjects.create.checkAgain'], exact: true }).click();
      const subjectRetry = await subjectRetryRequest;
      expect(subjectRetry.headers()['idempotency-key']).toBe(subjectRequests[0]!.key);
      expect(subjectRetry.postData()).toBe(subjectRequests[0]!.body);
      await expect(main.getByRole('form', { name: en['subjects.add.formLabel'] })).toHaveCount(0);
      await expect(main.getByRole('listitem').filter({ hasText: subjectName })).toBeVisible();
      const subjectsAfterRetry = await walkAll<ServerSubject>('subjects', { archived: 'false' });
      expect(subjectsAfterRetry.filter((subject) => subject.name === subjectName)).toHaveLength(1);
      expect(subjectsAfterRetry.find((subject) => subject.name === subjectName)!.id).toBe(subjectBeforeReload!.id);
      expect(await page.evaluate(() => Object.keys(sessionStorage).filter((key) => key.startsWith('tendo.pendingSubjectCreate.v1:')))).toEqual([]);
    } catch (error) {
      primaryFailure = error;
    } finally {
      n17FailurePhaseActive = false;
      for (const path of failedCreatePaths) n17ExpectedFailurePaths.delete(path);
      failedCreatePaths.clear();
      for (const route of ownedRoutes) {
        try { await page.unroute(route.url, route.handler); } catch (error) { cleanupFailure ??= error; }
      }
      ownedRoutes.length = 0;
      if (createdItemId) {
        try { await archiveCreated('items', createdItemId); } catch (error) { cleanupFailure ??= error; }
      }
      if (createdSubjectId) {
        try { await archiveCreated('subjects', createdSubjectId); } catch (error) { cleanupFailure ??= error; }
      }
    }
    if (primaryFailure) throw primaryFailure;
    if (cleanupFailure) throw cleanupFailure;
  });

  test('n25. existing accounts accept invitations, conflicts stay calm, and invite tokens are shown once', async ({ browser }) => {
    test.setTimeout(120_000);
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
    await page.goto('/members');
    const invitationPath = `/api/v1/households/${householdId}/invitations`;
    type OwnerInvitation = { id: string; status: string };
    type HouseholdMember = { login: string; role: string };
    const listOwnerInvitations = async (secretToken?: string): Promise<OwnerInvitation[]> => {
      const invitations: OwnerInvitation[] = [];
      let cursor: string | undefined;
      do {
        const query = new URLSearchParams({ limit: '100', ...(cursor ? { cursor } : {}) });
        const response = await page.request.get(`${invitationPath}?${query}`);
        expect(response.status()).toBe(200);
        const body = await response.json() as { items: OwnerInvitation[]; nextCursor: string | null };
        if (secretToken) {
          expect(JSON.stringify(body)).not.toContain(secretToken);
          const assertNoTokenField = (value: unknown): void => {
            if (Array.isArray(value)) { value.forEach(assertNoTokenField); return; }
            if (value && typeof value === 'object') {
              expect(Object.keys(value)).not.toContain('token');
              Object.values(value).forEach(assertNoTokenField);
            }
          };
          assertNoTokenField(body);
        }
        invitations.push(...body.items);
        cursor = body.nextCursor ?? undefined;
      } while (cursor);
      return invitations;
    };
    const listOwnerMembers = async (): Promise<HouseholdMember[]> => {
      const members: HouseholdMember[] = [];
      let cursor: string | undefined;
      do {
        const query = new URLSearchParams({ limit: '100', ...(cursor ? { cursor } : {}) });
        const response = await page.request.get(`/api/v1/households/${householdId}/members?${query}`);
        expect(response.status()).toBe(200);
        const body = await response.json() as { items: HouseholdMember[]; nextCursor: string | null };
        members.push(...body.items);
        cursor = body.nextCursor ?? undefined;
      } while (cursor);
      return members;
    };
    const expectInvitationStatus = async (id: string, status: string) => {
      const invitation = (await listOwnerInvitations()).find(item => item.id === id);
      expect(invitation, `invitation ${id} is present in owner list`).toBeTruthy();
      expect(invitation!.status).toBe(status);
    };
    const createInvite = async () => {
      const before = new Set((await listOwnerInvitations()).map(invitation => invitation.id));
      await page.getByRole('button', { name: en['members.invite.action'] }).click();
      const field = page.getByRole('textbox', { name: en['members.invite.link'] });
      const link = await field.inputValue();
      expect(link.startsWith(`${baseURL}/invite#`)).toBe(true);
      expect(new URL(link).hash).toMatch(/^#[A-Za-z0-9_-]{43}$/);
      const token = new URL(link).hash.slice(1);
      const invitations = await listOwnerInvitations(token);
      const created = invitations.filter(invitation => !before.has(invitation.id));
      expect(created).toHaveLength(1);
      const id = created[0]!.id;
      await page.reload();
      await expect(page.getByRole('heading', { level: 1, name: en['members.title'] })).toBeVisible();
      await waitForSettledHouseholdRequests(page);
      await expect(page.getByRole('heading', { name: en['members.invitations.title'] })).toBeVisible();
      const inviteRows = page.getByRole('listitem').filter({ has: page.getByRole('button', { name: en['members.invitation.revoke'] }) });
      if (await inviteRows.count()) await expect(inviteRows.first()).toBeVisible();
      else await expect(page.getByText(en['members.invitations.empty'], { exact: true })).toBeVisible();
      await expect(page.getByRole('textbox', { name: en['members.invite.link'] })).toHaveCount(0);
      await expect(page.locator('body')).not.toContainText(token);
      return { link, token, id };
    };
    const makeActor = async () => {
      const context = await browser.newContext({ baseURL, locale: 'en-US', timezoneId: 'Europe/Prague' });
      const actor = await context.newPage();
      await trackInFlightApiFetches(actor);
      const issues: string[] = [];
      const failures: string[] = [];
      let acceptRefreshFailureActive = false;
      let invalidInviteFailureActive = false;
      actor.on('console', message => {
        if (message.type() !== 'error') return;
        const path = new URL(message.location().url || 'about:blank').pathname;
        if (path === '/api/v1/session' && (message.text().includes('status of 401') || acceptRefreshFailureActive && message.text().includes('status of 503'))) return;
        if (path === '/api/v1/invitations/accept' && (message.text().includes('status of 409') || invalidInviteFailureActive && message.text().includes('status of 404'))) return;
        issues.push(`console: ${message.text()}`);
      });
      actor.on('pageerror', error => issues.push(`pageerror: ${error.message}`));
      actor.on('response', response => {
        const path = new URL(response.url()).pathname;
        const failure = `${response.request().method()} ${path} ${response.status()}`;
        if (response.status() >= 400 && !(failure === 'GET /api/v1/session 401')) failures.push(failure);
      });
      return { context, actor, issues, failures, setAcceptRefreshFailureActive: (active: boolean) => { acceptRefreshFailureActive = active; }, setInvalidInviteFailureActive: (active: boolean) => { invalidInviteFailureActive = active; }, assertClean: (expectedFailures: string[]) => {
        expect(issues).toEqual([]);
        expect([...failures].sort()).toEqual([...expectedFailures].sort());
      } };
    };
    const signIntoExisting = async (actor: Page, loginName: string) => {
      await actor.goto('/');
      await actor.getByLabel(en['login.login']).fill(loginName);
      await actor.getByLabel(en['login.password']).fill(existingAccountPassword);
      const loginResponsePromise = actor.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/session');
      await actor.getByRole('button', { name: en['login.submit'], exact: true }).click();
      const loginResponse = await loginResponsePromise;
      expect(loginResponse.status()).toBe(201);
      await expect(actor.getByRole('button', { name: en['header.signOut'] })).toBeVisible();
    };
    const member = await makeActor();
    try {
      const first = await createInvite();
      await member.actor.goto(first.link);
      await member.actor.locator('#invite-login').fill('g4_member_e2e');
      await member.actor.locator('#invite-password').fill(`${ownerPassword}-g4-member`);
      await member.actor.getByRole('button', { name: en['invite.create'] }).click();
      await expect(member.actor.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
      const beforeResponse = await member.actor.request.get('/api/v1/session');
      expect(beforeResponse.status()).toBe(200);
      const before = await beforeResponse.json() as { defaultHouseholdId: string; login: string };
      expect(before).toMatchObject({ login: 'g4_member_e2e', defaultHouseholdId: householdId });
      const membersBeforeAlreadyMember = await listOwnerMembers();
      expect(membersBeforeAlreadyMember.filter(member => member.login === 'g4_member_e2e' && member.role === 'member')).toHaveLength(1);
      await page.goto('/members');
      await waitForSettledHouseholdRequests(page);
      await expect(page.getByRole('heading', { level: 1, name: en['members.title'] })).toBeVisible();
      await expect(page.getByRole('heading', { name: en['members.invitations.title'] })).toBeVisible();
      await waitForSettledHouseholdRequests(page);
      const initialInviteRows = page.getByRole('listitem').filter({ has: page.getByRole('button', { name: en['members.invitation.revoke'] }) });
      if (await initialInviteRows.count()) await expect(initialInviteRows.first()).toBeVisible();
      else await expect(page.getByText(en['members.invitations.empty'], { exact: true })).toBeVisible();
      const memberInvite = await page.request.post(invitationPath, { headers: { Origin: baseURL, 'Idempotency-Key': `g4-n25-${crypto.randomUUID()}` }, data: {} });
      expect(memberInvite.status()).toBe(201);
      const memberInviteBody = await memberInvite.json() as { id: string; token: string };
      expect(memberInviteBody.id).toMatch(/^[0-9a-f-]{36}$/);
      const memberInviteLink = `${baseURL}/invite#${memberInviteBody.token}`;
      await page.reload();
      await expect(page.getByRole('heading', { level: 1, name: en['members.title'] })).toBeVisible();
      await expect(page.getByRole('heading', { name: en['members.invitations.title'] })).toBeVisible();
      await waitForSettledHouseholdRequests(page);
      const memberInviteRows = page.getByRole('listitem').filter({ has: page.getByRole('button', { name: en['members.invitation.revoke'] }) });
      await expect(memberInviteRows.first()).toBeVisible();
      const ownerInvitations = await listOwnerInvitations(memberInviteBody.token);
      expect(ownerInvitations.some(invitation => invitation.id === memberInviteBody.id)).toBe(true);
      await expect(page.getByRole('textbox', { name: en['members.invite.link'] })).toHaveCount(0);
      await expect(page.locator('body')).not.toContainText(memberInviteBody.token);
      await member.actor.goto(memberInviteLink);
      await expect(member.actor.getByRole('heading', { level: 1, name: en['invite.signedIn.title'].replace('{login}', 'g4_member_e2e') })).toBeVisible();
      await member.actor.getByRole('button', { name: en['invite.join'] }).click();
      await expect(member.actor.getByRole('status')).toContainText(en['invite.error.alreadyMember']);
      await expect(member.actor.getByRole('link', { name: en['invite.home'] })).toBeVisible();
      const afterResponse = await member.actor.request.get('/api/v1/session');
      expect(afterResponse.status()).toBe(200);
      const afterSession = await afterResponse.json() as { defaultHouseholdId: string; login: string };
      expect(afterSession).toMatchObject({ login: before.login, defaultHouseholdId: before.defaultHouseholdId });
      await expectInvitationStatus(memberInviteBody.id, 'pending');
      const membersAfterAlreadyMember = await listOwnerMembers();
      expect(membersAfterAlreadyMember.filter(member => member.login === 'g4_member_e2e' && member.role === 'member')).toHaveLength(1);
      member.assertClean(['POST /api/v1/invitations/accept 409']);
    } finally { await member.context.close(); }

    const conflictInvite = await createInvite();
    const conflict = await makeActor();
    try {
      await signIntoExisting(conflict.actor, 'existing_conflict_e2e');
      await waitForSettledHome(conflict.actor, 'Conflict household');
      await expect(conflict.actor.getByRole('banner')).toContainText('existing_conflict_e2e');
      const conflictSession = await conflict.actor.request.get('/api/v1/session');
      expect(conflictSession.status()).toBe(200);
      expect((await conflictSession.json() as { defaultHouseholdId: string }).defaultHouseholdId).toBe(conflictHouseholdId);
      await conflict.actor.goto(conflictInvite.link);
      await expect(conflict.actor.getByRole('heading', { level: 1, name: en['invite.signedIn.title'].replace('{login}', 'existing_conflict_e2e') })).toBeVisible();
      await waitForSettledHouseholdRequests(conflict.actor);
      const conflictAcceptResponse = conflict.actor.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/invitations/accept');
      await conflict.actor.getByRole('button', { name: en['invite.join'] }).click();
      const conflictAccept = await conflictAcceptResponse;
      expect(conflictAccept.status()).toBe(409);
      expect(await conflictAccept.json()).toMatchObject({ code: 'household_conflict' });
      await expect(conflict.actor.getByText(en['invite.error.householdConflict'], { exact: true })).toBeVisible();
      const conflictedSession = await conflict.actor.request.get('/api/v1/session');
      expect(conflictedSession.status()).toBe(200);
      expect((await conflictedSession.json() as { defaultHouseholdId: string }).defaultHouseholdId).toBe(conflictHouseholdId);
      expect((await conflict.actor.request.get(`/api/v1/households/${householdId}/items`)).status()).toBe(404);
      await expectInvitationStatus(conflictInvite.id, 'pending');
      const listedConflictMembers = await listOwnerMembers();
      expect(listedConflictMembers.some(member => member.login === 'existing_conflict_e2e')).toBe(false);
      const oldCookie = (await conflict.context.cookies()).find(cookie => cookie.name === 'tendo_session')?.value;
      expect(oldCookie).toBeTruthy();
      await conflict.actor.getByRole('button', { name: en['invite.signOutContinue'] }).click();
      await expect(conflict.actor.locator('#invite-login')).toBeVisible();
      await expect(conflict.actor.getByRole('button', { name: en['invite.create'] })).toBeVisible();
      expect((await conflict.context.cookies()).some(cookie => cookie.name === 'tendo_session')).toBe(false);
      expect(new URL(conflict.actor.url()).pathname).toBe('/invite');
      expect(new URL(conflict.actor.url()).hash).toBe(''); // The app strips the fragment from location and retains the token in invite state.
      const stale = await request.newContext({ baseURL });
      try { expect((await stale.get('/api/v1/session', { headers: { Cookie: `tendo_session=${oldCookie}` } })).status()).toBe(401); }
      finally { await stale.dispose(); }
      await conflict.actor.locator('#invite-login').fill('conflict_new_e2e');
      await conflict.actor.locator('#invite-password').fill(`${ownerPassword}-conflict-new`);
      await conflict.actor.getByRole('button', { name: en['invite.create'] }).click();
      await expect(conflict.actor.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
      expect((await (await conflict.actor.request.get('/api/v1/session')).json() as { defaultHouseholdId: string }).defaultHouseholdId).toBe(householdId);
      await expectInvitationStatus(conflictInvite.id, 'accepted');
      await waitForSettledHouseholdRequests(conflict.actor);
      conflict.assertClean(['POST /api/v1/invitations/accept 409']);
    } finally { await conflict.context.close(); }

    const freeInvite = await createInvite();
    const free = await makeActor();
    try {
      await signIntoExisting(free.actor, 'existing_free_e2e');
      await expect(free.actor.getByRole('heading', { level: 1, name: en['home.noHousehold.title'] })).toBeVisible();
      await free.actor.goto(freeInvite.link);
      await expect(free.actor.getByRole('heading', { level: 1, name: en['invite.signedIn.title'].replace('{login}', 'existing_free_e2e') })).toBeVisible();
      await waitForSettledHouseholdRequests(free.actor);
      const sessionPath = '/api/v1/session';
      let injectedRefreshFailure = false;
      const freeFailuresBeforeRefresh = free.failures.length;
      const rejectFirstRefresh = async (route: import('@playwright/test').Route) => {
        if (route.request().method() === 'GET' && !injectedRefreshFailure) {
          injectedRefreshFailure = true;
          return route.fulfill({ status: 503, contentType: 'application/problem+json', body: JSON.stringify(problem503) });
        }
        return route.fallback();
      };
      await free.actor.route(new RegExp(`^${escapeRegExp(baseURL)}${escapeRegExp(sessionPath)}$`), rejectFirstRefresh);
      free.setAcceptRefreshFailureActive(true);
      const acceptResponsePromise = free.actor.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/v1/invitations/accept');
      await free.actor.getByRole('button', { name: en['invite.join'], exact: true }).click();
      const acceptedResponse = await acceptResponsePromise;
      expect(acceptedResponse.status()).toBe(201);
      const acceptedNotice = free.actor.getByRole('status').filter({ hasText: en['invite.error.accepted'] });
      await expect(acceptedNotice).toBeVisible();
      await expect(acceptedNotice).toBeFocused();
      await expect(free.actor.getByRole('button', { name: en['invite.continue'], exact: true })).toBeVisible();
      await expectInvitationStatus(freeInvite.id, 'accepted');
      const joinedBeforeContinue = await listOwnerMembers();
      expect(joinedBeforeContinue.filter(member => member.login === 'existing_free_e2e' && member.role === 'member')).toHaveLength(1);
      free.setAcceptRefreshFailureActive(false);
      await free.actor.unroute(new RegExp(`^${escapeRegExp(baseURL)}${escapeRegExp(sessionPath)}$`), rejectFirstRefresh);
      const continueSessionPromise = free.actor.waitForResponse(response => response.request().method() === 'GET' && new URL(response.url()).pathname === sessionPath);
      await free.actor.getByRole('button', { name: en['invite.continue'], exact: true }).click();
      const continuedSessionResponse = await continueSessionPromise;
      expect(continuedSessionResponse.status()).toBe(200);
      await expect(free.actor.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
      expect(free.failures.slice(freeFailuresBeforeRefresh)).toEqual(['GET /api/v1/session 503']);
      const freeSession = await free.actor.request.get('/api/v1/session');
      expect(freeSession.status()).toBe(200);
      expect((await freeSession.json() as { defaultHouseholdId: string }).defaultHouseholdId).toBe(householdId);
      await free.actor.getByRole('link', { name: en['home.members'], exact: true }).click();
      await expect(free.actor.getByRole('listitem').filter({ hasText: 'existing_free_e2e' })).toContainText(en['members.role.member']);
      await free.actor.goto(freeInvite.link);
      await expect(free.actor.getByRole('heading', { level: 1, name: en['invite.signedIn.title'].replace('{login}', 'existing_free_e2e') })).toBeVisible();
      free.setInvalidInviteFailureActive(true);
      await free.actor.getByRole('button', { name: en['invite.join'] }).click();
      await expect(free.actor.getByRole('status')).toContainText(en['invite.error.invalid']);
      free.setInvalidInviteFailureActive(false);
      await expectInvitationStatus(freeInvite.id, 'accepted');
      const listedMembers = await listOwnerMembers();
      expect(listedMembers.find(member => member.login === 'existing_free_e2e')?.role).toBe('member');
      await waitForSettledHouseholdRequests(free.actor);
      free.assertClean(['GET /api/v1/session 503', 'POST /api/v1/invitations/accept 404']);
    } finally { await free.context.close(); }
  });

  test('n27. two signed-in sessions reject stale item edits and undo without overwriting persisted state', async ({ browser }) => {
    test.setTimeout(120_000);
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
    const subjectsResponse = await page.request.get(`/api/v1/households/${householdId}/subjects?archived=false`);
    expect(subjectsResponse.status()).toBe(200);
    const subjectId = ((await subjectsResponse.json()) as { items: Array<{ id: string }> }).items[0]!.id;
    const title = 'Two-session concurrency E2E';
    const create = await page.request.post(`/api/v1/households/${householdId}/items`, {
      headers: { Origin: baseURL, 'Idempotency-Key': `e2e-two-session-${crypto.randomUUID()}` },
      data: { title, subjectId },
    });
    expect(create.status()).toBe(201);
    const created = await create.json() as { id: string };
    const itemPath = `/api/v1/households/${householdId}/items/${created.id}`;
    const completionsPath = `${itemPath}/completions`;
    const contexts: import('@playwright/test').BrowserContext[] = [];
    const actorContext = async () => {
      const context = await browser.newContext({ baseURL, locale: 'en-US', timezoneId: 'Europe/Prague' });
      contexts.push(context); // owned by the cleanup below before any step can fail
      const actor = await context.newPage();
      await trackInFlightApiFetches(actor);
      await actor.goto('/');
      await actor.getByLabel(en['login.login']).fill(ownerLogin);
      await actor.getByLabel(en['login.password']).fill(ownerPassword);
      await actor.getByRole('button', { name: en['login.submit'], exact: true }).click();
      await expect(actor.getByRole('button', { name: en['header.signOut'] })).toBeVisible();
      await actor.goto(`/items/${created.id}`);
      await expect(actor.getByRole('heading', { level: 1, name: title })).toBeVisible();
      return { context, actor };
    };
    let primaryFailure: unknown;
    try {
      const first = await actorContext();
      const second = await actorContext();
      // Both item detail forms load version 1 (asynchronous GET + choices) before session A commits.
      await first.actor.getByRole('button', { name: en['itemDetail.edit.button'] }).click();
      await second.actor.getByRole('button', { name: en['itemDetail.edit.button'] }).click();
      const formA = first.actor.getByRole('form', { name: en['itemDetail.edit.formLabel'] });
      const formB = second.actor.getByRole('form', { name: en['itemDetail.edit.formLabel'] });
      await expect(formA.getByLabel(en['items.title'])).toHaveValue(title);
      await expect(formB.getByLabel(en['items.title'])).toHaveValue(title);
      await formA.getByLabel(en['items.title']).fill('Two-session winner');
      await formA.getByRole('button', { name: en['itemDetail.save'] }).click();
      await expect(first.actor.getByRole('status')).toContainText(en['itemDetail.updated']);
      await formB.getByLabel(en['items.title']).fill('Two-session stale draft');
      await formB.getByRole('button', { name: en['itemDetail.save'] }).click();
      await expect(second.actor.getByText(en['itemDetail.edit.conflict'], { exact: true })).toBeVisible();
      await expect(second.actor.getByText(en['itemDetail.edit.conflictHint'], { exact: true })).toBeVisible();
      await expect(formB.getByLabel(en['items.title'])).toHaveValue('Two-session stale draft');
      await expect(second.actor.getByText('Two-session winner', { exact: true })).toBeVisible();

      // The documented recovery is an explicit second save against the rebased ETag.
      await formB.getByRole('button', { name: en['itemDetail.save'] }).click();
      await expect(second.actor.getByRole('status')).toContainText(en['itemDetail.updated']);
      let current = await second.actor.request.get(itemPath);
      expect(current.status()).toBe(200);
      expect(current.headers()['etag']).toBe('"3"');
      expect(await current.json()).toMatchObject({ id: created.id, title: 'Two-session stale draft', done: false });
      const staleETag = current.headers()['etag']!;

      // Session A completes with the current ETag; session B tries to undo using its captured stale version.
      const completion = await first.actor.request.post(completionsPath, {
        headers: { Origin: baseURL, 'If-Match': staleETag, 'Idempotency-Key': `e2e-two-session-complete-${crypto.randomUUID()}` },
        data: { completedOn: expectedCompletionDate },
      });
      expect(completion.status()).toBe(201);
      const receipt = await completion.json() as { id: string; undoneAt: string | null };
      expect(receipt.undoneAt).toBeNull();
      const completedItem = await first.actor.request.get(itemPath);
      expect(completedItem.status()).toBe(200);
      const completionETag = completedItem.headers()['etag']!;
      expect(completionETag).toBe('"4"');
      const staleUndo = await second.actor.request.patch(`${completionsPath}/${receipt.id}`, {
        headers: { Origin: baseURL, 'If-Match': staleETag }, data: { undone: true },
      });
      expect(staleUndo.status(), 'stale completion undo must be rejected with 412').toBe(412);
      current = await second.actor.request.get(itemPath);
      expect(current.status()).toBe(200);
      expect(current.headers()['etag']).toBe(completionETag);
      expect(await current.json()).toMatchObject({ id: created.id, title: 'Two-session stale draft', done: true, lastCompletedOn: expectedCompletionDate });
      const history = await second.actor.request.get(completionsPath);
      expect(history.status()).toBe(200);
      const receipts = ((await history.json()) as { items: Array<{ id: string; undoneAt: string | null }> }).items;
      expect(receipts).toHaveLength(1);
      expect(receipts[0]).toMatchObject({ id: receipt.id, undoneAt: null });
    } catch (error) {
      primaryFailure = error;
    }
    // Attempt every cleanup step; a cleanup failure is reported only when the test body passed.
    const cleanupFailures: unknown[] = [];
    for (const context of contexts) await context.close().catch((error: unknown) => cleanupFailures.push(error));
    try {
      const latest = await page.request.get(itemPath);
      expect(latest.status(), 'n27 cleanup reads item').toBe(200);
      const archive = await page.request.patch(itemPath, { headers: { Origin: baseURL, 'If-Match': latest.headers()['etag']! }, data: { archived: true } });
      expect(archive.status(), 'n27 cleanup archives item').toBe(200);
    } catch (error) { cleanupFailures.push(error); }
    if (primaryFailure) throw primaryFailure;
    if (cleanupFailures.length) throw cleanupFailures[0];
  });

  test('n29. residual controls recover against the production API and PostgreSQL', async () => {
    test.setTimeout(120_000);
    const ownedRoutes: Array<{ url: RegExp; handler: (route: import('@playwright/test').Route) => void | Promise<void> }> = [];
    let primaryFailure: unknown;
    let cleanupFailure: unknown;
    const openWindow = (declared: string[] = []) => {
      if (failureWindows.has(page)) throw new Error('n29 failure window already open');
      failureWindows.set(page, { injected: [], declared, observed: [] });
    };
    const inject = async (url: RegExp, method: string, opts: { times?: number; abort?: boolean; match?: (req: import('@playwright/test').Request) => boolean } = {}) => {
      let count = 0;
      const handler = async (route: import('@playwright/test').Route) => {
        const req = route.request();
        if (req.method() !== method || (opts.match && !opts.match(req)) || count >= (opts.times ?? 1)) return route.fallback();
        const window = failureWindows.get(page);
        if (!window) throw new Error('n29 injection requires an open failure window');
        count += 1;
        const requestURL = req.url();
        const path = new URL(requestURL).pathname;
        window.injected.push({ key: `${method} ${path} ${opts.abort ? 'abort' : 503}`, url: requestURL });
        if (opts.abort) { await route.fetch(); return route.abort(); }
        return route.fulfill({ status: 503, contentType: 'application/problem+json', body: JSON.stringify(problem503) });
      };
      ownedRoutes.push({ url, handler });
      await page.route(url, handler);
      return { count: () => count, stop: async () => { await page.unroute(url, handler); ownedRoutes.splice(ownedRoutes.findIndex((entry) => entry.handler === handler), 1); } };
    };
    const closeWindow = async (routes: Array<{ stop: () => Promise<void> }> = []) => {
      const window = failureWindows.get(page);
      if (!window) throw new Error('n29 failure window is not open');
      await waitForSettledHouseholdRequests(page);
      for (const route of routes) await route.stop();
      expect([...window.observed].sort()).toEqual([...window.injected.map((entry) => entry.key), ...window.declared].sort());
      failureWindows.delete(page);
    };
    const getSubjectByName = async (name: string) => (await serverSubjects(false)).find((subject) => subject.name === name);
    const contexts: BrowserContext[] = [];
    try {
      await page.goto('/');
      await waitForSettledHome(page, householdName);

      // Catalogue lookup uses limit/archived query; label reads carry id= and remain untouched.
      const cataloguePath = `/api/v1/households/${householdId}/subjects`;
      const catalogueUrl = new RegExp(`^${escapeRegExp(baseURL)}${escapeRegExp(cataloguePath)}\\?.*$`);
      openWindow();
      const catalogueFailure = await inject(catalogueUrl, 'GET', { times: 2, match: (req) => { const query = new URL(req.url()).searchParams; return query.get('limit') === '100' && query.get('archived') === 'false' && !query.has('id'); } });
      const addItemButton = page.getByRole('button', { name: en['items.add'], exact: true });
      await addItemButton.click();
      await expect(page.getByText(en['items.subjects.error'], { exact: true })).toBeVisible();
      await page.getByRole('button', { name: en['items.cancel'], exact: true }).click();
      await expect(page.getByText(en['items.subjects.error'], { exact: true })).toHaveCount(0);
      await expect(page.getByRole('form', { name: en['items.add.formLabel'] })).toHaveCount(0);
      await expect(addItemButton).toBeFocused();
      await addItemButton.click();
      await expect(page.getByText(en['items.subjects.error'], { exact: true })).toBeVisible();
      expect(catalogueFailure.count()).toBe(2);
      await page.getByRole('button', { name: en['app.error.retry'], exact: true }).click();
      await expect(page.getByRole('form', { name: en['items.add.formLabel'] })).toBeVisible();
      await page.getByRole('form', { name: en['items.add.formLabel'] }).getByRole('button', { name: en['items.cancel'], exact: true }).click();
      await expect(addItemButton).toBeFocused();
      await closeWindow([catalogueFailure]);

      // Use a real persisted item and receipt, then exercise both independent detail loaders.
      await addItem({ title: 'N29 detail retry persisted', subject: 'Anička', historicalCompletedOn: expectedCompletionDate, repeat: { value: '1', unit: 'week', fluid: false } });
      const createdItem = await serverItem('N29 detail retry persisted');
      const itemPath = `/api/v1/households/${householdId}/items/${createdItem.id}`;
      const itemResponse = await page.request.get(itemPath);
      expect(itemResponse.status()).toBe(200);
      const itemBody = await itemResponse.json() as { id: string; attentionOn: string | null };
      expect(itemBody).toMatchObject({ id: createdItem.id, attentionOn: createdItem.attentionOn });
      const itemGetPath = itemPath;
      const homePath = `/api/v1/households/${householdId}/items/summary`;
      const groupPath = `/api/v1/households/${householdId}/items`;
      const homeRouteUrl = new RegExp(`^${escapeRegExp(baseURL)}(?:${escapeRegExp(homePath)}|${escapeRegExp(groupPath)})(?:\\?.*)?$`);
      openWindow();
      const homeFailures = await inject(homeRouteUrl, 'GET', { times: 2, match: (req) => new URL(req.url()).pathname === homePath || new URL(req.url()).pathname === groupPath });
      await page.goto('/');
      await expect(page.getByText(en['app.error.body'], { exact: true })).toBeVisible();
      await page.getByRole('button', { name: en['app.error.retry'], exact: true }).click();
      await waitForSettledHome(page, householdName);
      // Home may request either summary or groups first, but both paths are independently eligible.
      expect(homeFailures.count()).toBeGreaterThanOrEqual(1);
      await closeWindow([homeFailures]);

      // Both active and archived label lookups are separate requests for the visible home rows.
      const subjectsURL = new RegExp(`^${escapeRegExp(baseURL)}${escapeRegExp(cataloguePath)}\\?.*$`);
      openWindow();
      const failedLabels = await inject(subjectsURL, 'GET', { times: 2, match: (req) => new URL(req.url()).searchParams.has('id') });
      await page.goto('/');
      await expect(page.getByText(en['items.subjectLabels.error'], { exact: true })).toBeVisible();
      const labelRetry = page.getByRole('button', { name: en['items.subjectLabels.retry'], exact: true });
      await expect(labelRetry).toBeVisible();
      // A visible batch can contain active and archived subject lookups.
      expect(failedLabels.count()).toBeGreaterThanOrEqual(2);
      const labelResponse = page.waitForResponse((response) => response.request().method() === 'GET' && new URL(response.url()).pathname === cataloguePath && new URL(response.url()).searchParams.has('id') && response.status() === 200);
      await labelRetry.click();
      await labelResponse;
      await expect(page.getByText(en['items.subjectLabels.error'], { exact: true })).toHaveCount(0);
      await expect(page.getByRole('main').getByText('Anička', { exact: true }).first()).toBeVisible();
      await expect.poll(() => page.evaluate(() => { const active = document.activeElement; return active instanceof HTMLHeadingElement && active.tagName !== 'H1'; })).toBe(true);
      await closeWindow([failedLabels]);
      await page.getByRole('link', { name: en['nav.allItems'], exact: true }).click();
      await expect(page.getByRole('heading', { level: 1, name: en['allItems.title'] })).toBeVisible();
      const detailItemRow = page.getByRole('main').getByRole('listitem').filter({ hasText: 'N29 detail retry persisted' });
      await expect(detailItemRow).toBeVisible();
      openWindow();
      const itemFailure = await inject(new RegExp(`^${escapeRegExp(baseURL)}${escapeRegExp(itemGetPath)}(?:\\?.*)?$`), 'GET');
      await page.getByRole('main').getByRole('link').filter({ hasText: 'N29 detail retry persisted' }).click();
      await expect(page.getByText(en['app.error.body'], { exact: true })).toBeVisible();
      await page.getByRole('button', { name: en['app.error.retry'], exact: true }).click();
      await expect(page.getByRole('heading', { level: 1, name: 'N29 detail retry persisted' })).toBeVisible();
      await expect(page.getByRole('heading', { name: en['itemDetail.history'] })).toBeVisible();
      expect(itemFailure.count()).toBe(1);
      await closeWindow([itemFailure]);
      const historyPath = `${itemPath}/completions`;
      await page.getByRole('link', { name: en['nav.home'], exact: true }).click();
      await waitForSettledHome(page, householdName);
      await page.getByRole('link', { name: en['nav.allItems'], exact: true }).click();
      await expect(page.getByRole('heading', { level: 1, name: en['allItems.title'] })).toBeVisible();
      const historyItemRow = page.getByRole('main').getByRole('listitem').filter({ hasText: 'N29 detail retry persisted' });
      await expect(historyItemRow).toBeVisible();
      openWindow();
      const historyFailure = await inject(new RegExp(`^${escapeRegExp(baseURL)}${escapeRegExp(historyPath)}(?:\\?.*)?$`), 'GET');
      await historyItemRow.getByRole('link').click();
      await expect(page.getByText(en['itemDetail.historyError'], { exact: true })).toBeVisible();
      await page.getByRole('button', { name: en['app.error.retry'], exact: true }).click();
      await expect(page.locator('ol').getByRole('listitem')).toHaveCount(1);
      const historyAfterRetry = await page.request.get(historyPath);
      expect(historyAfterRetry.status()).toBe(200);
      expect((await historyAfterRetry.json() as { items: unknown[] }).items).toHaveLength(1);
      expect(historyFailure.count()).toBe(1);
      await closeWindow([historyFailure]);

      // People list failure is visibly retryable; linked account is persisted on write and clear.
      await page.goto('/'); await waitForSettledHome(page, householdName);
      const peoplePath = `/api/v1/households/${householdId}/subjects`;
      openWindow();
      const peopleFailure = await inject(new RegExp(`^${escapeRegExp(baseURL)}${escapeRegExp(peoplePath)}(?:\\?.*)?$`), 'GET');
      await page.goto('/people');
      await expect(page.getByText(en['app.error.body'], { exact: true })).toBeVisible();
      expect(peopleFailure.count()).toBe(1);
      await closeWindow([peopleFailure]);
      await page.getByRole('button', { name: en['app.error.retry'], exact: true }).click();
      await expect(page.getByRole('heading', { level: 1, name: en['subjects.title'] })).toBeVisible();
      const members = await page.request.get(`/api/v1/households/${householdId}/members`);
      expect(members.status()).toBe(200);
      const linkedMember = ((await members.json()) as { items: Array<{ login: string; userId: string }> }).items.find((member) => member.login === 'member_e2e');
      expect(linkedMember).toBeTruthy();
      const linkedName = 'N29 linked account residual';
      await page.getByRole('button', { name: en['subjects.add'], exact: true }).click();
      const personForm = page.getByRole('form', { name: en['subjects.add.formLabel'] });
      await personForm.getByLabel(en['subjects.name']).fill(linkedName);
      await expect(personForm.getByRole('combobox', { name: en['subjects.link.label'] })).toBeVisible();
      await personForm.getByRole('combobox', { name: en['subjects.link.label'] }).selectOption(linkedMember!.userId);
      await personForm.getByRole('button', { name: en['subjects.add.submit'] }).click();
      await expect(page.getByRole('listitem').filter({ hasText: linkedName })).toContainText(linkedMember!.login);
      const linkedSubject = await getSubjectByName(linkedName);
      expect(linkedSubject?.linkedUserId).toBe(linkedMember!.userId);
      const linkedPath = `${peoplePath}/${linkedSubject!.id}`;
      await page.getByRole('button', { name: en['subjects.edit.label'].replace('{name}', linkedName) }).click();
      const linkedEdit = page.getByRole('form', { name: en['subjects.edit.label'].replace('{name}', linkedName) });
      await linkedEdit.getByRole('combobox', { name: en['subjects.link.label'] }).selectOption('');
      await linkedEdit.getByRole('button', { name: en['subjects.save'] }).click();
      await expect(page.getByRole('listitem').filter({ hasText: linkedName })).not.toContainText(linkedMember!.login);
      expect((await (await page.request.get(linkedPath)).json() as { linkedUserId: string | null }).linkedUserId).toBeNull();

      // Members invitation list recovery, clipboard success and rejection fallback.
      await page.goto('/'); await waitForSettledHome(page, householdName);
      const invitationsPath = `/api/v1/households/${householdId}/invitations`;
      const invitationURL = new RegExp(`^${escapeRegExp(baseURL)}${escapeRegExp(invitationsPath)}(?:\\?.*)?$`);
      openWindow();
      const invitationListFailure = await inject(invitationURL, 'GET');
      await page.goto('/members');
      await expect(page.getByText(en['members.invitations.error'], { exact: true })).toBeVisible();
      expect(invitationListFailure.count()).toBe(1);
      await closeWindow([invitationListFailure]);
      const invitationRetry = page.waitForResponse((response) => response.request().method() === 'GET' && new URL(response.url()).pathname === invitationsPath && response.status() === 200);
      await page.getByRole('button', { name: en['app.error.retry'], exact: true }).click();
      await invitationRetry;
      await expect(page.getByText(en['members.invitations.error'], { exact: true })).toHaveCount(0);
      const invitationHeading = page.getByRole('heading', { name: en['members.invitations.title'] });
      const invitationContent = invitationHeading.locator('..');
      const invitationRows = invitationContent.getByRole('listitem');
      if (await invitationRows.count()) await expect(invitationRows.first()).toBeVisible();
      else await expect(invitationContent.getByText(en['members.invitations.empty'], { exact: true })).toBeVisible();
      await expect(page.getByRole('heading', { level: 1, name: en['members.title'] })).toBeVisible();
      await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
      await page.getByRole('button', { name: en['members.invite.action'] }).click();
      const linkField = page.getByRole('textbox', { name: en['members.invite.link'] });
      const shownLink = await linkField.inputValue();
      await page.getByRole('button', { name: en['members.invite.copy'], exact: true }).click();
      await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(shownLink);
      await page.getByRole('button', { name: en['members.invite.copiedButton'] }).click();
      await expect(page.getByRole('status')).toContainText(en['members.invite.copied']);
      await page.evaluate(() => Object.defineProperty(navigator.clipboard, 'writeText', { configurable: true, value: () => Promise.reject(new Error('clipboard denied')) }));
      await page.getByRole('button', { name: en['members.invite.copiedButton'] }).click();
      await expect(page.getByRole('status')).toContainText(en['members.invite.manualCopy']);
      await expect(linkField).toBeFocused();
      await expect(linkField).toHaveJSProperty('selectionStart', 0);
      await expect(linkField).toHaveJSProperty('selectionEnd', shownLink.length);

      // Lose the first create response after the server commits. Replay and revoke the exact created id.
      const beforeInvitesResponse = await page.request.get(`${invitationsPath}?limit=100`);
      expect(beforeInvitesResponse.status()).toBe(200);
      const beforeIds = new Set(((await beforeInvitesResponse.json()) as { items: Array<{ id: string }> }).items.map((item) => item.id));
      const createInvitePath = invitationsPath;
      const createdKeys: string[] = [];
      const inviteUrl = new RegExp(`^${escapeRegExp(baseURL)}${escapeRegExp(createInvitePath)}$`);
      openWindow();
      const lostCreate = await inject(inviteUrl, 'POST', { abort: true, times: 1, match: (req) => { createdKeys.push(req.headers()['idempotency-key'] ?? ''); return true; } });
      await page.getByRole('button', { name: en['members.invite.action'] }).click();
      await expect(page.getByRole('button', { name: en['members.invite.checkAgain'] })).toBeVisible();
      await page.getByRole('button', { name: en['members.invite.checkAgain'] }).click();
      await expect(page.getByText(en['members.invite.existingNoLink'], { exact: true })).toBeVisible();
      expect(lostCreate.count()).toBe(1);
      expect(createdKeys).toHaveLength(2);
      expect(createdKeys[1]).toBe(createdKeys[0]);
      const afterInvitesResponse = await page.request.get(`${invitationsPath}?limit=100`);
      expect(afterInvitesResponse.status()).toBe(200);
      const createdInvitations = ((await afterInvitesResponse.json()) as { items: Array<{ id: string; status: string }> }).items.filter((item) => !beforeIds.has(item.id));
      expect(createdInvitations).toHaveLength(1);
      await page.getByRole('button', { name: en['members.invite.revokeRecovered'] }).click();
      await expect(page.getByRole('status').filter({ hasText: en['members.invite.revoked'] })).toBeVisible();
      const persistedInvites = ((await (await page.request.get(`${invitationsPath}?limit=100`)).json()) as { items: Array<{ id: string; status: string }> }).items;
      expect(persistedInvites.find((item) => item.id === createdInvitations[0]!.id)?.status).toBe('revoked');
      await closeWindow([lostCreate]);

      // Invalid token acceptance is a real server 404, declared for this step only.
      openWindow(['POST /api/v1/invitations/accept 404']);
      await page.goto(`/invite#${'Z'.repeat(43)}`);
      await page.getByRole('button', { name: en['invite.join'], exact: true }).click();
      await closeWindow();
      await expect(page.getByRole('status')).toContainText(en['invite.error.invalid']);
      await expect(page.getByRole('link', { name: en['invite.home'], exact: true })).toBeVisible();
      await page.getByRole('link', { name: en['invite.home'], exact: true }).click();
      await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();

      expect(itemBody).toMatchObject({ id: createdItem.id, attentionOn: createdItem.attentionOn });
    } catch (error) { primaryFailure = error; }
    finally {
      n17FailurePhaseActive = false;
      for (const entry of ownedRoutes) await page.unroute(entry.url, entry.handler).catch((error: unknown) => { cleanupFailure ??= error; });
      failureWindows.delete(page);
      n17FailurePhaseActive = false;
      for (const context of contexts) await context.close().catch((error: unknown) => { cleanupFailure ??= error; });
      // The journey requires no leaked invitation/item state; archive the new item and People row.
      try {
        const currentItem = await page.request.get(`/api/v1/households/${householdId}/items`);
        expect(currentItem.status()).toBe(200);
        const itemRow = ((await currentItem.json()) as { items: Array<{ id: string; title: string }> }).items.find((item) => item.title === 'N29 detail retry persisted');
        if (itemRow) {
          const detail = await page.request.get(`/api/v1/households/${householdId}/items/${itemRow.id}`);
          const archived = await page.request.patch(`/api/v1/households/${householdId}/items/${itemRow.id}`, { headers: { Origin: baseURL, 'If-Match': detail.headers()['etag']! }, data: { archived: true } });
          expect(archived.status()).toBe(200);
        }
        const subject = await getSubjectByName('N29 linked account residual');
        if (subject) {
          const detail = await page.request.get(`/api/v1/households/${householdId}/subjects/${subject.id}`);
          const archived = await page.request.patch(`/api/v1/households/${householdId}/subjects/${subject.id}`, { headers: { Origin: baseURL, 'If-Match': detail.headers()['etag']! }, data: { archived: true } });
          expect(archived.status()).toBe(200);
        }
      } catch (error) { cleanupFailure ??= error; }
    }
    if (primaryFailure) throw primaryFailure;
    if (cleanupFailure) throw cleanupFailure;
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
    const n17FailedResponses = [`GET /api/v1/households/${householdId}/items/summary 503`, `GET /api/v1/households/${householdId}/members 403`];
    expect([...failedResponses].sort()).toEqual([...allowedFailedResponses, ...n17FailedResponses, subjectConflict, foreignDetailNotFound].sort());
    expect(problems).toEqual([]);
  });
});
