import { expect, test, type Browser, type BrowserContext, type Locator, type Page } from '@playwright/test';
import { createHash, randomBytes } from 'node:crypto';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { en } from '../../src/i18n/en';
import { cs } from '../../src/i18n/cs';
import { assertInterReady, capture } from './capture';
import { api, BASE_URL, firstRun, HOUSEHOLD, OWNER_LOGIN, ownerPassword, PASSWORD_RESET_DATES, pinHouseholdCreatedDates, pinInvitationDates, pinMcpGrantDates, pinMcpPolicyEventDates, pinMcpRequestExpiry, seed, type Fixture } from './fixtures';
import { assertArchiveSettled, assertHistoryPage, openMoreSheet, seedPagedHistory } from './archiveHistory';
import { openConsent, setOwnAiApps } from './consentStates';
import { assertDateOrderFollowsLanguage, launchCzechBrowser } from './dateOrder';
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
  await pinHouseholdCreatedDates(context);
  await pinMcpGrantDates(context);
  await pinMcpPolicyEventDates(context);
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
// A long form scrolls inside its dialog, but the title, the close control and the primary action stay fully on
// screen at either end of that scroll, so the form can always be dismissed or submitted without hunting for it.
async function expectDialogActionsOnScreen(dialog: Locator, submit: string, label: string) {
  const primary = dialog.getByRole('button', { name: submit, exact: true });
  await dialog.evaluate((element) => element.scrollTo({ top: 0 }));
  for (const target of [dialog.getByRole('heading', { level: 2 }), dialog.getByRole('button', { name: en['dialog.close'], exact: true }), primary]) {
    await expect(target, `${label} at the top`).toBeInViewport({ ratio: 1 });
  }
  await dialog.evaluate((element) => element.scrollTo({ top: element.scrollHeight }));
  for (const target of [dialog.getByRole('heading', { level: 2 }), dialog.getByRole('button', { name: en['dialog.close'], exact: true }), primary]) {
    await expect(target, `${label} at the end`).toBeInViewport({ ratio: 1 });
  }
  await dialog.evaluate((element) => element.scrollTo({ top: 0 }));
}
// 400% zoom of 1280x720 leaves 320x180 CSS px: two pinned bands would cover nearly all of it, so on a short viewport
// both scroll with the form and the focus scroll padding is dropped; every field Tab reaches is then fully visible.
async function expectShortDialogUnpinned(page: Page, dialog: Locator) {
  await page.setViewportSize({ width: 320, height: 180 });
  const layout = await dialog.evaluate((element) => {
    const style = (selector: string) => getComputedStyle(element.querySelector(selector)!).position;
    const own = getComputedStyle(element);
    return { header: style('header'), actions: style('[data-dialog-actions]'), padding: [own.scrollPaddingTop, own.scrollPaddingBottom] };
  });
  expect(layout).toEqual({ header: 'static', actions: 'static', padding: ['0px', '0px'] });
  // The full-height sheet follows the resize through `dvh`; Tab is only meaningful once it fits the short window,
  // otherwise a field inside the dialog's own scrollport can still sit below the viewport.
  const geometry = () => dialog.evaluate((element) => {
    const box = element.getBoundingClientRect();
    const focused = document.activeElement?.getBoundingClientRect();
    return { top: box.top, bottom: box.bottom, viewport: window.innerHeight, scrollTop: element.scrollTop,
      clientHeight: element.clientHeight, focused: focused ? [focused.top, focused.bottom] : null };
  });
  await expect.poll(async () => { const now = await geometry(); return now.top >= 0 && now.bottom <= now.viewport; },
    { message: 'short dialog fits the 180 px window' }).toBe(true);
  await dialog.getByLabel(en['items.title'], { exact: true }).focus();
  for (let step = 0; step < 4; step += 1) {
    await page.keyboard.press('Tab');
    try {
      await expect(page.locator(':focus')).toBeInViewport({ ratio: 1 });
    } catch (error) {
      throw new Error(`short dialog Tab ${step + 1}: focused control not fully visible ${JSON.stringify(await geometry())}`, { cause: error });
    }
  }
  await page.setViewportSize({ width: 320, height: 640 });
}
// The Create dialog focuses its name field after showModal; wait for it so capture()'s blur cannot race it.
async function openCreate(page: Page) {
  await page.getByRole('main').getByRole('button', { name: en['install.create.action'], exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel(en['install.create.name'], { exact: true })).toBeFocused();
  return dialog;
}
async function captureScreen(page: Page, base: string, fullPage = false) {
  for (const width of [390, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await capture(page, `${base}-${width}`, width, 900, fullPage);
  }
}

/** Opens the top bar's account menu and pins it open through capture()'s blur, as openMoreSheet does for More. */
async function openAccountMenu(page: Page) {
  const trigger = page.getByRole('banner').getByRole('button', { name: en['header.menu'].replace('{login}', OWNER_LOGIN), exact: true });
  await trigger.click();
  await expect(trigger).toHaveAttribute('aria-expanded', 'true');
  const panel = page.locator(`[id="${await trigger.getAttribute('aria-controls')}"]`);
  await expect(panel.getByText(en['header.signedInAs'].replace('{login}', OWNER_LOGIN), { exact: true })).toBeVisible();
  await expect(panel.getByRole('button', { name: en['header.signOut'], exact: true })).toBeVisible();
  const box = (await panel.boundingBox())!;
  expect(box.x, 'account panel left edge').toBeGreaterThanOrEqual(0);
  expect(box.x + box.width, 'account panel right edge').toBeLessThanOrEqual(page.viewportSize()!.width + 0.5);
  // A press on the panel's own padding keeps it open (useDismiss) and moves focus off the trigger.
  await panel.click({ position: { x: box.width / 2, y: 3 } });
  await expect(trigger).toHaveAttribute('aria-expanded', 'true');
}

/** One account's AI apps; the screen is settled once what apps may do now is shown. */
const ACCOUNT_AI_APPS = /^\/admin\/accounts\/[^/]+\/ai-apps$/;

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
  // MCP is on in the visual run, so the own AI apps section reads and shows before the page is settled.
  await expect(main.locator('[data-own-ai-apps-now]')).toHaveText(catalog['accountAiApps.now.both']);
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

/** A laid-out box, or a failure that names what was missing instead of a bare TypeError. */
async function boxOf(locator: Locator, what: string) {
  const box = await locator.boundingBox();
  if (!box) throw new Error(`${what} has no layout box`);
  return box;
}

/**
 * Opening a pending invitation's revoke confirmation must not crush the invitation's own status and dates: below
 * `md` the question and its two buttons move below the text; from `md` they sit beside it. Either way the text block
 * keeps the height it had before. Leaves the confirmation open on the first pending row.
 */
async function openRevokeConfirmation(page: Page, locale: 'en' | 'cs', width: number, placement: 'below' | 'beside') {
  const catalog = locale === 'cs' ? cs : en;
  const label = `${locale} ${width}`;
  const pending = page.getByRole('main').getByRole('list', { name: catalog['members.invitations.title'] })
    .getByRole('listitem').filter({ hasText: catalog['members.invitation.status.pending'] }).first();
  const text = pending.locator('p:has([data-invitation-details])');
  const before = await boxOf(text, `${label} invitation text`);
  await pending.getByRole('button', { name: catalog['members.invitation.revoke'], exact: true }).click();
  const question = pending.getByText(catalog['members.invitation.revokeConfirm'], { exact: true });
  await expect(question).toBeVisible();
  // Yes takes focus a task after the click, and that focus scrolls the row into view. Measure only once it has, so
  // the text and the question are read from the same scroll position.
  await expect(pending.getByRole('button', { name: catalog['members.invitation.yes'], exact: true })).toBeFocused();
  const after = await boxOf(text, `${label} invitation text`);
  expect(after.height, `${label}: revoke confirmation keeps the invitation text at its own height`)
    .toBeLessThanOrEqual(before.height + 1);
  const questionBox = await boxOf(question, `${label} revoke question`);
  if (placement === 'below') {
    expect(questionBox.y, `${label}: revoke confirmation sits below the invitation text`).toBeGreaterThanOrEqual(after.y + after.height - 1);
  } else {
    expect(questionBox.y, `${label}: revoke confirmation sits beside the invitation text`).toBeLessThan(after.y + after.height);
  }
  const rowBox = await boxOf(pending, `${label} invitation row`);
  for (const name of [catalog['members.invitation.yes'], catalog['members.invitation.cancel']]) {
    const box = await boxOf(pending.getByRole('button', { name, exact: true }), `${label} ${name}`);
    expect(box.height, `${label}: ${name} target height`).toBeGreaterThanOrEqual(44);
    expect(box.x + box.width, `${label}: ${name} stays inside the row`).toBeLessThanOrEqual(rowBox.x + rowBox.width + 1);
  }
  return pending;
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
  await expect(page.getByRole('heading', { level: 1, name: catalog['home.overview'] })).toBeVisible();
  await expect(page.getByRole('button', { name: catalog['household.menu.label'].replace('{name}', REFERENCE_HOUSEHOLD) })).toBeVisible();
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
// The needs-attention fact keeps one honest wording at every width: the group heading already says
// "Needs attention", so the row adds only the date the item has been waiting since.
async function assertNeedsSinceWording(page: Page, locale: 'en' | 'cs', width: number) {
  const catalog = locale === 'cs' ? cs : en;
  const date = referenceDate('2026-05-12', locale);
  const row = page.locator('main li[data-item-id]').filter({ hasText: "Renew Tom's passport" });
  const since = row.getByText(catalog['items.needsSinceCompact'].replace('{date}', date), { exact: true }).first();
  await expect(since, `needs-attention date at ${width}`).toBeVisible();
  await expect(row).not.toContainText(catalog['items.needsSince'].replace('{date}', date));
}
async function assertReferenceFactsAt390(page: Page, locale: 'en' | 'cs') {
  const catalog = locale === 'cs' ? cs : en;
  const tom = page.locator('main li[data-item-id]').filter({ hasText: "Renew Tom's passport" });
  await expect(tom.getByText(catalog['items.needsSinceCompact'].replace('{date}', referenceDate('2026-05-12', locale)), { exact: true }).first()).toBeVisible();
  await expect(tom.getByText(catalog['items.responsible.read'].replace('{name}', 'anna_novak'), { exact: true }).first()).toBeVisible();
  const car = page.locator('main li[data-item-id]').filter({ hasText: 'Schedule the car inspection' });
  await expect(car.getByText(referenceDate('2026-05-28', locale)).first()).toBeVisible();
  await expect(car.getByText(/anna_novak/).first()).toBeVisible();
}
// UX "Accessibility": no icon-only critical action. Every Home row's Done names itself in words at every width, inside
// a target of at least 44 px each way, and the word is not clipped by the control.
async function assertDoneInWords(page: Page, locale: 'en' | 'cs', width: number) {
  const word = (locale === 'cs' ? cs : en)['items.done'];
  const controls = await page.locator('main li[data-item-id] [data-action="done"]').evaluateAll((buttons) => buttons.map((button) => {
    const box = button.getBoundingClientRect();
    // innerText is the rendered text, so a word hidden with display:none is not in it. Every element that holds the
    // word's text must itself be text-sized, unclipped, visible and inside the control: an sr-only word keeps
    // text-sized line boxes but its own element is a 1 px clip, and an opacity:0 word fails checkVisibility. Rows
    // below the fold are measured too, so this reads layout and style, not hit-testing.
    const walker = document.createTreeWalker(button, NodeFilter.SHOW_TEXT);
    const holders: HTMLElement[] = [];
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      if (node.textContent?.trim() && node.parentElement) holders.push(node.parentElement);
    }
    const within = (rect: DOMRect) => rect.left >= box.left - 0.5 && rect.right <= box.right + 0.5
      && rect.top >= box.top - 0.5 && rect.bottom <= box.bottom + 0.5;
    return {
      word: (button as HTMLElement).innerText.trim(), width: box.width, height: box.height,
      textSized: holders.length > 0 && holders.every((holder) => {
        const rect = holder.getBoundingClientRect();
        return rect.width >= 12 && rect.height >= 10 && getComputedStyle(holder).clipPath === 'none'
          && getComputedStyle(holder).clip === 'auto';
      }),
      inside: holders.length > 0 && holders.every((holder) => within(holder.getBoundingClientRect())),
      visible: holders.length > 0 && holders.every((holder) => holder.checkVisibility({ opacityProperty: true, visibilityProperty: true })),
    };
  }));
  expect(controls.length, `Home Done controls at ${width}`).toBeGreaterThan(0);
  for (const control of controls) {
    expect(control, `Done in words at ${locale} ${width}`).toMatchObject({ word, textSized: true, inside: true, visible: true });
    expect(control.width, `Done target width at ${locale} ${width}`).toBeGreaterThanOrEqual(44);
    expect(control.height, `Done target height at ${locale} ${width}`).toBeGreaterThanOrEqual(44);
  }
}

// The title target must be 44 px, win hit-testing at both edges, and start inside its group panel.
async function assertTitleTargetGeometry(page: Page, width: number) {
  const row = page.locator('main li[data-item-id]').filter({ hasText: 'Replace the smoke alarm battery' });
  const link = row.getByRole('link', { name: 'Open item Replace the smoke alarm battery' });
  await expect(link).toBeVisible();
  // The link's stretched pseudo-element covers the whole row, so the row — not a minimum height on the title line —
  // is the 44 px target. Its own box is only the text block; the sampled points sit clear of the Done control.
  const geometry = await link.evaluate((element) => {
    const row = element.closest('li')!;
    const rect = row.getBoundingClientRect();
    const panel = element.closest('ul')!.getBoundingClientRect();
    const midX = rect.left + rect.width * 0.75;
    const hits = (y: number) => {
      const hit = document.elementFromPoint(midX, y);
      return Boolean(hit && (hit === element || element.contains(hit)));
    };
    return { height: rect.height, top: rect.top, panelTop: panel.top, topHit: hits(rect.top + 2), bottomHit: hits(rect.bottom - 2) };
  });
  expect(geometry.height, `row target height at ${width}`).toBeGreaterThanOrEqual(44);
  expect(geometry.topHit, `row target top-edge hit at ${width}`).toBe(true);
  expect(geometry.bottomHit, `row target bottom-edge hit at ${width}`).toBe(true);
  expect(geometry.top, `row target starts inside the group panel at ${width}`).toBeGreaterThanOrEqual(geometry.panelTop - 0.5);
}

/** The installation's AI apps with both reads settled: the settings, and the two changes the capture seeded. */
async function aiAppsSettled(page: Page, words: Record<keyof typeof en, string>) {
  await expect(page.getByRole('switch', { name: words['aiApps.enabled'], exact: true })).toBeChecked();
  await expect(page.getByRole('list', { name: words['aiApps.changes.title'], exact: true }).getByRole('listitem')).toHaveCount(2);
}

/** One account's AI apps with both reads settled: the facts, and the reference member's empty connected apps. */
async function accountAiAppsSettled(page: Page, words: Record<keyof typeof en, string>) {
  await expect(page.locator('[data-account-ai-apps-now]')).toBeVisible();
  await expect(page.getByText(words['accountApps.empty'], { exact: true })).toBeVisible();
}

// Captures one route in saved light then saved dark theme. Snapshot names must stay unique across the suite.
async function darkScreen(browser: Browser, storageState: string | undefined, path: string, name: string) {
  const context = await browser.newContext({ ...CONTEXT, ...(storageState ? { storageState } : {}), viewport: { width: 390, height: 900 } });
  await pinInvitationDates(context);
  await pinHouseholdCreatedDates(context);
  await pinMcpGrantDates(context);
  await pinMcpPolicyEventDates(context);
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
    } else if (path === '/admin/households') {
      await expect(page.getByRole('main').getByRole('list', { name: en['install.title'] }).getByRole('listitem').filter({ hasText: HOUSEHOLD })).toBeVisible();
    } else if (path === '/admin/accounts') {
      await expect(page.getByRole('main').getByRole('list', { name: en['admin.list.title'] }).getByRole('listitem').filter({ hasText: REFERENCE_MEMBER_LOGIN })).toBeVisible();
    } else if (path === '/admin/ai-apps') {
      await aiAppsSettled(page, en);
    } else if (ACCOUNT_AI_APPS.test(path)) {
      await accountAiAppsSettled(page, en);
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
    else if (path === '/admin/households') await expect(page.getByRole('main').getByRole('list', { name: en['install.title'] }).getByRole('listitem').filter({ hasText: HOUSEHOLD })).toBeVisible();
    else if (path === '/admin/accounts') await expect(page.getByRole('main').getByRole('list', { name: en['admin.list.title'] }).getByRole('listitem').filter({ hasText: REFERENCE_MEMBER_LOGIN })).toBeVisible();
    else if (path === '/admin/ai-apps') await aiAppsSettled(page, en);
    else if (ACCOUNT_AI_APPS.test(path)) await accountAiAppsSettled(page, en);
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
  await expect(page.getByRole('heading', { level: 1, name: en['home.overview'] })).toBeVisible();
  await expect(page.getByRole('button', { name: en['household.menu.label'].replace('{name}', HOUSEHOLD) })).toBeVisible();
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
  await expect(page.getByRole('heading', { level: 1, name: en['home.overview'] })).toBeVisible();
  await expect(page.getByRole('button', { name: en['household.menu.label'].replace('{name}', REFERENCE_HOUSEHOLD) })).toBeVisible();
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
    await assertNeedsSinceWording(page, 'en', width);
    if (width === 390) await assertReferenceFactsAt390(page, 'en');
    await assertDoneInWords(page, 'en', width);
    if ([320, 390, 1024, 1440].includes(width)) await assertTitleTargetGeometry(page, width);
    if (width === 1440 || width === 1024) {
      await assertInterReady(page, `reference-row-height-${width}`);
      const smokeRow = page.locator('main li[data-item-id]').filter({ hasText: 'Replace the smoke alarm battery' });
      const rowHeight = await smokeRow.evaluate((row) => row.getBoundingClientRect().height);
      console.info(`visual-reference-row-height width=${width} height=${rowHeight}`);
      if (width === 1440) {
        // Restored to the original budget: with no minimum height on the title, an ordinary row is its title line
        // plus one metadata line, about 64 px at 1440.
        expect(rowHeight, 'one-line reference row height at 1440').toBeGreaterThanOrEqual(56);
        expect(rowHeight, 'one-line reference row height at 1440').toBeLessThanOrEqual(68);
      }
    }
    if (width === 390) {
      // V5-06: one title line plus a calm metadata line that wraps honestly instead of clipping.
      const phoneRow = page.locator('main li[data-item-id]').filter({ hasText: 'Replace the smoke alarm battery' });
      const phoneRowHeight = await phoneRow.evaluate((row) => row.getBoundingClientRect().height);
      const phoneRows = await page.locator('main li[data-item-id]').evaluateAll((rows) => rows.map((row) => Math.round(row.getBoundingClientRect().height)));
      console.info(`visual-reference-phone-row-height height=${phoneRowHeight} rows=${phoneRows.join(',')}`);
      expect(phoneRowHeight, 'phone row height for an ordinary title at 390').toBeLessThanOrEqual(110);
    }
    await capture(page, `home-reference-en-light-${width}`, width, height, true);
  }

  await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
  await page.reload();
  await assertSettledReference(page, 'cs');
  for (const [width, height] of [[390, 844], [1440, 1080]] as const) {
    await page.setViewportSize({ width, height });
    await assertSettledReference(page, 'cs');
    await assertNeedsSinceWording(page, 'cs', width);
    if (width === 390) await assertReferenceFactsAt390(page, 'cs');
    await assertDoneInWords(page, 'cs', width);
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
  await page.reload();
  await expect(page.locator('html')).not.toHaveAttribute('data-theme', 'dark');
  // All items reuses the same state vocabulary: an upcoming item keeps its upcoming icon and names a plain date.
  await page.goto('/items?status=upcoming');
  const upcomingRow = page.locator('main li[data-item-id]').filter({ hasText: 'Schedule the car inspection' });
  await expect(upcomingRow).toBeVisible();
  await expect(upcomingRow.locator('[data-item-state]')).toHaveAttribute('data-item-state-kind', 'upcoming');
  const carDate = new Intl.DateTimeFormat('en-US', { dateStyle: 'medium', timeZone: 'UTC' }).format(new Date('2026-05-28T00:00:00Z'));
  await expect(upcomingRow.locator('[data-item-state]')).toContainText(en['items.rowAttentionDate'].replace('{date}', carDate));
  await expect(upcomingRow.locator('[data-item-state]')).not.toContainText('Needs attention');
  const upcomingRange = en['allItems.range'].replace('{from}', '1').replace('{to}', '3').replace('{total}', '3');
  await expect(page.getByRole('main').getByRole('heading', { level: 2, name: upcomingRange })).toBeVisible();
  await capture(page, 'all-items-upcoming-en-light-1440', 1440, 1080, false);
  await page.goto('/');
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
      await expectDialogActionsOnScreen(dialog, en['items.add.submit'], `add-repeat-${width}`);
      await capture(page, `add-repeat-${width}`, width, 900);
    }
    if (width === 320) await expectShortDialogUnpinned(page, dialog);
    await dialog.getByRole('form', { name: en['items.add.formLabel'] }).getByRole('button', { name: en['items.cancel'], exact: true }).click();
  }
  await context.close();
  await darkScreen(browser, statePath, '/', 'home-add');
});

