import { expect, request as playwrightRequest, test } from '@playwright/test';
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

  const invitation = await apiRequest(page, 'post', `${apiRoot}/invitations`, {}, { 'Idempotency-Key': crypto.randomUUID().replaceAll('-', '') });
  expect(invitation.status(), 'create member invitation').toBe(201);
  const { token } = await invitation.json() as { token: string };
  expect(token).toBeTruthy();
  const memberApi = await playwrightRequest.newContext({ baseURL });
  try {
    const accepted = await memberApi.post('/api/v1/auth/invitations/accept', {
      headers: { Origin: baseURL },
      data: { token, login: 'anna_novak', password: `${ownerPassword}-anna-member` },
    });
    expect(accepted.status(), 'accept member invitation').toBe(201);
  } finally {
    await memberApi.dispose();
  }
  const membersResponse = await page.request.get(`${apiRoot}/members`);
  expect(membersResponse.status()).toBe(200);
  const members = (await membersResponse.json() as { items: Array<{ userId: string; login: string }> }).items;
  const ownerId = members.find((member) => member.login === ownerLogin)?.userId;
  const annaId = members.find((member) => member.login === 'anna_novak')?.userId;
  expect(ownerId, 'owner member id').toBeTruthy();
  expect(annaId, 'Anna member id').toBeTruthy();

  async function createItem(item: { title: string; subject: string; attentionOn?: string | null; historicalCompletedOn?: string; recurrence?: { intervalValue: number; intervalUnit: string; mode: 'fixed' | 'after_completion' }; responsibleUserId?: string | null; notes?: string }) {
    const response = await apiRequest(page, 'post', `${apiRoot}/items`, {
      title: item.title,
      subjectId: subjects.get(item.subject),
      ...(item.attentionOn !== undefined ? { attentionOn: item.attentionOn } : {}),
      ...(item.historicalCompletedOn ? { historicalCompletedOn: item.historicalCompletedOn } : {}),
      recurrence: item.recurrence ?? null,
      ...(item.responsibleUserId !== undefined ? { responsibleUserId: item.responsibleUserId } : {}),
      ...(item.notes ? { notes: item.notes } : {}),
    }, { 'Idempotency-Key': crypto.randomUUID().replaceAll('-', '') });
    expect(response.status(), `create item ${item.title}`).toBe(201);
    return (await response.json()) as { id: string };
  }

  async function setWorkflow(itemId: string, workflowState: 'in_progress' | 'waiting' | 'paused') {
    const current = await page.request.get(`${apiRoot}/items/${itemId}`);
    expect(current.status()).toBe(200);
    const updated = await apiRequest(page, 'patch', `${apiRoot}/items/${itemId}`, { workflowState }, { 'If-Match': current.headers()['etag']! });
    expect(updated.status(), `set ${workflowState}`).toBe(200);
  }

  const fixedYear = { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' as const };
  const fixedQuarter = { intervalValue: 3, intervalUnit: 'month', mode: 'fixed' as const };
  const fluidMonth = { intervalValue: 1, intervalUnit: 'month', mode: 'after_completion' as const };
  const items = [
    await createItem({ title: "Book Anna's dental check-up", subject: 'Anna', historicalCompletedOn: '2025-05-14', recurrence: fixedYear, responsibleUserId: annaId }),
    await createItem({ title: 'Replace the smoke alarm battery', subject: 'Home', responsibleUserId: ownerId }),
    await createItem({ title: 'Give Rex his flea treatment', subject: 'Rex', attentionOn: '2026-05-10', recurrence: fluidMonth, responsibleUserId: ownerId }),
    await createItem({ title: "Renew Tom's passport", subject: 'Tom', attentionOn: '2026-05-12', responsibleUserId: annaId }),
    await createItem({ title: 'Prepare the balcony planters', subject: 'Home', attentionOn: '2026-05-13' }),
    await createItem({ title: 'Schedule the car inspection', subject: 'Škoda Octavia', attentionOn: '2026-05-28', recurrence: fixedYear, responsibleUserId: annaId }),
    await createItem({ title: 'Change the water filter', subject: 'Home', recurrence: fixedQuarter, historicalCompletedOn: '2026-03-14', responsibleUserId: ownerId }),
    await createItem({ title: 'Review home insurance', subject: 'Home', attentionOn: '2026-11-14', recurrence: fixedYear, responsibleUserId: annaId }),
    await createItem({ title: 'Sort the hallway cupboard', subject: 'Home', attentionOn: '2026-05-18', responsibleUserId: ownerId }),
    await createItem({ title: 'Plan a family weekend away', subject: 'Tom', attentionOn: '2026-06-20' }),
  ];
  await setWorkflow(items[4]!.id, 'in_progress');
  await setWorkflow(items[8]!.id, 'in_progress');
  await setWorkflow(items[9]!.id, 'waiting');

  await page.reload();
  await page.setViewportSize({ width: 1280, height: 800 });
  await page.waitForLoadState('networkidle');
  await expect(page.getByRole('heading', { level: 1, name: householdName })).toBeVisible();
  const escapeRegExp = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  for (const heading of [en['items.group.needs'], en['items.group.inProgress'], en['items.group.waiting'], en['items.group.upcoming']]) {
    await expect(page.getByRole('heading', { level: 2, name: new RegExp(`^${escapeRegExp(heading)}(\\s|$)`) })).toBeVisible();
  }
  for (const title of [
    "Book Anna's dental check-up", 'Replace the smoke alarm battery', 'Give Rex his flea treatment',
    "Renew Tom's passport", 'Prepare the balcony planters', 'Schedule the car inspection',
    'Change the water filter', 'Review home insurance',
    'Sort the hallway cupboard', 'Plan a family weekend away',
  ]) await expect(page.getByText(title, { exact: true })).toBeVisible();
  await expect(page.getByRole('navigation', { name: en['items.group.overview'] })).toBeVisible();
  await expect(page.getByRole('navigation', { name: en['items.group.overview'] }).getByRole('link')).toHaveText([
    `${en['items.group.needs']} 4`, `${en['items.group.inProgress']} 2`, `${en['items.group.waiting']} 1`, `${en['items.group.upcoming']} 3`,
  ]);
  await expect(page.getByText(en['items.responsible.read'].replace('{name}', 'anna_novak'), { exact: true }).first()).toBeVisible();
  await expect(page.getByText(en['items.responsible.you'], { exact: true }).first()).toBeVisible();

  mkdirSync(dirname(screenshotPath), { recursive: true });
  await page.screenshot({ path: screenshotPath, fullPage: true });
});
