import { expect, test, type Browser, type BrowserContextOptions, type Page } from '@playwright/test';
import { mkdirSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { DesignCapture, heightFor, open, WIDTHS, type Catalog } from './design/capture';
import { HeldRequest } from './design/hold';
import { seedLargeDataset } from './design/seed';
import {
  REFERENCE_MEMBER_LOGIN,
  REFERENCE_MEMBER_PASSWORD_SUFFIX,
  REFERENCE_OWNER_LOGIN,
  REFERENCE_TITLES,
  seedReferenceFixture,
} from './referenceFixture';
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
const MEMBER_PASSWORD = `${present(ownerPassword, 'E2E_OWNER_PASSWORD')}${REFERENCE_MEMBER_PASSWORD_SUFFIX}`;
/**
 * 35 everyday states x 6 widths x 2 languages x 2 themes, the 1000+ item Home at 2 widths x 2 x 2, and an extra
 * full-page image for the 4 states whose point can sit below the fold (Home, detail, and the two Members states at
 * the two phone widths) at 2 widths x 2 x 2. A partial matrix must fail rather than look like a complete baseline.
 */
const EXPECTED_CAPTURES = 35 * 6 * 2 * 2 + 2 * 2 * 2 + 4 * 2 * 2 * 2;
/** Below this width the invite link and the revoke confirmation are cut off by the fold, so they need a full image. */
const FULL_PAGE_PHONES = [320, 390] as const;

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

/**
 * A second real browser context signed in as the reference household's member. The member account (and its known
 * password) comes from the reference fixture's own invitation acceptance, so no extra invitation is created here.
 */
async function signInMember(browser: Browser) {
  const context = await browser.newContext(CONTEXT);
  const member = await context.newPage();
  await member.clock.install({ time: FIXED_NOW });
  await member.goto('/login');
  await member.evaluate(() => localStorage.setItem('tendo.locale', 'en'));
  await member.reload();
  await expect(member.getByRole('form', { name: en['login.heading'] })).toBeVisible();
  await member.getByLabel(en['login.login'], { exact: true }).fill(REFERENCE_MEMBER_LOGIN);
  await member.getByLabel(en['login.password'], { exact: true }).fill(MEMBER_PASSWORD);
  const signedIn = member.waitForResponse((response) => response.request().method() === 'POST'
    && new URL(response.url()).pathname === '/api/v1/session');
  await member.getByRole('button', { name: en['login.submit'], exact: true }).click();
  expect((await signedIn).status(), 'member sign-in').toBe(201);
  // The login screen has a level-1 heading of its own, so only the signed-in shell proves the session exists.
  await expect(member.getByRole('button', { name: en['header.signOut'], exact: true })).toBeVisible();
  const session = await api(member, 'get', '/api/v1/session');
  expect(session.status(), 'member session').toBe(200);
  expect((await session.json() as { login: string }).login, 'signed in as the reference member').toBe(REFERENCE_MEMBER_LOGIN);
  return { context, member };
}

async function createSubject(page: Page, apiRoot: string, name: string): Promise<string> {
  const response = await api(page, 'post', `${apiRoot}/subjects`, { name, type: 'home' }, { 'Idempotency-Key': idempotencyKey() });
  expect(response.status(), `create design subject ${name}`).toBe(201);
  return (await response.json() as { id: string }).id;
}

async function createItem(page: Page, apiRoot: string, title: string, subjectId: string): Promise<string> {
  const response = await api(page, 'post', `${apiRoot}/items`, { title, subjectId, recurrence: null }, { 'Idempotency-Key': idempotencyKey() });
  expect(response.status(), `create design item ${title}`).toBe(201);
  return (await response.json() as { id: string }).id;
}

async function archiveItem(page: Page, apiRoot: string, itemId: string) {
  const current = await api(page, 'get', `${apiRoot}/items/${itemId}`);
  expect(current.status(), 'read design item before archiving').toBe(200);
  const archived = await api(page, 'patch', `${apiRoot}/items/${itemId}`, { archived: true }, { 'If-Match': current.headers()['etag'] ?? '' });
  expect(archived.status(), 'archive design item').toBe(200);
}

async function archiveSubject(page: Page, apiRoot: string, subjectId: string) {
  const current = await api(page, 'get', `${apiRoot}/subjects/${subjectId}`);
  expect(current.status(), 'read design subject before archiving').toBe(200);
  const archived = await api(page, 'patch', `${apiRoot}/subjects/${subjectId}`, { archived: true }, { 'If-Match': current.headers()['etag'] ?? '' });
  expect(archived.status(), 'archive design subject').toBe(200);
}

/**
 * Issues a real one-time reset link for a member through the Admin center and returns it. The dialog is left open so
 * the caller can photograph it; the returned link stays valid because nothing redeems it.
 */
async function issueResetLink(page: Page, catalog: Catalog, login: string): Promise<string> {
  await open(page, '/admin');
  await expect(page.getByRole('heading', { level: 1, name: catalog['admin.title'] })).toBeVisible();
  const row = page.getByRole('main').getByRole('list', { name: catalog['admin.list.title'] })
    .getByRole('listitem').filter({ hasText: login });
  await row.getByRole('button', { name: catalog['admin.reset.actionLabel'].replace('{login}', login), exact: true })
    .or(row.getByRole('button', { name: catalog['admin.reset.renewLabel'].replace('{login}', login), exact: true }))
    .click();
  await page.getByRole('dialog').getByRole('button', { name: catalog['admin.reset.dialog.confirm'], exact: true }).click();
  const dialog = page.getByRole('dialog', { name: catalog['admin.reset.link.title'].replace('{login}', login) });
  const field = dialog.getByRole('textbox', { name: catalog['admin.reset.link.label'] });
  await expect(field).toBeVisible();
  const link = await field.inputValue();
  expect(link.startsWith(`${present(BASE_URL, 'E2E_BASE_URL')}/reset-password#`), 'admin dialog shows a real reset link').toBe(true);
  return link;
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

  // Signed out for the routes that live outside a household. The context stays open: the reset-link screen below
  // needs a signed-out page once the Admin center has issued a real link.
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
  // Single sign-on is a provider capability, not local state: the same stand-in the audit suite uses answers the probe.
  const oidcPattern = '**/api/v1/auth/oidc';
  await signedOut.route(oidcPattern, (route) => route.fulfill({
    status: 200, contentType: 'application/json', body: JSON.stringify({ enabled: true, displayName: 'OIDC Smoke Provider' }),
  }));
  await shots.state(signedOut, 'login-oidc', async (target, catalog) => {
    await open(target, '/login');
    await expect(target.getByRole('button', {
      name: catalog['login.oidc.submit'].replace('{provider}', 'OIDC Smoke Provider'), exact: true,
    })).toBeVisible();
  });
  await signedOut.unroute(oidcPattern);
  await shots.state(signedOut, 'not-found-signed-out', async (target, catalog) => {
    await open(target, '/no-such-page');
    await expect(target.getByRole('heading', { level: 1, name: catalog['app.notFound.title'] })).toBeVisible();
    await expect(target.getByRole('link', { name: catalog['app.notFound.signIn'], exact: true })).toBeVisible();
  });
  // A failed boot probe is the connection screen a signed-out visitor sees when Tendo cannot be reached.
  const sessionPattern = '**/api/v1/session';
  await signedOut.route(sessionPattern, (route) => route.fulfill({
    status: 503, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Unavailable', status: 503 }),
  }));
  await shots.state(signedOut, 'signed-out-unavailable', async (target, catalog) => {
    await open(target, '/');
    await expect(target.getByRole('heading', { level: 1, name: catalog['app.error.title'] })).toBeVisible();
    await expect(target.getByRole('button', { name: catalog['app.error.retry'], exact: true })).toBeVisible();
  });
  await signedOut.unroute(sessionPattern);

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

  // The one-time reset link is really issued by the server; the link stays valid because nothing redeems it.
  let resetLink = '';
  await shots.state(page, 'admin-reset-link', async (target, catalog) => {
    resetLink = await issueResetLink(target, catalog, REFERENCE_MEMBER_LOGIN);
    await expect(target.getByRole('textbox', { name: catalog['admin.reset.link.label'] })).toBeVisible();
  });

  // The reset screen the member receives: a fresh signed-out document at the real link, never submitted.
  expect(resetLink, 'admin-reset-link must issue a link for the reset screen').not.toBe('');
  await shots.state(signedOut, 'reset-password', async (target, catalog) => {
    await open(target, resetLink);
    await expect(target.getByRole('heading', { level: 1, name: catalog['reset.title'] })).toBeVisible();
    const form = target.getByRole('form', { name: catalog['reset.form.label'] });
    await expect(form).toBeVisible();
    await expect(form.getByLabel(catalog['reset.password'], { exact: true })).toBeVisible();
    await expect(form.getByLabel(catalog['reset.password.confirm'], { exact: true })).toBeVisible();
  });
  await signedOutContext.close();

  // A member sees the same shared screens without the owner-only invite and account controls.
  const memberSession = await signInMember(browser);
  await shots.state(memberSession.member, 'members-member', async (target, catalog) => {
    await open(target, '/members');
    await expect(target.getByRole('heading', { level: 1, name: catalog['members.title'] })).toBeVisible();
    await expect(target.getByText(catalog['members.loading'], { exact: true })).toHaveCount(0);
    const list = target.getByRole('region', { name: catalog['members.list.title'] });
    await expect(list.getByText(`${REFERENCE_MEMBER_LOGIN} (${catalog['members.you']})`, { exact: true })).toBeVisible();
    await expect(target.getByRole('button', { name: catalog['members.invite.action'], exact: true })).toHaveCount(0);
    await expect(target.getByRole('heading', { name: catalog['members.help.title'], exact: true })).toBeVisible();
  });
  await shots.state(memberSession.member, 'admin-member', async (target, catalog) => {
    await open(target, '/admin');
    await expect(target.getByRole('heading', { level: 1, name: catalog['admin.title'] })).toBeVisible();
    await expect(target.getByText(catalog['admin.onlyAdmins.title'], { exact: true })).toBeVisible();
    await expect(target.getByRole('list', { name: catalog['admin.list.title'] })).toHaveCount(0);
  });
  await memberSession.context.close();

  // A signed-in account that has not joined a household yet. This deployment only creates such an account through
  // single sign-on (unavailable in the capture runner), so the session probe answers with the same shape a real
  // household-less account receives: signed in, no defaultHouseholdId.
  const noHouseholdContext = await browser.newContext(CONTEXT);
  await noHouseholdContext.addCookies(await page.context().cookies());
  const noHousehold = await noHouseholdContext.newPage();
  await noHousehold.clock.install({ time: FIXED_NOW });
  await noHousehold.route(sessionPattern, (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({
      userId: '0198a2f0-0000-7000-8000-00000000000a', login: 'design_no_household', expiresAt: '2030-01-01T00:00:00Z',
    }),
  }));
  await shots.state(noHousehold, 'no-household', async (target, catalog) => {
    await open(target, '/');
    await expect(target.getByRole('heading', { level: 1, name: catalog['home.noHousehold.title'] })).toBeVisible();
    await expect(target.getByRole('button', { name: catalog['home.noHousehold.discard.action'], exact: true })).toBeVisible();
    await expect(target.getByText(catalog['home.noHousehold.how.title'], { exact: true })).toBeVisible();
  });
  await noHouseholdContext.close();

  // Invite links are shown once: every prepare reloads Members and creates a new one, so each language/theme pair
  // really starts from the button rather than from an earlier rendering of the same link.
  await shots.state(page, 'members-invite-link', async (target, catalog) => {
    await open(target, '/members');
    await expect(target.getByRole('heading', { level: 1, name: catalog['members.title'] })).toBeVisible();
    await expect(target.getByText(catalog['members.loading'], { exact: true })).toHaveCount(0);
    await target.getByRole('button', { name: catalog['members.invite.action'], exact: true }).click();
    await expect(target.getByLabel(catalog['members.invite.link'], { exact: true })).toHaveValue(/\/invite#.+/);
    await expect(target.getByRole('status').filter({ hasText: catalog['members.invite.created'] })).toBeVisible();
  }, { fullPageAt: FULL_PAGE_PHONES });

  // The invitation created above is pending; asking to revoke it opens the confirmation without revoking anything.
  await shots.state(page, 'members-revoke-confirm', async (target, catalog) => {
    await open(target, '/members');
    await expect(target.getByRole('heading', { level: 1, name: catalog['members.title'] })).toBeVisible();
    await expect(target.getByText(catalog['members.loading'], { exact: true })).toHaveCount(0);
    const pending = target.getByRole('list', { name: catalog['members.invitations.title'] })
      .getByRole('listitem').filter({ hasText: catalog['members.invitation.status.pending'] }).first();
    await expect(pending).toBeVisible();
    await pending.getByRole('button', { name: catalog['members.invitation.revoke'], exact: true }).click();
    await expect(pending.getByText(catalog['members.invitation.revokeConfirm'], { exact: true })).toBeVisible();
    await expect(pending.getByRole('button', { name: catalog['members.invitation.yes'], exact: true })).toBeVisible();
    await expect(pending.getByRole('button', { name: catalog['members.invitation.cancel'], exact: true })).toBeVisible();
  }, { fullPageAt: FULL_PAGE_PHONES });

  // Home while the items read is still open: the loading status is the real one, held per language/theme pair.
  const summaryPattern = `**/api/v1/households/${identity.householdId}/items/summary**`;
  const heldSummary = new HeldRequest(page, summaryPattern);
  await shots.state(page, 'home-loading', async (target, catalog) => {
    await heldSummary.hold();
    await open(target, '/');
    await expect(target.getByRole('main').getByRole('heading', { level: 1 })).toBeVisible();
    await expect(target.getByRole('status').filter({ hasText: catalog['items.loading'] })).toBeVisible();
  });
  await heldSummary.letGo();
  await expect(page.locator('main li[data-item-id]').first(), 'Home settles after the held items read').toBeVisible();

  // Server-error evidence is a browser-stubbed failing read, exactly as the visual suite does it.
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

  // Destructive states last: every capture above keeps the reference household intact. These use their own records
  // so an archived or conflicting row never changes what the earlier images show.
  // A plausible household title, so the 1000+ item Home scale image does not gain a fixture-looking row.
  const conflictTitle = 'Book the heating service';
  const conflictItemId = await createItem(page, identity.apiRoot, conflictTitle, present(reference.subjects.get('Home'), "subject 'Home'"));
  await shots.state(page, 'edit-conflict', async (target, catalog) => {
    await open(target, `/items/${conflictItemId}`);
    await expect(target.getByRole('heading', { level: 1, name: conflictTitle, exact: true })).toBeVisible();
    await target.getByRole('button', { name: catalog['itemDetail.edit.button'], exact: true }).click();
    const form = target.getByRole('form', { name: catalog['itemDetail.edit.formLabel'] });
    await expect(form).toBeVisible();
    await form.getByLabel(catalog['items.title']).fill(`${conflictTitle} — my edit`);
    // Someone else saves the same item after the form opened; the next save must surface the conflict, not overwrite.
    const currentConflict = await api(target, 'get', `${identity.apiRoot}/items/${conflictItemId}`);
    expect(currentConflict.status(), 'read conflict item before the concurrent save').toBe(200);
    const concurrent = await api(target, 'patch', `${identity.apiRoot}/items/${conflictItemId}`,
      { notes: `concurrent design edit ${crypto.randomUUID()}` }, { 'If-Match': currentConflict.headers()['etag'] ?? '' });
    expect(concurrent.status(), 'concurrent save for the conflict capture').toBe(200);
    await form.getByRole('button', { name: catalog['itemDetail.save'], exact: true }).click();
    await expect(target.getByText(catalog['itemDetail.edit.conflict'], { exact: true })).toBeVisible();
    await expect(form).toBeVisible();
  });

  const archivedTitle = 'Design archived item';
  const archivedItemId = await createItem(page, identity.apiRoot, archivedTitle, present(reference.subjects.get('Home'), "subject 'Home'"));
  await archiveItem(page, identity.apiRoot, archivedItemId);
  await shots.state(page, 'item-detail-archived', async (target, catalog) => {
    await open(target, `/items/${archivedItemId}`);
    await expect(target.getByRole('heading', { level: 1, name: archivedTitle, exact: true })).toBeVisible();
    await expect(target.getByText(catalog['itemDetail.archived'], { exact: true })).toBeVisible();
  });
  await shots.state(page, 'all-items-archived', async (target, catalog) => {
    await open(target, '/items');
    await expect(target.getByRole('heading', { level: 1, name: catalog['allItems.title'] })).toBeVisible();
    const status = target.getByRole('combobox', { name: catalog['allItems.status.label'] });
    await status.selectOption('archived');
    await expect(status).toHaveValue('archived');
    // The results heading states the count the screen really has: exactly the one archived fixture.
    const range = catalog['allItems.range'].replace('{from}', '1').replace('{to}', '1').replace('{total}', '1');
    await expect(target.getByRole('main').getByRole('heading', { name: range })).toBeVisible();
    await expect(target.locator('main li[data-item-id]').filter({ hasText: archivedTitle })).toHaveCount(1);
  });

  const archivedSubject = 'Design archived place';
  const archivedSubjectId = await createSubject(page, identity.apiRoot, archivedSubject);
  await archiveSubject(page, identity.apiRoot, archivedSubjectId);
  await shots.state(page, 'people-archived', async (target, catalog) => {
    await open(target, '/people');
    await expect(target.getByRole('heading', { level: 1, name: catalog['subjects.title'] })).toBeVisible();
    await expect(target.getByText(catalog['subjects.loading'], { exact: true })).toHaveCount(0);
    await target.getByRole('button', { name: catalog['subjects.showArchived'], exact: true }).click();
    await expect(target.getByRole('main').getByRole('heading', { name: catalog['subjects.archived.title'] })).toBeVisible();
    await expect(target.getByRole('main').getByText(archivedSubject, { exact: true })).toBeVisible();
  });

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
  expect(Object.keys(shots.metrics).length, 'metrics entry per capture').toBe(EXPECTED_CAPTURES);
  const pngs = readdirSync(dir).filter((name) => name.endsWith('.png')).map((name) => name.slice(0, -'.png'.length)).sort();
  expect(pngs, 'one screenshot per metrics entry').toEqual(Object.keys(shots.metrics).sort());
  console.info(`design-capture-complete captures=${Object.keys(shots.metrics).length} dir=${dir}`);
});