test('All items populated, no-match and dark', async ({ browser }) => {
  const { context, page } = await authenticated(browser);
  await page.goto('/items');
  await waitForAllItemsSummary(page);
  for (const width of [320, 390, 768, 1024, 1440]) await capture(page, `all-items-${width}`, width, width === 320 ? 640 : 900, false);
  // Density guard: a two-line row (title + one metadata line carrying subject, state and responsible) stays as
  // compact as a Home row at 1440, and so does a repeating row, whose cadence joins that same line. Rows are
  // chosen structurally, not by fixture title.
  await page.setViewportSize({ width: 1440, height: 900 });
  const compactRow = page.locator('main li[data-item-id]')
    .filter({ has: page.locator('[data-item-meta] [data-subject-pill]') })
    .filter({ has: page.locator('[data-item-meta] [data-item-state]') })
    .filter({ has: page.locator('[data-item-meta] [data-item-responsible]') })
    .first();
  await expect(compactRow, 'a two-line All items row exists at 1440').toHaveCount(1);
  const compactRowHeight = await compactRow.evaluate((row) => row.getBoundingClientRect().height);
  console.info(`visual-all-items-row-height height=${compactRowHeight}`);
  expect(compactRowHeight, 'two-line All items row height at 1440').toBeGreaterThanOrEqual(48);
  expect(compactRowHeight, 'two-line All items row height at 1440').toBeLessThanOrEqual(58);
  const repeatingRow = page.locator('main li[data-item-id]')
    .filter({ has: page.locator('[data-item-meta] [data-item-recurrence]') })
    .first();
  await expect(repeatingRow, 'a repeating All items row exists at 1440').toHaveCount(1);
  const repeatingRowHeight = await repeatingRow.evaluate((row) => row.getBoundingClientRect().height);
  console.info(`visual-all-items-repeating-row-height height=${repeatingRowHeight}`);
  expect(repeatingRowHeight, 'repeating All items row height at 1440').toBeGreaterThanOrEqual(48);
  expect(repeatingRowHeight, 'repeating All items row height at 1440').toBeLessThanOrEqual(58);
  // The desktop toolbar scans as one band: Show and the three refinements share a single row.
  const filterTops = await page.getByRole('main').getByRole('combobox')
    .evaluateAll((nodes) => nodes.map((node) => Math.round(node.getBoundingClientRect().top)));
  expect(filterTops, 'desktop filters share one row').toHaveLength(4);
  expect(new Set(filterTops).size, 'desktop filters share one row').toBe(1);
  await page.getByRole('searchbox', { name: en['allItems.search'], exact: true }).fill('no-such-visual-item');
  await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
  await expect(page.getByRole('main').getByRole('heading', { name: en['allItems.noMatch'] })).toBeVisible();
  await captureScreen(page, 'all-no-match');
  // Czech phone evidence for the same toolbar, list and states.
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
  await page.goto('/items');
  await expect(page.getByRole('heading', { level: 1, name: cs['allItems.title'] })).toBeVisible();
  await expect(page.getByRole('main').getByRole('listitem').first()).toBeVisible();
  await capture(page, 'all-items-cs-390', 390, 900, false);
  // A wrapped metadata line never starts or ends on a separator: at phone widths, in Czech, every dot that is
  // visible sits after a fact on its own line, and every line-leading dot is clipped outside the line's box.
  for (const width of [320, 390]) {
    await page.setViewportSize({ width, height: 900 });
    const lines = await page.locator('main li[data-item-id] [data-item-meta]').evaluateAll((metas) => metas.map((meta) => {
      const clip = meta.parentElement!.getBoundingClientRect();
      const start = meta.getBoundingClientRect().left;
      return [...meta.querySelectorAll(':scope > [data-item-meta-unit]')].map((unit, index) => {
        // In a left-aligned flex-wrap line a unit starts a line exactly when it sits at the line box's left edge,
        // whatever the heights of its neighbours.
        const leadsLine = Math.abs(unit.getBoundingClientRect().left - start) < 1;
        const shown = unit.querySelector(':scope > [data-item-separator]')!.getBoundingClientRect().left >= clip.left - 0.5;
        return { text: unit.textContent, index, leadsLine, shown };
      });
    }));
    const units = lines.flat();
    expect(units.length, `metadata units at ${width}`).toBeGreaterThanOrEqual(8);
    // The wrap case is really exercised: some fact after the first starts a line of its own.
    expect(units.some((unit) => unit.index > 0 && unit.leadsLine), `a wrapped fact at ${width}`).toBe(true);
    const strays = units.filter((unit) => unit.leadsLine === unit.shown || (unit.index === 0 && !unit.leadsLine))
      .map((unit) => `${unit.text} #${unit.index} leads=${unit.leadsLine} shown=${unit.shown}`);
    expect(strays, `metadata separators at ${width}`).toEqual([]);
  }
  // The reset and the "Filters" toggle share a phone line: they must never overlap, in either locale, at
  // either phone width, expanded or collapsed.
  const assertFilterControlsDoNotOverlap = async (label: string) => {
    const boxes = await page.evaluate(() => {
      const reset = document.querySelector('[data-all-items-clear]');
      const filters = document.querySelector('[data-all-items-filter-disclosure]');
      if (!reset || !filters) return null;
      const a = reset.getBoundingClientRect();
      const b = filters.getBoundingClientRect();
      return {
        overlap: Math.min(a.right, b.right) - Math.max(a.left, b.left) > 0
          && Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > 0,
        insideViewport: a.right <= window.innerWidth + 0.5 && a.left >= -0.5,
      };
    });
    expect(boxes, `reset visible for ${label}`).not.toBeNull();
    expect(boxes!.overlap, `Filters and reset do not overlap (${label})`).toBe(false);
    expect(boxes!.insideViewport, `reset inside the viewport (${label})`).toBe(true);
  };
  for (const [width, locale] of [[320, 'cs'], [390, 'cs'], [320, 'en'], [390, 'en']] as const) {
    await page.setViewportSize({ width, height: width === 320 ? 640 : 900 });
    await page.evaluate((value) => localStorage.setItem('tendo.locale', value), locale);
    await page.goto('/items');
    const catalog = locale === 'cs' ? cs : en;
    await expect(page.getByRole('heading', { level: 1, name: catalog['allItems.title'] })).toBeVisible();
    const disclosure = page.locator('[data-all-items-filter-disclosure]');
    await expect(disclosure).toBeVisible();
    if (await disclosure.getAttribute('aria-expanded') !== 'true') await disclosure.click();
    await page.getByLabel(catalog['items.subject']).selectOption({ index: 1 });
    await expect(page.locator('[data-all-items-clear]')).toBeVisible();
    await assertFilterControlsDoNotOverlap(`${locale} ${width} expanded`);
    await disclosure.click();
    await expect(disclosure).toHaveAttribute('aria-expanded', 'false');
    await assertFilterControlsDoNotOverlap(`${locale} ${width} collapsed`);
  }
  // 1024 with the reset visible: the band keeps four equal columns and the reset takes the line below, so
  // no label has to wrap and nothing is squeezed into a ragged fifth cell.
  await page.setViewportSize({ width: 1024, height: 900 });
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'en'));
  await page.goto('/items');
  await expect(page.locator('[data-all-items-filter-disclosure]')).toBeHidden();
  await page.getByRole('searchbox', { name: en['allItems.search'], exact: true }).fill('Kontrola');
  await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
  await expect(page.getByRole('main').getByRole('region', { name: en['allItems.results'] })).toBeVisible();
  await expect(page.locator('[data-all-items-clear]')).toBeVisible();
  const wideFilterTops = await page.getByRole('main').getByRole('combobox')
    .evaluateAll((nodes) => nodes.map((node) => Math.round(node.getBoundingClientRect().top)));
  expect(wideFilterTops, 'four filters at 1024').toHaveLength(4);
  expect(new Set(wideFilterTops).size, 'four filters share one row at 1024').toBe(1);
  const responsibleLabel = page.locator('label').filter({ has: page.getByLabel(en['items.responsible.label']) }).first();
  const [responsibleLabelHeight, responsibleSelectHeight] = await Promise.all([
    responsibleLabel.evaluate((node) => node.getBoundingClientRect().height),
    page.getByLabel(en['items.responsible.label']).evaluate((node) => node.getBoundingClientRect().height),
  ]);
  expect(responsibleLabelHeight - responsibleSelectHeight, 'the responsible filter label stays on one line at 1024').toBeLessThanOrEqual(30);
  await capture(page, 'all-items-1024-clear', 1024, 900, false);
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'en'));
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
  for (const width of [390, 768, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await expectDialogActionsOnScreen(page.getByRole('dialog'), en['itemDetail.save'], `edit-conflict-${width}`);
  }
  // Evidence shows the conflict summary and the rebased form from the top of the dialog.
  await page.getByRole('dialog').evaluate((dialog) => dialog.scrollTo({ top: 0 }));
  await captureScreen(page, 'edit-conflict');
  await context.close();
});

