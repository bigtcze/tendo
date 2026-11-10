import { expect, test, type Browser, type BrowserContext, type Locator, type Page } from '@playwright/test';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { en } from '../../src/i18n/en';
import { cs } from '../../src/i18n/cs';
import { assertInterReady, capture } from './capture';
import { api, BASE_URL, firstRun, HOUSEHOLD, OWNER_LOGIN, ownerPassword, pinInvitationDates, seed, type Fixture } from './fixtures';
import { captureLargeDataset } from './largeData';
import { REFERENCE_COUNTS, REFERENCE_HOUSEHOLD, REFERENCE_MEMBER_LOGIN, REFERENCE_TITLES, seedReferenceFixture } from '../referenceFixture';

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
let visualResetLink = '';

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

/** The Account groups must be settled and every choice must be either on one row or fully stacked, never partly wrapped. */
async function assertSegmentedLayout(group: Locator, count: number) {
  const geometry = await group.evaluate((element) => {
    const box = element.getBoundingClientRect();
    const options = [...element.querySelectorAll('button')].map((button) => {
      const rect = button.getBoundingClientRect();
      return { top: Math.round(rect.top), left: rect.left, right: rect.right, height: rect.height };
    });
    return { box: { left: box.left, right: box.right }, options, innerWidth: window.innerWidth };
  });
  expect(geometry.options).toHaveLength(count);
  expect([1, count], 'segments form one row or a full stack').toContain(new Set(geometry.options.map((option) => option.top)).size);
  for (const option of geometry.options) {
    expect(option.height, 'segment target height').toBeGreaterThanOrEqual(44);
    expect(option.left).toBeGreaterThanOrEqual(geometry.box.left - 1);
    expect(option.right).toBeLessThanOrEqual(geometry.box.right + 1);
  }
  expect(geometry.box.right).toBeLessThanOrEqual(geometry.innerWidth);
}

async function assertAccountSettled(page: Page, locale: 'en' | 'cs', activeLanguage: 'en' | 'cs') {
  const catalog = locale === 'cs' ? cs : en;
  await expect(page.getByRole('heading', { level: 1, name: catalog['account.title'] })).toBeVisible();
  const main = page.getByRole('main');
  const appearance = main.getByRole('group', { name: catalog['account.appearance.title'] });
  await expect(appearance.getByRole('button', { name: catalog['account.appearance.light'] })).toHaveAttribute('aria-pressed', 'true');
  await assertSegmentedLayout(appearance, 3);
  const language = main.getByRole('group', { name: catalog['language.label'] });
  await expect(language.getByRole('button', { name: activeLanguage === 'cs' ? 'Čeština' : 'English' })).toHaveAttribute('aria-pressed', 'true');
  await assertSegmentedLayout(language, 2);
}

