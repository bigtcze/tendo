import { expect, test } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { en } from '../src/i18n/en';

function required(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required; run scripts/e2e-smoke.sh with TENDO_README_SCREENSHOT`);
  return value;
}

const baseURL = required('E2E_BASE_URL');
const setupToken = required('E2E_SETUP_TOKEN');
const ownerPassword = required('E2E_OWNER_PASSWORD');
const screenshotPath = resolve(required('E2E_SCREENSHOT_PATH'));
const householdName = 'Novák family';
const ownerLogin = 'novak_owner';

async function apiRequest(page: import('@playwright/test').Page, method: 'post' | 'patch', path: string, data: unknown, headers: Record<string, string> = {}) {
  return page.request[method](path, {
    headers: { Origin: baseURL, ...headers },
    data,
  });
}

test('capture populated README household home screen', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: en['onboarding.title'] })).toBeVisible();
  await page.locator('#onboarding-timezone').selectOption('Europe/Prague');
  await page.getByLabel(en['onboarding.setupCode'], { exact: true }).fill(setupToken);
  await page.getByLabel(en['onboarding.householdName']).fill(householdName);
  await page.locator('#onboarding-login').fill(ownerLogin);
  await page.locator('#onboarding-password').fill(ownerPassword);
  await page.getByRole('button', { name: en['onboarding.submit'] }).click();
  await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();

  const sessionResponse = await page.request.get('/api/v1/session');
  expect(sessionResponse.status()).toBe(200);
  const { defaultHouseholdId: householdId } = await sessionResponse.json() as { defaultHouseholdId: string };
  expect(householdId).toMatch(/^[0-9a-f-]{36}$/);
  const apiRoot = `/api/v1/households/${householdId}`;

  const subjects = new Map<string, string>();
  for (const subject of [
    { name: 'Anna', type: 'person' },
    { name: 'Tom', type: 'person' },
    { name: 'Home', type: 'home' },
    { name: 'Škoda Octavia', type: 'vehicle' },
    { name: 'Rex', type: 'pet' },
  ]) {
    const response = await apiRequest(page, 'post', `${apiRoot}/subjects`, subject, { 'Idempotency-Key': crypto.randomUUID().replaceAll('-', '') });
    expect(response.status(), `create subject ${subject.name}`).toBe(201);
    subjects.set(subject.name, (await response.json() as { id: string }).id);
  }

  async function createItem(item: { title: string; subject: string; attentionOn?: string | null; historicalCompletedOn?: string; recurrence?: { intervalValue: number; intervalUnit: string; mode: 'fixed' | 'after_completion' }; notes?: string }) {
    const response = await apiRequest(page, 'post', `${apiRoot}/items`, {
      title: item.title,
      subjectId: subjects.get(item.subject),
      ...(item.attentionOn !== undefined ? { attentionOn: item.attentionOn } : {}),
      ...(item.historicalCompletedOn ? { historicalCompletedOn: item.historicalCompletedOn } : {}),
      recurrence: item.recurrence ?? null,
      ...(item.notes ? { notes: item.notes } : {}),
    }, { 'Idempotency-Key': crypto.randomUUID().replaceAll('-', '') });
    expect(response.status(), `create item ${item.title}`).toBe(201);
    return (await response.json()) as { id: string };
  }

  await createItem({ title: "Book Anna's dental check-up", subject: 'Anna', historicalCompletedOn: '2025-04-14', recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' } });
  await createItem({ title: 'Replace the smoke alarm battery', subject: 'Home' });
  const passport = await createItem({ title: "Renew Tom's passport", subject: 'Tom' });
  const passportResponse = await page.request.get(`${apiRoot}/items/${passport.id}`);
  expect(passportResponse.status()).toBe(200);
  const passportEtag = passportResponse.headers()['etag'];
  expect(passportEtag).toBeTruthy();
  const started = await apiRequest(page, 'patch', `${apiRoot}/items/${passport.id}`, { workflowState: 'in_progress' }, { 'If-Match': passportEtag! });
  expect(started.status()).toBe(200);

  const waitingItem = await createItem({ title: 'Car service — waiting for parts', subject: 'Škoda Octavia', notes: 'The garage will call when the part arrives.' });
  const waitingGet = await page.request.get(`${apiRoot}/items/${waitingItem.id}`);
  const waitingEtag = waitingGet.headers()['etag'];
  expect(waitingEtag).toBeTruthy();
  const waiting = await apiRequest(page, 'patch', `${apiRoot}/items/${waitingItem.id}`, { workflowState: 'waiting' }, { 'If-Match': waitingEtag! });
  expect(waiting.status()).toBe(200);

  await createItem({ title: "Rex's vaccination", subject: 'Rex', attentionOn: '2026-10-14', recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' } });
  await createItem({ title: 'Change the water filter', subject: 'Home', attentionOn: '2026-08-14', recurrence: { intervalValue: 3, intervalUnit: 'month', mode: 'fixed' } });
  await createItem({ title: 'Pay home insurance', subject: 'Home', attentionOn: '2026-11-14', recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' } });
  await page.reload();
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.waitForLoadState('networkidle');
  await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
  const escapeRegExp = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  for (const heading of [en['items.group.needs'], en['items.group.inProgress'], en['items.group.waiting'], en['items.group.upcoming']]) {
    await expect(page.getByRole('heading', { level: 2, name: new RegExp(`^${escapeRegExp(heading)}(\\s|$)`) })).toBeVisible();
  }
  for (const title of [
    "Book Anna's dental check-up",
    'Replace the smoke alarm battery',
    'Car service — waiting for parts',
    "Rex's vaccination",
    'Change the water filter',
    'Pay home insurance',
  ]) await expect(page.getByText(title, { exact: true })).toBeVisible();

  mkdirSync(dirname(screenshotPath), { recursive: true });
  await page.screenshot({ path: screenshotPath, fullPage: true });
});