test('People current and archived', async ({ browser }) => {
  const { context, page } = await authenticated(browser);
  await page.goto('/people'); await captureScreen(page, 'people-current', true);
  await capture(page, 'people-1024', 1024, 900, true);
  // Locale-independent guard: at 390 the name keeps a readable column and never breaks mid-word, because the
  // wrapping is driven by the name's width rather than by a breakpoint.
  const assertPeopleNames = async (label: string) => {
    const names = await page.getByRole('main').locator('li:has([data-subject-name])').evaluateAll((rows) => rows.map((row) => {
      const name = row.querySelector<HTMLElement>('[data-subject-name]')!;
      const rect = name.getBoundingClientRect();
      const lineHeight = Number.parseFloat(getComputedStyle(name).lineHeight) || 22;
      // With normal word wrapping the text must still fit its column: otherwise a word had to be broken
      // mid-word, which is exactly the squeeze this guard exists to catch.
      const previousWrap = name.style.overflowWrap;
      name.style.overflowWrap = 'normal';
      const fitsWithoutMidWordBreak = name.scrollWidth <= name.clientWidth + 1;
      name.style.overflowWrap = previousWrap;
      return { text: name.textContent?.trim().slice(0, 24), width: Math.round(rect.width), lines: Math.max(1, Math.round(rect.height / lineHeight)), fitsWithoutMidWordBreak };
    }));
    console.info(`visual-people-names ${label} ${JSON.stringify(names)}`);
    expect(names.length).toBeGreaterThan(1);
    for (const name of names) {
      expect(name.width, `People name column at 390 (${label})`).toBeGreaterThanOrEqual(150);
      expect(name.fitsWithoutMidWordBreak, `People name is not broken mid-word at 390 (${label})`).toBe(true);
    }
  };
  await page.setViewportSize({ width: 390, height: 900 });
  await expect(page.getByRole('main').locator('[data-subject-name]').first()).toBeVisible();
  await assertPeopleNames('en');
  const peopleRows = await page.getByRole('main').locator('li:has([data-subject-name])').evaluateAll((rows) => rows.map((row) => {
    const actions = (row.querySelector('button[aria-label^="Edit"]') ?? row.querySelector('a[href^="/items?subject="]')!).getBoundingClientRect();
    const rowRect = row.getBoundingClientRect();
    return { height: Math.round(rowRect.height), rightAligned: Math.round(rowRect.right - actions.right) <= 24 };
  }));
  console.info(`visual-people-row-heights ${JSON.stringify(peopleRows.slice(0, 3))}`);
  expect(peopleRows.length).toBeGreaterThan(1);
  for (const row of peopleRows) {
    // Whether the actions wrap is width-driven (assertPeopleNames holds that invariant), so this only checks
    // that they stay right-aligned and the row stays compact.
    expect(row.rightAligned, 'People actions stay right-aligned at 390').toBe(true);
    expect(row.height, 'People row height at 390').toBeLessThanOrEqual(150);
  }
  // From sm the same markup keeps the actions on the name line without any breakpoint-specific layout.
  await page.setViewportSize({ width: 768, height: 900 });
  const inlineActions = await page.getByRole('main').locator('li:has([data-subject-name])').first().evaluate((row) => {
    const name = row.querySelector('[data-subject-name]')!.getBoundingClientRect();
    const actions = row.querySelector('button[aria-label^="Edit"]')!.getBoundingClientRect();
    return actions.top < name.bottom;
  });
  expect(inlineActions, 'People actions sit on the name line at 768').toBe(true);
  await page.setViewportSize({ width: 320, height: 640 });
  await expect(page.getByRole('main').locator('[data-subject-name]').first()).toBeVisible();
  const narrowRow = await page.getByRole('main').locator('li:has([data-subject-name])').first().evaluate((row) => {
    const name = row.querySelector('[data-subject-name]')!.getBoundingClientRect();
    const edit = row.querySelector('button[aria-label^="Edit"]')!.getBoundingClientRect();
    return { nameWidth: Math.round(name.width), actionsBelow: edit.top >= name.bottom, overflow: document.documentElement.scrollWidth - window.innerWidth };
  });
  expect(narrowRow.overflow, 'People page overflow at 320').toBeLessThanOrEqual(0);
  expect(narrowRow.actionsBelow, 'People row actions drop below the metadata at 320').toBe(true);
  expect(narrowRow.nameWidth, 'People name column at 320').toBeGreaterThanOrEqual(150);
  await capture(page, 'people-320', 320, 640, false);
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
  await page.reload();
  await expect(page.getByRole('heading', { level: 1, name: cs['subjects.title'] })).toBeVisible();
  await expect(page.getByRole('main').locator('[data-subject-name]').first()).toBeVisible();
  await page.setViewportSize({ width: 390, height: 900 });
  await assertPeopleNames('cs');
  await capture(page, 'people-cs-390', 390, 900, false);
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'en'));
  await page.reload();
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
  const confirmingCs = await openRevokeConfirmation(page, 'cs', 390, 'below');
  await capture(page, 'members-revoke-confirm-cs-390', 390, 900, true);
  await confirmingCs.getByRole('button', { name: cs['members.invitation.cancel'], exact: true }).click();
  // Either side of the `md` switch: below the text at 700, beside it again from 768.
  for (const [width, placement] of [[700, 'below'], [768, 'beside']] as const) {
    await page.setViewportSize({ width, height: 900 });
    await assertMembersSettled(page, 'cs');
    const confirming = await openRevokeConfirmation(page, 'cs', width, placement);
    await confirming.getByRole('button', { name: cs['members.invitation.cancel'], exact: true }).click();
  }
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'en'));
  await page.reload();
  await page.setViewportSize({ width: 320, height: 900 });
  await assertMembersSettled(page, 'en');
  const confirmingEn = await openRevokeConfirmation(page, 'en', 320, 'below');
  await confirmingEn.getByRole('button', { name: en['members.invitation.cancel'], exact: true }).click();
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
  await expect(memberPage.getByRole('heading', { level: 1, name: en['home.overview'] })).toBeVisible();
  await expect(memberPage.getByRole('banner').getByRole('button', { name: en['household.menu.label'].replace('{name}', HOUSEHOLD), exact: true })).toBeVisible();
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
  await expect(memberPage.getByRole('heading', { level: 1, name: en['home.overview'] })).toBeVisible();
  await expect(memberPage.getByRole('banner').getByRole('button', { name: en['household.menu.label'].replace('{name}', HOUSEHOLD), exact: true })).toBeVisible();
  await memberPage.goto('/admin');
  await expect(memberPage.getByRole('heading', { name: en['admin.onlyAdmins.title'] })).toBeVisible();
  await expect(memberPage.getByRole('main').getByRole('list', { name: en['admin.list.title'] })).toHaveCount(0);
  await expect(memberPage.getByRole('navigation', { name: en['nav.label'] }).getByRole('link', { name: en['admin.title'], exact: true })).toBeHidden();
  await capture(memberPage, 'admin-member-390', 390, 900, true);
  await memberPage.setViewportSize({ width: 1440, height: 900 });
  await capture(memberPage, 'admin-member-1440', 1440, 900, false);
  await memberContext.close();
});