async function assertMembersSettled(page: Page, locale: 'en' | 'cs') {
  const catalog = locale === 'cs' ? cs : en;
  await expect(page.getByRole('heading', { level: 1, name: catalog['members.title'] })).toBeVisible();
  const main = page.getByRole('main');
  const rows = main.getByRole('list', { name: catalog['members.list.title'] }).getByRole('listitem');
  await expect(rows).toHaveCount(3);
  await expect(rows.filter({ hasText: 'novak_owner' })).toBeVisible();
  await expect(main.getByText(catalog['members.invitation.status.pending'], { exact: true })).toBeVisible();
  await expect(main.getByRole('button', { name: catalog['members.invitation.revoke'], exact: true })).toBeVisible();
}
// The status filter label gains its count from the summary request after the first row renders. Capture only once
// that settled label is present, so a native select never re-lays out its option text during screenshot rasterization.
async function waitForAllItemsSummary(page: Page) {
  const status = page.getByRole('main').getByRole('combobox').first();
  await expect(status.locator('option:checked')).toHaveText(/^Active \(\d+\)$/);
}
function escapeRegExp(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}
// Every reference capture starts from the same settled UI: real heading counts, ten resolved titles and subject pills.
async function assertSettledReference(page: Page, locale: 'en' | 'cs') {
  const catalog = locale === 'cs' ? cs : en;
  await expect(page.getByRole('heading', { level: 1, name: REFERENCE_HOUSEHOLD })).toBeVisible();
  await expect(page.locator('main li[data-item-id]')).toHaveCount(REFERENCE_TITLES.length);
  const headings: Array<[string, number]> = [
    [catalog['items.group.needs'], REFERENCE_COUNTS.needsAttention],
    [catalog['items.group.inProgress'], REFERENCE_COUNTS.inProgress],
    [catalog['items.group.waiting'], REFERENCE_COUNTS.waiting],
    [catalog['items.group.upcoming'], REFERENCE_COUNTS.upcoming],
  ];
  for (const [label, count] of headings) {
    await expect(page.getByRole('heading', { level: 2, name: new RegExp(`^${escapeRegExp(label)}\\s+${count}\\b`) })).toBeVisible();
  }
  for (const title of REFERENCE_TITLES) await expect(page.getByText(title, { exact: true })).toBeVisible();
  await expect(page.getByText(catalog['items.subjectLabels.loading'], { exact: true })).toHaveCount(0);
  await expect(page.getByText(catalog['items.subject.unknown'], { exact: true })).toHaveCount(0);
  for (const subject of ['Home', 'Rex', 'Tom', 'Anna', 'Škoda Octavia']) {
    await expect(page.getByRole('main').getByText(subject, { exact: true }).first()).toBeVisible();
  }
}
function referenceDate(iso: string, locale: 'en' | 'cs') {
  return new Intl.DateTimeFormat(locale === 'cs' ? 'cs-CZ' : 'en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' })
    .format(new Date(`${iso}T00:00:00Z`));
}
async function assertReferenceFactsAt390(page: Page, locale: 'en' | 'cs') {
  const catalog = locale === 'cs' ? cs : en;
  const tom = page.locator('main li[data-item-id]').filter({ hasText: "Renew Tom's passport" });
  await expect(tom.getByText(catalog['items.needsSince'].replace('{date}', referenceDate('2026-05-12', locale)), { exact: true }).first()).toBeVisible();
  await expect(tom.getByText(catalog['items.responsible.read'].replace('{name}', 'anna_novak'), { exact: true }).first()).toBeVisible();
  const car = page.locator('main li[data-item-id]').filter({ hasText: 'Schedule the car inspection' });
  await expect(car.getByText(referenceDate('2026-05-28', locale)).first()).toBeVisible();
  await expect(car.getByText(/anna_novak/).first()).toBeVisible();
}
// The title target must be 44 px, win hit-testing at both edges, and start inside its group panel.
async function assertTitleTargetGeometry(page: Page, width: number) {
  const row = page.locator('main li[data-item-id]').filter({ hasText: 'Replace the smoke alarm battery' });
  const link = row.getByRole('link', { name: 'Open item Replace the smoke alarm battery' });
  await expect(link).toBeVisible();
  const geometry = await link.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    const panel = element.closest('ul')!.getBoundingClientRect();
    const midX = rect.left + rect.width / 2;
    const hits = (y: number) => {
      const hit = document.elementFromPoint(midX, y);
      return Boolean(hit && (hit === element || element.contains(hit)));
    };
    return { height: rect.height, top: rect.top, panelTop: panel.top, topHit: hits(rect.top + 2), bottomHit: hits(rect.bottom - 2) };
  });
  expect(geometry.height, `title target height at ${width}`).toBeGreaterThanOrEqual(44);
  expect(geometry.topHit, `title target top-edge hit at ${width}`).toBe(true);
  expect(geometry.bottomHit, `title target bottom-edge hit at ${width}`).toBe(true);
  expect(geometry.top, `title target starts inside the group panel at ${width}`).toBeGreaterThanOrEqual(geometry.panelTop - 0.5);
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
    } else if (path === '/admin') {
      await expect(page.getByRole('main').getByRole('list', { name: en['admin.list.title'] })).toBeVisible();
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
    else if (path === '/admin') await expect(page.getByRole('main').getByRole('list', { name: en['admin.list.title'] })).toBeVisible();
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
  await page.getByRole('button', { name: en['onboarding.submit'] }).click();
  await expect(page.getByRole('alert')).toHaveText(en['onboarding.error.setupCodeEmpty']);
  for (const width of [390, 1440]) await capture(page, `onboarding-error-${width}`, width, 900, true);
  household = await firstRun(page);
  await page.context().storageState({ path: statePath });
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: HOUSEHOLD })).toBeVisible();
  await expect(page.locator('main li[data-item-id]')).toHaveCount(0);
  await captureScreen(page, 'home-empty');
});

