import { expect, test, type Locator, type Page, type Response } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { en } from '../src/i18n/en';
import { cs } from '../src/i18n/cs';
import { HOME_GROUP_ROW_CAP } from '../src/features/items/homeGroups';
import { measureCapture, type CaptureMetrics } from './audit/metrics';
import { JourneyRecorder, runFirstRunSetup, runHouseholdJourneys } from './audit/journeys';

const baseURL = process.env.E2E_BASE_URL!;
const dir = resolve(process.env.E2E_AUDIT_DIR!);
const NOW = '2026-05-14';
test.describe.configure({ mode: 'serial' });
test.use({ reducedMotion: 'reduce' });
test.setTimeout(1_800_000);

const routeOverflow: Record<string, boolean> = {};
const routeOverflowElements: Record<string, Array<{ tag: string; text: string; right: number }>> = {};
const routeMetrics: Record<string, CaptureMetrics> = {};
const journeyMetrics: Record<string, import('./audit/journeys').JourneyResult> = {};
const shownInviteTokens: string[] = [];

async function capture(page: Page, path: string, fullPage = true, mask: Locator[] = []) {
  const width = page.viewportSize()?.width;
  if (!width) throw new Error(`Capture requires a viewport: ${path}`);
  const height = page.viewportSize()?.height ?? 900;
  await page.setViewportSize({ width, height });
  expect(await page.evaluate(() => window.innerWidth), `viewport width for ${path}`).toBe(width);
  await expect(page.getByRole('heading', { level: 1 }).first()).toBeVisible();
  const file = path.split('/').pop()!;
  const overflow = await page.evaluate(() => {
    const clientWidth = document.documentElement.clientWidth;
    return {
      overflow: document.documentElement.scrollWidth > clientWidth,
      offenders: [...document.querySelectorAll<HTMLElement>('*')].filter(element => element.getBoundingClientRect().right > clientWidth + 0.5).slice(0, 10).map(element => ({ tag: element.tagName.toLowerCase(), text: (element.innerText || element.textContent || '').trim().slice(0, 80), right: element.getBoundingClientRect().right })),
    };
  });
  routeOverflow[file] = overflow.overflow;
  if (overflow.overflow) routeOverflowElements[file] = overflow.offenders;
  if (file.startsWith('audit-all-items-page2-')) {
    const pager = page.getByRole('navigation', { name: en['allItems.pages'] });
    await expect(pager).toBeInViewport();
    await expect(pager.getByRole('button', { name: en['allItems.previous'], exact: true })).toBeInViewport();
    await expect(pager.getByRole('button', { name: en['allItems.next'], exact: true })).toBeInViewport();
    await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
    await page.mouse.move(0, 0);
    await page.screenshot({ path, fullPage, mask });
    return;
  }
  const resultSelector = file.startsWith('home-30-') ? 'main li[data-item-id] a' : file.startsWith('audit-all-items-') ? 'main li[data-item-id] a' : undefined;
  const captureFocus = file === 'audit-all-items-cs-320-active-filter.png'
    ? page.getByRole('heading', { level: 1, name: cs['allItems.title'] })
    : undefined;
  if (captureFocus) await captureFocus.focus();
  routeMetrics[file] = await measureCapture(page, resultSelector);
  if (captureFocus) await captureFocus.focus();
  // measureCapture tabs through the overview; capture it in its pristine, unscrolled state.
  const overview = page.locator(`main nav[aria-label="${en['items.group.overview']}"], main nav[aria-label="${cs['items.group.overview']}"]`);
  if (await overview.count()) await overview.evaluate((nav) => { nav.scrollLeft = 0; });
  // Measurement tabs through page content; restore viewport captures to the intended position afterwards.
  if (file === 'audit-all-items-cs-320-active-filter.png') {
    await page.getByRole('heading', { level: 1, name: cs['allItems.title'] }).scrollIntoViewIfNeeded();
  } else {
    await page.evaluate(() => window.scrollTo(0, 0));
  }
  // Leave no measurement focus ring or pointer hover in the evidence image.
  await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
  await page.mouse.move(0, 0);
  await expect.poll(() => page.evaluate(() => {
    const focused = document.querySelector(':focus-visible');
    return focused ? `${focused.tagName} ${focused.textContent?.trim().slice(0, 40)} active=${document.activeElement?.tagName}` : null;
  }), `no focus ring remains in ${file}`).toBeNull();
  await page.screenshot({ path, fullPage, mask });
}

/**
 * V3-07 shell evidence: viewport (not full-page) captures, so the fixed phone bar appears where users see it.
 * Asserts the shell variant at each size; dark is forced on <html> for readiness only (V3-14 owns preference).
 */
async function captureShell(page: Page) {
  const shot = async (name: string) => {
    const size = page.viewportSize()!;
    expect(await page.evaluate(() => [window.innerWidth, window.innerHeight]), `viewport for ${name}`).toEqual([size.width, size.height]);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth), `${name} has no horizontal overflow`).toBe(true);
    await page.screenshot({ path: join(dir, name) });
  };
  const nav = page.getByRole('navigation', { name: en['nav.label'] });
  const evidence: Record<string, string> = {};
  for (const [width, height, expected] of [[320, 640, 'fixed'], [390, 844, 'fixed'], [768, 1024, 'fixed'], [640, 360, 'static'], [1024, 768, 'sticky'], [1440, 900, 'sticky']] as const) {
    await page.setViewportSize({ width, height });
    await page.goto('/');
    await expect(page.getByRole('heading', { level: 1, name: 'Audit household' })).toBeVisible();
    await expect(page.locator('main li[data-item-id]').first(), `loaded Home rows at ${width}x${height}`).toBeVisible();
    evidence[`${width}x${height}`] = await nav.evaluate((element) => getComputedStyle(element).position);
    expect(evidence[`${width}x${height}`], `household navigation presentation at ${width}x${height}`).toBe(expected);
    await shot(`shell-home-en-${width}x${height}.png`);
    if (width === 390 || width === 1440) {
      await page.locator('html').evaluate((html) => html.setAttribute('data-theme', 'dark'));
      await shot(`shell-home-forced-dark-en-${width}.png`);
      await page.locator('html').evaluate((html) => html.removeAttribute('data-theme'));
    }
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/members');
  await expect(page.getByRole('heading', { level: 1, name: en['members.title'] })).toBeVisible();
  await expect(page.getByText(en['members.loading'], { exact: true })).toHaveCount(0);
  await expect(page.getByRole('main').getByText(`audit_owner (${en['members.you']})`, { exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: en['members.invitations.title'] })).toBeVisible();
  await page.keyboard.press('Tab');
  await expect(page.getByRole('link', { name: en['app.skipToContent'] })).toBeFocused();
  await shot('shell-skip-link-en-390.png');
  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  const footerClear = await page.evaluate((label) => {
    const bar = document.querySelector(`nav[aria-label="${label}"]`)!.getBoundingClientRect();
    const language = document.querySelector('body footer [role="group"]')!.getBoundingClientRect();
    return language.bottom <= bar.top;
  }, en['nav.label']);
  expect(footerClear, 'scrolled to the end, the language switch sits fully above the fixed bar').toBe(true);
  await shot('shell-members-end-en-390.png');
  return evidence;
}