test('Installation households and one household’s accounts with their actions', async ({ browser }) => {
  test.setTimeout(90_000);
  const { context, page } = await authenticated(browser);
  // The installation list: every household of this installation, names only, never their contents.
  await page.goto('/admin/households');
  await expect(page.getByRole('heading', { level: 1, name: en['install.page.title'] })).toBeVisible();
  const households = page.getByRole('main').getByRole('list', { name: en['install.title'] });
  const ownRow = households.getByRole('listitem').filter({ hasText: HOUSEHOLD });
  await expect(ownRow.getByRole('link', { name: HOUSEHOLD, exact: true })).toBeVisible();
  // The created line renders the pinned server timestamp (the test clock only drives item dates), so a baseline
  // that depends on it cannot go stale, and this assertion fails if the pin is dropped.
  await expect(ownRow).toContainText(en['install.created'].replace('{date}', 'May 14, 2026'));

  // The create dialog starts a household from this list: a name, the zone its dates follow and the account that
  // administers it. The seeded installation's directory is small and ordered by login, so the search settles on one
  // stable account; a chosen account keeps Create enabled, which is the state this baseline shows. Cancel leaves the
  // list exactly as it was found.
  const createDialog = await openCreate(page);
  await createDialog.getByLabel(en['install.create.name'], { exact: true }).fill('Created household');
  const createSearch = createDialog.getByLabel(en['install.add.query'], { exact: true });
  await createSearch.fill(REFERENCE_MEMBER_LOGIN);
  await createSearch.press('Enter');
  const createChoice = createDialog.getByRole('radio', { name: REFERENCE_MEMBER_LOGIN, exact: true });
  await expect(createChoice).toBeVisible();
  await createChoice.check();
  await expect(createDialog.getByRole('button', { name: en['install.create.submit'], exact: true })).toBeEnabled();
  await capture(page, 'admin-households-create-390', 390, 900, false);
  await capture(page, 'admin-households-create-1440', 1440, 900, false);
  await createDialog.getByRole('button', { name: en['admin.action.cancel'], exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await darkScreen(browser, statePath, '/admin/households', 'admin-households');

  // The drill-down: opening a row is a plain navigation, so the heading carries the household's name.
  await page.goto('/admin/households');
  await page.setViewportSize({ width: 390, height: 900 });
  await households.getByRole('listitem').filter({ hasText: HOUSEHOLD }).getByRole('link', { name: HOUSEHOLD, exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: HOUSEHOLD })).toBeVisible();
  const accounts = page.getByRole('main').getByRole('list', { name: en['admin.list.title'] });
  await expect(accounts.getByRole('listitem').filter({ hasText: OWNER_LOGIN })).toContainText(en['members.role.admin']);
  await expect(accounts.getByRole('listitem').filter({ hasText: REFERENCE_MEMBER_LOGIN })).toContainText(en['members.role.member']);
  // Each row carries its two membership actions; the viewer's own row asks about leaving.
  await expect(accounts.getByRole('button', { name: en['admin.action.demoteLabel'].replace('{login}', OWNER_LOGIN), exact: true })).toBeVisible();
  await expect(accounts.getByRole('button', { name: en['admin.action.leaveLabel'].replace('{login}', OWNER_LOGIN), exact: true })).toBeVisible();
  await expect(accounts.getByRole('button', { name: en['admin.action.promoteLabel'].replace('{login}', REFERENCE_MEMBER_LOGIN), exact: true })).toBeVisible();
  await expect(accounts.getByRole('button', { name: en['admin.action.removeLabel'].replace('{login}', REFERENCE_MEMBER_LOGIN), exact: true })).toBeVisible();
  // The third action opens the add dialog on the installation's account directory. The seeded visual installation has
  // a small, fixed set of accounts, and the server orders them by login, so the empty search settles on stable rows;
  // the capture waits for the count line and every row before it, and Cancel leaves the page as it was.
  const addButton = page.getByRole('main').getByRole('button', { name: en['install.add.action'], exact: true });
  await expect(addButton).toBeVisible();
  const addDialog = await openAdd(page, en['install.add.action']);
  const directoryResponse = await page.request.get('/api/v1/admin/accounts?limit=20');
  expect(directoryResponse.status()).toBe(200);
  const directory = (await directoryResponse.json() as { items: Array<{ login: string }> }).items;
  expect(directory.length, 'the visual installation has a fixed, small account directory').toBeGreaterThan(0);
  // The count names this page, not the whole match; the visual fixture's directory fits one page.
  const addCountTemplate = directory.length === 1 ? en['install.add.count.one'] : en['install.add.count.other'];
  await expect(addDialog.locator('[aria-live="polite"][aria-atomic="true"]')).toHaveText(addCountTemplate.replace('{count}', String(directory.length)));
  const addChoices = addDialog.getByRole('listitem');
  await expect(addChoices).toHaveCount(directory.length);
  for (const [index, entry] of directory.entries()) await expect(addChoices.nth(index)).toHaveText(entry.login);
  await capture(page, 'admin-households-add-390', 390, 900, false);
  await capture(page, 'admin-households-add-1440', 1440, 900, false);
  await addDialog.getByRole('button', { name: en['admin.action.cancel'], exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await captureScreen(page, 'admin-households-detail', true);

  // Czech phone width after captureScreen left the page at 1440.
  await page.goto('/admin/households');
  await page.setViewportSize({ width: 390, height: 900 });
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
  await page.reload();
  await expect(page.getByRole('heading', { level: 1, name: cs['install.page.title'] })).toBeVisible();
  await expect(page.getByRole('main').getByRole('list', { name: cs['install.title'] }).getByRole('listitem').filter({ hasText: HOUSEHOLD })).toBeVisible();
  await capture(page, 'admin-households-cs-390', 390, 900, true);
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'en'));
  await context.close();

  // A plain member opens the same address directly: the server refuses and no household is listed.
  const memberContext = await browser.newContext({ ...CONTEXT, viewport: { width: 390, height: 844 } });
  const memberPage = await memberContext.newPage();
  await memberPage.clock.install({ time: new Date('2026-05-14T08:00:00Z') });
  await memberPage.goto('/login');
  await memberPage.getByLabel(en['onboarding.login'], { exact: true }).fill('visual_member');
  await memberPage.getByLabel(en['login.password'], { exact: true }).fill(`${ownerPassword}-member`);
  await memberPage.getByRole('button', { name: en['login.submit'], exact: true }).click();
  await expect(memberPage.getByRole('heading', { level: 1, name: en['home.overview'] })).toBeVisible();
  await memberPage.goto('/admin/households');
  await expect(memberPage.getByRole('heading', { name: en['install.onlyAdmins.title'] })).toBeVisible();
  await expect(memberPage.getByRole('main').getByRole('list', { name: en['install.title'] })).toHaveCount(0);
  await expect(memberPage.getByRole('navigation', { name: en['admin.sections.label'] })).toHaveCount(0);
  await capture(memberPage, 'admin-households-member-390', 390, 900, true);
  await memberContext.close();
});

test('Installation accounts and the delete dialog', async ({ browser }) => {
  test.setTimeout(90_000);
  const { context, page } = await authenticated(browser);
  // The directory is the whole installation, not one household: the viewer's own account, the invited member and the
  // reference member. The seeded visual installation is small and the server orders rows by login, so the capture
  // settles on a fixed list.
  await page.goto('/admin/accounts');
  await expect(page.getByRole('heading', { level: 1, name: en['install.page.title'] })).toBeVisible();
  const switcher = page.getByRole('navigation', { name: en['admin.sections.label'] });
  await expect(switcher.getByRole('link', { name: en['admin.sections.accounts'], exact: true })).toHaveAttribute('aria-current', 'page');
  const directory = page.getByRole('main').getByRole('list', { name: en['admin.list.title'] });
  const referenceRow = directory.getByRole('listitem').filter({ hasText: REFERENCE_MEMBER_LOGIN });
  const ownRow = directory.getByRole('listitem').filter({ hasText: OWNER_LOGIN });
  await expect(referenceRow.locator('[data-install-login]')).toHaveText(REFERENCE_MEMBER_LOGIN);
  // No account deletes itself: the viewer's own row is a login and nothing else.
  await expect(ownRow).toContainText(en['members.you']);
  await expect(ownRow.getByRole('button')).toHaveCount(0);

  // The dialog asks for the login itself, so the capture shows the confirmation text before the enabled action. It is
  // only a capture: Cancel is pressed, no account is deleted, and the list stays exactly as it was found.
  const deleteLabel = en['install.accounts.delete.label'].replace('{login}', REFERENCE_MEMBER_LOGIN);
  await referenceRow.getByRole('button', { name: deleteLabel, exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('heading', { name: en['install.accounts.delete.title'].replace('{login}', REFERENCE_MEMBER_LOGIN) })).toBeVisible();
  const confirmField = dialog.getByLabel(en['install.accounts.delete.confirm'].replace('{login}', REFERENCE_MEMBER_LOGIN), { exact: true });
  await confirmField.fill(REFERENCE_MEMBER_LOGIN);
  await expect(dialog.getByRole('button', { name: en['install.accounts.delete.action'], exact: true })).toBeEnabled();
  await capture(page, 'admin-accounts-delete-390', 390, 900, false);
  await capture(page, 'admin-accounts-delete-1440', 1440, 900, false);
  await dialog.getByRole('button', { name: en['admin.action.cancel'], exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await expect(referenceRow).toBeVisible();

  // The directory in the saved light theme at both widths, then the same in the saved dark theme.
  await darkScreen(browser, statePath, '/admin/accounts', 'admin-accounts');

  // Czech phone width after the dark pass restored the light theme.
  await page.goto('/admin/accounts');
  await page.setViewportSize({ width: 390, height: 900 });
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
  await page.reload();
  await expect(page.getByRole('heading', { level: 1, name: cs['install.page.title'] })).toBeVisible();
  await expect(page.getByRole('main').getByRole('list', { name: cs['admin.list.title'] }).getByRole('listitem').filter({ hasText: REFERENCE_MEMBER_LOGIN })).toBeVisible();
  await capture(page, 'admin-accounts-cs-390', 390, 900, true);
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'en'));
  await context.close();
});

/** Saves read only and then the starting settings back through the API: two changes, and nothing left changed. */
async function seedAiAppsChanges(page: Page) {
  const path = '/api/v1/admin/mcp-policy';
  const settings = { enabled: true, defaultAccountEnabled: true, defaultMaxScopes: ['tendo:read', 'tendo:write'] };
  const current = await api(page, 'get', path);
  expect(current.status()).toBe(200);
  expect(await current.json()).toEqual({ ...settings, operatorEnabled: true });
  let version = current.headers().etag;
  for (const defaultMaxScopes of [['tendo:read'], settings.defaultMaxScopes]) {
    const saved = await page.request.put(path, { headers: { Origin: BASE_URL!, 'If-Match': version }, data: { ...settings, defaultMaxScopes } });
    expect(saved.status()).toBe(200);
    version = saved.headers().etag;
  }
}

test('Installation AI apps', async ({ browser }) => {
  test.setTimeout(60_000);
  const { context, page } = await authenticated(browser);
  // The visual run starts with apps allowed by the server configuration and the shipped installation defaults: on,
  // accounts on, read and change. Two real saves through the API, read only and then back, give Recent changes its
  // two rows and leave the settings as they were, so the consent captures that follow still find apps allowed. The
  // browser itself saves nothing.
  await seedAiAppsChanges(page);
  await page.goto('/admin/ai-apps');
  await expect(page.getByRole('heading', { level: 1, name: en['install.page.title'] })).toBeVisible();
  const switcher = page.getByRole('navigation', { name: en['admin.sections.label'] });
  await expect(switcher.getByRole('link', { name: en['admin.sections.aiApps'], exact: true })).toHaveAttribute('aria-current', 'page');
  await expect(page.getByRole('switch', { name: en['aiApps.enabled'], exact: true })).toBeChecked();
  await expect(page.getByRole('switch', { name: en['aiApps.accounts'], exact: true })).toBeChecked();
  await expect(page.getByRole('checkbox', { name: new RegExp(`^${en['aiApps.access.read']}`) })).toBeChecked();
  await expect(page.getByRole('checkbox', { name: new RegExp(`^${en['aiApps.access.write']}`) })).toBeChecked();
  await expect(page.getByText(en['aiApps.serverOff'])).toHaveCount(0);
  await aiAppsSettled(page, en);
  const changes = page.getByRole('list', { name: en['aiApps.changes.title'], exact: true }).getByRole('listitem');
  await expect(changes.nth(0)).toContainText(en['aiApps.changes.most'].replace('{access}', en['aiApps.changes.access.both']));
  await expect(changes.nth(1)).toContainText(en['aiApps.changes.most'].replace('{access}', en['aiApps.changes.access.read']));
  await expect(changes.nth(0)).toContainText(en['aiApps.changes.by'].replace('{login}', OWNER_LOGIN).replace('{when}', ''));
  await captureScreen(page, 'admin-ai-apps', true);
  await page.setViewportSize({ width: 320, height: 640 });
  await capture(page, 'admin-ai-apps-320', 320, 640, true);

  // Nothing chosen: the field asks for one before anything is sent. Reloading drops the unsaved edit.
  await page.setViewportSize({ width: 390, height: 900 });
  await page.getByRole('checkbox', { name: new RegExp(`^${en['aiApps.access.read']}`) }).uncheck();
  await page.getByRole('checkbox', { name: new RegExp(`^${en['aiApps.access.write']}`) }).uncheck();
  await page.getByRole('button', { name: en['aiApps.save'], exact: true }).click();
  await expect(page.getByText(en['aiApps.access.missing'], { exact: true })).toBeVisible();
  await capture(page, 'admin-ai-apps-missing-390', 390, 900, true);
  await page.reload();
  await expect(page.getByRole('checkbox', { name: new RegExp(`^${en['aiApps.access.read']}`) })).toBeChecked();
  await aiAppsSettled(page, en);

  await darkScreen(browser, statePath, '/admin/ai-apps', 'admin-ai-apps');

  await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
  await page.reload();
  await expect(page.getByRole('heading', { level: 1, name: cs['install.page.title'] })).toBeVisible();
  await aiAppsSettled(page, cs);
  await capture(page, 'admin-ai-apps-cs-390', 390, 900, true);
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'en'));
  await context.close();
});

