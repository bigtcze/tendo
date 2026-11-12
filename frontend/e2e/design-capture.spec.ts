import { expect, test, type BrowserContextOptions, type Page } from '@playwright/test';
import { mkdirSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { DesignCapture, heightFor, open, WIDTHS, type Catalog } from './design/capture';
import { seedLargeDataset } from './design/seed';
import { REFERENCE_OWNER_LOGIN, REFERENCE_TITLES, seedReferenceFixture } from './referenceFixture';
import { en } from '../src/i18n/en';
import { api, HOUSEHOLD, idempotencyKey, ownerPassword } from './visual/fixtures';

function required(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required; run scripts/e2e-smoke.sh with TENDO_DESIGN_CAPTURE_DIR`);
  return value;
}

/** Fixture lookups fail with a named message instead of a non-null assertion. */
function present<T>(value: T | undefined | null, what: string): T {
  if (value === undefined || value === null) throw new Error(`design fixture is missing ${what}`);
  return value;
}

const dir = resolve(required('E2E_DESIGN_CAPTURE_DIR'));
const BASE_URL = process.env.E2E_BASE_URL;
const CONTEXT: BrowserContextOptions = {
  baseURL: BASE_URL,
  reducedMotion: 'reduce',
  colorScheme: 'light',
  locale: 'en-US',
  timezoneId: 'Europe/Prague',
  viewport: { width: 390, height: 844 },
};
// Item business dates are fixed by the runner; the browser clock is pinned to the same instant for relative copy.
const FIXED_NOW = new Date('2026-05-14T08:00:00Z');
const EXPECTED_CAPTURES = 20 * 6 * 2 * 2 + 2 * 2 * 2 + 2 * 2 * 2 * 2;

/**
 * First run through the real onboarding form. Unlike the visual fixture it does not expect the v4 household-name
 * heading afterwards, so the capture also runs on design prototypes that change the Home heading.
 */
async function onboard(page: Page) {
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: en['onboarding.title'] })).toBeVisible();
  await page.locator('#onboarding-timezone').selectOption('Europe/Prague');
  await page.getByLabel(en['onboarding.setupCode'], { exact: true }).fill(required('E2E_SETUP_TOKEN'));
  await page.getByLabel(en['onboarding.householdName']).fill(HOUSEHOLD);
  await page.locator('#onboarding-login').fill(REFERENCE_OWNER_LOGIN);
  await page.locator('#onboarding-password').fill(present(ownerPassword, 'E2E_OWNER_PASSWORD'));
  await page.getByRole('button', { name: en['onboarding.submit'] }).click();
  await expect(page.getByRole('heading', { level: 1, name: en['onboarding.title'] })).toHaveCount(0);
  await expect(page.getByRole('button', { name: en['items.add'], exact: true }).first()).toBeVisible();
  const response = await api(page, 'get', '/api/v1/session');
  expect(response.status(), 'session after onboarding').toBe(200);
  const session = await response.json() as { defaultHouseholdId: string; userId: string };
  return { householdId: session.defaultHouseholdId, apiRoot: `/api/v1/households/${session.defaultHouseholdId}`, ownerId: session.userId };
}

test.describe.configure({ mode: 'serial' });
test.use({ reducedMotion: 'reduce', colorScheme: 'light', locale: 'en-US', timezoneId: 'Europe/Prague' });
// The matrix is large (every state x 6 widths x 2 languages x 2 themes); the runner allows the same hour as the audit.
test.setTimeout(3_600_000);

test('capture every route and state across widths, languages and themes', async ({ page, browser }) => {
  mkdirSync(dir, { recursive: true });
  // The output directory is cleared below, so it must be a capture directory, never an arbitrary non-empty folder.
  const foreign = readdirSync(dir).filter((name) => !name.endsWith('.png') && name !== 'metrics.json');
  expect(foreign, `${dir} holds files that are not design captures; use an empty or capture-only directory`).toEqual([]);
  // A design baseline is a complete set: stale images from an earlier run must not look like current evidence.
  for (const name of readdirSync(dir)) rmSync(join(dir, name));
  const shots = new DesignCapture(dir);
  await page.clock.install({ time: FIXED_NOW });

  // Signed-out first. Onboarding is the one state that only exists before the household does.
  await shots.state(page, 'onboarding', async (target, catalog) => {
    await open(target, '/');
    await expect(target.getByRole('heading', { level: 1, name: catalog['onboarding.title'] })).toBeVisible();
  });

  await page.goto('/');
  await page.evaluate(() => {
    localStorage.setItem('tendo.locale', 'en');
    localStorage.setItem('tendo.theme', 'light');
  });
  const identity = await onboard(page);

  const signedOutContext = await browser.newContext(CONTEXT);
  const signedOut = await signedOutContext.newPage();
  await signedOut.clock.install({ time: FIXED_NOW });
  await shots.state(signedOut, 'login', async (target, catalog) => {
    await open(target, '/login');
    await expect(target.getByRole('heading', { level: 1, name: catalog['login.welcome'] })).toBeVisible();
  });
  await shots.state(signedOut, 'login-error', async (target, catalog) => {
    await open(target, '/login');
    await target.getByLabel(catalog['login.login'], { exact: true }).fill('design_missing_user');
    await target.getByLabel(catalog['login.password'], { exact: true }).fill('design-wrong-password');
    await target.getByRole('button', { name: catalog['login.submit'], exact: true }).click();
    await expect(target.getByRole('alert')).toHaveText(catalog['login.error.invalid']);
  });
  // A complete-looking but unknown token is the invite accept screen; nothing is submitted, so no secret is used.
  await shots.state(signedOut, 'invite-accept', async (target, catalog) => {
    await open(target, `/invite#${'Z'.repeat(43)}`);
    await expect(target.getByRole('heading', { level: 1, name: catalog['invite.title'] })).toBeVisible();
    await expect(target.getByLabel(catalog['invite.login'], { exact: true })).toBeVisible();
  });
  await shots.state(signedOut, 'reset-password-invalid', async (target, catalog) => {
    await open(target, '/reset-password#not-a-real-token');
    await expect(target.getByRole('heading', { level: 1, name: catalog['reset.invalid.title'] })).toBeVisible();
  });
  await signedOutContext.close();

  // Home checks avoid the household-name heading so the same capture runs unchanged on design prototypes.
  // Signed in: an authentic empty Home first, then the approved reference household through the real API.
  await shots.state(page, 'home-empty', async (target, catalog) => {
    await open(target, '/');
    await expect(target.getByRole('main').getByRole('heading', { level: 1 })).toBeVisible();
    await expect(target.getByText(catalog['home.empty.title'], { exact: true })).toBeVisible();
  });

  const reference = await seedReferenceFixture(page, {
    householdId: identity.householdId,
    apiRoot: identity.apiRoot,
    ownerPassword,
  });

  await shots.state(page, 'home-populated', async (target) => {
    await open(target, '/');
    await expect(target.getByRole('main').getByRole('heading', { level: 1 })).toBeVisible();
    await expect(target.locator('main li[data-item-id]')).toHaveCount(REFERENCE_TITLES.length);
  }, { fullPageAt: [390, 1440] });

  await shots.state(page, 'all-items', async (target, catalog) => {
    await open(target, '/items');
    await expect(target.getByRole('heading', { level: 1, name: catalog['allItems.title'] })).toBeVisible();
    await expect(target.locator('main li[data-item-id]').first()).toBeVisible();
  });

  await shots.state(page, 'all-items-no-match', async (target, catalog) => {
    await open(target, '/items');
    await target.getByRole('searchbox', { name: catalog['allItems.search'], exact: true }).fill('design-no-such-item-xyz');
    await target.getByRole('button', { name: catalog['allItems.searchButton'], exact: true }).click();
    await expect(target.getByRole('heading', { name: catalog['allItems.noMatch'] })).toBeVisible();
  });

  const detailItem = present(reference.items[0], 'reference item 1');
  await shots.state(page, 'item-detail', async (target) => {
    await open(target, `/items/${detailItem.id}`);
    await expect(target.getByRole('heading', { level: 1, name: detailItem.title, exact: true })).toBeVisible();
  }, { fullPageAt: [390, 1440] });

  // Done history is real data: complete the recurring reference item once through the API, then reopen it.
  const current = await api(page, 'get', `${identity.apiRoot}/items/${detailItem.id}`);
  expect(current.status(), 'read detail fixture before completion').toBe(200);
  const completed = await api(page, 'post', `${identity.apiRoot}/items/${detailItem.id}/completions`, {},
    { 'Idempotency-Key': idempotencyKey(), 'If-Match': current.headers()['etag'] ?? '' });
  expect(completed.status(), 'complete detail fixture for its history').toBe(201);
  await shots.state(page, 'item-detail-history', async (target, catalog) => {
    await open(target, `/items/${detailItem.id}`);
    await expect(target.getByRole('heading', { level: 1, name: detailItem.title, exact: true })).toBeVisible();
    await expect(target.getByRole('heading', { name: catalog['itemDetail.history'] })).toBeVisible();
    await expect(target.getByRole('main').locator('ol li')).toHaveCount(2);
  });

  const openAdd = async (target: Page, catalog: Catalog) => {
    await open(target, '/');
    await target.getByRole('button', { name: catalog['items.add'], exact: true }).click();
    const form = target.getByRole('form', { name: catalog['items.add.formLabel'] });
    await expect(form).toBeVisible();
    return form;
  };
  await shots.state(page, 'add-item', async (target, catalog) => {
    await openAdd(target, catalog);
  });
  await shots.state(page, 'add-item-repeat', async (target, catalog) => {
    const form = await openAdd(target, catalog);
    await form.getByRole('switch', { name: catalog['items.repeat'], exact: true }).check();
    await expect(form.getByRole('spinbutton')).toBeVisible();
  });

  const editItem = present(reference.items[1], 'reference item 2');
  await shots.state(page, 'edit-item', async (target, catalog) => {
    await open(target, `/items/${editItem.id}`);
    await expect(target.getByRole('heading', { level: 1, name: editItem.title, exact: true })).toBeVisible();
    await target.getByRole('button', { name: catalog['itemDetail.edit.button'], exact: true }).click();
    await expect(target.getByRole('form', { name: catalog['itemDetail.edit.formLabel'] })).toBeVisible();
  });

  await shots.state(page, 'people', async (target, catalog) => {
    await open(target, '/people');
    await expect(target.getByRole('heading', { level: 1, name: catalog['subjects.title'] })).toBeVisible();
    await expect(target.getByText(catalog['subjects.loading'], { exact: true })).toHaveCount(0);
    await expect(target.getByRole('main').getByText('Škoda Octavia', { exact: true })).toBeVisible();
  });

  await shots.state(page, 'members', async (target, catalog) => {
    await open(target, '/members');
    await expect(target.getByRole('heading', { level: 1, name: catalog['members.title'] })).toBeVisible();
    await expect(target.getByText(catalog['members.loading'], { exact: true })).toHaveCount(0);
    await expect(target.getByRole('main').getByText(`${REFERENCE_OWNER_LOGIN} (${catalog['members.you']})`, { exact: true })).toBeVisible();
  });

  await shots.state(page, 'account', async (target, catalog) => {
    await open(target, '/account');
    await expect(target.getByRole('heading', { level: 1, name: catalog['account.title'] })).toBeVisible();
  });

  await shots.state(page, 'admin', async (target, catalog) => {
    await open(target, '/admin');
    await expect(target.getByRole('heading', { level: 1, name: catalog['admin.title'] })).toBeVisible();
    await expect(target.getByText(catalog['admin.loading'], { exact: true })).toHaveCount(0);
    await expect(target.getByRole('main').getByRole('list', { name: catalog['admin.list.title'] })).toBeVisible();
  });

  await shots.state(page, 'not-found', async (target, catalog) => {
    await open(target, '/no-such-page');
    await expect(target.getByRole('heading', { level: 1, name: catalog['app.notFound.title'] })).toBeVisible();
  });

  // Server-error evidence is a browser-stubbed failing read, exactly as the visual suite does it.
  const summaryPattern = `**/api/v1/households/${identity.householdId}/items/summary**`;
  await page.route(summaryPattern, (route) => route.fulfill({
    status: 500,
    contentType: 'application/problem+json',
    body: JSON.stringify({ type: 'about:blank', title: 'Internal Server Error', status: 500 }),
  }));
  await shots.state(page, 'server-error', async (target, catalog) => {
    await open(target, '/');
    await expect(target.getByRole('main').getByRole('heading', { level: 1 })).toBeVisible();
    await expect(target.getByRole('alert')).toContainText(catalog['app.error.body']);
    await expect(target.getByRole('button', { name: catalog['app.error.retry'], exact: true })).toBeVisible();
  });
  await page.unroute(summaryPattern);

  // The 1000+ item Home is only worth the seeding cost at the two widths where list scale is a design question.
  await seedLargeDataset(page, identity.apiRoot, present(reference.subjects.get('Home'), "subject 'Home'"));
  await shots.state(page, 'home-large', async (target) => {
    await open(target, '/');
    await expect(target.locator('main li[data-item-id]').first()).toBeVisible();
  }, { widths: [390, 1440] });

  const report = {
    fixedBusinessDate: '2026-05-14T08:00:00Z',
    widths: WIDTHS,
    heights: Object.fromEntries(WIDTHS.map((width) => [width, heightFor(width)])),
    captures: shots.metrics,
  };
  const serialized = JSON.stringify(report, null, 2);
  for (const name of ['E2E_SETUP_TOKEN', 'E2E_OWNER_PASSWORD']) {
    expect(serialized, 'metrics.json must not contain runner credentials').not.toContain(required(name));
  }
  writeFileSync(join(dir, 'metrics.json'), serialized);
  // 20 states x 6 widths x 2 languages x 2 themes, the 1000+ item Home at 2 widths x 2 x 2, and full-page Home and
  // detail at 2 widths x 2 x 2: a partial matrix must fail rather than look like a complete baseline.
  expect(Object.keys(shots.metrics).length, 'metrics entry per capture').toBe(EXPECTED_CAPTURES);
  const pngs = readdirSync(dir).filter((name) => name.endsWith('.png')).map((name) => name.slice(0, -'.png'.length)).sort();
  expect(pngs, 'one screenshot per metrics entry').toEqual(Object.keys(shots.metrics).sort());
  console.info(`design-capture-complete captures=${Object.keys(shots.metrics).length} dir=${dir}`);
});
