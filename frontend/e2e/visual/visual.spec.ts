import { expect, test, type Browser, type BrowserContext, type Page } from '@playwright/test';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { en } from '../../src/i18n/en';
import { cs } from '../../src/i18n/cs';
import { capture } from './capture';
import { api, BASE_URL, firstRun, HOUSEHOLD, ownerPassword, pinInvitationDates, seed, type Fixture } from './fixtures';
import { captureLargeDataset } from './largeData';

test.describe.configure({ mode: 'serial' });
// Browser time zone feeds the onboarding default and date rendering; pin it so baselines match on UTC CI runners.
const TIME_ZONE = 'Europe/Prague';
const CONTEXT = { baseURL: BASE_URL, reducedMotion: 'reduce', locale: 'en-US', timezoneId: TIME_ZONE } as const;
test.use({ reducedMotion: 'reduce', colorScheme: 'light', locale: 'en-US', timezoneId: TIME_ZONE });
// The owner session cookie lives in a private (0700) per-run directory removed after the suite.
const stateDir = mkdtempSync(join(tmpdir(), 'tendo-visual-'));
const statePath = join(stateDir, 'owner-state.json');
test.afterAll(() => rmSync(stateDir, { recursive: true, force: true }));
let household: Awaited<ReturnType<typeof firstRun>> | undefined;
let fixture: Fixture | undefined;

async function authenticated(browser: Browser): Promise<{ context: BrowserContext; page: Page }> {
  const context = await browser.newContext({ ...CONTEXT, storageState: statePath, viewport: { width: 390, height: 844 } });
  await pinInvitationDates(context);
  const page = await context.newPage();
  await page.clock.install({ time: new Date('2026-05-14T08:00:00Z') });
  return { context, page };
}
function requiredFixture(): Fixture {
  if (!fixture) throw new Error('seed fixture test did not run');
  return fixture;
}
// The Add dialog focuses its title field after showModal; wait for it so capture()'s blur cannot race it.
async function openAdd(page: Page, label: string) {
  await page.getByRole('button', { name: label, exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole('textbox').first()).toBeFocused();
  return dialog;
}
async function captureScreen(page: Page, base: string, fullPage = false) {
  for (const width of [390, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await capture(page, `${base}-${width}`, width, 900, fullPage);
  }
}
// The status filter label gains its count from the summary request after the first row renders. Capture only once
// that settled label is present, so a native select never re-lays out its option text during screenshot rasterization.
async function waitForAllItemsSummary(page: Page) {
  const status = page.getByRole('main').getByRole('combobox').first();
  await expect(status.locator('option:checked')).toHaveText(/^Active \(\d+\)$/);
}
// Captures one route in saved light then saved dark theme. Snapshot names must stay unique across the suite.
async function darkScreen(browser: Browser, storageState: string | undefined, path: string, name: string) {
  const context = await browser.newContext({ ...CONTEXT, ...(storageState ? { storageState } : {}), viewport: { width: 390, height: 900 } });
  await pinInvitationDates(context);
  const page = await context.newPage();
  await page.clock.install({ time: new Date('2026-05-14T08:00:00Z') });
  for (const width of [390, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await page.goto(path);
    await expect(page.locator('main')).toBeVisible();
    if (name === 'home-add') {
      await openAdd(page, en['items.add']);
    } else if (path === '/') {
      await expect(page.locator('main li[data-item-id]')).toHaveCount(requiredFixture().items.length);
    } else if (path === '/items') {
      await expect(page.locator('main li[data-item-id]').first()).toBeVisible();
      await waitForAllItemsSummary(page);
    } else {
      await expect(page.getByRole('heading', { level: 1 }).first()).toBeVisible();
    }
    const rows = await page.locator('main li[data-item-id] a[aria-label]').allTextContents();
    await capture(page, `${name}-light-${width}`, width, 900, false);
    await page.evaluate(() => localStorage.setItem('tendo.theme', 'dark'));
    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await expect(page.locator('main')).toBeVisible();
    if (name === 'home-add') await openAdd(page, en['items.add']);
    else await expect(page.locator('main li[data-item-id] a[aria-label]')).toHaveText(rows);
    if (path === '/items') await waitForAllItemsSummary(page);
    await page.evaluate(() => window.scrollTo(0, 0));
    await capture(page, `${name}-dark-${width}`, width, 900, false);
    await page.evaluate(() => localStorage.setItem('tendo.theme', 'light'));
  }
  await context.close();
}

// First run intentionally precedes fixture creation to capture authentic onboarding and empty Home.
test('onboarding and empty Home', async ({ page }) => {
  test.setTimeout(60_000);
  await page.clock.install({ time: new Date('2026-05-14T08:00:00Z') });
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: en['onboarding.title'] })).toBeVisible();
  await expect(page.locator('#onboarding-timezone')).toHaveValue(TIME_ZONE);
  for (const width of [320, 390, 768, 1440]) await capture(page, `onboarding-${width}`, width, width === 320 ? 640 : 900, true);
  await page.evaluate(() => { localStorage.setItem('tendo.theme', 'dark'); document.documentElement.dataset.theme = 'dark'; });
  for (const width of [390, 1440]) await capture(page, `onboarding-dark-${width}`, width, 900, false);
  await page.evaluate(() => { localStorage.removeItem('tendo.theme'); document.documentElement.removeAttribute('data-theme'); });
  household = await firstRun(page);
  await page.context().storageState({ path: statePath });
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: HOUSEHOLD })).toBeVisible();
  await expect(page.locator('main li[data-item-id]')).toHaveCount(0);
  await captureScreen(page, 'home-empty');
});

