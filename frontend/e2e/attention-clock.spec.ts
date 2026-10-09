import { expect, test } from '@playwright/test';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { en } from '../src/i18n/en';

function required(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is required; run scripts/e2e-smoke.sh`);
  return value;
}

const baseURL = required('E2E_BASE_URL');
const ownerPassword = required('E2E_OWNER_PASSWORD');
const clockNow = required('E2E_CLOCK_NOW');
const phase = process.env.E2E_CLOCK_PHASE;
if (phase !== 'before' && phase !== 'after') throw new Error('E2E_CLOCK_PHASE must be before or after; run scripts/e2e-smoke.sh');
const fixturePath = required('E2E_CLOCK_FIXTURE_PATH');
const title = 'E2E clock boundary future item';

test.describe.configure({ mode: 'serial' });

test('future one-off item crosses midnight into needs attention', async ({ page }) => {
  expect(clockNow).toBe(phase === 'before' ? '2026-01-15T22:59:59Z' : '2026-01-15T23:00:00Z');
  let fixture: { id: string; etag: string; householdId: string; body: Record<string, unknown> };
  if (phase === 'after') {
    try { fixture = JSON.parse(readFileSync(fixturePath, 'utf8')) as typeof fixture; }
    catch { throw new Error('attention-clock.json fixture missing or invalid; run the before phase first'); }
  }

  await page.goto(new URL('/', baseURL).toString());
  await page.getByLabel(en['login.login']).fill('owner_e2e');
  await page.getByLabel(en['login.password']).fill(ownerPassword);
  await page.getByRole('button', { name: en['login.submit'], exact: true }).click();
  await expect(page.getByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible();
  const session = await page.request.get('/api/v1/session');
  expect(session.status()).toBe(200);
  const householdId = (await session.json() as { defaultHouseholdId: string }).defaultHouseholdId;
  const group = (name: string) => page.getByRole('heading', { level: 2, name, exact: true }).locator('xpath=ancestor::section[1]');
  const row = (name: string) => group(name).getByRole('listitem').filter({ hasText: title });

  if (phase === 'before') {
    await page.getByRole('button', { name: en['items.add'], exact: true }).click();
    const form = page.getByRole('form', { name: en['items.add.formLabel'] });
    await expect(form.getByRole('switch', { name: en['items.repeat'], exact: true })).not.toBeChecked();
    await form.getByLabel(en['items.title']).fill(title);
    await form.getByLabel(en['items.subject'], { exact: true }).selectOption({ label: 'Anička' });
    await form.getByLabel(en['items.attentionDate']).fill('2026-01-16');
    await form.getByRole('button', { name: en['items.add.submit'] }).click();
    await expect(form).toHaveCount(0);
    await expect(row(en['items.group.upcoming'])).toBeVisible();
    await expect(row(en['items.group.needs'])).toHaveCount(0);
    const items = await page.request.get(`/api/v1/households/${householdId}/items?done=false`);
    expect(items.status()).toBe(200);
    const found = ((await items.json()) as { items: Array<{ id: string; title: string }> }).items.find((item) => item.title === title);
    expect(found).toBeDefined();
    const response = await page.request.get(`/api/v1/households/${householdId}/items/${found!.id}`);
    expect(response.status()).toBe(200);
    fixture = { id: found!.id, etag: response.headers()['etag']!, householdId, body: await response.json() as Record<string, unknown> };
    expect(fixture.etag).toBeTruthy();
    expect(fixture.body).toMatchObject({ attention: 'upcoming', attentionOn: '2026-01-16', workflowState: 'open', done: false, archived: false, recurrence: null });
    const completions = await page.request.get(`/api/v1/households/${householdId}/items/${fixture.id}/completions`);
    expect(completions.status()).toBe(200);
    expect((await completions.json()).items).toEqual([]);
    mkdirSync(fixturePath.substring(0, fixturePath.lastIndexOf('/')), { recursive: true });
    writeFileSync(fixturePath, JSON.stringify(fixture), 'utf8');
  } else {
    expect(fixture!.householdId).toBe(householdId);
    await expect(row(en['items.group.needs'])).toBeVisible();
    await expect(row(en['items.group.upcoming'])).toHaveCount(0);
    const response = await page.request.get(`/api/v1/households/${householdId}/items/${fixture!.id}`);
    expect(response.status()).toBe(200);
    expect(response.headers()['etag']).toBe(fixture!.etag);
    const body = await response.json() as Record<string, unknown>;
    expect(body.id).toBe(fixture!.id);
    expect(body).toEqual({ ...fixture!.body, attention: 'needs_attention' });
    const completions = await page.request.get(`/api/v1/households/${householdId}/items/${fixture!.id}/completions`);
    expect(completions.status()).toBe(200);
    expect((await completions.json()).items).toEqual([]);

    const disposableTitle = 'E2E clock completion receipt item';
    await page.getByRole('button', { name: en['items.add'], exact: true }).click();
    const form = page.getByRole('form', { name: en['items.add.formLabel'] });
    await expect(form.getByRole('switch', { name: en['items.repeat'], exact: true })).not.toBeChecked();
    await form.getByLabel(en['items.title']).fill(disposableTitle);
    await form.getByLabel(en['items.subject'], { exact: true }).selectOption({ label: 'Anička' });
    await form.getByRole('button', { name: en['items.add.submit'] }).click();
    await expect(form).toHaveCount(0);
    const createdItems = await page.request.get(`/api/v1/households/${householdId}/items?done=false`);
    expect(createdItems.status()).toBe(200);
    const disposable = ((await createdItems.json()) as { items: Array<{ id: string; title: string }> }).items.find((item) => item.title === disposableTitle);
    expect(disposable).toBeDefined();
    const disposableResponse = await page.request.get(`/api/v1/households/${householdId}/items/${disposable!.id}`);
    expect(disposableResponse.status()).toBe(200);
    const done = await page.request.post(`/api/v1/households/${householdId}/items/${disposable!.id}/completions`, {
      headers: { Origin: baseURL, 'If-Match': disposableResponse.headers()['etag']!, 'Idempotency-Key': 'e2e-clock-completion-receipt' },
      data: {},
    });
    expect(done.status()).toBe(201);
    const receipt = await done.json() as { completedOn: string };
    expect(receipt.completedOn).toBe('2026-01-16');
  }
});