test('approved reference fixture at approved widths, locales and themes', async ({ browser }) => {
  test.setTimeout(120_000);
  const { context, page } = await authenticated(browser);
  const session = await page.request.get('/api/v1/session');
  expect(session.status()).toBe(200);
  const { defaultHouseholdId: householdId } = await session.json() as { defaultHouseholdId: string };
  const apiRoot = `/api/v1/households/${householdId}`;
  const reference = await seedReferenceFixture(page, { householdId, apiRoot, ownerPassword });
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: REFERENCE_HOUSEHOLD })).toBeVisible();
  await expect(page.locator('main li[data-item-id]')).toHaveCount(REFERENCE_TITLES.length);
  const summaryResponse = await page.request.get(`${apiRoot}/items/summary`);
  expect(summaryResponse.status()).toBe(200);
  const summary = await summaryResponse.json() as Record<string, number>;
  for (const [key, value] of Object.entries(REFERENCE_COUNTS)) expect(summary[key], `summary ${key}`).toBe(value);
  for (const title of REFERENCE_TITLES) await expect(page.getByText(title, { exact: true })).toBeVisible();

  await assertSettledReference(page, 'en');
  for (const [width, height] of [[1440, 1080], [390, 844], [320, 640], [360, 740], [768, 900], [1024, 900]] as const) {
    await page.setViewportSize({ width, height });
    await assertSettledReference(page, 'en');
    if (width === 390) await assertReferenceFactsAt390(page, 'en');
    if ([320, 390, 1024, 1440].includes(width)) await assertTitleTargetGeometry(page, width);
    if (width === 1440 || width === 1024) {
      await assertInterReady(page, `reference-row-height-${width}`);
      const smokeRow = page.locator('main li[data-item-id]').filter({ hasText: 'Replace the smoke alarm battery' });
      const rowHeight = await smokeRow.evaluate((row) => row.getBoundingClientRect().height);
      console.info(`visual-reference-row-height width=${width} height=${rowHeight}`);
      if (width === 1440) {
        expect(rowHeight, 'one-line reference row height at 1440').toBeGreaterThanOrEqual(48);
        expect(rowHeight, 'one-line reference row height at 1440').toBeLessThanOrEqual(56);
      }
    }
    await capture(page, `home-reference-en-light-${width}`, width, height, true);
  }

  await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
  await page.reload();
  await assertSettledReference(page, 'cs');
  for (const [width, height] of [[390, 844], [1440, 1080]] as const) {
    await page.setViewportSize({ width, height });
    await assertSettledReference(page, 'cs');
    if (width === 390) await assertReferenceFactsAt390(page, 'cs');
    await capture(page, `home-reference-cs-light-${width}`, width, height, true);
  }

  await page.evaluate(() => { localStorage.setItem('tendo.locale', 'en'); localStorage.setItem('tendo.theme', 'dark'); });
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await assertSettledReference(page, 'en');
  for (const [width, height] of [[390, 844], [1440, 1080]] as const) {
    await page.setViewportSize({ width, height });
    await assertSettledReference(page, 'en');
    if (width === 390) await assertReferenceFactsAt390(page, 'en');
    await capture(page, `home-reference-en-dark-${width}`, width, height, true);
  }
  await page.evaluate(() => localStorage.setItem('tendo.theme', 'light'));
  // Title and Done are separate controls: the title links to detail, Done completes without navigating, and Undo restores.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/');
  await assertSettledReference(page, 'en');
  const smokeRow = page.locator('main li[data-item-id]').filter({ hasText: 'Replace the smoke alarm battery' });
  await smokeRow.getByRole('link', { name: 'Open item Replace the smoke alarm battery' }).click();
  await expect(page).toHaveURL(/\/items\/[0-9a-f-]{36}$/);
  await page.goBack();
  await assertSettledReference(page, 'en');
  const restoredRow = page.locator('main li[data-item-id]').filter({ hasText: 'Replace the smoke alarm battery' });
  await restoredRow.getByRole('button', { name: 'Done: Replace the smoke alarm battery' }).click();
  await expect(page).toHaveURL('/');
  const undo = page.getByRole('button', { name: en['items.undo'], exact: true });
  await expect(undo).toBeVisible();
  await undo.click();
  await expect(page.locator('main li[data-item-id]').filter({ hasText: 'Replace the smoke alarm battery' })).toBeVisible();
  // The reference fixture is a gated visual milestone; archive it so later stress captures keep their bounded shape.
  for (const item of reference.items) {
    const current = await api(page, 'get', `${apiRoot}/items/${item.id}`);
    expect(current.status()).toBe(200);
    const archived = await api(page, 'patch', `${apiRoot}/items/${item.id}`, { archived: true }, { 'If-Match': current.headers()['etag'] ?? '' });
    expect(archived.status()).toBe(200);
  }
  await context.storageState({ path: statePath });
  await context.close();
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
  test.setTimeout(90_000);
  const { context, page } = await authenticated(browser);
  const invite = await page.request.post(`${requiredFixture().apiRoot}/invitations`, {
    headers: { Origin: BASE_URL!, 'Idempotency-Key': crypto.randomUUID().replaceAll('-', '') },
    data: {},
  });
  expect(invite.status()).toBe(201);
  const { token: inviteToken } = await invite.json() as { token: string };
  await page.goto('/members');
  await assertMembersSettled(page, 'en');
  await captureScreen(page, 'members-owner', true);
  await page.setViewportSize({ width: 320, height: 640 });
  await assertMembersSettled(page, 'en');
  await capture(page, 'members-owner-320', 320, 640, true);
  await page.setViewportSize({ width: 390, height: 900 });
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
  await page.reload();
  await assertMembersSettled(page, 'cs');
  await capture(page, 'members-owner-cs-390', 390, 900, true);
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'en'));
  await page.reload();
  await context.close();
  await darkScreen(browser, statePath, '/members', 'members');
  // The same pending token renders the real acceptance screen for a signed-out visitor.
  const inviteContext = await browser.newContext({ ...CONTEXT, viewport: { width: 390, height: 844 } });
  const invitePage = await inviteContext.newPage();
  await invitePage.clock.install({ time: new Date('2026-05-14T08:00:00Z') });
  await invitePage.goto(`/invite#${inviteToken}`);
  await expect(invitePage.getByRole('heading', { level: 1, name: en['invite.title'] })).toBeVisible();
  await expect(invitePage.getByLabel(en['invite.login'])).toBeVisible();
  await expect(invitePage.getByLabel(en['onboarding.password'])).toBeVisible();
  await expect(invitePage.getByRole('button', { name: en['invite.create'], exact: true })).toBeVisible();
  await captureScreen(invitePage, 'invite-accept', true);
  await inviteContext.close();
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

