import { expect, test, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { resolve, join } from 'node:path';
import { en } from '../src/i18n/en';
import { cs } from '../src/i18n/cs';

const dir = resolve(process.env.E2E_AUDIT_DIR!);
const baseURL = process.env.E2E_BASE_URL!;
test.describe.configure({ mode: 'serial' });
test.use({ reducedMotion: 'reduce' });
test.setTimeout(300_000);

async function api(page: Page, method: 'get' | 'post' | 'patch', path: string, data?: unknown, headers: Record<string, string> = {}) {
  return page.request[method](path, { headers: { Origin: baseURL, ...headers }, ...(data === undefined ? {} : { data }) });
}
async function capture(page: Page, name: string, width: number, fullPage = false) {
  await page.setViewportSize({ width, height: 844 });
  expect(await page.evaluate(() => innerWidth)).toBe(width);
  await page.screenshot({ path: join(dir, name), fullPage });
}
function key() { return crypto.randomUUID().replaceAll('-', ''); }

async function onboard(page: Page, name: string, login: string) {
  await page.goto('/');
  await page.getByLabel(en['onboarding.setupCode'], { exact: true }).fill(process.env.E2E_SETUP_TOKEN!);
  await page.getByLabel(en['onboarding.householdName']).fill(name);
  await page.locator('#onboarding-login').fill(login);
  await page.locator('#onboarding-password').fill(process.env.E2E_OWNER_PASSWORD!);
  await page.getByRole('button', { name: en['onboarding.submit'] }).click();
  await expect(page.getByRole('heading', { level: 1, name })).toBeVisible();
}

test('capture Direction A with real synthetic households', async ({ page, browser }) => {
  mkdirSync(dir, { recursive: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.clock.setFixedTime(new Date('2026-05-14T08:00:00Z'));
  await page.evaluate(() => { document.documentElement.dataset.testDate = '2026-05-14'; });
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: en['onboarding.title'] })).toBeVisible();
  await onboard(page, 'Dům U Lípy', 'direction_a_empty');
  await capture(page, 'home-empty-390-en.png', 390);
  await capture(page, 'home-empty-1440-en.png', 1440);

  // Seed the same public household only after the empty screen evidence is captured.
  await expect(page.getByRole('heading', { level: 1, name: 'Dům U Lípy' })).toBeVisible();
  const session = await api(page, 'get', '/api/v1/session');
  expect(session.status()).toBe(200);
  const { defaultHouseholdId: householdId, userId } = await session.json() as { defaultHouseholdId: string; userId: string };
  const root = `/api/v1/households/${householdId}`;
  const subjects: string[] = [];
  for (const subject of [{ name: 'Anna Nováková', type: 'person' }, { name: 'Tomáš Dvořák', type: 'person' }, { name: 'Dům', type: 'home' }, { name: 'Škoda Octavia', type: 'vehicle' }, { name: 'Rex', type: 'pet' }, { name: 'Zahrada', type: 'custom' }]) {
    const response = await api(page, 'post', `${root}/subjects`, subject, { 'Idempotency-Key': key() });
    expect(response.status()).toBe(201); subjects.push((await response.json() as { id: string }).id);
  }
  const items: Array<{ id: string; title: string }> = [];
  const titles = ['Zalít bylinky', 'Vyčistit filtr v konvici', 'Objednat krmivo pro Rexe', 'Kontrola nejneobhospodařovávatelnějšího příslušenství domácnosti'];
  for (let i = 0; i < 30; i++) {
    const body: Record<string, unknown> = { title: titles[i] ?? `Domácí úkol ${String(i + 1).padStart(2, '0')}`, subjectId: subjects[i % subjects.length], attentionOn: i % 4 === 0 ? null : '2026-03-30', responsibleUserId: i % 2 ? userId : null, recurrence: i % 3 === 0 ? { intervalValue: 1, intervalUnit: 'month', mode: i % 2 ? 'after_completion' : 'fixed' } : null };
    const response = await api(page, 'post', `${root}/items`, body, { 'Idempotency-Key': key() });
    expect(response.status()).toBe(201);
    const item = await response.json() as { id: string; title: string };
    items.push(item);
    if (i === 7 || i === 9) {
      const current = await api(page, 'get', `${root}/items/${item.id}`);
      const etag = current.headers()['etag']!;
      await api(page, 'patch', `${root}/items/${item.id}`, { workflowState: i === 7 ? 'in_progress' : 'waiting' }, { 'If-Match': etag });
    }
  }
  await page.setViewportSize({ width: 390, height: 844 }); await page.goto('/'); await expect(page.locator('main li[data-item-id]').first()).toBeVisible();
  for (const width of [360, 390, 768, 1024, 1440]) await capture(page, `home-populated-${width}-en.png`, width);
  await page.setViewportSize({ width: 1440, height: 844 }); await page.goto(`/items/${items[2]!.id}`); await expect(page.getByRole('heading', { level: 1, name: items[2]!.title })).toBeVisible(); await expect(page.locator('.desktop-item-shell main li[data-item-id]').first()).toBeVisible(); await capture(page, 'home-selected-pane-1440-en.png', 1440); await page.goBack(); await expect(page.locator('main li[data-item-id]').first()).toBeVisible();
  for (const width of [390, 1440]) {
    await page.setViewportSize({ width, height: 844 });
    await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs')); await page.reload();
    await expect(page.locator('main li[data-item-id]').first()).toBeVisible();
    await capture(page, `home-populated-${width}-cs.png`, width);
    await page.evaluate(() => localStorage.setItem('tendo.locale', 'en')); await page.reload();
  }
  await page.setViewportSize({ width: 1440, height: 844 }); await page.goto('/items'); await capture(page, 'all-items-1440-en.png', 1440);
  await page.setViewportSize({ width: 390, height: 844 }); await page.goto('/');
  await page.getByRole('button', { name: en['nav.more'], exact: true }).click();
  await expect(page.getByRole('group', { name: en['nav.more'] }).getByRole('link', { name: en['nav.members'], exact: true })).toBeVisible();
  await capture(page, 'navigation-more-390-en.png', 390);
  await page.getByRole('button', { name: en['nav.more'], exact: true }).click();
  await page.setViewportSize({ width: 390, height: 844 }); await page.goto('/');
  await page.getByRole('button', { name: en['items.add'], exact: true }).click();
  await expect(page.getByRole('dialog', { name: en['items.add.formLabel'] })).toBeVisible();
  await expect(page.getByRole('form', { name: en['items.add.formLabel'] })).toBeVisible();
  await capture(page, 'add-oneoff-390-en.png', 390);
  await page.getByRole('button', { name: en['items.cancel'], exact: true }).click();
  await page.setViewportSize({ width: 1440, height: 844 }); await page.goto('/');
  await page.getByRole('button', { name: en['items.add'], exact: true }).click();
  await capture(page, 'add-oneoff-1440-en.png', 1440);
  await page.getByRole('button', { name: en['items.cancel'], exact: true }).click();
  await page.setViewportSize({ width: 390, height: 844 }); await page.goto('/');
  await page.getByRole('button', { name: en['items.add'], exact: true }).click();
  await page.getByRole('switch', { name: en['items.repeat'] }).check();
  await page.getByRole('switch', { name: en['items.fluid'] }).check();
  await expect(page.getByText(en['items.fluid.on'], { exact: true })).toBeVisible();
  await capture(page, 'add-repeat-390-en.png', 390);
  await page.getByRole('button', { name: en['items.cancel'], exact: true }).click();
  await page.setViewportSize({ width: 1440, height: 844 }); await page.goto('/');
  await page.getByRole('button', { name: en['items.add'], exact: true }).click();
  await page.getByRole('switch', { name: en['items.repeat'] }).check(); await page.getByRole('switch', { name: en['items.fluid'] }).check();
  await capture(page, 'add-repeat-1440-en.png', 1440);
  await page.getByRole('button', { name: en['items.cancel'], exact: true }).click();
  await page.setViewportSize({ width: 390, height: 844 }); await page.evaluate(() => localStorage.setItem('tendo.locale', 'cs')); await page.reload();
  await page.getByRole('button', { name: cs['items.add'], exact: true }).click();
  await page.getByRole('switch', { name: cs['items.repeat'] }).check(); await page.getByRole('switch', { name: cs['items.fluid'] }).check();
  await capture(page, 'add-repeat-390-cs.png', 390);
  await page.getByRole('button', { name: cs['items.cancel'], exact: true }).click();
  await page.evaluate(() => localStorage.setItem('tendo.locale', 'en')); await page.reload();
  const detailId = items[2]!.id;
  await page.setViewportSize({ width: 390, height: 844 }); await page.goto(`/items/${detailId}`); await expect(page.getByRole('heading', { level: 1, name: items[2]!.title })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole('button', { name: en['itemDetail.edit.button'], exact: true }).click();
  const editForm = page.getByRole('form', { name: en['itemDetail.edit.formLabel'] });
  await expect(editForm).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 }); await capture(page, 'edit-390-en.png', 390);
  await editForm.getByRole('button', { name: en['items.cancel'], exact: true }).click();
  await page.setViewportSize({ width: 390, height: 844 }); await page.goto(`/items/${detailId}`); await capture(page, 'detail-390-en.png', 390);
  await page.setViewportSize({ width: 1440, height: 844 }); await page.goto(`/items/${detailId}`);
  await page.getByRole('button', { name: en['itemDetail.edit.button'], exact: true }).click();
  const wideEditForm = page.getByRole('form', { name: en['itemDetail.edit.formLabel'] }); await expect(wideEditForm).toBeVisible(); await capture(page, 'edit-1440-en.png', 1440);
  await wideEditForm.getByRole('button', { name: en['items.cancel'], exact: true }).click();
  await page.goto(`/items/${detailId}`); await capture(page, 'detail-1440-en.png', 1440);
  await page.setViewportSize({ width: 1440, height: 844 }); await page.goto(`/items/${detailId}`);
  await page.getByRole('button', { name: en['items.done'], exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: items[2]!.title })).toBeVisible();
  await expect(page.getByRole('button', { name: en['items.undo'], exact: true })).toBeVisible();
  await capture(page, 'detail-done-undo-1440-en.png', 1440);
  await page.getByRole('button', { name: en['items.undo'], exact: true }).click();
  await page.setViewportSize({ width: 390, height: 844 }); await page.goto(`/items/${detailId}`);
  await page.getByRole('button', { name: en['items.done'], exact: true }).click();
  await expect(page.getByRole('button', { name: en['items.undo'], exact: true })).toBeVisible();
  await capture(page, 'detail-done-undo-390-en.png', 390);
  await page.getByRole('button', { name: en['items.undo'], exact: true }).click();
  const csContext = await browser.newContext({ locale: 'cs-CZ', reducedMotion: 'reduce', viewport: { width: 390, height: 844 } });
  await csContext.clock.install({ time: new Date('2026-05-14T08:00:00Z') });
  await csContext.addCookies(await page.context().cookies());
  const csPage = await csContext.newPage(); await csPage.clock.setFixedTime(new Date('2026-05-14T08:00:00Z')); await csPage.evaluate(() => { document.documentElement.dataset.testDate = '2026-05-14'; }); await csPage.goto('/'); await csPage.evaluate(() => localStorage.setItem('tendo.locale', 'cs')); await csPage.reload();
  await csPage.setViewportSize({ width: 390, height: 844 }); await csPage.goto(`/items/${items[2]!.id}`); await expect(csPage.getByRole('heading', { level: 1 })).toBeVisible();
  await capture(csPage, 'detail-390-cs.png', 390); await csPage.setViewportSize({ width: 1440, height: 844 }); await capture(csPage, 'detail-1440-cs.png', 1440);
  await csContext.close();
});