test('Installation account AI apps', async ({ browser }) => {
  test.setTimeout(60_000);
  const { context, page } = await authenticated(browser);
  // Reached from the reference member's row in Accounts. Nothing is set for that account and nothing is saved, so it
  // follows the shipped installation defaults and the captures that follow find apps as they were.
  await page.goto('/admin/accounts');
  const openLabel = en['accountAiApps.openLabel'].replace('{login}', REFERENCE_MEMBER_LOGIN);
  const link = page.getByRole('link', { name: openLabel, exact: true });
  const href = await link.getAttribute('href');
  expect(href).toMatch(/^\/admin\/accounts\/[0-9a-f-]{36}\/ai-apps$/);
  await link.click();
  const heading = en['accountAiApps.titleFor'].replace('{login}', REFERENCE_MEMBER_LOGIN);
  await expect(page.getByRole('heading', { level: 1, name: heading })).toBeVisible();
  const useGroup = page.getByRole('group', { name: en['accountAiApps.use.title'], exact: true });
  await expect(useGroup.getByRole('radio', { name: en['accountAiApps.use.default'], exact: true })).toBeChecked();
  await expect(page.locator('[data-account-ai-apps-now]')).toHaveText(en['accountAiApps.now.both']);
  await accountAiAppsSettled(page, en);
  await captureScreen(page, 'admin-account-ai-apps', true);
  await page.setViewportSize({ width: 320, height: 640 });
  await capture(page, 'admin-account-ai-apps-320', 320, 640, true);

  // A direct visit carries no login, so the dark pass and Czech show the generic heading.
  await darkScreen(browser, statePath, href!, 'admin-account-ai-apps');

  await page.setViewportSize({ width: 390, height: 900 });
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
  await page.reload();
  await expect(page.getByRole('heading', { level: 1, name: cs['accountAiApps.title'] })).toBeVisible();
  await expect(page.getByRole('radio', { name: cs['accountAiApps.use.default'], exact: true }).first()).toBeChecked();
  await accountAiAppsSettled(page, cs);
  await capture(page, 'admin-account-ai-apps-cs-390', 390, 900, true);
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'en'));
  await context.close();
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
  // The top bar's account menu open: who is signed in, Account settings and Sign out in words, at the narrowest
  // phone and on a desktop.
  for (const [width, height] of [[320, 640], [1440, 900]] as const) {
    await page.setViewportSize({ width, height });
    await assertAccountSettled(page, 'en', 'en');
    await openAccountMenu(page);
    await capture(page, `account-menu-${width}`, width, height, false);
    await page.keyboard.press('Escape');
  }
  await context.close();
  await darkScreen(browser, statePath, '/account', 'account');
});