test('Admin center and the member refusal', async ({ browser }) => {
  test.setTimeout(90_000);
  const { context, page } = await authenticated(browser);
  await page.goto('/admin');
  await expect(page.getByRole('heading', { level: 1, name: en['admin.title'] })).toBeVisible();
  const accounts = page.getByRole('main').getByRole('list', { name: en['admin.list.title'] });
  // The named fixtures are what this capture is about; another seeded member must not invalidate it.
  const ownerAccountRow = accounts.getByRole('listitem').filter({ hasText: OWNER_LOGIN });
  const referenceAccountRow = accounts.getByRole('listitem').filter({ hasText: REFERENCE_MEMBER_LOGIN });
  const visualMemberAccountRow = accounts.getByRole('listitem').filter({ hasText: 'visual_member' });
  await expect(ownerAccountRow).toBeVisible();
  await expect(ownerAccountRow).toContainText(en['members.you']);
  await expect(referenceAccountRow).toContainText(en['members.role.member']);
  await expect(visualMemberAccountRow).toContainText(en['members.role.member']);
  expect(await accounts.getByRole('listitem').count()).toBeGreaterThanOrEqual(3);
  await darkScreen(browser, statePath, '/admin', 'admin');

  // Czech phone width after the shared dark pass restored the light theme.
  await page.goto('/admin');
  await page.setViewportSize({ width: 390, height: 900 });
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
  await page.reload();
  await expect(page.getByRole('heading', { level: 1, name: cs['admin.title'] })).toBeVisible();
  await expect(page.getByRole('main').getByRole('list', { name: cs['admin.list.title'] })).toBeVisible();
  await capture(page, 'admin-cs-390', 390, 900, true);
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'en'));
  await context.close();

  // A plain member who opens the address directly: the server refuses and no account is listed.
  const memberContext = await browser.newContext({ ...CONTEXT, viewport: { width: 390, height: 844 } });
  const memberPage = await memberContext.newPage();
  await memberPage.clock.install({ time: new Date('2026-05-14T08:00:00Z') });
  await memberPage.goto('/login');
  await memberPage.getByLabel(en['onboarding.login'], { exact: true }).fill('visual_member');
  await memberPage.getByLabel(en['login.password'], { exact: true }).fill(`${ownerPassword}-member`);
  await memberPage.getByRole('button', { name: en['login.submit'], exact: true }).click();
  await expect(memberPage.getByRole('heading', { level: 1, name: HOUSEHOLD })).toBeVisible();
  await memberPage.goto('/admin');
  await expect(memberPage.getByRole('heading', { name: en['admin.onlyAdmins.title'] })).toBeVisible();
  await expect(memberPage.getByRole('main').getByRole('list', { name: en['admin.list.title'] })).toHaveCount(0);
  await expect(memberPage.getByRole('navigation', { name: en['nav.label'] }).getByRole('link', { name: en['admin.title'], exact: true })).toBeHidden();
  await capture(memberPage, 'admin-member-390', 390, 900, true);
  await memberPage.setViewportSize({ width: 1440, height: 900 });
  await capture(memberPage, 'admin-member-1440', 1440, 900, false);
  await memberContext.close();
});