test('API-seeded shared household state', async ({ browser }) => {
  test.setTimeout(60_000);
  if (!household) throw new Error('onboarding test did not establish household');
  const { context, page } = await authenticated(browser);
  fixture = await seed(page, household);
  await context.storageState({ path: statePath });
  await context.close();
});

test('Home populated and dark', async ({ browser }) => {
  test.setTimeout(60_000);
  const { context, page } = await authenticated(browser);
  for (const width of [320, 390, 768, 1440]) {
    await page.goto('/');
    await expect(page.locator('html')).not.toHaveAttribute('data-theme', 'dark');
    await expect(page.locator('main li[data-item-id]')).toHaveCount(requiredFixture().items.length);
    await capture(page, `home-populated-${width}`, width, width === 320 ? 640 : 900, false);
  }
  await context.close();
  await darkScreen(browser, statePath, '/', 'home');
});

test('Add form and Repeat cadence', async ({ browser }) => {
  const { context, page } = await authenticated(browser);
  await page.goto('/');
  for (const width of [320, 390, 768, 1440]) {
    const dialog = await openAdd(page, en['items.add']);
    await capture(page, `add-form-${width}`, width, width === 320 ? 640 : 900);
    if (width === 390 || width === 1440) {
      await dialog.getByRole('switch', { name: en['items.repeat'], exact: true }).check();
      await expect(dialog.getByRole('spinbutton')).toBeVisible();
      await capture(page, `add-repeat-${width}`, width, 900);
    }
    await dialog.getByRole('form', { name: en['items.add.formLabel'] }).getByRole('button', { name: en['items.cancel'], exact: true }).click();
  }
  await context.close();
  await darkScreen(browser, statePath, '/', 'home-add');
});

test('All items populated, no-match and dark', async ({ browser }) => {
  const { context, page } = await authenticated(browser);
  await page.goto('/items');
  await waitForAllItemsSummary(page);
  for (const width of [320, 390, 768, 1440]) await capture(page, `all-items-${width}`, width, width === 320 ? 640 : 900, false);
  await page.getByRole('searchbox', { name: en['allItems.search'], exact: true }).fill('no-such-visual-item');
  await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
  await expect(page.getByRole('main').getByRole('heading', { name: en['allItems.noMatch'] })).toBeVisible();
  await captureScreen(page, 'all-no-match');
  await darkScreen(browser, statePath, '/items', 'all-items');
  await context.close();
});

