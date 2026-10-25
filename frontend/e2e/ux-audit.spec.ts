import { expect, test, type Page, type Response } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { en } from '../src/i18n/en';
import { cs } from '../src/i18n/cs';

const baseURL = process.env.E2E_BASE_URL!;
const dir = resolve(process.env.E2E_AUDIT_DIR!);
const NOW = '2026-05-14';
test.describe.configure({ mode: 'serial' });
test.use({ reducedMotion: 'reduce' });
test.setTimeout(1_800_000);

const routeOverflow: Record<string, boolean> = {};
const routeOverflowElements: Record<string, Array<{ tag: string; text: string; right: number }>> = {};

async function capture(page: Page, path: string, fullPage = true) {
  const width = page.viewportSize()?.width;
  if (!width) throw new Error(`Capture requires a viewport: ${path}`);
  await page.setViewportSize({ width, height: 900 });
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
  await page.screenshot({ path, fullPage });
}

async function api(page: Page, method: 'get' | 'post' | 'patch', path: string, data?: unknown, headers: Record<string, string> = {}) {
  return page.request[method](path, { headers: { Origin: baseURL, ...headers }, ...(data === undefined ? {} : { data }) });
}
function key() { return crypto.randomUUID().replaceAll('-', ''); }
function rng(seed: number) { let s = seed >>> 0; return () => { s = (s * 1664525 + 1013904223) >>> 0; return s / 0x100000000; }; }

 test('capture staged UX and scale audit', async ({ page, browser }) => {
  mkdirSync(dir, { recursive: true });
  const metrics: { fixedBusinessDate: string; stages: Record<string, Record<string, unknown>>; seededStateCounts: Record<string, number>; listPageSizes: number[]; stageSeeding: Record<string, unknown>; seedDurationMs?: number } = { fixedBusinessDate: NOW, stages: {}, seededStateCounts: {}, listPageSizes: [], stageSeeding: {} };
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: en['onboarding.title'] })).toBeVisible();
  for (const width of [320, 390, 768, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await capture(page, join(dir, `onboarding-en-${width}.png`));
  }
  await page.locator('#onboarding-timezone').selectOption('Europe/Prague');
  await page.getByLabel(en['onboarding.setupCode'], { exact: true }).fill(process.env.E2E_SETUP_TOKEN!);
  await page.getByLabel(en['onboarding.householdName']).fill('Audit household');
  await page.locator('#onboarding-login').fill('audit_owner');
  await page.locator('#onboarding-password').fill(process.env.E2E_OWNER_PASSWORD!);
  await page.getByRole('button', { name: en['onboarding.submit'] }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Audit household' })).toBeVisible();
  for (const width of [320, 390, 768, 1440]) {
    await page.setViewportSize({ width, height: 900 });
    await capture(page, join(dir, `home-after-setup-en-${width}.png`));
  }
  const session = await api(page, 'get', '/api/v1/session');
  expect(session.status()).toBe(200);
  const { defaultHouseholdId: householdId } = await session.json() as { defaultHouseholdId: string };
  const root = `/api/v1/households/${householdId}`;
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
  for (const width of [320, 390, 768, 1440]) {
    const context = await browser.newContext({ reducedMotion: 'reduce', viewport: { width, height: 900 } });
    const signedOut = await context.newPage();
    await signedOut.goto('/');
    await expect(signedOut.getByRole('heading', { level: 1, name: en['login.title'] })).toBeVisible();
    await capture(signedOut, join(dir, `audit-login-en-${width}.png`));
    await signedOut.getByLabel(en['login.login'], { exact: true }).fill('audit_missing_user');
    await signedOut.getByLabel(en['login.password'], { exact: true }).fill('wrong-audit-password');
    await signedOut.getByRole('button', { name: en['login.submit'], exact: true }).click();
    await expect(signedOut.getByRole('alert')).toHaveText(en['login.error.invalid']);
    await capture(signedOut, join(dir, `audit-login-error-en-${width}.png`));
    if (width === 390) {
      await signedOut.goto('/');
      await signedOut.evaluate(() => localStorage.setItem('tendo.locale', 'cs'));
      await signedOut.reload();
      await expect(signedOut.getByRole('heading', { level: 1, name: cs['login.title'] })).toBeVisible();
      await capture(signedOut, join(dir, 'audit-login-cs-390.png'));
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
  const createdItems: Array<{ generatorIndex: number; id: string; recurrence: { mode: string } | null; archived: boolean; done: boolean; workflowState: string }> = [];
  const random = rng(0x5eed1234);
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
  const stages = [ ['empty', 0], ['1', 1], ['30', 30], ['500', 500], ['1200', 1200] ] as const;
  let created = 0;
  let previousTarget = 0;
  metrics.stageSeeding = {};
  for (const [stage, target] of stages) {
    const seedStart = Date.now();
    const pending: Array<Promise<void>> = [];
    while (created < target) {
      const n = created++;
      const recurrence = random() < .55 ? null : { intervalValue: 1 + Math.floor(random() * 3), intervalUnit: (['day', 'week', 'month', 'year'] as const)[Math.floor(random() * 4)], mode: random() < .5 ? 'fixed' : 'after_completion' };
      const title = random() < .05 ? `Kontrola nejneobhospodařovávatelnějšího příslušenství domácnosti ${n} ${'dlouhý'.repeat(18)}` : `Audit task ${String(n + 1).padStart(4, '0')}`;
      const day = Math.floor(random() * 90) - 45;
      const d = new Date(`${NOW}T00:00:00Z`); d.setUTCDate(d.getUTCDate() + day);
      const historical = Boolean(recurrence && random() < .12);
      const item: Record<string, unknown> = { title, recurrence, subjectId: activeSubjects[Math.floor(random() * activeSubjects.length)], attentionOn: historical || random() < .1 ? null : d.toISOString().slice(0, 10), ...(random() < .12 ? { notes: `Audit notes ${n}: ${'detail '.repeat(8)}` } : {}) };
      if (historical) item.historicalCompletedOn = '2025-12-01';
      const responsibleChoice = random();
      if (responsibleChoice < .34) item.responsibleUserId = ownerUserId;
      else if (responsibleChoice < .67) item.responsibleUserId = invited.userId;
      else item.responsibleUserId = null;
      pending.push((async () => {
        const response = await api(page, 'post', `${root}/items`, item, { 'Idempotency-Key': key() });
        expect(response.status(), `create item ${n}: ${await response.text()}`).toBe(201);
        const result = await response.json() as { id: string; recurrence: { mode: string } | null; done: boolean; archived: boolean; workflowState: string };
        createdItems.push({ generatorIndex: n, id: result.id, recurrence: result.recurrence, done: result.done, archived: result.archived, workflowState: result.workflowState });
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
      const accentQuery = 'kontrola nejneobhospodarovavatelnejsiho';
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
      await expect.poll(async () => (await (await api(page, 'get', `${root}/items/${target!.id}`)).json() as { workflowState: string }).workflowState).toBe('paused');
      await page.getByRole('link', { name: en['itemDetail.backAllItems'], exact: true }).click();
      await expect(page).toHaveURL(searchUrl);
      await expect(searchbox).toHaveValue(query);
      await expect(page.locator(`main li[data-item-id="${target!.id}"]`)).toContainText(en['items.group.paused']);
      await page.getByRole('link', { name: target!.title, exact: true }).click();
      await page.getByRole('button', { name: en['itemDetail.resume'], exact: true }).click();
      await expect.poll(async () => (await (await api(page, 'get', `${root}/items/${target!.id}`)).json() as { workflowState: string }).workflowState).toBe('open');
      await expect(page.getByRole('button', { name: en['itemDetail.pause'], exact: true })).toBeEnabled();
      await expect(page.getByRole('button', { name: en['itemDetail.resume'], exact: true })).toHaveCount(0);
      await capture(page, join(dir, 'audit-500-find-manage-detail-en-390.png'));
      metrics.stages['500-find-manage-en-390'] = { accentQuery, accentActive, findQuery: query, findMs, findApiCalls: apiCalls, domElements: await page.evaluate(() => document.getElementsByTagName('*').length) };
    }
    if (stage === '1200') {
      const tall = createdItems.find(item => item.recurrence?.mode === 'fixed' && !item.archived);
      expect(tall, 'recurring item fixture').toBeTruthy();
      const recurringHistoryItemId = tall!.id;
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
      for (const width of [320, 390, 768, 1440]) {
        await page.setViewportSize({ width, height: 900 }); await page.goto('/items');
        const range = new RegExp(en['allItems.range'].replace('{from}', '\\d+').replace('{to}', '\\d+').replace('{total}', '\\d+'));
        await expect(page.getByRole('main').getByRole('heading', { name: range })).toBeVisible();
        await capture(page, join(dir, `audit-all-items-en-${width}.png`));
        await page.getByRole('searchbox', { name: en['allItems.search'], exact: true }).fill('nejneobhospodařovávatelnějšího');
        await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
        await page.getByLabel(en['allItems.status.label']).selectOption('active');
        await expect(page.getByRole('main').getByRole('heading', { name: range })).toBeVisible();
        await expect(page.locator('main li[data-item-id]').first()).toBeVisible();
        await capture(page, join(dir, `audit-all-items-search-en-${width}.png`));
        await page.getByRole('searchbox', { name: en['allItems.search'], exact: true }).fill('');
        await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
        await page.getByLabel(en['allItems.status.label']).selectOption('done');
        await expect(page.getByRole('main').getByRole('heading', { name: range })).toBeVisible();
        await capture(page, join(dir, `audit-all-items-done-en-${width}.png`));
        await page.getByRole('searchbox', { name: en['allItems.search'], exact: true }).fill('audit-no-such-item-xyz');
        await page.getByRole('button', { name: en['allItems.searchButton'], exact: true }).click();
        await expect(page.getByRole('main').locator('p.font-display').getByText(en['allItems.noMatch'], { exact: true })).toBeVisible();
        await capture(page, join(dir, `audit-all-items-nomatch-en-${width}.png`));
        if (width === 390 || width === 1440) {
          const csItemsContext = await browser.newContext({ locale: 'cs-CZ', reducedMotion: 'reduce', viewport: { width, height: 900 } });
          await csItemsContext.addCookies(await page.context().cookies());
          const csItemsPage = await csItemsContext.newPage();
          await csItemsPage.goto('/'); await csItemsPage.evaluate(() => localStorage.setItem('tendo.locale', 'cs')); await csItemsPage.goto('/items');
          const csRange = new RegExp(cs['allItems.range'].replace('{from}', '\\d+').replace('{to}', '\\d+').replace('{total}', '\\d+'));
          await expect(csItemsPage.getByRole('main').getByRole('heading', { name: csRange })).toBeVisible();
          await capture(csItemsPage, join(dir, `audit-all-items-cs-${width}.png`));
          await csItemsContext.close();
        }
        await page.goto('/');
        await page.getByRole('button', { name: en['items.add'] }).click();
        await expect(page.getByRole('switch', { name: en['items.repeat'] })).toBeVisible();
        await page.getByRole('switch', { name: en['items.repeat'] }).check();
        await expect(page.getByText(en['items.fluid'], { exact: true })).toBeVisible();
        await capture(page, join(dir, `audit-add-form-en-${width}.png`));
        const csContext = await browser.newContext({ locale: 'cs-CZ', reducedMotion: 'reduce', viewport: { width, height: 900 } });
        await csContext.addCookies(await page.context().cookies());
        const csPage = await csContext.newPage();
        await csPage.goto('/'); await csPage.evaluate(() => localStorage.setItem('tendo.locale', 'cs')); await csPage.reload();
        await csPage.getByRole('button', { name: cs['items.add'] }).click();
        await expect(csPage.getByRole('switch', { name: cs['items.repeat'] })).toBeVisible();
        await csPage.getByRole('switch', { name: cs['items.repeat'] }).check();
        await expect(csPage.getByText(cs['items.fluid'], { exact: true })).toBeVisible();
        await capture(csPage, join(dir, `audit-add-form-cs-${width}.png`));
        await csPage.goto(`/items/${recurringHistoryItemId}`);
        await expect(csPage.getByRole('heading', { name: cs['itemDetail.history'] })).toBeVisible();
        await expect(csPage.getByRole('main').locator('ol li')).toHaveCount(2);
        await capture(csPage, join(dir, `audit-item-detail-cs-${width}.png`));
        await csContext.close();
        if (width === 390 || width === 1440) {
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
        await capture(page, join(dir, `audit-item-edit-en-${width}.png`));
        await page.goto('/');
        await page.getByRole('navigation', { name: en['nav.label'] }).getByRole('link', { name: en['home.people'], exact: true }).click();
        await expect(page.getByRole('heading', { name: en['subjects.title'] })).toBeVisible();
        await expect(page.getByText(en['subjects.loading'], { exact: true })).toHaveCount(0);
        await expect(page.getByRole('main').getByText('Anna Nováková', { exact: true })).toBeVisible();
        await expect(page.getByRole('main').getByText('Škoda Octavia', { exact: true })).toBeVisible();
        await capture(page, join(dir, `audit-people-things-en-${width}.png`));
        await page.goto('/');
        await page.getByRole('navigation', { name: en['nav.label'] }).getByRole('link', { name: en['home.members'], exact: true }).click();
        await expect(page.getByRole('heading', { name: en['members.title'] })).toBeVisible();
        await expect(page.getByText(en['members.loading'], { exact: true })).toHaveCount(0);
        await expect(page.getByRole('main').getByText(`audit_owner (${en['members.you']})`, { exact: true })).toBeVisible();
        await expect(page.getByRole('main').getByText('audit_member', { exact: true })).toBeVisible();
        await expect(page.getByRole('heading', { name: en['members.invitations.title'] })).toBeVisible();
        await capture(page, join(dir, `audit-members-en-${width}.png`));
        await page.getByRole('button', { name: 'Create invite link' }).click();
        await expect(page.getByLabel('Invitation link')).toBeVisible();
        await capture(page, join(dir, `audit-members-invite-en-${width}.png`));
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
    const widths = [320, 390, 768, 1440];
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
      const file = `home-${stage}-${locale}-${width}`;
      await capture(targetPage, join(dir, `${file}.png`));
      await capture(targetPage, join(dir, `${file}-fold.png`), false);
      metrics.stages[`${stage}-${locale}-${width}`] = { requests, requestCount: requests.length, responseBytes: requests.reduce((n, r) => n + r.bytes, 0), timeToContentMs, viewport: actualViewport, innerWidth, ...observed, showMoreVisible: await targetPage.locator('button[data-group-more]').first().isVisible().catch(() => false) };
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
        // Each group is capped at 100 rows; then its complete count is available in All items.
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
          await page.waitForLoadState('networkidle');
        }
        expect(expectedRows, `capped rows cannot exceed active item count at ${width}px`).toBeLessThanOrEqual(actualActiveTotal);
        expect(await page.locator('main li[data-item-id]').count(), `expanded rows at ${width}px`).toBe(expectedRows);
        expect(await page.locator('main a[data-group-all]').count()).toBe(expectedSeeAllLinks);
        (metrics.stages[`1200-en-${width}`] as Record<string, unknown>).showMoreClicks = clickMetrics;
        (metrics.stages[`1200-en-${width}`] as Record<string, unknown>).seeAllLinks = await page.locator('main a[data-group-all]').evaluateAll(links => links.map(link => ({ group: link.getAttribute('data-group-all'), label: link.textContent?.trim() })));
        await capture(page, join(dir, `home-1200-en-${width}-expanded.png`));
      }
    }
  }
  const activeItems = await verifyList(false, false);
  const doneItems = await verifyList(true, false);
  const archivedItems = await verifyList(false, true);
  const serverStates = activeItems.reduce<Record<string, number>>((counts, item) => { counts[item.workflowState] = (counts[item.workflowState] ?? 0) + 1; return counts; }, {});
  metrics.seededStateCounts = { ...serverStates, done: doneItems.length, archived: archivedItems.length, activeUndone: activeItems.length };
  expect(activeItems.length + doneItems.length + archivedItems.length).toBe(1200);
  metrics.stages.routeOverflow = routeOverflow;
  metrics.stages.routeOverflowElements = routeOverflowElements;
  writeFileSync(join(dir, 'metrics.json'), JSON.stringify(metrics, null, 2));
  const overflowFiles = Object.entries(routeOverflow).filter(([, overflow]) => overflow).map(([file]) => file);
  expect(overflowFiles, `route captures with horizontal overflow: ${overflowFiles.join(', ') || 'none'}`).toEqual([]);
 });
