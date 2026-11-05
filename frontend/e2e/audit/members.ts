import { expect, type Browser, type Page } from '@playwright/test';
import { join } from 'node:path';
import { en } from '../../src/i18n/en';
import { cs } from '../../src/i18n/cs';
import { expectNoAxeViolations } from '../a11y';

const widths = [320, 360, 390, 768, 1024, 1440] as const;
const heightFor = (width: number) => width <= 320 ? 640 : width < 768 ? 844 : width < 1440 ? 1024 : 900;
type Capture = (page: Page, path: string, fullPage?: boolean, mask?: import('@playwright/test').Locator[]) => Promise<void>;

async function open(page: Page, width: number, locale: 'en' | 'cs' = 'en') {
  await page.setViewportSize({ width, height: heightFor(width) });
  await page.goto('/members');
  await expect(page.getByRole('heading', { level: 1, name: (locale === 'cs' ? cs : en)['members.title'] })).toBeVisible();
  await expect(page.getByText((locale === 'cs' ? cs : en)['members.loading'], { exact: true })).toHaveCount(0);
}

async function shot(page: Page, path: string) {
  const size = page.viewportSize();
  if (!size) throw new Error(`Missing viewport for ${path}`);
  expect(await page.evaluate(() => [window.innerWidth, window.innerHeight]), `viewport for ${path}`).toEqual([size.width, size.height]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth), `${path} has no horizontal overflow`).toBe(true);
  await page.screenshot({ path });
}

async function czechContext(browser: Browser, page: Page, width: number) {
  const context = await browser.newContext({ locale: 'cs-CZ', reducedMotion: 'reduce', viewport: { width, height: heightFor(width) } });
  await context.addCookies(await page.context().cookies());
  const csPage = await context.newPage();
  await csPage.goto('/');
  await csPage.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
  return { context, page: csPage };
}

