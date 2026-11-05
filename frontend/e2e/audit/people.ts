import { expect, type Browser, type Page } from '@playwright/test';
import { join } from 'node:path';
import { en } from '../../src/i18n/en';
import { cs } from '../../src/i18n/cs';
import type { MessageKey } from '../../src/i18n';

/**
 * V3-11 People and things evidence: viewport captures (the fixed phone bar is shown where users see it),
 * empty/populated/dialog/conflict/archive/restore/error/loading states, long Czech names, forced-dark readiness
 * and a 1,000+ subject walk with bounded replacement pages.
 */
type Capture = (page: Page, path: string, fullPage?: boolean) => Promise<void>;
type Api = (page: Page, method: 'get' | 'post' | 'patch', path: string, data?: unknown, headers?: Record<string, string>) => ReturnType<Page['request']['get']>;
type Messages = Record<MessageKey, string>;

const WIDTHS = [320, 390, 768, 1024, 1440] as const;
const PAGE_SIZE = 50;
export const LONG_PERSON = 'Marie Nejneobhospodařovávatelnější-Dvořáková ze Šťáhlav';
export const LONG_PLACE = 'Chalupa po prababičce v Krkonoších nad Špindlerovým Mlýnem, včetně kůlny a domku';

const heightFor = (width: number) => (width <= 320 ? 640 : width < 768 ? 844 : width < 1440 ? 1024 : 900);
const rows = (page: Page) => page.getByRole('main').locator('li:has([data-subject-name])');
const key = () => crypto.randomUUID().replaceAll('-', '');

async function openPeople(page: Page, m: Messages, width: number) {
  await page.setViewportSize({ width, height: heightFor(width) });
  await page.goto('/people');
  await expect(page.getByRole('heading', { level: 1, name: m['subjects.title'] })).toBeVisible();
  await expect(page.getByText(m['subjects.loading'], { exact: true })).toHaveCount(0);
}

async function czechPage(browser: Browser, page: Page, width: number) {
  const context = await browser.newContext({ locale: 'cs-CZ', reducedMotion: 'reduce', viewport: { width, height: heightFor(width) } });
  await context.addCookies(await page.context().cookies());
  const csPage = await context.newPage();
  await csPage.goto('/'); await csPage.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
  return { context, csPage };
}

async function forcedDark(page: Page, capture: Capture, path: string) {
  await page.locator('html').evaluate((html) => html.setAttribute('data-theme', 'dark'));
  await capture(page, path, false);
  await page.locator('html').evaluate((html) => html.removeAttribute('data-theme'));
}

/** Content must clear the fixed phone bar once scrolled to the end (full-page images stitch the bar mid-page). */
async function assertEndClearsBar(page: Page, m: Messages, label: string) {
  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  const toggle = page.getByRole('main').getByRole('button', { name: m['subjects.showArchived'], exact: true });
  await expect(toggle).toBeVisible();
  const clearance = await toggle.evaluate((element, navLabel) => {
    const bar = document.querySelector(`nav[aria-label="${navLabel}"]`)!;
    if (getComputedStyle(bar).position !== 'fixed') return null;
    return bar.getBoundingClientRect().top - element.getBoundingClientRect().bottom;
  }, m['nav.label']);
  if (clearance !== null) expect(clearance, `${label}: Show archived clears the fixed bar at scroll end`).toBeGreaterThanOrEqual(0);
  await page.evaluate(() => window.scrollTo(0, 0));
  return clearance;
}

export async function capturePeopleEmpty(page: Page, browser: Browser, dir: string, capture: Capture) {
  for (const width of [390, 1440]) {
    await openPeople(page, en, width);
    await expect(page.getByRole('main').getByRole('heading', { name: en['subjects.empty.title'] })).toBeVisible();
    await capture(page, join(dir, `audit-people-empty-en-${width}.png`), false);
  }
  const { context, csPage } = await czechPage(browser, page, 390);
  await openPeople(csPage, cs, 390);
  await expect(csPage.getByRole('main').getByRole('heading', { name: cs['subjects.empty.title'] })).toBeVisible();
  await capture(csPage, join(dir, 'audit-people-empty-cs-390.png'), false);
  await context.close();
}

