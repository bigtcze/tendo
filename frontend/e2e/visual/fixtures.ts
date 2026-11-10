import { expect, type BrowserContext, type Page } from '@playwright/test';
import type { components } from '../../../api/generated/api';
import { en } from '../../src/i18n/en';
import { REFERENCE_HOUSEHOLD, REFERENCE_OWNER_LOGIN } from '../referenceFixture';

export const BASE_URL = process.env.E2E_BASE_URL;
const SETUP_TOKEN = process.env.E2E_SETUP_TOKEN;
const OWNER_PASSWORD = process.env.E2E_OWNER_PASSWORD;
if (!BASE_URL || !SETUP_TOKEN || !OWNER_PASSWORD) throw new Error('visual native runner credentials are required');
export const HOUSEHOLD = REFERENCE_HOUSEHOLD;
export const OWNER_LOGIN = REFERENCE_OWNER_LOGIN;
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
  // Šárka is a non-member person (a child); Tomáš is the invited member, so his person is the auto-provisioned one.
  for (const subject of [
    { name: 'Šárka Černá – neobyčejně dlouhé jméno', type: 'person' },
    { name: 'Škoda Octavia', type: 'vehicle' }, { name: 'Domácnost', type: 'home' },
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
  // Reuse the member's canonical person, renamed for readable visual copy, instead of a duplicate standalone person.
  const memberPerson = await api(page, 'get', `${identity.apiRoot}/members/${memberId}/person`);
  expect(memberPerson.status(), 'read visual member person').toBe(200);
  const memberPersonBody = await memberPerson.json() as { id: string };
  const renamedMember = await api(page, 'patch', `${identity.apiRoot}/subjects/${memberPersonBody.id}`,
    { name: 'Tomáš Dvořák' }, { 'If-Match': memberPerson.headers()['etag'] ?? '' });
  expect(renamedMember.status(), 'rename visual member person').toBe(200);
  subjects.push({ name: 'Tomáš Dvořák', id: memberPersonBody.id });
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

// TENDO_TEST_CLOCK_NOW fixes item business dates only; invitation timestamps come from the server's real clock,
// so the rendered "Created … · Expires …" line would change every day. Pin those fields in invitation list
// responses to the fixed visual instant (lifetime 7 days, as on the server); everything else stays the real response.
const INVITATION_CREATED_AT = '2026-05-14T08:00:00Z';
const INVITATION_EXPIRES_AT = '2026-05-21T08:00:00Z';

export async function pinInvitationDates(context: BrowserContext) {
  await context.route((url) => /^\/api\/v1\/households\/[^/]+\/invitations$/.test(url.pathname), async (route) => {
    if (route.request().method() !== 'GET') return route.fallback();
    const response = await route.fetch();
    if (response.status() !== 200) return route.fulfill({ response });
    const page = await response.json() as components['schemas']['InvitationList'];
    const items = page.items.map((invitation) => ({
      ...invitation, createdAt: INVITATION_CREATED_AT, expiresAt: INVITATION_EXPIRES_AT,
      acceptedAt: invitation.acceptedAt && INVITATION_CREATED_AT, revokedAt: invitation.revokedAt && INVITATION_CREATED_AT,
    }));
    await route.fulfill({ response, json: { ...page, items } });
  });
}