/**
 * The content's right edge inside main: the furthest right anything the page actually draws reaches, as an offset from
 * main's content box. Drawn means visible text, a control, an image or icon, or a box that paints a border, background
 * or shadow. Plain layout boxes (the shell's measure wrapper, a page's root section) are not drawn, so they cannot
 * make a page that ends at its own narrower width look as wide as the measure. Dialogs and fixed layers (the floating
 * notice) belong to the viewport, not the page.
 */
async function contentReach(page: Page) {
  return page.getByRole('main').evaluate((main) => {
    const style = getComputedStyle(main);
    const box = main.getBoundingClientRect();
    const left = box.left + parseFloat(style.paddingLeft);
    const available = box.right - parseFloat(style.paddingRight) - left;
    const outsidePage = (element: Element) => {
      for (let node: Element | null = element; node && node !== main; node = node.parentElement) {
        const computed = getComputedStyle(node);
        if (node.tagName === 'DIALOG' || computed.position === 'fixed' || computed.visibility === 'hidden' || computed.display === 'none') return true;
      }
      return false;
    };
    const painted = (color: string) => color !== 'transparent' && !/rgba\([^)]*,\s*0\)$/.test(color);
    const drawn = (element: Element) => {
      if (element.matches('button, input, select, textarea, img, svg, hr, progress')) return true;
      const computed = getComputedStyle(element);
      if (painted(computed.backgroundColor) || computed.boxShadow !== 'none') return true;
      return (['Top', 'Right', 'Bottom', 'Left'] as const).some((side) => parseFloat(computed[`border${side}Width`]) > 0
        && computed[`border${side}Style`] !== 'none' && painted(computed[`border${side}Color`]));
    };
    let right = left;
    const reach = (rect: DOMRect) => { if (rect.width > 1 && rect.height > 1) right = Math.max(right, rect.right); };
    for (const element of main.querySelectorAll('*')) {
      if (!outsidePage(element) && drawn(element)) reach(element.getBoundingClientRect());
    }
    const walker = document.createTreeWalker(main, NodeFilter.SHOW_TEXT);
    for (let text = walker.nextNode(); text; text = walker.nextNode()) {
      if (!text.textContent?.trim() || !text.parentElement || outsidePage(text.parentElement)) continue;
      const range = document.createRange();
      range.selectNodeContents(text);
      for (const rect of range.getClientRects()) reach(rect);
    }
    // Where the page begins: its level-1 heading, the one element every page has at its leading edge. (Glyph boxes of
    // wrapped metadata can sit a pixel left of the column, so the heading, not the leftmost text, marks the start.)
    const heading = main.querySelector('h1');
    const start = heading ? heading.getBoundingClientRect().left - left : Number.NaN;
    return { start: Math.round(start), reach: Math.round(right - left), available: Math.round(available) };
  });
}