test('Account', async ({ browser }) => {
  const { context, page } = await authenticated(browser);
  await page.goto('/account');
  await assertAccountSettled(page, 'en', 'en');
  await captureScreen(page, 'account', true);
  await page.setViewportSize({ width: 320, height: 640 });
  await assertAccountSettled(page, 'en', 'en');
  await capture(page, 'account-320', 320, 640, true);
  await page.setViewportSize({ width: 390, height: 900 });
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
  await page.reload();
  await assertAccountSettled(page, 'cs', 'cs');
  await capture(page, 'account-cs-390', 390, 900, true);
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'en'));
  await page.reload();
  await context.close();
  await darkScreen(browser, statePath, '/account', 'account');
});

test('Not found signed in and signed out', async ({ browser }) => {
  const { context, page } = await authenticated(browser);
  await page.goto('/no-such-page');
  await expect(page.getByRole('heading', { level: 1, name: en['app.notFound.title'] })).toBeVisible();
  await captureScreen(page, 'not-found');
  await context.close();
  const signedOut = await browser.newContext({ ...CONTEXT, viewport: { width: 390, height: 844 } });
  const signedOutPage = await signedOut.newPage();
  await signedOutPage.clock.install({ time: new Date('2026-05-14T08:00:00Z') });
  await signedOutPage.goto('/no-such-page');
  await expect(signedOutPage.getByRole('heading', { level: 1, name: en['app.notFound.title'] })).toBeVisible();
  await captureScreen(signedOutPage, 'not-found-signed-out');
  await signedOut.close();
});

test('Signed-out connection error', async ({ browser }) => {
  const context = await browser.newContext({ ...CONTEXT, viewport: { width: 390, height: 844 } });
  const page = await context.newPage();
  await page.clock.install({ time: new Date('2026-05-14T08:00:00Z') });
  await page.route('**/api/v1/session', (route) => route.fulfill({
    status: 503, contentType: 'application/problem+json',
    body: JSON.stringify({ type: 'about:blank', title: 'Unavailable', status: 503 }),
  }));
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: en['app.error.title'] })).toBeVisible();
  await captureScreen(page, 'error-screen');
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

/** Opens the Admin center, asks for a reset, and returns the one-time link the dialog shows. */
async function issueVisualReset(page: Page): Promise<string> {
  const resetLabel = en['admin.reset.actionLabel'].replace('{login}', 'visual_member');
  const renewLabel = en['admin.reset.renewLabel'].replace('{login}', 'visual_member');
  const row = page.getByRole('main').getByRole('list', { name: en['admin.list.title'] })
    .getByRole('listitem').filter({ hasText: 'visual_member' });
  await row.getByRole('button', { name: resetLabel, exact: true }).or(row.getByRole('button', { name: renewLabel, exact: true })).click();
  await page.getByRole('dialog').getByRole('button', { name: en['admin.reset.dialog.confirm'], exact: true }).click();
  const linkDialog = page.getByRole('dialog', { name: en['admin.reset.link.title'].replace('{login}', 'visual_member') });
  const field = linkDialog.getByRole('textbox', { name: en['admin.reset.link.label'] });
  await expect(field).toBeFocused();
  const link = await field.inputValue();
  // The native runner listens on a random port, so the real origin would change the pixels every run.
  // Only the displayed text is pinned (no region is masked); the real link is returned for the redeem captures.
  await field.evaluate((input: HTMLInputElement, pinned) => {
    input.value = pinned;
    input.setSelectionRange(0, pinned.length);
  }, `https://tendo.example/reset-password#${VISUAL_RESET_TOKEN}`);
  return link;
}

