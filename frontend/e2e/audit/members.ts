import { expect, type Browser, type Page } from '@playwright/test';
import { join } from 'node:path';
import { en } from '../../src/i18n/en';
import { cs } from '../../src/i18n/cs';
import { expectNoAxeViolations } from '../a11y';
import { applySavedDarkPreference, applySystemAppearance } from './appearance';

const widths = [320, 360, 390, 768, 1024, 1440] as const;
const heightFor = (width: number) => width <= 320 ? 640 : width < 768 ? 844 : width < 1440 ? 1024 : 900;
type Capture = (page: Page, path: string, fullPage?: boolean, mask?: import('@playwright/test').Locator[]) => Promise<void>;

async function open(page: Page, width: number, locale: 'en' | 'cs' = 'en') {
  const strings = locale === 'cs' ? cs : en;
  await page.setViewportSize({ width, height: heightFor(width) });
  await page.goto('/members');
  await expect(page.getByRole('navigation', { name: strings['nav.label'] })).toBeVisible();
  await expect(page.getByRole('heading', { level: 1, name: strings['members.title'] })).toBeVisible();
  const members = page.getByRole('main').getByRole('region', { name: strings['members.list.title'] });
  await expect(members).toBeVisible();
  await expect(members.getByRole('list', { name: strings['members.list.title'] }).getByRole('listitem').first()).toBeVisible();
  await expect(page.getByRole('main').getByText(strings['members.loading'], { exact: true })).toHaveCount(0);
}

function expectedWidth(path: string) {
  const match = path.match(/-(\d+)\.png$/);
  if (!match) throw new Error(`Capture filename must end with a viewport width: ${path}`);
  return Number(match[1]);
}

async function assertCaptureWidth(page: Page, path: string) {
  expect(await page.evaluate(() => window.innerWidth), `filename width for ${path}`).toBe(expectedWidth(path));
  expect(page.viewportSize()?.width, `configured viewport width for ${path}`).toBe(expectedWidth(path));
}

async function shot(page: Page, path: string, mask: import('@playwright/test').Locator[] = []) {
  const size = page.viewportSize();
  if (!size) throw new Error(`Missing viewport for ${path}`);
  const filenameWidth = expectedWidth(path);
  expect(await page.evaluate(() => window.innerWidth), `filename width for ${path}`).toBe(filenameWidth);
  expect(size.width, `configured viewport width for ${path}`).toBe(filenameWidth);
  expect(await page.evaluate(() => [window.innerWidth, window.innerHeight]), `viewport for ${path}`).toEqual([size.width, size.height]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth), `${path} has no horizontal overflow`).toBe(true);
  await page.screenshot({ path, mask });
}

/**
 * Measures invitation metadata in the shipped theme. Dark is reached through the saved preference (a reload), never by
 * writing token values inline, so the ratio reflects the CSS users receive.
 */
async function assertHistoryMetadataContrast(page: Page, theme: 'light' | 'dark') {
  if (theme === 'dark') await applySavedDarkPreference(page);
  const metadata = page.locator('[aria-labelledby="invitations-history-heading"] li.bg-sand div p.text-muted').last();
  await expect(metadata, `${theme} history metadata is present`).toBeVisible();
  const measurement = await metadata.evaluate(element => {
    const parse = (value: string) => value.match(/[\d.]+/g)!.slice(0, 3).map(Number);
    const luminance = (color: number[]) => color.map(channel => channel / 255 <= 0.04045 ? channel / 255 / 12.92 : ((channel / 255 + 0.055) / 1.055) ** 2.4)
      .reduce((sum, channel, index) => sum + channel * [0.2126, 0.7152, 0.0722][index]!, 0);
    const foreground = luminance(parse(getComputedStyle(element).color));
    const row = element.closest('li');
    if (!row) throw new Error('Invitation metadata must belong to a history row');
    const rowBackground = getComputedStyle(row).backgroundColor;
    const background = luminance(parse(rowBackground));
    return {
      ratio: (Math.max(foreground, background) + 0.05) / (Math.min(foreground, background) + 0.05),
      foreground: getComputedStyle(element).color,
      rowBackground,
    };
  });
  const { ratio } = measurement;
  console.log(`${theme} invitation history metadata contrast: ${ratio.toFixed(2)}:1 (text=${measurement.foreground}, row=${measurement.rowBackground})`);
  expect(ratio, `${theme} invitation metadata meets WCAG AA for normal text`).toBeGreaterThanOrEqual(4.5);
}