test('Detail open, done and archived', async ({ browser }) => {
  const { context, page } = await authenticated(browser);
  const item = requiredFixture().items[0];
  await page.goto(`/items/${item.id}`);
  await expect(page.getByRole('heading', { level: 1, name: item.title })).toBeVisible();
  await captureScreen(page, 'detail-open', true);
  await darkScreen(browser, statePath, `/items/${item.id}`, 'detail');
  await page.goto(`/items/${item.id}`);
  const current = await api(page, 'get', `${requiredFixture().apiRoot}/items/${item.id}`);
  const etag = current.headers()['etag'];
  expect(etag).toBeTruthy();
  const completion = await api(page, 'post', `${requiredFixture().apiRoot}/items/${item.id}/completions`, {}, { 'Idempotency-Key': crypto.randomUUID().replaceAll('-', ''), 'If-Match': etag ?? '' });
  expect(completion.status()).toBe(201);
  await page.reload(); await captureScreen(page, 'detail-done-history', true);
  const archiveCurrent = await api(page, 'get', `${requiredFixture().apiRoot}/items/${item.id}`);
  const archived = await api(page, 'patch', `${requiredFixture().apiRoot}/items/${item.id}`, { archived: true }, { 'If-Match': archiveCurrent.headers()['etag'] ?? '' });
  expect(archived.status()).toBe(200);
  await page.reload(); await captureScreen(page, 'detail-archived', true);
  await context.close();
});

test('Edit form and optimistic conflict', async ({ browser }) => {
  const { context, page } = await authenticated(browser);
  const item = requiredFixture().items[1];
  await page.goto(`/items/${item.id}`);
  const openEdit = async () => {
    await page.goto(`/items/${item.id}`);
    await page.getByRole('button', { name: en['itemDetail.edit.button'], exact: true }).click();
    const form = page.getByRole('form', { name: en['itemDetail.edit.formLabel'] });
    await expect(form).toBeVisible();
    return form;
  };
  await openEdit();
  await captureScreen(page, 'edit-form');
  const current = await api(page, 'get', `${requiredFixture().apiRoot}/items/${item.id}`);
  const changed = await api(page, 'patch', `${requiredFixture().apiRoot}/items/${item.id}`, { notes: 'Concurrent edit to trigger conflict.' }, { 'If-Match': current.headers()['etag'] ?? '' });
  expect(changed.status()).toBe(200);
  await page.reload();
  const conflictForm = await openEdit();
  await conflictForm.getByLabel(en['items.title']).fill('Conflicting visual edit');
  const stale = await api(page, 'get', `${requiredFixture().apiRoot}/items/${item.id}`);
  const concurrent = await api(page, 'patch', `${requiredFixture().apiRoot}/items/${item.id}`, { notes: 'Concurrent edit to trigger 412.' }, { 'If-Match': stale.headers()['etag'] ?? '' });
  expect(concurrent.status()).toBe(200);
  await conflictForm.getByRole('button', { name: en['itemDetail.save'], exact: true }).click();
  await expect(page.getByText(en['itemDetail.edit.conflict'], { exact: true })).toBeVisible();
  await captureScreen(page, 'edit-conflict');
  await context.close();
});

test('People current and archived', async ({ browser }) => {
  const { context, page } = await authenticated(browser);
  await page.goto('/people'); await captureScreen(page, 'people-current', true);
  const subject = requiredFixture().subjects[0];
  const current = await api(page, 'get', `${requiredFixture().apiRoot}/subjects/${subject.id}`);
  const response = await api(page, 'patch', `${requiredFixture().apiRoot}/subjects/${subject.id}`, { archived: true }, { 'If-Match': current.headers()['etag'] ?? '' });
  expect(response.status()).toBe(200);
  await page.reload();
  await page.getByRole('button', { name: en['subjects.showArchived'], exact: true }).click();
  await expect(page.getByRole('main').getByText(subject.name, { exact: true })).toBeVisible();
  await captureScreen(page, 'people-archived', true);
  await darkScreen(browser, statePath, '/people', 'people');
  await context.close();
});