export async function seedPeopleExtras(page: Page, api: Api, root: string, linkedUserId: string) {
  for (const [name, type, extra] of [[LONG_PERSON, 'person', { linkedUserId }], [LONG_PLACE, 'home', {}]] as const) {
    expect([...name].length, `${name} fits the 100 code point limit`).toBeLessThanOrEqual(100);
    const response = await api(page, 'post', `${root}/subjects`, { name, type, ...extra }, { 'Idempotency-Key': key() });
    expect(response.status(), `create long-name subject ${name}`).toBe(201);
  }
}

async function editDialog(page: Page, m: Messages, name: string) {
  await page.getByRole('main').getByRole('button', { name: m['subjects.edit.label'].replace('{name}', name), exact: true }).click();
  const form = page.getByRole('form', { name: m['subjects.edit.label'].replace('{name}', name) });
  await expect(form.getByLabel(m['subjects.name'])).toBeFocused();
  return form;
}

async function conflict(page: Page, api: Api, root: string, width: number, dir: string, capture: Capture, from: string, server: string) {
  const form = await editDialog(page, en, from);
  const list = await api(page, 'get', `${root}/subjects?archived=false&limit=100`);
  expect(list.status()).toBe(200);
  const id = (await list.json() as { items: Array<{ id: string; name: string }> }).items.find((subject) => subject.name === from)?.id;
  expect(id, `${from} exists for the conflict capture`).toBeTruthy();
  const current = await api(page, 'get', `${root}/subjects/${id}`);
  expect(current.status()).toBe(200);
  const renamed = await api(page, 'patch', `${root}/subjects/${id}`, { name: server }, { 'If-Match': current.headers()['etag']! });
  expect(renamed.status(), 'server-side rename for the conflict capture').toBe(200);
  await form.getByLabel(en['subjects.name']).fill('Petr Novák');
  await form.getByRole('button', { name: en['subjects.save'], exact: true }).click();
  const latest = page.getByRole('form', { name: en['subjects.edit.label'].replace('{name}', server) });
  await expect(latest.getByText(en['subjects.notice.conflict'], { exact: true })).toBeVisible();
  await expect(latest.getByLabel(en['subjects.name'])).toHaveValue(server);
  await capture(page, join(dir, `audit-people-conflict-en-${width}.png`), false);
  await latest.getByRole('button', { name: en['subjects.cancel'], exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
}

export async function capturePeopleStates(page: Page, browser: Browser, dir: string, capture: Capture, api: Api, root: string) {
  const evidence: Record<string, unknown> = {};
  for (const width of WIDTHS) {
    await openPeople(page, en, width);
    await expect(page.getByRole('main').getByText(LONG_PERSON, { exact: true })).toBeVisible();
    await expect(page.getByRole('main').getByText(en['subjects.link.row'].replace('{login}', 'audit_member'), { exact: true })).toBeVisible();
    await capture(page, join(dir, `audit-people-en-${width}.png`), false);
    evidence[`endClearance-en-${width}`] = await assertEndClearsBar(page, en, `en ${width}px`);
    const { context, csPage } = await czechPage(browser, page, width);
    await openPeople(csPage, cs, width);
    await expect(csPage.getByRole('main').getByText(LONG_PLACE, { exact: true })).toBeVisible();
    await capture(csPage, join(dir, `audit-people-cs-${width}.png`), false);
    if (width === 320) evidence['endClearance-cs-320'] = await assertEndClearsBar(csPage, cs, 'cs 320px');
    await context.close();
  }

  for (const width of [390, 1440]) {
    await openPeople(page, en, width);
    await page.getByRole('main').getByRole('button', { name: en['subjects.add'], exact: true }).click();
    const add = page.getByRole('form', { name: en['subjects.add.formLabel'] });
    await expect(add.getByLabel(en['subjects.name'])).toBeFocused();
    await capture(page, join(dir, `audit-people-add-en-${width}.png`), false);
    await add.getByRole('button', { name: en['subjects.add.submit'], exact: true }).click();
    await expect(add.getByRole('alert').first()).toBeVisible();
    await capture(page, join(dir, `audit-people-add-validation-en-${width}.png`), false);
    await add.getByRole('button', { name: en['subjects.cancel'], exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);

    await editDialog(page, en, LONG_PERSON);
    await capture(page, join(dir, `audit-people-edit-linked-en-${width}.png`), false);
    if (width === 390) await forcedDark(page, capture, join(dir, 'audit-people-edit-forced-dark-en-390.png'));
    await page.getByRole('form', { name: en['subjects.edit.label'].replace('{name}', LONG_PERSON) })
      .getByRole('button', { name: en['subjects.cancel'], exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);

    if (width === 390) await conflict(page, api, root, width, dir, capture, 'Petr', 'Petr Svoboda');
    else await conflict(page, api, root, width, dir, capture, 'Petr Svoboda', 'Petr');

    await editDialog(page, en, 'Kolo');
    await page.getByRole('form', { name: en['subjects.edit.label'].replace('{name}', 'Kolo') })
      .getByRole('button', { name: en['subjects.archive'], exact: true }).click();
    const archived = page.getByRole('status').filter({ hasText: en['subjects.notice.archived'].replace('{name}', 'Kolo') });
    await expect(archived).toBeVisible();
    await expect(archived.getByRole('button', { name: en['subjects.undo'], exact: true })).toBeVisible();
    await capture(page, join(dir, `audit-people-archived-undo-en-${width}.png`), false);
    await archived.getByRole('button', { name: en['subjects.undo'], exact: true }).click();
    // Undo clears the notice and returns focus to the restored row's Edit (existing V2.1 behavior).
    await expect(page.getByRole('main').getByRole('button', { name: en['subjects.edit.label'].replace('{name}', 'Kolo'), exact: true })).toBeFocused();
    await expect(rows(page).filter({ hasText: 'Kolo' })).toHaveCount(1);

    await page.getByRole('main').getByRole('button', { name: en['subjects.showArchived'], exact: true }).click();
    await expect(page.getByRole('main').getByRole('heading', { name: en['subjects.archived.title'] })).toBeVisible();
    await expect(page.getByRole('main').getByRole('button', { name: en['subjects.restore.label'].replace('{name}', 'Rex'), exact: true })).toBeVisible();
    await capture(page, join(dir, `audit-people-archived-view-en-${width}.png`), false);
    if (width === 1440) await forcedDark(page, capture, join(dir, 'audit-people-archived-view-forced-dark-en-1440.png'));

    await openPeople(page, en, width);
    await forcedDark(page, capture, join(dir, `audit-people-forced-dark-en-${width}.png`));
  }

  const { context, csPage } = await czechPage(browser, page, 390);
  await openPeople(csPage, cs, 390);
  await editDialog(csPage, cs, LONG_PLACE);
  await capture(csPage, join(dir, 'audit-people-edit-cs-390.png'), false);
  await csPage.getByRole('form', { name: cs['subjects.edit.label'].replace('{name}', LONG_PLACE) })
    .getByRole('button', { name: cs['subjects.cancel'], exact: true }).click();
  await csPage.getByRole('main').getByRole('button', { name: cs['subjects.showArchived'], exact: true }).click();
  await expect(csPage.getByRole('main').getByRole('heading', { name: cs['subjects.archived.title'] })).toBeVisible();
  await capture(csPage, join(dir, 'audit-people-archived-view-cs-390.png'), false);
  await context.close();

  const isList = (url: URL) => url.pathname.endsWith('/subjects');
  for (const width of [390, 1440]) {
    await page.route((url) => isList(url), (route) => route.request().method() === 'GET'
      ? route.fulfill({ status: 503, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Unavailable', status: 503, code: 'unavailable' }) })
      : route.fallback());
    await page.setViewportSize({ width, height: heightFor(width) });
    await page.goto('/people');
    await expect(page.getByRole('main').getByRole('button', { name: en['app.error.retry'], exact: true })).toBeVisible();
    await capture(page, join(dir, `audit-people-error-en-${width}.png`), false);
    await page.unrouteAll({ behavior: 'wait' });
  }

  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  await page.route((url) => isList(url), async (route) => { await held; await route.fallback(); });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/people');
  await expect(page.getByRole('main').getByText(en['subjects.loading'], { exact: true })).toBeVisible();
  await capture(page, join(dir, 'audit-people-loading-en-390.png'), false);
  release();
  await expect(rows(page).first()).toBeVisible();
  await page.unrouteAll({ behavior: 'wait' });
  return evidence;
}

export async function capturePeopleScale(page: Page, dir: string, capture: Capture, api: Api, root: string) {
  const target = 1000;
  const pending: Array<Promise<void>> = [];
  for (let n = 1; n <= target; n++) {
    const subject = { name: `Audit person ${String(n).padStart(4, '0')}`, type: (['person', 'home', 'vehicle', 'pet', 'custom'] as const)[n % 5] };
    pending.push((async () => {
      const response = await api(page, 'post', `${root}/subjects`, subject, { 'Idempotency-Key': key() });
      expect(response.status(), `create scale subject ${subject.name}`).toBe(201);
    })());
    if (pending.length >= 8) await Promise.all(pending.splice(0));
  }
  await Promise.all(pending);
  const expectedNames: string[] = [];
  let cursor: string | null = null;
  do {
    const response = await api(page, 'get', `${root}/subjects?archived=false&limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`);
    expect(response.status()).toBe(200);
    const data = await response.json() as { items: Array<{ name: string }>; nextCursor: string | null };
    expectedNames.push(...data.items.map((subject) => subject.name)); cursor = data.nextCursor;
  } while (cursor);
  const activeTotal = expectedNames.length;
  expect(activeTotal, 'active subjects exceed 1,000').toBeGreaterThan(1000);
  expect(new Set(expectedNames).size, 'fixture names identify active subjects uniquely').toBe(activeTotal);
  const maxPages = Math.ceil(activeTotal / PAGE_SIZE);

  const evidence: Record<string, unknown> = { activeTotal };
  for (const width of [390, 1440]) {
    await openPeople(page, en, width);
    await expect(rows(page)).toHaveCount(PAGE_SIZE);
    const main = page.getByRole('main');
    await main.getByRole('button', { name: en['subjects.page.next'], exact: true }).click();
    await expect(main.getByText(en['subjects.page.number'].replace('{n}', '2'), { exact: true })).toBeVisible();
    await expect(rows(page)).toHaveCount(PAGE_SIZE);
    await capture(page, join(dir, `audit-people-1000-page2-en-${width}.png`), false);
    if (width === 1440) {
      const visited: string[] = []; let pages = 0; let maxRows = 0; let maxDom = 0;
      await main.getByRole('button', { name: en['subjects.page.first'], exact: true }).click();
      await expect(main.getByRole('button', { name: en['subjects.page.first'], exact: true })).toHaveCount(0);
      // Observe only the browser's own list requests during the walk (fixture API calls use page.request, not the page).
      const listRequests: URL[] = [];
      const onRequest = (request: import('@playwright/test').Request) => {
        const url = new URL(request.url());
        if (request.method() === 'GET' && url.pathname === `${root}/subjects`) listRequests.push(url);
      };
      page.on('request', onRequest);
      try {
        for (;;) {
          pages++;
          expect(pages, 'People paging terminates within the expected number of pages').toBeLessThanOrEqual(maxPages);
          await expect(main.getByText(en['subjects.page.number'].replace('{n}', String(pages)), { exact: true })).toBeVisible();
          const names = await rows(page).locator('[data-subject-name]').allTextContents();
          maxRows = Math.max(maxRows, names.length); visited.push(...names);
          maxDom = Math.max(maxDom, await page.evaluate(() => document.getElementsByTagName('*').length));
          const next = main.getByRole('button', { name: en['subjects.page.next'], exact: true });
          if (!(await next.count())) break;
          const before = names[0];
          await next.click();
          await expect(rows(page).locator('[data-subject-name]').first()).not.toHaveText(before!);
        }
      } finally { page.off('request', onRequest); }
      expect(maxRows, 'each People page renders at most one page of rows').toBeLessThanOrEqual(PAGE_SIZE);
      expect(visited.length, 'rendered row occurrences across the walk equal the active total (no repeats)').toBe(activeTotal);
      expect([...visited].sort(), 'walking every page reaches every active subject exactly once').toEqual([...expectedNames].sort());
      expect(listRequests.length, 'one bounded list request per Next click, no eager page walking').toBe(pages - 1);
      expect(listRequests.every((url) => Number(url.searchParams.get('limit')) === PAGE_SIZE && url.searchParams.get('cursor')),
        'every walk request is a cursor page of PAGE_SIZE').toBe(true);
      await capture(page, join(dir, 'audit-people-1000-last-page-en-1440.png'), false);
      Object.assign(evidence, { pagesWalked: pages, maxRowsPerPage: maxRows, maxDomElements: maxDom, walkListRequests: listRequests.length });
    }
  }
  return evidence;
}
