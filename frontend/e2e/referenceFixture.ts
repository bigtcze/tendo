import { expect, request as playwrightRequest, type Page } from '@playwright/test';

/** The human-approved V4 reference household and its ten named tasks (counts 4/2/1/3). */
export const REFERENCE_HOUSEHOLD = 'Novák family';
export const REFERENCE_OWNER_LOGIN = 'novak_owner';
export const REFERENCE_MEMBER_LOGIN = 'anna_novak';
export const REFERENCE_MEMBER_PASSWORD_SUFFIX = '-anna-member';
export const REFERENCE_TITLES = [
  "Book Anna's dental check-up",
  'Replace the smoke alarm battery',
  'Give Rex his flea treatment',
  "Renew Tom's passport",
  'Prepare the balcony planters',
  'Schedule the car inspection',
  'Change the water filter',
  'Review home insurance',
  'Sort the hallway cupboard',
  'Plan a family weekend away',
] as const;
export const REFERENCE_COUNTS = { needsAttention: 4, inProgress: 2, waiting: 1, upcoming: 3, paused: 0 } as const;

export type ReferenceSeed = {
  householdId: string;
  apiRoot: string;
  ownerId: string;
  memberId: string;
  subjects: Map<string, string>;
  items: Array<{ id: string; title: string }>;
};

const baseURL = process.env.E2E_BASE_URL;
if (!baseURL) throw new Error('E2E_BASE_URL is required for the reference fixture');

function idempotencyKey() {
  return crypto.randomUUID().replaceAll('-', '');
}

async function ownerRequest(page: Page, method: 'post' | 'patch', path: string, data: unknown, headers: Record<string, string> = {}) {
  return page.request[method](path, { headers: { Origin: baseURL!, ...headers }, data });
}

/** Seeds the approved reference household through the real API; the caller owns onboarding and the session. */
export async function seedReferenceFixture(
  page: Page,
  options: { householdId: string; apiRoot: string; ownerPassword: string },
): Promise<ReferenceSeed> {
  const { householdId, apiRoot, ownerPassword } = options;
  const subjects = new Map<string, string>();
  for (const subject of [
    { name: 'Anna', type: 'person' },
    { name: 'Tom', type: 'person' },
    { name: 'Home', type: 'home' },
    { name: 'Škoda Octavia', type: 'vehicle' },
    { name: 'Rex', type: 'pet' },
  ]) {
    const response = await ownerRequest(page, 'post', `${apiRoot}/subjects`, subject, { 'Idempotency-Key': idempotencyKey() });
    expect(response.status(), `create subject ${subject.name}`).toBe(201);
    subjects.set(subject.name, (await response.json() as { id: string }).id);
  }

  const invitation = await ownerRequest(page, 'post', `${apiRoot}/invitations`, {}, { 'Idempotency-Key': idempotencyKey() });
  expect(invitation.status(), 'create member invitation').toBe(201);
  const { token } = await invitation.json() as { token: string };
  expect(token).toBeTruthy();
  const memberApi = await playwrightRequest.newContext({ baseURL });
  try {
    const accepted = await memberApi.post('/api/v1/auth/invitations/accept', {
      headers: { Origin: baseURL! },
      data: { token, login: REFERENCE_MEMBER_LOGIN, password: `${ownerPassword}${REFERENCE_MEMBER_PASSWORD_SUFFIX}` },
    });
    expect(accepted.status(), 'accept member invitation').toBe(201);
  } finally {
    await memberApi.dispose();
  }

  const membersResponse = await page.request.get(`${apiRoot}/members`);
  expect(membersResponse.status()).toBe(200);
  const members = (await membersResponse.json() as { items: Array<{ userId: string; login: string }> }).items;
  const ownerId = members.find((member) => member.login === REFERENCE_OWNER_LOGIN)?.userId;
  const memberId = members.find((member) => member.login === REFERENCE_MEMBER_LOGIN)?.userId;
  expect(ownerId, 'owner member id').toBeTruthy();
  expect(memberId, 'Anna member id').toBeTruthy();

  const fixedYear = { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' as const };
  const fixedQuarter = { intervalValue: 3, intervalUnit: 'month', mode: 'fixed' as const };
  const fluidMonth = { intervalValue: 1, intervalUnit: 'month', mode: 'after_completion' as const };
  const items: Array<{ id: string; title: string }> = [];
  const createItem = async (item: {
    title: string; subject: string; attentionOn?: string; historicalCompletedOn?: string;
    recurrence?: typeof fixedYear | typeof fixedQuarter | typeof fluidMonth; responsibleUserId?: string;
  }) => {
    const response = await ownerRequest(page, 'post', `${apiRoot}/items`, {
      title: item.title,
      subjectId: subjects.get(item.subject),
      ...(item.attentionOn !== undefined ? { attentionOn: item.attentionOn } : {}),
      ...(item.historicalCompletedOn ? { historicalCompletedOn: item.historicalCompletedOn } : {}),
      recurrence: item.recurrence ?? null,
      ...(item.responsibleUserId !== undefined ? { responsibleUserId: item.responsibleUserId } : {}),
    }, { 'Idempotency-Key': idempotencyKey() });
    expect(response.status(), `create item ${item.title}`).toBe(201);
    items.push({ id: (await response.json() as { id: string }).id, title: item.title });
  };

  await createItem({ title: "Book Anna's dental check-up", subject: 'Anna', historicalCompletedOn: '2025-05-14', recurrence: fixedYear, responsibleUserId: memberId });
  await createItem({ title: 'Replace the smoke alarm battery', subject: 'Home', responsibleUserId: ownerId });
  await createItem({ title: 'Give Rex his flea treatment', subject: 'Rex', attentionOn: '2026-05-10', recurrence: fluidMonth, responsibleUserId: ownerId });
  await createItem({ title: "Renew Tom's passport", subject: 'Tom', attentionOn: '2026-05-12', responsibleUserId: memberId });
  await createItem({ title: 'Prepare the balcony planters', subject: 'Home', attentionOn: '2026-05-13' });
  await createItem({ title: 'Schedule the car inspection', subject: 'Škoda Octavia', attentionOn: '2026-05-28', recurrence: fixedYear, responsibleUserId: memberId });
  await createItem({ title: 'Change the water filter', subject: 'Home', recurrence: fixedQuarter, historicalCompletedOn: '2026-03-14', responsibleUserId: ownerId });
  await createItem({ title: 'Review home insurance', subject: 'Home', attentionOn: '2026-11-14', recurrence: fixedYear, responsibleUserId: memberId });
  await createItem({ title: 'Sort the hallway cupboard', subject: 'Home', attentionOn: '2026-05-18', responsibleUserId: ownerId });
  await createItem({ title: 'Plan a family weekend away', subject: 'Tom', attentionOn: '2026-06-20' });

  const setWorkflow = async (itemId: string, workflowState: 'in_progress' | 'waiting' | 'paused') => {
    const current = await page.request.get(`${apiRoot}/items/${itemId}`);
    expect(current.status()).toBe(200);
    const updated = await ownerRequest(page, 'patch', `${apiRoot}/items/${itemId}`, { workflowState }, { 'If-Match': current.headers()['etag']! });
    expect(updated.status(), `set ${workflowState}`).toBe(200);
  };
  await setWorkflow(items[4]!.id, 'in_progress');
  await setWorkflow(items[8]!.id, 'in_progress');
  await setWorkflow(items[9]!.id, 'waiting');

  return { householdId, apiRoot, ownerId: ownerId!, memberId: memberId!, subjects, items };
}