/** Captures the household members flow after the production audit has seeded an owner, member and invites. */
export async function captureMembers(page: Page, browser: Browser, dir: string, capture: Capture) {
  for (const width of widths) {
    await open(page, width);
    await expect(page.getByRole('main').getByText(/audit_owner \(you\)/)).toBeVisible();
    await expect(page.getByRole('main').getByText('audit_member', { exact: true })).toBeVisible();
    if (width === 390) await expectNoAxeViolations(page, 'members-owner');
    await capture(page, join(dir, `audit-members-owner-en-${width}.png`), false);
    if (width === 390 || width === 1440) {
      await page.locator('html').evaluate((html) => html.setAttribute('data-theme', 'dark'));
      await shot(page, join(dir, `audit-members-owner-forced-dark-en-${width}.png`));
      await page.locator('html').evaluate((html) => html.removeAttribute('data-theme'));
    }

    const { context, page: csPage } = await czechContext(browser, page, width);
    await open(csPage, width, 'cs');
    await capture(csPage, join(dir, `audit-members-owner-cs-${width}.png`), false);
    await context.close();
  }

  const ownerContext = await browser.newContext({ reducedMotion: 'reduce', viewport: { width: 390, height: 844 } });
  await ownerContext.addCookies(await page.context().cookies());
  const emptyOwnerPage = await ownerContext.newPage();
  await emptyOwnerPage.route('**/api/v1/households/*/invitations?*', async route => {
    if (route.request().method() === 'GET') await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [], nextCursor: null }) });
    else await route.fallback();
  });
  await open(emptyOwnerPage, 390);
  await emptyOwnerPage.evaluate(() => window.scrollTo(0, 0));
  await expect(emptyOwnerPage.getByRole('list', { name: en['members.invitations.title'] })).toBeEmpty();
  await capture(emptyOwnerPage, join(dir, 'audit-members-empty-invitations-en-390.png'), false);
  await ownerContext.close();
  await page.getByRole('button', { name: en['members.invite.action'], exact: true }).click();
  const link = page.getByLabel(en['members.invite.link'], { exact: true });
  await expect(link).toHaveValue(/\/invite#.+/);
  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  await expect(link).toBeInViewport();
  await capture(page, join(dir, 'audit-members-link-shown-en-390.png'), false, [link]);
  await expect(page.getByRole('heading', { name: en['members.invitations.title'] })).toBeVisible();
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.getByRole('button', { name: en['members.invite.copy'], exact: true }).click();
  // Production Chromium has no granted clipboard permission in this audit context; verify the safe manual fallback.
  await expect(page.getByText(en['members.invite.manualCopy'], { exact: true })).toBeVisible();
  await expect(link).toBeFocused();
  await capture(page, join(dir, 'audit-members-link-manual-copy-en-390.png'), false, [link]);
  await page.evaluate(() => window.scrollTo(0, 0));
  await page.getByRole('button', { name: en['members.invite.action'], exact: true }).click();
  await expect(page.getByLabel(en['members.invite.link'], { exact: true })).toBeVisible();
  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  await expect(page.getByRole('list', { name: en['members.invitations.title'] }).getByRole('listitem').filter({ hasText: en['members.invitation.status.pending'] }).first()).toBeVisible();
  await page.getByRole('list', { name: en['members.invitations.title'] }).getByRole('listitem').filter({ hasText: en['members.invitation.status.pending'] }).first().getByRole('button', { name: en['members.invitation.revoke'], exact: true }).click();
  await expect(page.getByRole('button', { name: en['members.invitation.yes'], exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: en['members.invitation.cancel'], exact: true })).toBeVisible();
  await capture(page, join(dir, 'audit-members-revoke-confirm-en-390.png'), false);
  await page.getByRole('button', { name: en['members.invitation.cancel'], exact: true }).click();

  const pending = page.getByRole('list', { name: en['members.invitations.title'] }).getByRole('listitem').filter({ hasText: en['members.invitation.status.pending'] }).first();
  await pending.getByRole('button', { name: en['members.invitation.revoke'], exact: true }).click();
  await expect(pending.getByText(en['members.invitation.revokeConfirm'], { exact: true })).toBeVisible();
  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  await expect(pending).toBeInViewport();
  await pending.getByRole('button', { name: en['members.invitation.cancel'], exact: true }).click();
  await page.reload();
  await open(page, 390);
  const cleanPending = page.getByRole('list', { name: en['members.invitations.title'] }).getByRole('listitem').filter({ hasText: en['members.invitation.status.pending'] }).first();
  await cleanPending.getByRole('button', { name: en['members.invitation.revoke'], exact: true }).click();
  await expect(cleanPending.getByText(en['members.invitation.revokeConfirm'], { exact: true })).toBeVisible();
  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  await expect(cleanPending).toBeInViewport();
  await capture(page, join(dir, 'audit-members-revoke-confirm-en-390.png'), false);
  await cleanPending.getByRole('button', { name: en['members.invitation.cancel'], exact: true }).click();
  await page.evaluate(() => window.scrollTo(0, 0));
  await expect(page.getByRole('region', { name: en['members.invite.title'] })).toBeInViewport();
  await page.route('**/api/v1/households/*/invitations', route => route.request().method() === 'POST'
    ? route.abort('failed') : route.fallback());
  await page.getByRole('button', { name: en['members.invite.action'], exact: true }).click();
  await expect(page.getByRole('button', { name: en['members.invite.checkAgain'], exact: true })).toBeVisible();
  await capture(page, join(dir, 'audit-members-check-again-en-390.png'), false);
  await page.unrouteAll({ behavior: 'wait' });

  await page.route('**/api/v1/households/*/members**', route => route.fulfill({ status: 503, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Unavailable', status: 503 }) }));
  await page.goto('/members');
  await expect(page.getByRole('button', { name: en['app.error.retry'], exact: true })).toBeVisible();
  await capture(page, join(dir, 'audit-members-error-en-390.png'), false);
  await page.unrouteAll({ behavior: 'wait' });
  await page.goto('/members');
  await expect(page.getByRole('heading', { level: 1, name: en['members.title'] })).toBeVisible();
  await page.evaluate(() => window.scrollTo(0, 0));
  await capture(page, join(dir, 'audit-members-loading-en-390.png'), false);

  const memberContext = await browser.newContext({ reducedMotion: 'reduce', viewport: { width: 1440, height: 900 } });
  const memberPage = await memberContext.newPage();
  await memberPage.goto('/login');
  await memberPage.getByLabel(en['login.login'], { exact: true }).fill('audit_member');
  await memberPage.getByLabel(en['login.password'], { exact: true }).fill('audit-member-password-long-enough');
  await memberPage.getByRole('button', { name: en['login.submit'], exact: true }).click();
  await expect(memberPage.getByRole('heading', { level: 1, name: 'Audit household' })).toBeVisible();
  await open(memberPage, 1440);
  await expectNoAxeViolations(memberPage, 'members-non-owner');
  await capture(memberPage, join(dir, 'audit-members-non-owner-en-1440.png'), false);
  const { context, page: csMemberPage } = await czechContext(browser, memberPage, 1440);
  await open(csMemberPage, 1440, 'cs');
  await capture(csMemberPage, join(dir, 'audit-members-non-owner-cs-1440.png'), false);
  await context.close();
  await memberContext.close();
}