test('Members owner and non-owner', async ({ browser }) => {
  const { context, page } = await authenticated(browser);
  await page.goto('/members'); await captureScreen(page, 'members-owner', true);
  await context.close();
  await darkScreen(browser, statePath, '/members', 'members');
  const memberContext = await browser.newContext({ ...CONTEXT, viewport: { width: 390, height: 844 } });
  const memberPage = await memberContext.newPage();
  await memberPage.clock.install({ time: new Date('2026-05-14T08:00:00Z') });
  await memberPage.goto('/login');
  await memberPage.getByLabel(en['onboarding.login'], { exact: true }).fill('visual_member');
  await memberPage.getByLabel(en['login.password'], { exact: true }).fill(`${ownerPassword}-member`);
  await memberPage.getByRole('button', { name: en['login.submit'], exact: true }).click();
  await expect(memberPage.getByRole('heading', { level: 1, name: HOUSEHOLD })).toBeVisible();
  await memberPage.goto('/members'); await captureScreen(memberPage, 'members-non-owner', true);
  await memberContext.close();
});

test('Account', async ({ browser }) => {
  const { context, page } = await authenticated(browser);
  await page.goto('/account'); await captureScreen(page, 'account', true); await darkScreen(browser, statePath, '/account', 'account');
  await context.close();
});

test('Signed-out login and login error', async ({ browser }) => {
  const context = await browser.newContext({ ...CONTEXT, viewport: { width: 390, height: 844 } });
  const page = await context.newPage(); await page.clock.install({ time: new Date('2026-05-14T08:00:00Z') });
  await page.goto('/login'); await captureScreen(page, 'login', true);
  await page.getByLabel(en['onboarding.login'], { exact: true }).fill('unknown_visual_login');
  await page.getByLabel(en['login.password'], { exact: true }).fill('invalid-visual-password');
  await page.getByRole('button', { name: en['login.submit'], exact: true }).click();
  await expect(page.getByRole('alert')).toBeVisible(); await captureScreen(page, 'login-error', true);
  await darkScreen(browser, undefined, '/login', 'login');
  await context.close();
});

test('Czech Home, add form and detail', async ({ browser }) => {
  const { context, page } = await authenticated(browser);
  await page.goto('/'); await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs')); await page.reload();
  await captureScreen(page, 'home-cs', true);
  await openAdd(page, cs['items.add']);
  await captureScreen(page, 'add-cs', false);
  await page.getByRole('dialog').getByRole('button', { name: cs['items.cancel'], exact: true }).first().click();
  await page.goto(`/items/${requiredFixture().items[0].id}`);
  await captureScreen(page, 'detail-cs', true);
  await context.close();
});

test('Home and All items retry errors', async ({ browser }) => {
  const { context, page } = await authenticated(browser);
  for (const [path, name] of [['/', 'home-server-error'], ['/items', 'all-server-error']] as const) {
    await page.route('**/api/v1/households/*/items/summary*', route => route.fulfill({ status: 503, contentType: 'application/problem+json', body: '{"type":"about:blank","title":"Unavailable","status":503}' }));
    await page.goto(path); await expect(page.getByRole('button', { name: en['app.error.retry'], exact: true })).toBeVisible();
    await captureScreen(page, name);
    await page.unrouteAll();
  }
  await context.close();
});

test('Large active dataset remains bounded', async ({ browser }) => {
  test.setTimeout(300_000);
  const { context, page } = await authenticated(browser);
  await captureLargeDataset(page, requiredFixture());
  await context.close();
});
