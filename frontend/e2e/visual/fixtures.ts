import { expect, type Page } from '@playwright/test';
import { en } from '../../src/i18n/en';

export const BASE_URL = process.env.E2E_BASE_URL;
const SETUP_TOKEN = process.env.E2E_SETUP_TOKEN;
const OWNER_PASSWORD = process.env.E2E_OWNER_PASSWORD;
if (!BASE_URL || !SETUP_TOKEN || !OWNER_PASSWORD) throw new Error('visual native runner credentials are required');
export const HOUSEHOLD = 'Rodina Novákových';
export const OWNER_LOGIN = 'visual_owner';
export type Subject = { name: string; id: string };
export type VisualItem = { id: string; title: string };
export type Fixture = { householdId: string; apiRoot: string; ownerId: string; memberId: string; subjects: Subject[]; items: VisualItem[] };
export const idempotencyKey = () => crypto.randomUUID().replaceAll('-', '');

export async function api(page: Page, method: 'get' | 'post' | 'patch', path: string, data?: unknown, headers: Record<string, string> = {}) {
  return page.request[method](path, { headers: { Origin: BASE_URL!, ...headers }, ...(data === undefined ? {} : { data }) });
}

export async function firstRun(page: Page) {
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 1, name: en['onboarding.title'] })).toBeVisible();
  await page.locator('#onboarding-timezone').selectOption('Europe/Prague');
  await page.getByLabel(en['onboarding.setupCode'], { exact: true }).fill(SETUP_TOKEN!);
  await page.getByLabel(en['onboarding.householdName']).fill(HOUSEHOLD);
  await page.locator('#onboarding-login').fill(OWNER_LOGIN);
  await page.locator('#onboarding-password').fill(OWNER_PASSWORD!);
  await page.getByRole('button', { name: en['onboarding.submit'] }).click();
  await expect(page.getByRole('heading', { level: 1, name: HOUSEHOLD })).toBeVisible();
  const response = await api(page, 'get', '/api/v1/session');
  expect(response.status()).toBe(200);
  const session = await response.json() as { defaultHouseholdId: string; userId: string };
  return { householdId: session.defaultHouseholdId, apiRoot: `/api/v1/households/${session.defaultHouseholdId}`, ownerId: session.userId };
}

export async function seed(page: Page, identity: Awaited<ReturnType<typeof firstRun>>): Promise<Fixture> {
  const subjects: Subject[] = [];
  for (const subject of [
    { name: 'Šárka Černá – neobyčejně dlouhé jméno', type: 'person' },
    { name: 'Tomáš Dvořák', type: 'person' }, { name: 'Škoda Octavia', type: 'vehicle' }, { name: 'Domácnost', type: 'home' },
  ]) {
    const response = await api(page, 'post', `${identity.apiRoot}/subjects`, subject, { 'Idempotency-Key': idempotencyKey() });
    expect(response.status(), `create subject ${subject.name}`).toBe(201);
    subjects.push({ name: subject.name, id: (await response.json() as { id: string }).id });
  }
  const invite = await api(page, 'post', `${identity.apiRoot}/invitations`, {}, { 'Idempotency-Key': idempotencyKey() });
  expect(invite.status()).toBe(201);
  const inviteToken = (await invite.json() as { token: string }).token;
  const accepted = await api(page, 'post', '/api/v1/auth/invitations/accept', { token: inviteToken, login: 'visual_member', password: `${OWNER_PASSWORD}-member` });
  expect(accepted.status()).toBe(201);
  const memberId = (await accepted.json() as { userId: string }).userId;
  const items: VisualItem[] = [];
  const create = async (title: string, subject: string, extra: Record<string, unknown> = {}) => {
    const response = await api(page, 'post', `${identity.apiRoot}/items`, { title, subjectId: subjects.find(value => value.name === subject)?.id, recurrence: null, ...extra }, { 'Idempotency-Key': idempotencyKey() });
    expect(response.status(), `create ${title}`).toBe(201);
    items.push({ id: (await response.json() as { id: string }).id, title });
  };
  await create('Kontrola domácí lékárničky a bezpečného uložení přípravků', 'Domácnost', { notes: 'Dlouhá poznámka s českou diakritikou: žluťoučký kůň úpěl ďábelské ódy. '.repeat(3), responsibleUserId: identity.ownerId });
  await create('Výměna filtru', 'Domácnost', { responsibleUserId: memberId });
  await create('Péče o Škodu Octavia', 'Škoda Octavia', { recurrence: { intervalValue: 6, intervalUnit: 'month', mode: 'fixed' } });
  await create('Kontrola pojištění', 'Domácnost', { attentionOn: '2026-05-14' });
  return { ...identity, memberId, subjects, items };
}

export const ownerPassword = OWNER_PASSWORD;