async function api(page: Page, method: 'get' | 'post' | 'patch', path: string, data?: unknown, headers: Record<string, string> = {}) {
  return page.request[method](path, { headers: { Origin: baseURL, ...headers }, ...(data === undefined ? {} : { data }) });
}
function key() { return crypto.randomUUID().replaceAll('-', ''); }
function rng(seed: number) { let s = seed >>> 0; return () => { s = (s * 1664525 + 1013904223) >>> 0; return s / 0x100000000; }; }

 test('capture staged UX and scale audit', async ({ page, browser }) => {
  mkdirSync(dir, { recursive: true });
  const metrics: { fixedBusinessDate: string; stages: Record<string, Record<string, unknown>>; seededStateCounts: Record<string, number>; listPageSizes: number[]; stageSeeding: Record<string, unknown>; routes?: Record<string, CaptureMetrics>; journeys?: Record<string, import('./audit/journeys').JourneyResult>; seedDurationMs?: number } = { fixedBusinessDate: NOW, stages: {}, seededStateCounts: {}, listPageSizes: [], stageSeeding: {} };
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: en['onboarding.title'] })).toBeVisible();
  for (const width of [320, 360, 390, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await capture(page, join(dir, `onboarding-en-${width}.png`));
  }
  for (const width of [390, 1440]) {
    const csSetupContext = await browser.newContext({ locale: 'cs-CZ', reducedMotion: 'reduce', viewport: { width, height: 900 } });
    const csSetupPage = await csSetupContext.newPage();
    await csSetupPage.goto('/'); await csSetupPage.evaluate(() => localStorage.setItem('tendo.locale', 'cs')); await csSetupPage.reload();
    await expect(csSetupPage.getByRole('heading', { level: 1, name: cs['onboarding.title'] })).toBeVisible();
    await capture(csSetupPage, join(dir, `onboarding-cs-${width}.png`));
    await csSetupContext.close();
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: en['onboarding.submit'] }).click();
  await expect(page.getByRole('alert').first()).toBeVisible();
  await capture(page, join(dir, 'onboarding-error-en-390.png'));
  await page.setViewportSize({ width: 390, height: 844 });
  const setupRecorder = new JourneyRecorder(); setupRecorder.start();
  journeyMetrics['J9 first-run setup 390x844'] = await runFirstRunSetup(page, process.env.E2E_SETUP_TOKEN!, process.env.E2E_OWNER_PASSWORD!, setupRecorder);
  await expect(page.getByRole('heading', { level: 1, name: 'Audit household' })).toBeVisible();
  for (const width of [320, 360, 390, 768, 1024, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await capture(page, join(dir, `home-after-setup-en-${width}.png`));
  }
  const session = await api(page, 'get', '/api/v1/session');
  expect(session.status()).toBe(200);
  const { defaultHouseholdId: householdId } = await session.json() as { defaultHouseholdId: string };
  const root = `/api/v1/households/${householdId}`;

  // Capture the household's genuine API-backed empty state before this audit seeds its large backlog.
  await page.goto('/items');
  await expect(page.getByRole('main').getByRole('heading', { name: en['allItems.none'] })).toBeVisible();
  for (const [width, height] of [[390, 844], [1440, 900]] as const) {
    await page.setViewportSize({ width, height });
    await capture(page, join(dir, `audit-all-items-empty-${width}.png`), false);
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: 'Audit household' })).toBeVisible();
  const invite = await api(page, 'post', `${root}/invitations`, {}, { 'Idempotency-Key': key() });
  expect(invite.status(), 'create member invitation').toBe(201);
  const invitation = await invite.json() as { token: string };
  const accept = await api(page, 'post', '/api/v1/auth/invitations/accept', { token: invitation.token, login: 'audit_member', password: 'audit-member-password-long-enough' });
  expect(accept.status(), 'accept member invitation').toBe(201);
  const invited = await accept.json() as { userId: string };
  const ownerUserId = (await session.json() as { userId: string }).userId;
  const auditInviteResponse = await api(page, 'post', `${root}/invitations`, {}, { 'Idempotency-Key': key() });
  expect(auditInviteResponse.status(), 'create audit acceptance invitation').toBe(201);
  const auditInvite = await auditInviteResponse.json() as { token: string };
  for (const width of [320, 360, 390, 768, 1024, 1440]) {
    const context = await browser.newContext({ reducedMotion: 'reduce', viewport: { width, height: 900 } });
    const signedOut = await context.newPage();
    await signedOut.goto('/');
    await expect(signedOut.getByRole('heading', { level: 1, name: en['login.title'] })).toBeVisible();
    await capture(signedOut, join(dir, `audit-login-en-${width}.png`));
    if (width === 390) {
      await signedOut.locator('html').evaluate((html) => html.setAttribute('data-theme', 'dark'));
      await capture(signedOut, join(dir, 'audit-login-dark-en-390.png'));
      await signedOut.locator('html').evaluate((html) => html.removeAttribute('data-theme'));
    }
    await signedOut.getByLabel(en['login.login'], { exact: true }).fill('audit_missing_user');
    await signedOut.getByLabel(en['login.password'], { exact: true }).fill('wrong-audit-password');
    await signedOut.getByRole('button', { name: en['login.submit'], exact: true }).click();
    await expect(signedOut.getByRole('alert')).toHaveText(en['login.error.invalid']);
    await capture(signedOut, join(dir, `audit-login-error-en-${width}.png`));
    if (width === 390 || width === 1440) {
      await signedOut.goto('/');
      await signedOut.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
      await signedOut.reload();
      await expect(signedOut.getByRole('heading', { level: 1, name: cs['login.title'] })).toBeVisible();
      await capture(signedOut, join(dir, `audit-login-cs-${width}.png`));
    }
    await signedOut.goto('/');
    await signedOut.evaluate(() => localStorage.setItem('tendo.locale', 'en'));
    await signedOut.reload();
    await signedOut.goto(`/invite#${auditInvite.token}`);
    await expect(signedOut.getByRole('heading', { level: 1, name: en['invite.title'] })).toBeVisible();
    await capture(signedOut, join(dir, `audit-invite-accept-en-${width}.png`));
    if (width === 390) {
      await signedOut.goto('/');
      await signedOut.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
      await signedOut.goto(`/invite#${auditInvite.token}`);
      await expect(signedOut.getByRole('heading', { level: 1, name: cs['invite.title'] })).toBeVisible();
      await capture(signedOut, join(dir, 'audit-invite-accept-cs-390.png'));
    }
    await context.close();
  }
  const subjects: string[] = [];
  const activeSubjects: string[] = [];
  for (const subject of [
    ...['Anna Nováková', 'Tomáš Dvořák', 'Šárka Černá', 'Jiří Čapek', 'Eliška', 'Petr'].map(name => ({ name, type: 'person' })),
    { name: 'Dům', type: 'home' }, { name: 'Škoda Octavia', type: 'vehicle' }, { name: 'Kolo', type: 'vehicle' },
    { name: 'Rex', type: 'pet' }, { name: 'Micka', type: 'pet' }, { name: 'Zahrada', type: 'custom' },
    { name: 'Chata', type: 'home' }, { name: 'Ford', type: 'vehicle' }, { name: 'Babička', type: 'person' },
  ]) {
    const response = await api(page, 'post', `${root}/subjects`, subject, { 'Idempotency-Key': key() });
    expect(response.status(), `create subject ${subject.name}`).toBe(201);
    const value = await response.json(); subjects.push(value.id);
    if (subjects.length % 5 !== 0) activeSubjects.push(value.id);
    if (subjects.length % 5 === 0) {
      const archive = await api(page, 'patch', `${root}/subjects/${value.id}`, { archived: true }, { 'If-Match': response.headers()['etag'] ?? '' });
      expect(archive.status(), `archive subject ${subject.name}`).toBe(200);
    }
  }
  const createdItems: Array<{ generatorIndex: number; id: string; title: string; recurrence: { mode: string } | null; archived: boolean; done: boolean; workflowState: string }> = [];
  async function verifyList(done: boolean, archived: boolean) {
    const all: Array<{ workflowState: string; done: boolean; archived: boolean }> = [];
    let cursor: string | undefined;
    do {
      const response = await api(page, 'get', `${root}/items?limit=100&done=${done}&archived=${archived}${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`);
      expect(response.status(), `verify full item list done=${done} archived=${archived}`).toBe(200);
      const data = await response.json() as { items: Array<{ workflowState: string; done: boolean; archived: boolean }>; nextCursor: string | null };
      all.push(...data.items);
      cursor = data.nextCursor ?? undefined;
      metrics.listPageSizes.push(data.items.length);
    } while (cursor);
    return all;
  }
  const random = rng(0x5eed1234);
  const stages = [ ['empty', 0], ['1', 1], ['30', 30], ['100', 100], ['500', 500], ['1200', 1200] ] as const;
  let created = 0;
  let previousTarget = 0;
  metrics.stageSeeding = {};
  for (const [stage, target] of stages) {
    const seedStart = Date.now();
    const pending: Array<Promise<void>> = [];
    while (created < target) {
      const n = created++;
      const recurrence = random() < .55 ? null : { intervalValue: 1 + Math.floor(random() * 3), intervalUnit: (['day', 'week', 'month', 'year'] as const)[Math.floor(random() * 4)], mode: random() < .5 ? 'fixed' : 'after_completion' };
      const title = n === 8 ? `Kontrola nejneobhospodařovávatelnějšího příslušenství české domácnosti ${'dlouhý'.repeat(18)}` : random() < .05 ? `Kontrola nejneobhospodařovávatelnějšího příslušenství domácnosti ${n} ${'dlouhý'.repeat(18)}` : `Audit task ${String(n + 1).padStart(4, '0')}`;
      const day = Math.floor(random() * 90) - 45;
      const d = new Date(`${NOW}T00:00:00Z`); d.setUTCDate(d.getUTCDate() + day);
      const historical = Boolean(recurrence && random() < .12);
      const item: Record<string, unknown> = { title, recurrence, subjectId: activeSubjects[Math.floor(random() * activeSubjects.length)], attentionOn: historical || random() < .1 ? null : d.toISOString().slice(0, 10), ...(n === 7 ? { notes: `První odstavec s českou diakritikou: žluťoučký kůň úpěl ďábelské ódy.

Druhý odstavec popisuje dlouhou poznámku pro domácnost a její postup.

Třetí odstavec obsahuje další důležité podrobnosti, které mají zůstat čitelné i na telefonu.` } : random() < .12 ? { notes: `Audit notes ${n}: ${'detail '.repeat(8)}` } : {}) };
      if (historical) item.historicalCompletedOn = '2025-12-01';
      const responsibleChoice = random();
      if (responsibleChoice < .34) item.responsibleUserId = ownerUserId;
      else if (responsibleChoice < .67) item.responsibleUserId = invited.userId;
      else item.responsibleUserId = null;
      pending.push((async () => {
        const response = await api(page, 'post', `${root}/items`, item, { 'Idempotency-Key': key() });
        expect(response.status(), `create item ${n}: ${await response.text()}`).toBe(201);
        const result = await response.json() as { id: string; title: string; recurrence: { mode: string } | null; done: boolean; archived: boolean; workflowState: string };
        createdItems.push({ generatorIndex: n, id: result.id, title: result.title, recurrence: result.recurrence, done: result.done, archived: result.archived, workflowState: result.workflowState });
      })());
      if (pending.length >= 8) { await Promise.all(pending.splice(0)); }
    }
    await Promise.all(pending);
    createdItems.sort((a, b) => a.generatorIndex - b.generatorIndex);
    const postCount = target - previousTarget;
    expect(createdItems.length, `cumulative item POST count at stage ${stage}`).toBe(target);
    (metrics.stageSeeding as Record<string, unknown>)[stage] = { postCount, cumulativeCreated: createdItems.length, durationMs: Date.now() - seedStart };
    const newlyCreated = createdItems.slice(previousTarget, target);
    const oneOffs = newlyCreated.filter(item => item.recurrence === null);
    for (const item of newlyCreated) {
      const index = item.generatorIndex;
      if (index % 12 === 0) {
        const current = await api(page, 'get', `${root}/items/${item.id}`);
        expect(current.status(), `read item before workflow update ${item.id}`).toBe(200);
        const update = await api(page, 'patch', `${root}/items/${item.id}`, { workflowState: 'in_progress' }, { 'If-Match': current.headers()['etag']! });
        expect(update.status(), `set item in_progress ${item.id}`).toBe(200);
      } else if (index % 20 === 1) {
        const current = await api(page, 'get', `${root}/items/${item.id}`);
        expect(current.status()).toBe(200);
        const update = await api(page, 'patch', `${root}/items/${item.id}`, { workflowState: 'waiting' }, { 'If-Match': current.headers()['etag']! });
        expect(update.status(), `set item waiting ${item.id}`).toBe(200);
      } else if (index % 20 === 2) {
        const current = await api(page, 'get', `${root}/items/${item.id}`);
        expect(current.status()).toBe(200);
        const update = await api(page, 'patch', `${root}/items/${item.id}`, { workflowState: 'paused' }, { 'If-Match': current.headers()['etag']! });
        expect(update.status(), `set item paused ${item.id}`).toBe(200);
      }
      if (index % 20 === 4) {
        const current = await api(page, 'get', `${root}/items/${item.id}`);
        expect(current.status()).toBe(200);
        const archive = await api(page, 'patch', `${root}/items/${item.id}`, { archived: true }, { 'If-Match': current.headers()['etag']! });
        expect(archive.status(), `archive item ${item.id}`).toBe(200);
      }
    }
    for (const item of oneOffs.filter((_, index) => index % 10 === 0)) {
      if (item.generatorIndex % 20 === 4) continue;
      // Keep generator item 0 active so the 1-item stage really shows one row on Home.
      if (item.generatorIndex === 0) continue;
      const current = await api(page, 'get', `${root}/items/${item.id}`);
      expect(current.status()).toBe(200);
      const completion = await api(page, 'post', `${root}/items/${item.id}/completions`, {}, { 'Idempotency-Key': key(), 'If-Match': current.headers()['etag']! });
      expect(completion.status(), `complete one-off item ${item.id}`).toBe(201);
    }
    previousTarget = target;
    if (stage === '500') {
      // V2 DoD scale item: at 500 items a phone user finds one item by search, manages it, and keeps context.
      let target: { id: string; title: string; etag: string } | undefined;
      for (const candidate of createdItems.filter(item => item.generatorIndex > 250)) {
        const current = await api(page, 'get', `${root}/items/${candidate.id}`);
        expect(current.status()).toBe(200);
        const detail = await current.json() as { title: string; done: boolean; archived: boolean; workflowState: string };
        if (!detail.done && !detail.archived && detail.workflowState === 'open' && /^Audit task \d{4}$/.test(detail.title)) { target = { id: candidate.id, title: detail.title, etag: current.headers()['etag']! }; break; }
      }
      expect(target, '500-stage open target item').toBeTruthy();
      const query = target!.title.toLowerCase();
      const accentQuery = 'nejneobhospodarovavatelnejsiho';
      const accentSummary = await api(page, 'get', `${root}/items/summary?q=${encodeURIComponent(accentQuery)}`);
      expect(accentSummary.status()).toBe(200);
      const accentCounts = await accentSummary.json() as { needsAttention: number; inProgress: number; waiting: number; upcoming: number; paused: number };
      const accentActive = accentCounts.needsAttention + accentCounts.inProgress + accentCounts.waiting + accentCounts.upcoming + accentCounts.paused;
      expect(accentActive, '500-stage accent-insensitive matches').toBeGreaterThan(0);
      const range = (from: number, to: number, total: number) => en['allItems.range'].replace('{from}', String(from)).replace('{to}', String(to)).replace('{total}', String(total));
      const apiCalls: string[] = [];
      const onApi = (req: import('@playwright/test').Request) => { const url = new URL(req.url()); if (url.pathname.startsWith('/api/')) apiCalls.push(`${req.method()} ${url.pathname}`); };
      await page.setViewportSize({ width: 390, height: 900 });
      await page.goto('/');
      await page.getByRole('navigation', { name: en['nav.label'] }).getByRole('link', { name: en['home.allItems'], exact: true }).click();
      await expect(page.getByRole('heading', { level: 1, name: en['allItems.title'] })).toBeVisible();
      const searchbox = page.getByRole('searchbox', { name: en['allItems.search'], exact: true });
      await searchbox.fill(accentQuery);
      await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
      await expect(page.getByRole('main').getByRole('heading', { level: 2, name: range(1, Math.min(25, accentActive), accentActive), exact: true })).toBeVisible();
      page.on('request', onApi);
      const findStarted = Date.now();
      await searchbox.fill(query);
      await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
      await expect(page.getByRole('main').getByRole('heading', { level: 2, name: range(1, 1, 1), exact: true })).toBeVisible();
      await expect(page.locator('main li[data-item-id]')).toHaveCount(1);
      const findMs = Date.now() - findStarted;
      page.off('request', onApi);
      const searchUrl = page.url();
      expect(new URL(searchUrl).searchParams.get('q')).toBe(query);
      await page.getByRole('link', { name: target!.title, exact: true }).click();
      await expect(page.getByRole('heading', { level: 1, name: target!.title })).toBeVisible();
      await page.getByRole('button', { name: en['itemDetail.pause'], exact: true }).click();
      await expect.poll(async () => (await (await api(page, 'get', `${root}/items/${target!.id}`)).json() as { workflowState: string }).workflowState, { timeout: 10_000, message: 'item pauses after the action' }).toBe('paused');
      await page.getByRole('link', { name: en['itemDetail.backAllItems'], exact: true }).click();
      await expect(page).toHaveURL(searchUrl);
      await expect(searchbox).toHaveValue(query);
      await expect(page.locator(`main li[data-item-id="${target!.id}"]`)).toContainText(en['items.group.paused']);
      await page.getByRole('link', { name: target!.title, exact: true }).click();
      await page.getByRole('button', { name: en['itemDetail.resume'], exact: true }).click();
      await expect.poll(async () => (await (await api(page, 'get', `${root}/items/${target!.id}`)).json() as { workflowState: string }).workflowState, { timeout: 10_000, message: 'item resumes after the action' }).toBe('open');
      await expect(page.getByRole('button', { name: en['itemDetail.pause'], exact: true })).toBeEnabled();
      await expect(page.getByRole('button', { name: en['itemDetail.resume'], exact: true })).toHaveCount(0);
      await capture(page, join(dir, 'audit-500-find-manage-detail-en-390.png'));
      metrics.stages['500-find-manage-en-390'] = { accentQuery, accentActive, findQuery: query, findMs, findApiCalls: apiCalls, domElements: await page.evaluate(() => document.getElementsByTagName('*').length) };
    }
    if (stage === '1200') {
      const tall = createdItems.find(item => item.recurrence?.mode === 'fixed' && !item.archived);
      expect(tall, 'recurring item fixture').toBeTruthy();
      const recurringHistoryItemId = tall!.id;
      const afterCompletionFixture = createdItems.find(item =>
        item.recurrence?.mode === 'after_completion' && !item.archived &&
        ![1, 2, 4].includes(item.generatorIndex % 20),
      );
      expect(afterCompletionFixture, 'after-completion item fixture').toBeTruthy();
      const afterCompletionItemId = afterCompletionFixture!.id;
      let doneDetailFixture: { id: string } | undefined;
      let archivedDetailFixture: { id: string } | undefined;
      for (const candidate of createdItems.filter(item => item.recurrence === null)) {
        const current = await api(page, 'get', `${root}/items/${candidate.id}`);
        expect(current.status()).toBe(200);
        const detail = await current.json() as { done: boolean; archived: boolean };
        if (detail.done && !doneDetailFixture) doneDetailFixture = candidate;
        if (detail.archived && !archivedDetailFixture) archivedDetailFixture = candidate;
        if (doneDetailFixture && archivedDetailFixture) break;
      }
      expect(doneDetailFixture, 'done one-off detail fixture').toBeTruthy();
      expect(archivedDetailFixture, 'archived detail fixture').toBeTruthy();
      for (const cycle of [0, 1]) {
        const current = await api(page, 'get', `${root}/items/${recurringHistoryItemId}`);
        expect(current.status(), 'read recurring history fixture').toBe(200);
        const complete = await api(page, 'post', `${root}/items/${recurringHistoryItemId}/completions`, {}, { 'Idempotency-Key': key(), 'If-Match': current.headers()['etag']! });
        expect(complete.status(), `complete recurring cycle ${cycle + 1}`).toBe(201);
      }
      for (const width of [320, 360, 390, 768, 1024, 1440]) {
        const height = width === 320 ? 640 : width === 390 ? 844 : 900;
        await page.setViewportSize({ width, height }); await page.goto('/items');
        const range = new RegExp(en['allItems.range'].replace('{from}', '\\d+').replace('{to}', '\\d+').replace('{total}', '\\d+'));
        await expect(page.getByRole('main').getByRole('heading', { name: range })).toBeVisible();
        const searchLabel = page.locator('label[for="all-items-search"]');
        const pageTitle = page.getByRole('heading', { level: 1, name: en['allItems.title'], exact: true });
        await expect(searchLabel).toBeVisible();
        const [titleRect, labelRect, headerRect] = await Promise.all([
          pageTitle.boundingBox(), searchLabel.boundingBox(), page.locator('header').boundingBox(),
        ]);
        expect(titleRect, `All items h1 box at ${width}px`).not.toBeNull();
        expect(labelRect, `All items search label box at ${width}px`).not.toBeNull();
        expect(headerRect, `All items header box at ${width}px`).not.toBeNull();
        expect(titleRect!.y + titleRect!.height, `All items h1 must clear the search label at ${width}px`).toBeLessThanOrEqual(labelRect!.y);
        expect(titleRect!.y, `All items h1 must start below the header at ${width}px`).toBeGreaterThanOrEqual(headerRect!.y + headerRect!.height);
        const layout = await page.evaluate(() => ({
          overflow: document.documentElement.scrollWidth > window.innerWidth,
          offenders: [...document.querySelectorAll<HTMLElement>('*')].filter((element) => element.getBoundingClientRect().right > window.innerWidth + 0.5)
            .map((element) => ({ tag: element.tagName, className: element.className, text: (element.innerText || '').slice(0, 50), right: element.getBoundingClientRect().right })),
        }));
        if (layout.overflow) console.error(`All items overflow at ${width}px`, layout.offenders);
        expect(layout.overflow, `All items horizontal overflow at ${width}px`).toBe(false);
        if (width <= 390) {
          const firstRow = page.locator('main li[data-item-id]').first();
          const titleLink = firstRow.getByRole('link');
          const [rowRect, titleRect, bottomBarTop] = await Promise.all([
            firstRow.boundingBox(), titleLink.boundingBox(),
            page.getByRole('navigation', { name: en['nav.label'] }).evaluate((node) => node.getBoundingClientRect().top),
          ]);
          const titleFocusRect = await titleLink.evaluate((node) => {
            const rect = node.getBoundingClientRect();
            const style = getComputedStyle(node);
            const offset = Number.parseFloat(style.outlineOffset) || 0;
            const width = Number.parseFloat(style.outlineWidth) || 0;
            return { top: rect.top - offset - width, bottom: rect.bottom + offset + width };
          });
          expect(rowRect).not.toBeNull();
          expect(titleRect).not.toBeNull();
          expect(titleRect!.y).toBeGreaterThan(rowRect!.y);
          expect(titleRect!.height).toBeGreaterThanOrEqual(44);
          if (width === 390) {
            expect(titleFocusRect.top, 'All items title focus outline should clear the card border').toBeGreaterThan(rowRect!.y);
            expect(rowRect!.y + rowRect!.height, 'All items first row should fit above phone navigation at 390px').toBeLessThan(bottomBarTop);
          } else {
            expect(titleFocusRect.bottom, 'All items first title and focus outline must fit above phone navigation at 320px').toBeLessThan(bottomBarTop);
          }
        }
        if (width >= 1024) {
          const searchRect = await page.getByRole('searchbox', { name: en['allItems.search'], exact: true }).evaluate((node) => node.getBoundingClientRect());
          const resultsRect = await page.locator('main li[data-item-id]').first().evaluate((node) => node.getBoundingClientRect());
          expect(resultsRect.left, 'Desktop results should sit beside the filter rail').toBeGreaterThan(searchRect.right);
        }
        await capture(page, join(dir, `audit-all-items-en-${width}.png`), false);
        if (width === 390) {
          const disclosure = page.locator('[data-all-items-filter-disclosure]');
          expect(await disclosure.evaluate((node) => node.getBoundingClientRect().height)).toBeGreaterThanOrEqual(44);
          await disclosure.click();
          await expect(disclosure).toHaveAttribute('aria-expanded', 'true');
          const filtersPanel = page.locator(`#${await disclosure.getAttribute('aria-controls')}`);
          await expect(filtersPanel.getByLabel(en['items.subject'])).toBeVisible();
          await capture(page, join(dir, 'audit-all-items-filters-expanded-390.png'), false);
          await disclosure.click();
          await expect(disclosure).toHaveAttribute('aria-expanded', 'false');
          await disclosure.click();
          await page.getByLabel(en['items.subject']).selectOption({ index: 1 });
          await expect(page.locator('[data-all-items-clear]')).toHaveCount(1);
          await expect(page.getByRole('button', { name: /Filters \(1\)/ })).toBeVisible();
          await capture(page, join(dir, 'audit-all-items-filtered-count-390.png'), false);
          await page.goto('/items');
        }
        if (width === 390 || width === 1440) {
          await page.evaluate(() => document.documentElement.setAttribute('data-theme', 'dark'));
          await capture(page, join(dir, `audit-all-items-dark-${width}.png`), false);
          await page.evaluate(() => document.documentElement.removeAttribute('data-theme'));
          await page.getByRole('button', { name: en['allItems.next'], exact: true }).click();
          await expect(page.getByRole('button', { name: en['allItems.previous'], exact: true })).toBeVisible();
          const pageTwoPager = page.getByRole('navigation', { name: en['allItems.pages'] });
          await pageTwoPager.scrollIntoViewIfNeeded();
          await expect(pageTwoPager).toBeInViewport();
          await expect(pageTwoPager.getByRole('button', { name: en['allItems.previous'], exact: true })).toBeInViewport();
          await expect(pageTwoPager.getByRole('button', { name: en['allItems.next'], exact: true })).toBeInViewport();
          await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
          await page.mouse.move(0, 0);
          await page.screenshot({ path: join(dir, `audit-all-items-page2-${width}.png`) });
          await page.goto('/items');
          // Failure/retry view is a browser-stubbed transport response, not a real offline household test.
          await page.route('**/api/v1/households/*/items/summary*', route => route.fulfill({ status: 503, contentType: 'application/json', body: '{}' }));
          await page.reload();
          await expect(page.getByRole('button', { name: en['app.error.retry'], exact: true })).toBeVisible();
          await capture(page, join(dir, `audit-all-items-error-${width}.png`), false);
          await page.unrouteAll();
          await page.goto('/items');
        }
        await page.getByRole('searchbox', { name: en['allItems.search'], exact: true }).fill('nejneobhospodarovavatelnejsiho');
        await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
        await page.getByLabel(en['allItems.status.label']).selectOption('active');
        await expect(page.getByRole('main').getByRole('heading', { name: range })).toBeVisible();
        await expect(page.locator('main li[data-item-id]').first()).toBeVisible();
        await capture(page, join(dir, `audit-all-items-search-unaccented-en-${width}.png`), false);
        await page.getByRole('searchbox', { name: en['allItems.search'], exact: true }).fill('nejneobhospodařovávatelnějšího');
        await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
        await expect(page.locator('main li[data-item-id]')).toHaveCount(25);
        await expect(page.locator('main li[data-item-id] a').first()).toContainText('Kontrola');
        await capture(page, join(dir, `audit-all-items-search-accented-en-${width}.png`), false);
        await page.getByRole('searchbox', { name: en['allItems.search'], exact: true }).fill('');
        await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
        await page.getByLabel(en['allItems.status.label']).selectOption('done');
        await expect(page.getByRole('main').getByRole('heading', { name: range })).toBeVisible();
        await capture(page, join(dir, `audit-all-items-done-en-${width}.png`), false);
        await page.getByRole('searchbox', { name: en['allItems.search'], exact: true }).fill('audit-no-such-item-xyz');
        await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
        await expect(page.getByRole('main').getByRole('heading', { name: en['allItems.noMatch'] })).toBeVisible();
        await capture(page, join(dir, `audit-all-items-nomatch-en-${width}.png`), false);
        if (width === 320 || width === 390 || width === 1440) {
          const csHeight = width === 320 ? 640 : width === 390 ? 844 : 900;
          const csItemsContext = await browser.newContext({ locale: 'cs-CZ', reducedMotion: 'reduce', viewport: { width, height: csHeight } });
          await csItemsContext.addCookies(await page.context().cookies());
          const csItemsPage = await csItemsContext.newPage();
          await csItemsPage.goto('/'); await csItemsPage.evaluate(() => localStorage.setItem('tendo.locale', 'cs')); await csItemsPage.reload(); await csItemsPage.goto('/items');
          const csRange = new RegExp(cs['allItems.range'].replace('{from}', '\\d+').replace('{to}', '\\d+').replace('{total}', '\\d+'));
          await expect(csItemsPage.getByRole('main').getByRole('heading', { name: csRange })).toBeVisible();
          if (width === 320) {
            await csItemsPage.evaluate(() => window.scrollTo(0, 0));
            await capture(csItemsPage, join(dir, 'audit-all-items-cs-320.png'), false);
            await csItemsPage.getByLabel(cs['allItems.status.label']).selectOption('needs');
            await expect(csItemsPage.getByLabel(cs['allItems.status.label'])).toHaveValue('needs');
            await expect(csItemsPage.locator('[data-all-items-filter-disclosure]')).toHaveAttribute('aria-expanded', 'false');
            const firstStatusOnlyTitle = csItemsPage.locator('main li[data-item-id]').first().getByRole('link');
            const firstStatusOnlyBox = await firstStatusOnlyTitle.boundingBox();
            const bottomBar = await csItemsPage.getByRole('navigation', { name: cs['nav.label'] }).evaluate((node) => node.getBoundingClientRect().top);
            expect(firstStatusOnlyBox).not.toBeNull();
            expect(firstStatusOnlyBox!.y + firstStatusOnlyBox!.height).toBeLessThan(bottomBar);
            await capture(csItemsPage, join(dir, 'audit-all-items-cs-320-status-only.png'), false);
          }
          if (width <= 390 && !(width === 320 && csItemsPage.url().includes('status=needs'))) {
            const csFirstLink = csItemsPage.locator('main li[data-item-id]').first().getByRole('link');
            const csFirstLinkBox = await csFirstLink.boundingBox();
            const csBottomBar = await csItemsPage.getByRole('navigation', { name: cs['nav.label'] }).evaluate((node) => node.getBoundingClientRect().top);
            expect(csFirstLinkBox).not.toBeNull();
            expect(csFirstLinkBox!.height).toBeGreaterThanOrEqual(44);
            if (width === 390) {
              const csRow = await csItemsPage.locator('main li[data-item-id]').first().evaluate((node) => node.getBoundingClientRect());
              expect(csRow.bottom, 'Czech first row should fit above the 390px phone navigation').toBeLessThan(csBottomBar);
            } else {
              expect(csFirstLinkBox!.y + csFirstLinkBox!.height, 'Czech first title should clear bottom navigation at 320px').toBeLessThan(csBottomBar);
            }
          }
          if (width === 320) {
            await csItemsPage.locator('[data-all-items-filter-disclosure]').click();
            const csSubject = csItemsPage.getByLabel(cs['items.subject']);
            const selectedSubjectId = await csSubject.locator('option').nth(1).getAttribute('value');
            await csSubject.selectOption({ index: 1 });
            await expect(csItemsPage.locator('[data-all-items-clear]')).toBeVisible();
            await expect(csItemsPage.getByRole('button', { name: /Filtry \(1\)/ })).toBeVisible();
            await expect(csSubject).toHaveValue(selectedSubjectId!);
            await expect(csItemsPage.getByLabel(cs['allItems.status.label'])).toHaveValue('needs');
            await csItemsPage.getByLabel(cs['allItems.status.label']).scrollIntoViewIfNeeded();
            await expect(csItemsPage.getByLabel(cs['allItems.status.label'])).toBeInViewport();
            await expect(csItemsPage.getByRole('heading', { level: 1, name: cs['allItems.title'] })).toBeInViewport();
            await capture(csItemsPage, join(dir, 'audit-all-items-cs-320-active-filter.png'), false);
          }
          if (width !== 320) {
            await csItemsPage.evaluate(() => window.scrollTo(0, 0));
            await capture(csItemsPage, join(dir, `audit-all-items-cs-${width}.png`), false);
          }
          await csItemsContext.close();
        }
        await page.goto('/');
        await page.setViewportSize({ width, height: 900 });
        await page.getByRole('button', { name: en['items.add'] }).click();
        const addForm = page.getByRole('form', { name: en['items.add.formLabel'] });
        await expect(addForm).toBeVisible();
        await expect(addForm.getByLabel(en['items.title'])).toBeFocused();
        await capture(page, join(dir, `audit-add-empty-en-${width}.png`), false);
        if (width === 320 || width === 390) await capture(page, join(dir, `audit-add-empty-placeholder-en-${width}.png`), false);
        expect(await page.evaluate(() => document.documentElement.scrollWidth), `add form overflow at ${width}px`).toBeLessThanOrEqual(width);
        await addForm.getByRole('switch', { name: en['items.repeat'], exact: true }).check();
        await expect(addForm.getByRole('switch', { name: /Count the next/ })).toBeVisible();
        await capture(page, join(dir, `audit-add-fixed-en-${width}.png`), false);
        await addForm.getByRole('switch', { name: /Count the next/ }).check();
        await capture(page, join(dir, `audit-add-after-completion-en-${width}.png`), false);
        await addForm.getByRole('switch', { name: /Count the next/ }).uncheck();
        await addForm.getByRole('switch', { name: en['items.repeat'], exact: true }).uncheck();
        await addForm.getByRole('button', { name: en['items.add.submit'], exact: true }).click();
        await expect(addForm.getByRole('alert')).toBeVisible();
        await capture(page, join(dir, `audit-add-validation-en-${width}.png`), false);
        if (width === 390 || width === 1440) {
          await page.locator('html').evaluate((html) => html.setAttribute('data-theme', 'dark'));
          await capture(page, join(dir, `audit-add-forced-dark-en-${width}.png`), false);
          await page.locator('html').evaluate((html) => html.removeAttribute('data-theme'));
        }
        await page.getByRole('dialog').locator('header button[aria-label="Cancel"]').click();
        await page.setViewportSize({ width, height: 900 });
        const afterCompletionDetail = createdItems.find(item => item.id === afterCompletionItemId)!;
        const detailResponse = await api(page, 'get', `${root}/items/${afterCompletionItemId}`);
        expect(detailResponse.status(), 'after-completion Edit fixture remains available').toBe(200);
        const detailItem = await detailResponse.json() as { title: string; recurrence: { mode: string } | null; archived: boolean; done: boolean };
        expect(detailItem).toMatchObject({ title: afterCompletionDetail.title, recurrence: { mode: 'after_completion' }, archived: false, done: false });
        await page.goto(`/items/${afterCompletionItemId}`);
        await expect(page.getByRole('heading', { level: 1, name: detailItem.title, exact: true }), 'loaded the intended after-completion item detail').toBeVisible({ timeout: 10_000 });
        const editButton = page.getByRole('button', { name: en['itemDetail.edit.button'], exact: true });
        await expect(editButton, 'owner can edit the loaded after-completion item').toBeVisible({ timeout: 10_000 });
        await editButton.click({ timeout: 10_000 });
        const editForAfterMode = page.getByRole('form', { name: en['itemDetail.edit.formLabel'] });
        await expect(editForAfterMode.getByRole('switch', { name: /Count the next/ })).toBeVisible();
        await expect(editForAfterMode.getByRole('switch', { name: /Count the next/ })).toBeChecked();
        await capture(page, join(dir, `audit-item-edit-after-completion-en-${width}.png`), false);
        await page.getByRole('dialog').locator('header button[aria-label="Cancel"]').click();
        const csContext = await browser.newContext({ locale: 'cs-CZ', reducedMotion: 'reduce', viewport: { width, height: 900 } });
        await csContext.addCookies(await page.context().cookies());
        const csPage = await csContext.newPage();
        await csPage.goto('/'); await csPage.evaluate(() => localStorage.setItem('tendo.locale', 'cs')); await csPage.reload();
        await csPage.setViewportSize({ width, height: 900 });
        await csPage.getByRole('button', { name: cs['items.add'] }).click();
        const csAddForm = csPage.getByRole('form', { name: cs['items.add.formLabel'] });
        await expect(csAddForm).toBeVisible();
        await capture(csPage, join(dir, `audit-add-empty-cs-${width}.png`), false);
        if (width === 320 || width === 390) await capture(csPage, join(dir, `audit-add-empty-placeholder-cs-${width}.png`), false);
        await csAddForm.getByRole('switch', { name: cs['items.repeat'], exact: true }).check();
        await expect(csAddForm.getByRole('switch', { name: /Další/ })).toBeVisible();
        await capture(csPage, join(dir, `audit-add-fixed-cs-${width}.png`), false);
        await csAddForm.getByRole('switch', { name: /Další/ }).check();
        await capture(csPage, join(dir, `audit-add-after-completion-cs-${width}.png`), false);
        await csAddForm.getByRole('switch', { name: cs['items.repeat'], exact: true }).uncheck();
        await csAddForm.getByRole('button', { name: cs['items.add.submit'], exact: true }).click();
        await expect(csAddForm.getByRole('alert')).toBeVisible();
        await capture(csPage, join(dir, `audit-add-validation-cs-${width}.png`), false);
        await csPage.getByRole('dialog').locator('header button[aria-label]').click();
        await csPage.goto(`/items/${recurringHistoryItemId}`);
        await expect(csPage.getByRole('heading', { name: cs['itemDetail.history'] })).toBeVisible();
        await expect(csPage.getByRole('main').locator('ol li')).toHaveCount(2);
        await capture(csPage, join(dir, `audit-item-detail-cs-${width}.png`));
        await csContext.close();
        if (width === 390 || width === 1440) {
          const longNotesFixture = createdItems[7];
          if (!longNotesFixture) throw new Error('long notes item fixture missing');
          await page.goto(`/items/${longNotesFixture.id}`);
          await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
          await expect(page.getByText(/První odstavec/)).toBeVisible();
          const noteText = await page.locator('main p.whitespace-pre-wrap').textContent();
          expect(noteText).toContain('\n\n');
          const noteParagraphs = await page.locator('main p.whitespace-pre-wrap').evaluate(element => getComputedStyle(element).whiteSpace);
          expect(noteParagraphs).toBe('pre-wrap');
          await capture(page, join(dir, `audit-item-detail-long-notes-en-${width}.png`));
          const longTitleFixture = createdItems[8];
          if (!longTitleFixture) throw new Error('long Czech title fixture missing');
          await page.goto(`/items/${longTitleFixture.id}`);
          await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
          await capture(page, join(dir, `audit-item-detail-long-title-en-${width}.png`));
          if (width === 390) {
            const csLongTitleContext = await browser.newContext({ locale: 'cs-CZ', reducedMotion: 'reduce', viewport: { width, height: 900 } });
            await csLongTitleContext.addCookies(await page.context().cookies());
            const csLongTitlePage = await csLongTitleContext.newPage();
            await csLongTitlePage.goto('/'); await csLongTitlePage.evaluate(() => localStorage.setItem('tendo.locale', 'cs')); await csLongTitlePage.reload();
            await csLongTitlePage.goto(`/items/${longTitleFixture.id}`);
            await expect(csLongTitlePage.getByRole('heading', { level: 1 })).toBeVisible();
            await expect(csLongTitlePage.getByRole('heading', { level: 1 })).toContainText('Kontrola');
            await capture(csLongTitlePage, join(dir, 'audit-item-detail-long-title-cs-390.png'));
            await csLongTitleContext.close();
          }
          await page.goto(`/items/${doneDetailFixture!.id}`);
          await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
          await expect(page.getByText(en['itemDetail.state.done'], { exact: true })).toBeVisible();
          await capture(page, join(dir, `audit-item-detail-done-en-${width}.png`));
          await page.goto(`/items/${archivedDetailFixture!.id}`);
          await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
          await expect(page.getByRole('main').getByText(/Archived\./)).toBeVisible();
          await capture(page, join(dir, `audit-item-detail-archived-en-${width}.png`));
        }
        await page.goto(`/items/${recurringHistoryItemId}`);
        await expect(page.getByRole('heading', { name: en['itemDetail.history'] })).toBeVisible();
        const history = page.getByRole('main').getByRole('list').filter({ has: page.getByText(new RegExp(en['itemDetail.historyDone'].replace('{date}', ''))) });
        await expect(history.locator('li')).toHaveCount(2);
        expect(recurringHistoryItemId).toBe(tall!.id);
        await capture(page, join(dir, `audit-item-detail-en-${width}.png`));
        await page.getByRole('button', { name: en['itemDetail.edit.button'] }).click();
        await expect(page.getByRole('form', { name: en['itemDetail.edit.formLabel'] })).toBeVisible();
        await capture(page, join(dir, `audit-item-edit-en-${width}.png`), false);
        if (width === 390) {
          const conflictItem = await api(page, 'get', `${root}/items/${recurringHistoryItemId}`);
          expect(conflictItem.status()).toBe(200);
          const conflictBody = await conflictItem.json() as { title: string };
          const editTitle = page.getByRole('form', { name: en['itemDetail.edit.formLabel'] }).getByLabel(en['items.title']);
          await editTitle.fill(`${conflictBody.title} user edit`);
          const serverConflict = await api(page, 'patch', `${root}/items/${recurringHistoryItemId}`, { title: `${conflictBody.title} server edit` }, { 'If-Match': conflictItem.headers()['etag']! });
          expect(serverConflict.status()).toBe(200);
          await page.getByRole('form', { name: en['itemDetail.edit.formLabel'] }).getByRole('button', { name: en['itemDetail.save'], exact: true }).click();
          await expect(page.getByText(en['itemDetail.edit.conflict'], { exact: true })).toBeVisible();
          await expect(page.getByText(en['itemDetail.edit.conflictHint'], { exact: true })).toBeVisible();
          await capture(page, join(dir, 'audit-item-edit-conflict-en-390.png'), false);
        }
        if (width === 390 || width === 1440) {
          const csRoute = await browser.newContext({ locale: 'cs-CZ', reducedMotion: 'reduce', viewport: { width, height: 900 } });
          await csRoute.addCookies(await page.context().cookies());
          const csRoutePage = await csRoute.newPage();
          await csRoutePage.goto('/'); await csRoutePage.evaluate(() => localStorage.setItem('tendo.locale', 'cs')); await csRoutePage.reload();
          // Czech All items is captured above as a viewport fold; re-capturing it here would overwrite that evidence with a full-page image.
          for (const [path, name, file] of [['/people', 'subjects.title', `audit-people-things-cs-${width}.png`], ['/members', 'members.title', `audit-members-cs-${width}.png`], ['/account', 'account.title', `audit-account-cs-${width}.png`]] as const) {
            await csRoutePage.goto(path);
            await expect(csRoutePage.getByRole('heading', { level: 1, name: cs[name] })).toBeVisible();
            await capture(csRoutePage, join(dir, file));
          }
          await csRoutePage.goto(`/items/${recurringHistoryItemId}`);
          await expect(csRoutePage.getByRole('heading', { name: cs['itemDetail.history'] })).toBeVisible();
          await csRoutePage.getByRole('button', { name: cs['itemDetail.edit.button'], exact: true }).click();
          await expect(csRoutePage.getByRole('form', { name: cs['itemDetail.edit.formLabel'] })).toBeVisible();
          await capture(csRoutePage, join(dir, `audit-item-edit-cs-${width}.png`), false);
          await csRoutePage.goto(`/items/${doneDetailFixture!.id}`); await expect(csRoutePage.getByText(cs['itemDetail.state.done'], { exact: true })).toBeVisible();
          await capture(csRoutePage, join(dir, `audit-item-detail-done-cs-${width}.png`));
          await csRoutePage.goto(`/items/${archivedDetailFixture!.id}`); await expect(csRoutePage.getByRole('heading', { level: 1 })).toBeVisible();
          await capture(csRoutePage, join(dir, `audit-item-detail-archived-cs-${width}.png`));
          if (width === 390) {
            const csErrorContext = await browser.newContext({ locale: 'cs-CZ', reducedMotion: 'reduce', viewport: { width: 390, height: 844 } });
            await csErrorContext.addCookies(await page.context().cookies());
            const csErrorPage = await csErrorContext.newPage();
            await csErrorPage.goto('/'); await csErrorPage.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
            const errorSession = await api(csErrorPage, 'get', '/api/v1/session');
            expect(errorSession.status()).toBe(200);
            const csHouseholdForError = (await errorSession.json() as { defaultHouseholdId: string }).defaultHouseholdId;
            await csErrorPage.route(`**/api/v1/households/${csHouseholdForError}/items/summary**`, async route => {
              await route.fulfill({ status: 503, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Unavailable', status: 503, code: 'unavailable' }) });
            });
            await csErrorPage.goto('/'); await expect(csErrorPage.getByRole('heading', { level: 1 })).toBeVisible();
            await expect(csErrorPage.getByRole('button', { name: cs['app.error.retry'], exact: true })).toBeVisible();
            await capture(csErrorPage, join(dir, 'audit-home-server-error-cs-390.png'));
            await csErrorContext.close();
          }
          await csRoute.close();
        }
        await page.goto('/');
        await page.getByRole('navigation', { name: en['nav.label'] }).getByRole('link', { name: en['home.people'], exact: true }).click();
        await expect(page.getByRole('heading', { name: en['subjects.title'] })).toBeVisible();
        await expect(page.getByText(en['subjects.loading'], { exact: true })).toHaveCount(0);
        await expect(page.getByRole('main').getByText('Anna Nováková', { exact: true })).toBeVisible();
        await expect(page.getByRole('main').getByText('Škoda Octavia', { exact: true })).toBeVisible();
        await capture(page, join(dir, `audit-people-things-en-${width}.png`));
        if (width === 390) {
          const offlineContext = await browser.newContext({ reducedMotion: 'reduce', viewport: { width: 390, height: 844 } });
          await offlineContext.addCookies(await page.context().cookies());
          const offlinePage = await offlineContext.newPage();
          await offlinePage.goto('/');
          const offlineSession = await api(offlinePage, 'get', '/api/v1/session');
          expect(offlineSession.status()).toBe(200);
          const householdForError = (await offlineSession.json() as { defaultHouseholdId: string }).defaultHouseholdId;
          await offlinePage.route(`**/api/v1/households/${householdForError}/items/summary**`, async route => {
            await route.fulfill({ status: 503, contentType: 'application/problem+json', body: JSON.stringify({ type: 'about:blank', title: 'Unavailable', status: 503, code: 'unavailable' }) });
          });
          await offlinePage.goto('/');
          await expect(offlinePage.getByRole('heading', { level: 1 })).toBeVisible();
          await expect(offlinePage.getByRole('button', { name: en['app.error.retry'], exact: true })).toBeVisible();
          await capture(offlinePage, join(dir, 'audit-home-server-error-en-390.png')); await offlineContext.close();
        }
        await page.goto('/');
        await page.getByRole('navigation', { name: en['nav.label'] }).getByRole('link', { name: en['home.members'], exact: true }).click();
        await expect(page.getByRole('heading', { name: en['members.title'] })).toBeVisible();
        await expect(page.getByText(en['members.loading'], { exact: true })).toHaveCount(0);
        await expect(page.getByRole('main').getByText(`audit_owner (${en['members.you']})`, { exact: true })).toBeVisible();
        await expect(page.getByRole('main').getByText('audit_member', { exact: true })).toBeVisible();
        await expect(page.getByRole('heading', { name: en['members.invitations.title'] })).toBeVisible();
        await capture(page, join(dir, `audit-members-en-${width}.png`));
        const memberContext = await browser.newContext({ reducedMotion: 'reduce', viewport: { width, height: 900 } });
        const memberPage = await memberContext.newPage(); await memberPage.goto('/login');
        await memberPage.getByLabel(en['login.login'], { exact: true }).fill('audit_member');
        await memberPage.getByLabel(en['login.password'], { exact: true }).fill('audit-member-password-long-enough');
        await memberPage.getByRole('button', { name: en['login.submit'], exact: true }).click();
        await expect(memberPage.getByRole('heading', { level: 1, name: 'Audit household' })).toBeVisible();
        await memberPage.goto('/members'); await expect(memberPage.getByRole('heading', { level: 1, name: en['members.title'] })).toBeVisible();
        await capture(memberPage, join(dir, `audit-members-non-owner-en-${width}.png`));
        if (width === 390 || width === 1440) {
          await memberPage.goto('/'); await memberPage.evaluate(() => localStorage.setItem('tendo.locale', 'cs')); await memberPage.reload();
          await memberPage.goto('/members'); await expect(memberPage.getByRole('heading', { level: 1, name: cs['members.title'] })).toBeVisible();
          await capture(memberPage, join(dir, `audit-members-non-owner-cs-${width}.png`));
        }
        await memberContext.close();
        await page.getByRole('button', { name: en['members.invite.action'], exact: true }).click();
        const inviteLink = page.getByLabel(en['members.invite.link'], { exact: true });
        await expect(inviteLink).toHaveValue(/\/invite#.+/);
        shownInviteTokens.push((await inviteLink.inputValue()).split('#')[1]!);
        // The one-time invitation token is a bearer secret: mask the link field in committed evidence.
        await capture(page, join(dir, `audit-members-invite-en-${width}.png`), true, [inviteLink]);
        await page.goto('/');
        await page.getByRole('navigation', { name: en['nav.label'] }).getByRole('link', { name: en['home.account'], exact: true }).click();
        await expect(page.getByRole('heading', { name: en['account.title'] })).toBeVisible();
        await expect(page.getByText(en['account.signedInAs'].replace('{login}', 'audit_owner'), { exact: true })).toBeVisible();
        await expect(page.getByText(en['account.oidc.loading'], { exact: true })).toHaveCount(0);
        await capture(page, join(dir, `audit-account-en-${width}.png`));
      }
      await page.setViewportSize({ width: 640, height: 900 }); await page.goto('/');
      await capture(page, join(dir, 'home-1200-en-640-reflow-proxy.png'));
    }
    const widths = [320, 360, 390, 768, 1024, 1440];
    for (const width of widths) for (const locale of ['en', 'cs']) {
      const context = locale === 'cs' ? await browser.newContext({ locale: 'cs-CZ', reducedMotion: 'reduce', viewport: { width, height: 900 } }) : null;
      if (context) await context.addCookies(await page.context().cookies());
      const targetPage = context ? await context.newPage() : page;
      if (targetPage === page) await targetPage.setViewportSize({ width, height: 900 });
      if (targetPage !== page) {
        await targetPage.goto('/');
        await targetPage.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
      }
      const requests: Array<{ method: string; path: string; bytes: number }> = [];
      const pageSizes: number[] = [];
      const responseListener = async (response: Response): Promise<void> => {
        const req = response.request(); const url = new URL(response.url());
        if (!url.pathname.startsWith('/api/')) return;
        const body = await response.body().catch(() => Buffer.alloc(0));
        const path = url.pathname.replace(/[0-9a-f]{8}-[0-9a-f-]{27}/gi, ':id') + url.search;
        requests.push({ method: req.method(), path, bytes: body.length });
        if (url.pathname.endsWith('/items') && body.length > 0) {
          const json = JSON.parse(body.toString()) as { items?: unknown[] };
          if (json.items) pageSizes.push(json.items.length);
        }
      };
      targetPage.on('response', responseListener);
      const started = Date.now(); await targetPage.goto('/');
      const empty = targetPage.getByText((locale === 'cs' ? cs : en)['home.empty.title']);
      const row = targetPage.locator('li[data-item-id]').first();
      await expect.poll(async () => (await row.isVisible().catch(() => false)) || (await empty.isVisible().catch(() => false)), { timeout: 10_000 }).toBe(true);
      const timeToContentMs = Date.now() - started;
      await targetPage.waitForLoadState('networkidle');
      const overview = targetPage.getByRole('main').getByRole('navigation', { name: (locale === 'cs' ? cs : en)['items.group.overview'] });
      if (stage === 'empty') await expect(targetPage.getByRole('main').getByRole('button', { name: (locale === 'cs' ? cs : en)['items.add'], exact: true })).toBeVisible();
      if (stage === '1200') await expect(overview).toBeVisible();
      const actualViewport = targetPage.viewportSize();
      const innerWidth = await targetPage.evaluate(() => window.innerWidth);
      expect(innerWidth, `${locale} viewport width`).toBe(width);
      const observed = await targetPage.evaluate(() => {
        const clientWidth = document.documentElement.clientWidth;
        const offenders = [...document.querySelectorAll<HTMLElement>('*')].filter(element => element.getBoundingClientRect().right > clientWidth + 0.5).slice(0, 10).map(element => ({ tag: element.tagName.toLowerCase(), itemId: element.getAttribute('data-item-id'), text: (element.innerText || element.textContent || '').trim().slice(0, 80), right: element.getBoundingClientRect().right, width: element.getBoundingClientRect().width }));
        return { dom: document.getElementsByTagName('*').length, rows: document.querySelectorAll('li[data-item-id]').length, overflow: document.documentElement.scrollWidth > document.documentElement.clientWidth, height: document.documentElement.scrollHeight, overflowOffenders: offenders };
      });
      if (stage === '1') expect(observed.rows, `1-item stage shows exactly one Home row (${locale} ${width}px)`).toBe(1);
      if (stage === '100') {
        await expect(overview, `100-item overview is visible (${locale} ${width}px)`).toBeVisible();
        const renderedGroups = await targetPage.locator('main section[id^="group-"] ul').evaluateAll((lists) =>
          lists.map((list) => list.querySelectorAll(':scope > li[data-item-id]').length),
        );
        const expectedGroupCount = await targetPage.locator('main li[data-item-id]')
          .evaluateAll((rows) => new Set(rows.map((row) => row.closest('section')?.id)).size);
        const renderedListGroupCount = await targetPage.locator('main section[id^="group-"]')
          .evaluateAll((sections) => sections.filter((section) => section.querySelector('ul > li[data-item-id]')).length);
        expect(renderedListGroupCount, 'each populated group has one rendered list').toBe(expectedGroupCount);
        expect(renderedGroups.length, 'every populated list has rendered rows').toBe(renderedListGroupCount);
        expect(renderedGroups.length, 'the 100-item fixture populates multiple Home groups').toBeGreaterThan(1);
        expect(renderedGroups.every((count) => count > 0 && count <= HOME_GROUP_ROW_CAP), 'each populated Home group respects its row cap').toBe(true);
      }
      const file = `home-${stage}-${locale}-${width}`;
      await capture(targetPage, join(dir, `${file}.png`));
      await capture(targetPage, join(dir, `${file}-fold.png`), false);
      metrics.stages[`${stage}-${locale}-${width}`] = { requests, requestCount: requests.length, responseBytes: requests.reduce((n, r) => n + r.bytes, 0), timeToContentMs, viewport: actualViewport, innerWidth, ...observed, accessibility: routeMetrics[`${file}.png`], showMoreVisible: await targetPage.locator('button[data-group-more]').first().isVisible().catch(() => false) };
      metrics.listPageSizes.push(...pageSizes);
      targetPage.off('response', responseListener);
      if (context) await context.close();
    }
    if (stage === '1200') {
      const actualActiveItems = await verifyList(false, false);
      const actualActiveTotal = actualActiveItems.length;
      const homeSummaryResponse = await page.request.get(`/api/v1/households/${householdId}/items/summary`);
      expect(homeSummaryResponse.status()).toBe(200);
      const homeSummary = await homeSummaryResponse.json() as { needsAttention: number; inProgress: number; waiting: number; upcoming: number; paused: number };
      const groupCounts = [homeSummary.needsAttention, homeSummary.inProgress, homeSummary.waiting, homeSummary.upcoming, homeSummary.paused];
      const expectedInitial = groupCounts.reduce((total, count) => total + Math.min(count, 25), 0);
      const expectedRows = groupCounts.reduce((total, count) => total + Math.min(count, 100), 0);
      const expectedSeeAllLinks = groupCounts.filter((count) => count > 100).length;
      for (const width of [390, 1440]) {
        await page.setViewportSize({ width, height: 900 }); await page.goto('/');
        const clickMetrics: Array<Record<string, number>> = [];
        await page.getByRole('heading', { level: 1 }).first().waitFor({ state: 'visible' });
        const groupMore = page.locator('main button[data-group-more]');
        await expect(page.locator('main li[data-item-id]')).toHaveCount(expectedInitial);
        if (groupCounts.some((count) => count > 25)) await expect(groupMore.first()).toBeVisible();
        for (let click = 1; click <= 200; click++) {
          const more = groupMore.first();
          if (!(await more.isVisible().catch(() => false))) break;
          await more.scrollIntoViewIfNeeded();
          const rowsBefore = await page.locator('main li[data-item-id]').count();
          const before = Date.now(); await more.click();
          await expect.poll(() => page.locator('main li[data-item-id]').count()).toBeGreaterThan(rowsBefore);
          const timeToRenderMs = Date.now() - before;
          const state = await page.evaluate(() => ({ dom: document.getElementsByTagName('*').length, rows: document.querySelectorAll('main li[data-item-id]').length, height: document.documentElement.scrollHeight }));
          clickMetrics.push({ click, ...state, timeToRenderMs });
        }
        expect(expectedRows, `capped rows cannot exceed active item count at ${width}px`).toBeLessThanOrEqual(actualActiveTotal);
        await expect(page.locator('main li[data-item-id]'), `expanded rows at ${width}px`).toHaveCount(expectedRows);
        await expect(page.locator('main a[data-group-all]')).toHaveCount(expectedSeeAllLinks);
        (metrics.stages[`1200-en-${width}`] as Record<string, unknown>).showMoreClicks = clickMetrics;
        (metrics.stages[`1200-en-${width}`] as Record<string, unknown>).seeAllLinks = await page.locator('main a[data-group-all]').evaluateAll(links => links.map(link => ({ group: link.getAttribute('data-group-all'), label: link.textContent?.trim() })));
        // Home is one bounded stack at every width: no side-by-side columns, rows never wider than max-w-3xl (48rem).
        const stack = await page.locator('main [data-home-layout="stack"]').evaluate((element) => ({
          width: element.getBoundingClientRect().width,
          lefts: [...element.querySelectorAll(':scope > section')].map((section) => Math.round(section.getBoundingClientRect().left)),
        }));
        expect(stack.width, `Home stack measure at ${width}px`).toBeLessThanOrEqual(768.5);
        expect(new Set(stack.lefts).size, `Home groups share one column at ${width}px`).toBe(1);
        // Each overview chip's 2px outline + 2px offset must stay inside the horizontally clipped nav when focused.
        const overviewNav = page.getByRole('main').getByRole('navigation', { name: en['items.group.overview'] });
        for (const link of await overviewNav.getByRole('link').all()) {
          await link.focus();
          await expect.poll(async () => link.evaluate((element) => {
            const nav = element.closest('nav')!.getBoundingClientRect();
            const rect = element.getBoundingClientRect();
            return rect.left - 4 >= nav.left - 0.5 && rect.right + 4 <= nav.right + 0.5 && rect.top - 4 >= nav.top - 0.5 && rect.bottom + 4 <= nav.bottom + 0.5;
          }), `overview focus ring fits inside the nav at ${width}px`).toBe(true);
        }
        await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur());
        await overviewNav.evaluate((nav) => { nav.scrollLeft = 0; });
        await capture(page, join(dir, `home-1200-en-${width}-expanded.png`), false);
        if (width === 1440) {
          await page.locator('html').evaluate((html) => html.setAttribute('data-theme', 'dark'));
          await capture(page, join(dir, `home-1200-en-${width}-expanded-forced-dark.png`), false);
          await page.locator('html').evaluate((html) => html.removeAttribute('data-theme'));
        }
      }
      const darkContext = await browser.newContext({ colorScheme: 'dark', reducedMotion: 'reduce', viewport: { width: 390, height: 844 } });
      await darkContext.addCookies(await page.context().cookies());
      const darkPage = await darkContext.newPage();
      await darkPage.goto('/');
      await expect(darkPage.getByRole('heading', { level: 1, name: 'Audit household' })).toBeVisible();
      await darkPage.waitForLoadState('networkidle');
      await capture(darkPage, join(dir, 'home-1200-en-390-dark-scheme.png'));
      metrics.stages.darkMode = {
        requestedColorScheme: 'dark',
        renderedIdenticallyToLight: await darkPage.evaluate(() => getComputedStyle(document.body).backgroundColor === 'rgb(250, 247, 242)'),
      };
      expect(metrics.stages.darkMode.renderedIdenticallyToLight).toBe(true);
      for (const width of [390, 1440]) {
        await darkPage.setViewportSize({ width, height: 900 });
        await expect(darkPage.evaluate(() => window.innerWidth)).resolves.toBe(width);
        await darkPage.locator('html').evaluate((html) => html.setAttribute('data-theme', 'dark'));
        await capture(darkPage, join(dir, `home-1200-en-${width}-forced-dark.png`));
        await darkPage.locator('html').evaluate((html) => html.removeAttribute('data-theme'));
      }
      await darkContext.close();
      metrics.stages.shell = await captureShell(page);
    }
  }
  const activeItems = await verifyList(false, false);
  const doneItems = await verifyList(true, false);
  const archivedItems = await verifyList(false, true);
  const serverStates = activeItems.reduce<Record<string, number>>((counts, item) => { counts[item.workflowState] = (counts[item.workflowState] ?? 0) + 1; return counts; }, {});
  metrics.seededStateCounts = { ...serverStates, done: doneItems.length, archived: archivedItems.length, activeUndone: activeItems.length };
  expect(activeItems.length + doneItems.length + archivedItems.length).toBe(1200);
  Object.assign(journeyMetrics, await runHouseholdJourneys(page, browser, householdId, process.env.E2E_OWNER_PASSWORD!));
  metrics.journeys = journeyMetrics;
  metrics.routes = routeMetrics;
  metrics.stages.routeOverflow = routeOverflow;
  metrics.stages.routeOverflowElements = routeOverflowElements;
  const safeMetrics = JSON.stringify(metrics);
  for (const secret of [process.env.E2E_SETUP_TOKEN, process.env.E2E_OWNER_PASSWORD, 'audit-member-password-long-enough', invitation.token, auditInvite.token, ...shownInviteTokens]) {
    expect(secret, 'audit secret fixture is present').toBeTruthy();
    expect(safeMetrics, 'metrics.json must not contain credential or invitation secrets').not.toContain(secret!);
  }
  writeFileSync(join(dir, 'metrics.json'), safeMetrics);
  const overflowFiles = Object.entries(routeOverflow).filter(([, overflow]) => overflow).map(([file]) => file);
  expect(overflowFiles, `route captures with horizontal overflow: ${overflowFiles.join(', ') || 'none'}`).toEqual([]);
 });