test('Household pages share one content measure at 1920', async ({ browser }) => {
  test.setTimeout(90_000);
  const { context, page } = await authenticated(browser);
  await page.setViewportSize({ width: 1920, height: 1080 });
  const item = requiredFixture().items[1];
  const pages: Array<[string, string, () => Promise<void>]> = [
    ['Home', '/', async () => { await expect(page.locator('main li[data-item-id]').first()).toBeVisible(); }],
    ['All items', '/items', async () => { await expect(page.locator('main li[data-item-id]').first()).toBeVisible(); await waitForAllItemsSummary(page); }],
    ['item detail', `/items/${item.id}`, async () => { await expect(page.getByRole('heading', { level: 1, name: item.title })).toBeVisible(); }],
    ['People and things', '/people', async () => { await expect(page.getByRole('main').locator('[data-subject-name]').first()).toBeVisible(); }],
    ['Members', '/members', async () => { await expect(page.getByRole('main').getByRole('list', { name: en['members.list.title'] })).toBeVisible(); }],
    ['Admin center', '/admin', async () => { await expect(page.getByRole('main').getByRole('list', { name: en['admin.list.title'] })).toBeVisible(); }],
    ['Account', '/account', async () => { await expect(page.locator('#account-identity-heading')).toBeVisible(); }],
  ];
  const spans: Record<string, { start: number; end: number }> = {};
  const expected: Record<string, { start: number; end: number }> = {};
  for (const [name, path, settled] of pages) {
    await page.goto(path);
    await settled();
    const { start, reach, available } = await contentReach(page);
    // The canvas is wider than the measure, so a page that fills its measure stops visibly short of main's edge.
    expect(available, `main content width at 1920 (${name})`).toBeGreaterThan(1152 + 100);
    spans[name] = { start, end: reach };
    // The measure sits in the middle of main, so the spare width is shared by both sides instead of piling up as one
    // void on the right. Every household page starts at that one left edge and draws out to exactly the 72rem
    // measure: none stretches across the canvas, none ends at its own cap. Account keeps its 48rem (768 px) form
    // measure inside the centred column.
    const inset = Math.round((available - 1152) / 2);
    expected[name] = { start: inset, end: inset + (name === 'Account' ? 768 : 1152) };
  }
  console.info(`visual-content-measure-1920 ${JSON.stringify(spans)}`);
  expect(spans).toEqual(expected);
  for (const [base, path] of [['wide-home', '/'], ['wide-people', '/people'], ['wide-account', '/account']] as const) {
    await page.goto(path);
    await pages.find(([, candidate]) => candidate === path)![2]();
    await capture(page, `${base}-1920`, 1920, 1080, false);
  }
  await context.close();
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

// A Czech person's browser lays out the native date field day-first; an English one would show mm/dd/yyyy here.
test('Czech Home, add form and detail', async () => {
  const czechBrowser = await launchCzechBrowser();
  try {
    const { context, page } = await authenticated(czechBrowser);
    await page.goto('/'); await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs')); await page.reload();
    await captureScreen(page, 'home-cs', true);
    await openAdd(page, cs['items.add']);
    await captureScreen(page, 'add-cs', false);
    expect(await assertDateOrderFollowsLanguage(page, 'add-cs'), 'Czech add form shows its date field').toBeGreaterThanOrEqual(1);
    // Cancel lives inside the form; the dialog's own close is a separate icon in the header, so no `.first()` is needed.
    await page.getByRole('dialog').getByRole('form').getByRole('button', { name: cs['items.cancel'], exact: true }).click();
    await page.goto(`/items/${requiredFixture().items[0].id}`);
    await captureScreen(page, 'detail-cs', true);
    await context.close();
  } finally {
    await czechBrowser.close();
  }
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
          ...PASSWORD_RESET_DATES,
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

// Appended last so every earlier baseline keeps its data: the archived set is the ten reference items plus the
// archived detail item, and the paged-history item is created only here.
test('Archive and paged item history', async ({ browser }) => {
  test.setTimeout(120_000);
  const { context, page } = await authenticated(browser);
  const summary = await api(page, 'get', `${requiredFixture().apiRoot}/items/summary`);
  expect(summary.status()).toBe(200);
  const archivedTotal = (await summary.json() as { archived: number }).archived;
  expect(archivedTotal, 'reference items plus the archived detail item').toBe(REFERENCE_TITLES.length + 1);

  // Desktop: Archive is its own rail entry and is the current page; All items is not.
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/');
  const nav = page.getByRole('navigation', { name: en['nav.label'] });
  await nav.getByRole('link', { name: en['nav.archive'], exact: true }).click();
  await expect(page).toHaveURL(/\/items\?status=archived$/);
  await assertArchiveSettled(page, 'en', archivedTotal);
  await expect(nav.getByRole('link', { name: en['nav.archive'], exact: true })).toHaveAttribute('aria-current', 'page');
  await expect(nav.getByRole('link', { name: en['home.allItems'], exact: true })).not.toHaveAttribute('aria-current', 'page');
  for (const theme of ['light', 'dark'] as const) {
    await page.evaluate((value) => localStorage.setItem('tendo.theme', value), theme);
    for (const width of [390, 1440]) {
      await page.setViewportSize({ width, height: 900 });
      await page.reload();
      if (theme === 'dark') await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
      await assertArchiveSettled(page, 'en', archivedTotal);
      await capture(page, `archive-${theme}-${width}`, width, 900, false);
    }
  }
  await page.evaluate(() => localStorage.setItem('tendo.theme', 'light'));
  // Phone: Archive lives in the More sheet and is marked as the current page there.
  await page.setViewportSize({ width: 390, height: 900 });
  await page.reload();
  await assertArchiveSettled(page, 'en', archivedTotal);
  await openMoreSheet(page, 'en');
  await capture(page, 'archive-more-390', 390, 900, false);
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
  await page.reload();
  await assertArchiveSettled(page, 'cs', archivedTotal);
  await capture(page, 'archive-cs-390', 390, 900, false);
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'en'));

  // History: 25 receipts make a full first page of 20 and a second page of 5, stated in the History pages navigation.
  const item = await seedPagedHistory(page, requiredFixture());
  await page.goto(`/items/${item.id}`);
  await expect(page.getByRole('heading', { level: 1, name: item.title })).toBeVisible();
  for (const width of [390, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await page.reload();
    const firstPage = await assertHistoryPage(page, 'en', 1);
    await capture(page, `history-page-1-${width}`, width, 900, true);
    await firstPage.older.click();
    await assertHistoryPage(page, 'en', 2);
    await capture(page, `history-page-2-${width}`, width, 900, true);
  }
  await page.setViewportSize({ width: 390, height: 900 });
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
  await page.reload();
  await (await assertHistoryPage(page, 'cs', 1)).older.click();
  await assertHistoryPage(page, 'cs', 2);
  await capture(page, 'history-cs-page-2-390', 390, 900, true);
  await page.evaluate(() => { localStorage.setItem('tendo.locale', 'en'); localStorage.setItem('tendo.theme', 'dark'); });
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await assertHistoryPage(page, 'en', 1);
  await capture(page, 'history-dark-page-1-390', 390, 900, true);
  await page.evaluate(() => localStorage.setItem('tendo.theme', 'light'));
  await context.close();
});

// The one connected app this test creates is disconnected again before it ends, so no baseline sees it.
test('MCP consent and Connected apps', async ({ browser }) => {
  test.setTimeout(120_000);
  const clientName = 'Notes Assistant';
  const redirect = 'http://127.0.0.1:43123/callback';
  const { context, page } = await authenticated(browser);
  await pinMcpRequestExpiry(context);
  const registered = await api(page, 'post', '/oauth/register', { redirect_uris: [redirect], client_name: clientName });
  expect(registered.status(), 'register the visual client').toBe(201);
  const clientId = (await registered.json() as { client_id: string }).client_id;
  const verifier = randomBytes(32).toString('base64url');
  const query = new URLSearchParams({
    response_type: 'code', client_id: clientId, redirect_uri: redirect,
    code_challenge: createHash('sha256').update(verifier).digest('base64url'), code_challenge_method: 'S256',
    state: randomBytes(12).toString('base64url'), resource: `${BASE_URL}/mcp`,
  });

  // One request serves every consent capture. The page clock is paused before it loads, so Allow stays in its
  // waiting state until the clock is moved on; the request's expiry is pinned like the other server timestamps.
  await page.clock.pauseAt(new Date('2026-05-14T08:01:00Z'));
  const authorized = page.waitForResponse((response) => new URL(response.url()).pathname === '/oauth/authorize');
  await page.goto(`/oauth/authorize?${query}`);
  const authorizeResponse = await authorized;
  expect(authorizeResponse.status(), 'authorize redirects to the consent page').toBe(303);
  const location = authorizeResponse.headers()['location'] ?? '';
  expect(location).toMatch(/^\/connect#request=[A-Za-z0-9_-]{43}$/);
  const handle = location.slice('/connect#request='.length);
  const allow = (catalog: Record<keyof typeof en, string>) => page.getByRole('main').getByRole('button', { name: catalog['connect.allow'], exact: true });
  const settled = async (catalog: Record<keyof typeof en, string>) => {
    await expect(page.getByRole('heading', { level: 1, name: catalog['connect.heading'].replace('{client}', clientName) })).toBeVisible();
    await expect(page.getByRole('main').getByText(catalog['connect.allowHint'], { exact: true })).toBeVisible();
    await expect(allow(catalog)).toBeDisabled();
  };
  await settled(en);
  await captureScreen(page, 'consent', true);
  await page.clock.runFor(1_000);
  await expect(allow(en)).toBeEnabled();
  await expect(page.getByRole('main').getByText(en['connect.allowHint'], { exact: true })).toHaveCount(0);
  await capture(page, 'consent-allow-390', 390, 900, true);
  // The same request after a reload: the handle survives in session storage, and Allow waits again.
  await page.evaluate(() => localStorage.setItem('tendo.theme', 'dark'));
  await page.reload();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await settled(en);
  await captureScreen(page, 'consent-dark', true);
  await page.evaluate(() => { localStorage.setItem('tendo.theme', 'light'); localStorage.setItem('tendo.locale', 'cs'); });
  await page.setViewportSize({ width: 390, height: 900 });
  await page.reload();
  await settled(cs);
  await capture(page, 'consent-cs-390', 390, 900, true);
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'en'));
  await page.clock.resume();

  // A carried authorization error is its own page: no request, just the reason and the way home.
  const problem = await context.newPage();
  await problem.goto('/connect#error=invalid_client');
  await expect(problem.getByRole('heading', { level: 1, name: en['connect.problem.title'] })).toBeVisible();
  await expect(problem.getByRole('main').getByText(en['connect.problem.invalid_client'], { exact: true })).toBeVisible();
  await captureScreen(problem, 'consent-error', true);
  await problem.close();

  // The connected app comes from approving that same request through the API and exchanging its code.
  const decided = await api(page, 'post', `/api/v1/mcp/authorization-requests/${handle}/decision`, { decision: 'approve' });
  expect(decided.status(), 'approve the visual request').toBe(200);
  const code = new URL((await decided.json() as { redirectTo: string }).redirectTo).searchParams.get('code') ?? '';
  const exchanged = await page.request.post('/oauth/token', { form: {
    grant_type: 'authorization_code', code, redirect_uri: redirect, client_id: clientId, code_verifier: verifier,
  } });
  expect(exchanged.status(), 'exchange the visual code').toBe(200);
  const grants = await api(page, 'get', '/api/v1/mcp/grants');
  expect(grants.status()).toBe(200);
  const [grant] = (await grants.json() as { items: Array<{ id: string; clientName: string }> }).items;
  expect(grant?.clientName).toBe(clientName);
  await context.close();

  const apps = await authenticated(browser);
  await apps.page.goto('/account');
  await assertAccountSettled(apps.page, 'en', 'en');
  const row = apps.page.locator('main li[data-grant-id]');
  await expect(row).toHaveCount(1);
  await expect(row).toContainText(clientName);
  await expect(row).toContainText(en['connectedApps.connected'].replace('{date}', 'May 14, 2026'));
  await expect(row).toContainText(en['connectedApps.expires'].replace('{date}', 'Aug 12, 2026'));
  await captureScreen(apps.page, 'account-apps', true);
  await apps.page.setViewportSize({ width: 390, height: 900 });
  await apps.page.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
  await apps.page.reload();
  await assertAccountSettled(apps.page, 'cs', 'cs');
  await expect(row).toHaveCount(1);
  await capture(apps.page, 'account-apps-cs-390', 390, 900, true);
  // Dark is captured full page in the same context: the section sits below the fold of a 900 px viewport.
  await apps.page.evaluate(() => { localStorage.setItem('tendo.locale', 'en'); localStorage.setItem('tendo.theme', 'dark'); });
  for (const width of [390, 1440]) {
    await apps.page.setViewportSize({ width, height: 900 });
    await apps.page.reload();
    await expect(apps.page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await expect(apps.page.getByRole('heading', { level: 1, name: en['account.title'] })).toBeVisible();
    await expect(row).toHaveCount(1);
    await capture(apps.page, `account-apps-dark-${width}`, width, 900, true);
  }
  await apps.page.evaluate(() => localStorage.setItem('tendo.theme', 'light'));
  await apps.context.close();

  const cleanup = await browser.newContext({ ...CONTEXT, storageState: statePath });
  const removed = await cleanup.request.delete(`/api/v1/mcp/grants/${grant!.id}`, { headers: { Origin: BASE_URL! } });
  expect(removed.status(), 'disconnect the visual app').toBe(204);
  await cleanup.close();
});

// Appended after the consent group: the owner's own AI app settings narrow the consent, then block it, and are put
// back before the test ends. The requests are left pending and expire on their own; no app is ever connected.
test('MCP consent with less allowed and blocked', async ({ browser }) => {
  test.setTimeout(90_000);
  const clientName = 'Notes Assistant';
  const redirect = 'http://127.0.0.1:43123/callback';
  const { context, page } = await authenticated(browser);
  await pinMcpRequestExpiry(context);
  await page.clock.pauseAt(new Date('2026-05-14T08:01:00Z'));
  try {
    // Read only: the app asks for read and change, so the card lists what it gets and, quieter, what it does not.
    await setOwnAiApps(page, true, ['tendo:read']);
    await openConsent(page, clientName, redirect);
    await expect(page.getByRole('heading', { level: 1, name: en['connect.heading'].replace('{client}', clientName) })).toBeVisible();
    const granted = page.getByRole('list', { name: en['connect.scopes.intro'], exact: true }).getByRole('listitem');
    await expect(granted).toHaveText([en['connect.scope.read']]);
    const withheld = page.getByRole('list', { name: en['connect.scopes.withheld'], exact: true }).getByRole('listitem');
    await expect(withheld).toHaveText([en['connect.scope.write']]);
    await expect(page.getByRole('main').getByRole('button', { name: en['connect.allow'], exact: true })).toBeDisabled();
    await captureScreen(page, 'consent-narrowed', true);

    // Switched off: nothing the app asks for is allowed, so the card offers only the way back to the app.
    await setOwnAiApps(page, false, ['tendo:read', 'tendo:write']);
    await openConsent(page, clientName, redirect);
    const blocked = (catalog: Record<keyof typeof en, string>) => page.getByRole('heading', {
      level: 1, name: catalog['connect.blocked.heading'].replace('{client}', clientName),
    });
    await expect(blocked(en)).toBeVisible();
    await expect(page.getByRole('main').getByRole('button', { name: en['connect.back'], exact: true })).toBeEnabled();
    await expect(page.getByRole('main').getByRole('button', { name: en['connect.allow'], exact: true })).toHaveCount(0);
    await captureScreen(page, 'consent-blocked', true);
    await page.setViewportSize({ width: 390, height: 900 });
    await page.evaluate(() => localStorage.setItem('tendo.theme', 'dark'));
    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await expect(blocked(en)).toBeVisible();
    await capture(page, 'consent-blocked-dark-390', 390, 900, true);
    await page.evaluate(() => { localStorage.setItem('tendo.theme', 'light'); localStorage.setItem('tendo.locale', 'cs'); });
    await page.reload();
    await expect(blocked(cs)).toBeVisible();
    await capture(page, 'consent-blocked-cs-390', 390, 900, true);
    await page.evaluate(() => localStorage.setItem('tendo.locale', 'en'));
  } finally {
    try {
      await setOwnAiApps(page, true, ['tendo:read', 'tendo:write']);
    } finally {
      await context.close();
    }
  }
});