/** A fixed, valid-shaped token keeps the one-time-link dialog and the reset screen byte-identical between runs. */
const VISUAL_RESET_TOKEN = 'V'.repeat(43);
async function pinPasswordResetIssuing(context: BrowserContext) {
  await context.route(
    (url) => /^\/api\/v1\/households\/[^/]+\/accounts\/[^/]+\/password-reset$/.test(url.pathname),
    async (route) => {
      if (route.request().method() !== 'POST') return route.fallback();
      // The visual capture never redeems the link, so it needs no server-side token; fixed times keep the
      // expiry text stable while the real server clock would change it every day.
      await route.fulfill({
        status: 201,
        contentType: 'application/json',
        body: JSON.stringify({
          token: VISUAL_RESET_TOKEN,
          createdAt: '2026-05-14T08:00:00Z',
          expiresAt: '2026-05-15T08:00:00Z',
        }),
      });
    },
  );
}

test('Admin reset-link dialog', async ({ browser }) => {
  test.setTimeout(90_000);
  const { context, page } = await authenticated(browser);
  await pinPasswordResetIssuing(context);
  await page.goto('/admin');
  await expect(page.getByRole('main').getByRole('list', { name: en['admin.list.title'] })).toBeVisible();

  visualResetLink = await issueVisualReset(page);
  expect(visualResetLink.startsWith(`${BASE_URL}/reset-password#`)).toBe(true);
  await capture(page, 'admin-reset-link-light-1440', 1440, 900, false);
  await capture(page, 'admin-reset-link-light-390', 390, 900, false);

  // Saved dark theme survives the reload; a new issuance gives the same one-time-link dialog in dark.
  await page.evaluate(() => localStorage.setItem('tendo.theme', 'dark'));
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(page.getByRole('main').getByRole('list', { name: en['admin.list.title'] })).toBeVisible();
  visualResetLink = await issueVisualReset(page);
  await capture(page, 'admin-reset-link-dark-390', 390, 900, false);

  await page.evaluate(() => localStorage.setItem('tendo.theme', 'light'));
  await context.close();
});

test('Reset-password screen', async ({ browser }) => {
  test.setTimeout(90_000);
  expect(visualResetLink, 'the admin dialog test must issue the link first').not.toBe('');

  async function openReset(path: string, init?: () => void) {
    const context = await browser.newContext({ ...CONTEXT, viewport: { width: 1440, height: 900 } });
    if (init) await context.addInitScript(init);
    const page = await context.newPage();
    await page.clock.install({ time: new Date('2026-05-14T08:00:00Z') });
    await page.goto(path);
    return { context, page };
  }

  const light = await openReset(visualResetLink);
  await expect(light.page.getByRole('heading', { level: 1, name: en['reset.title'] })).toBeVisible();
  await capture(light.page, 'reset-password-light-1440', 1440, 900, true);
  await capture(light.page, 'reset-password-light-390', 390, 900, true);
  await light.context.close();

  const czech = await openReset(visualResetLink, () => localStorage.setItem('tendo.locale', 'cs'));
  await expect(czech.page.getByRole('heading', { level: 1, name: cs['reset.title'] })).toBeVisible();
  await capture(czech.page, 'reset-password-cs-390', 390, 900, true);
  await czech.context.close();

  const dark = await openReset(visualResetLink, () => localStorage.setItem('tendo.theme', 'dark'));
  await expect(dark.page.getByRole('heading', { level: 1, name: en['reset.title'] })).toBeVisible();
  await expect(dark.page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await capture(dark.page, 'reset-password-dark-390', 390, 900, true);
  await dark.context.close();

  // A malformed fragment is an invalid link; nothing is submitted.
  const invalid = await browser.newContext({ ...CONTEXT, viewport: { width: 390, height: 900 } });
  const invalidPage = await invalid.newPage();
  await invalidPage.clock.install({ time: new Date('2026-05-14T08:00:00Z') });
  await invalidPage.goto('/reset-password#not-a-real-token');
  await expect(invalidPage.getByRole('heading', { level: 1, name: en['reset.invalid.title'] })).toBeVisible();
  await capture(invalidPage, 'reset-password-invalid-390', 390, 900, true);
  await invalid.close();
});