/** Captures the household members flow after the production audit has seeded an owner, member and invites. */
export async function captureMembers(page: Page, browser: Browser, dir: string, capture: Capture) {
  for (const width of widths) {
    await open(page, width);
    await expect(page.getByRole('main').getByText(/audit_owner \(you\)/)).toBeVisible();
    await expect(page.getByRole('main').getByText('audit_member', { exact: true })).toBeVisible();
    const history = page.getByRole('list', { name: en['members.invitations.title'] });
    const accepted = history.getByRole('listitem').filter({ hasText: en['members.invitation.status.accepted'] }).first();
    const pending = history.getByRole('listitem').filter({ hasText: en['members.invitation.status.pending'] }).first();
    await expect(accepted).toBeVisible();
    await expect(accepted.locator('p').nth(1)).toContainText(/Created .+ · Expires .+/);
    await expect(pending).toBeVisible();
    await expect(pending.getByRole('button', { name: en['members.invitation.revoke'], exact: true })).toBeVisible();
    await expect(pending.locator('p').nth(1)).toContainText(/Created .+ · Expires .+/);
    if (width === 320) {
      const inviteAction = page.getByRole('button', { name: en['members.invite.action'], exact: true });
      const nav = page.getByRole('navigation', { name: en['nav.label'] });
      await expect(inviteAction).toBeInViewport();
      const [actionBox, navBox] = await Promise.all([inviteAction.boundingBox(), nav.boundingBox()]);
      expect(actionBox && navBox && actionBox.y + actionBox.height < navBox.y, '320px invite action stays above the fixed navigation').toBe(true);
    }
    if (width === 390) {
      await expectNoAxeViolations(page, 'members-owner');
      await assertHistoryMetadataContrast(page, 'light');
    }
    const ownerCapture = join(dir, `audit-members-owner-en-${width}.png`);
    await assertCaptureWidth(page, ownerCapture);
    await capture(page, ownerCapture, false);
    if (width === 320) {
      await pending.scrollIntoViewIfNeeded();
      await expect(pending).toBeInViewport();
      await expect(pending.locator('p').nth(1)).toContainText(/Expires .+/);
      await expect(pending.getByRole('button', { name: en['members.invitation.revoke'], exact: true })).toBeVisible();
      await shot(page, join(dir, 'audit-members-history-en-320.png'));
      await page.evaluate(() => window.scrollTo(0, 0));
    }
    if (width === 390 || width === 1440) {
      await assertHistoryMetadataContrast(page, 'dark');
      await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
      const darkCapture = join(dir, `audit-members-owner-dark-en-${width}.png`);
      await shot(page, darkCapture);
      const darkPixels = await page.screenshot();
      await applySystemAppearance(page);
      const lightPixels = await page.screenshot();
      expect(darkPixels.equals(lightPixels), `dark and light captures differ at ${width}px`).toBe(false);
    }

    await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
    await page.reload();
    await open(page, width, 'cs');
    const ownerCzechCapture = join(dir, `audit-members-owner-cs-${width}.png`);
    await assertCaptureWidth(page, ownerCzechCapture);
    await capture(page, ownerCzechCapture, false);
    if (width === 390) {
      await applySavedDarkPreference(page);
      await expect(page.locator('html')).toHaveAttribute('lang', 'cs');
      await capture(page, join(dir, 'audit-members-owner-dark-cs-390.png'), false);
      await applySystemAppearance(page);
    }
    await page.evaluate(() => localStorage.setItem('tendo.locale', 'en'));
    await page.reload();
  }

  const ownerContext = await browser.newContext({ reducedMotion: 'reduce', viewport: { width: 390, height: 844 } });
  await ownerContext.addCookies(await page.context().cookies());
  const permissionPage = await ownerContext.newPage();
  await permissionPage.route('**/api/v1/households/*/invitations', route => route.request().method() === 'POST'
    ? route.fulfill({ status: 403, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Forbidden', status: 403, code: 'owner_required' }) })
    : route.fallback());
  await open(permissionPage, 390);
  await expect(permissionPage.getByRole('main').getByRole('region', { name: en['members.invite.title'] })).toBeVisible();
  await permissionPage.getByRole('button', { name: en['members.invite.action'], exact: true }).click();
  await expect(permissionPage.getByRole('heading', { name: en['members.help.title'], exact: true })).toBeVisible();
  await expect(permissionPage.getByText(en['members.invite.ownerRequired'], { exact: true })).toBeVisible();
  await expect(permissionPage.getByRole('status')).toContainText(en['members.invite.ownerRequired']);
  await expect(permissionPage.getByRole('main').getByRole('button', { name: en['members.invite.action'], exact: true })).toHaveCount(0);
  await expect(permissionPage.getByRole('main').getByRole('heading', { name: en['members.invitations.title'] })).toHaveCount(0);
  await capture(permissionPage, join(dir, 'audit-members-owner-required-en-390.png'), false);
  await ownerContext.close();

  const emptyContext = await browser.newContext({ reducedMotion: 'reduce', viewport: { width: 390, height: 844 } });
  await emptyContext.addCookies(await page.context().cookies());
  const emptyOwnerPage = await emptyContext.newPage();
  await emptyOwnerPage.route('**/api/v1/households/*/invitations**', async route => {
    if (route.request().method() === 'GET') await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [], nextCursor: null }) });
    else await route.fallback();
  });
  await open(emptyOwnerPage, 390);
  await emptyOwnerPage.evaluate(() => window.scrollTo(0, 0));
  await expect(emptyOwnerPage.getByText(en['members.invitations.empty'], { exact: true })).toBeVisible();
  await capture(emptyOwnerPage, join(dir, 'audit-members-empty-invitations-en-390.png'), false);
  await emptyContext.close();
  const linkContext = await browser.newContext({ reducedMotion: 'reduce', viewport: { width: 390, height: 844 } });
  await linkContext.addCookies(await page.context().cookies());
  const linkPage = await linkContext.newPage();
  const shownLinkPath = join(dir, 'audit-members-link-shown-en-390.png');
  const manualCopyPath = join(dir, 'audit-members-link-manual-copy-en-390.png');
  await linkPage.route('**/api/v1/households/*/invitations', route => route.request().method() === 'POST'
    ? route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify({ id: 'e0000000-0000-4000-8000-000000000001', token: 'e'.repeat(43) }) })
    : route.fallback());
  await open(linkPage, 390);
  await linkPage.getByRole('button', { name: en['members.invite.action'], exact: true }).click();
  const link = linkPage.getByLabel(en['members.invite.link'], { exact: true });
  await expect(link).toHaveValue(/\/invite#.+/);
  await link.scrollIntoViewIfNeeded();
  await expect(link).toBeInViewport();
  await expect(linkPage.getByRole('status').filter({ hasText: en['members.invite.created'] })).toBeVisible();
  await expect(linkPage.getByRole('heading', { name: en['members.invitations.title'] })).toBeVisible();
  await shot(linkPage, shownLinkPath, [link]);
  await linkPage.getByRole('button', { name: en['members.invite.copy'], exact: true }).click();
  const fallbackNotice = linkPage.getByRole('region', { name: en['members.invite.title'] }).getByRole('status').filter({ hasText: en['members.invite.manualCopy'] });
  await expect(fallbackNotice).toBeVisible();
  await expect(link).toBeFocused();
  await expect(link).toHaveJSProperty('selectionStart', 0);
  await expect(link).toHaveJSProperty('selectionEnd', (await link.inputValue()).length);
  await link.scrollIntoViewIfNeeded();
  await fallbackNotice.scrollIntoViewIfNeeded();
  await expect(link).toBeInViewport();
  await expect(fallbackNotice).toBeInViewport();
  await shot(linkPage, manualCopyPath, [link]);
  await linkContext.close();
  await page.reload();
  await open(page, 390);
  const cleanPending = page.getByRole('list', { name: en['members.invitations.title'] }).getByRole('listitem').filter({ hasText: en['members.invitation.status.pending'] }).first();
  await cleanPending.getByRole('button', { name: en['members.invitation.revoke'], exact: true }).click();
  await expect(cleanPending.getByText(en['members.invitation.revokeConfirm'], { exact: true })).toBeVisible();
  await cleanPending.scrollIntoViewIfNeeded();
  await expect(cleanPending).toBeInViewport();
  await expect(cleanPending.getByRole('button', { name: en['members.invitation.yes'], exact: true })).toBeInViewport();
  await expect(cleanPending.getByRole('button', { name: en['members.invitation.cancel'], exact: true })).toBeInViewport();
  await expect(page.getByRole('region', { name: en['members.invite.title'] }).getByRole('status').filter({ hasText: en['members.invite.manualCopy'] })).toHaveCount(0);
  await shot(page, join(dir, 'audit-members-revoke-confirm-en-390.png'));
  await cleanPending.getByRole('button', { name: en['members.invitation.cancel'], exact: true }).click();
  await page.evaluate(() => window.scrollTo(0, 0));
  await expect(page.getByRole('region', { name: en['members.invite.title'] })).toBeInViewport();
  await page.unroute('**/api/v1/households/*/invitations');
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
  let releaseMembers!: () => void;
  const membersGate = new Promise<void>(resolve => { releaseMembers = resolve; });
  await page.route('**/api/v1/households/*/members**', async route => {
    if (route.request().method() === 'GET') { await membersGate; await route.fallback(); }
    else await route.fallback();
  });
  await page.goto('/members');
  await expect(page.getByRole('navigation', { name: en['nav.label'] })).toBeVisible();
  await expect(page.getByRole('heading', { level: 1, name: en['members.title'] })).toBeVisible();
  await expect(page.getByRole('main').getByRole('status').filter({ hasText: en['members.loading'] })).toBeVisible();
  await page.evaluate(() => window.scrollTo(0, 0));
  await capture(page, join(dir, 'audit-members-loading-en-390.png'), false);
  releaseMembers();
  await expect(page.getByRole('main').getByRole('status').filter({ hasText: en['members.loading'] })).toHaveCount(0);
  await page.unrouteAll({ behavior: 'wait' });

  const memberContext = await browser.newContext({ reducedMotion: 'reduce', viewport: { width: 1440, height: 900 } });
  const memberPage = await memberContext.newPage();
  await memberPage.goto('/login');
  await memberPage.getByLabel(en['onboarding.login'], { exact: true }).fill('audit_member');
  await memberPage.getByLabel(en['login.password'], { exact: true }).fill('audit-member-password-long-enough');
  await memberPage.getByRole('button', { name: en['login.submit'], exact: true }).click();
  await expect(memberPage.getByRole('heading', { level: 1, name: 'Audit household' })).toBeVisible();
  await memberPage.evaluate(() => localStorage.setItem('tendo.locale', 'en'));
  await memberPage.reload();
  for (const width of [390, 1440] as const) {
    await open(memberPage, width);
    if (width === 390) await expectNoAxeViolations(memberPage, 'members-non-owner');
    await capture(memberPage, join(dir, `audit-members-non-owner-en-${width}.png`), false);
  }
  await memberPage.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
  await memberPage.reload();
  for (const width of [390, 1440] as const) {
    await open(memberPage, width, 'cs');
    await capture(memberPage, join(dir, `audit-members-non-owner-cs-${width}.png`), false);
  }
  await memberContext.close();
}
