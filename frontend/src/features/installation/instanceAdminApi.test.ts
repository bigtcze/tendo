import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  assignAdminHouseholdAccount, changeAdminHouseholdAccountRole, listAdminDirectory, listAdminHouseholdAccounts,
  listAdminHouseholds, readAdminHouseholdAccount, removeAdminHouseholdAccount,
  type AdminHousehold, type AdminHouseholdAccount,
} from './instanceAdminApi';
import { HOUSEHOLD_ID, installFakeServer, json } from '../../test/fakeServer';

const householdsPath = 'GET /api/v1/admin/households';
const directoryPath = 'GET /api/v1/admin/accounts';
const accountsPath = `GET /api/v1/admin/households/${HOUSEHOLD_ID}/accounts`;
const userId = '0198a2f0-0000-7000-8000-000000000002';
const accountPath = `/api/v1/admin/households/${HOUSEHOLD_ID}/accounts/${userId}`;
const household: AdminHousehold = {
  id: HOUSEHOLD_ID, name: 'Veselí', timezone: 'Europe/Prague', createdAt: '2026-10-06T10:00:00Z',
};
const account: AdminHouseholdAccount = {
  userId, login: 'petr', role: 'member', signInMethods: { password: true, oidc: false },
};
const problem = (status: number, code?: string) => json(status, { type: 'about:blank', title: 'x', status, ...(code ? { code } : {}) });

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('installation Admin center API', () => {
  it('pages both lists with the fixed page size and passes the cursor through', async () => {
    const fake = installFakeServer({
      [householdsPath]: json(200, { items: [household], nextCursor: 'h2' }),
      [accountsPath]: json(200, { items: [account], nextCursor: null }),
    });
    await expect(listAdminHouseholds()).resolves.toEqual({ kind: 'ok', items: [household], nextCursor: 'h2' });
    await expect(listAdminHouseholdAccounts(HOUSEHOLD_ID, 'c1')).resolves.toEqual({ kind: 'ok', items: [account], nextCursor: null });
    expect(fake.requests[0].query.get('limit')).toBe('50');
    expect(fake.requests[0].query.get('cursor')).toBeNull();
    expect(fake.requests[1].query.get('limit')).toBe('50');
    expect(fake.requests[1].query.get('cursor')).toBe('c1');
  });

  it('separates the missing installation grant from every other refusal', async () => {
    installFakeServer({ [householdsPath]: problem(403, 'instance_admin_required') });
    await expect(listAdminHouseholds()).resolves.toEqual({ kind: 'forbidden' });

    installFakeServer({ [householdsPath]: problem(403, 'forbidden_origin') });
    await expect(listAdminHouseholds()).resolves.toEqual({ kind: 'rejected' });

    // A household-admin refusal is not the installation one: it must not read as "only an installation administrator".
    installFakeServer({ [householdsPath]: problem(403, 'admin_required') });
    await expect(listAdminHouseholds()).resolves.toEqual({ kind: 'rejected' });
  });

  it('maps the remaining statuses the same way for both lists', async () => {
    for (const [status, expected] of [
      [401, { kind: 'unauthenticated' }],
      [404, { kind: 'notFound' }],
      [500, { kind: 'error' }],
    ] as const) {
      installFakeServer({ [householdsPath]: problem(status), [accountsPath]: problem(status) });
      await expect(listAdminHouseholds()).resolves.toEqual(expected);
      await expect(listAdminHouseholdAccounts(HOUSEHOLD_ID)).resolves.toEqual(expected);
    }

    installFakeServer({ [accountsPath]: problem(403, 'instance_admin_required') });
    await expect(listAdminHouseholdAccounts(HOUSEHOLD_ID)).resolves.toEqual({ kind: 'forbidden' });
  });

  it('reports an unreachable server as the common unavailable state', async () => {
    installFakeServer({ [householdsPath]: () => Promise.reject(new Error('offline')), [accountsPath]: () => Promise.reject(new Error('offline')) });
    await expect(listAdminHouseholds()).resolves.toEqual({ kind: 'error' });
    await expect(listAdminHouseholdAccounts(HOUSEHOLD_ID)).resolves.toEqual({ kind: 'error' });
  });
});

describe('installation account directory', () => {
  it('searches with the trimmed login substring, the fixed page size and the cursor', async () => {
    const fake = installFakeServer({
      [directoryPath]: json(200, { items: [{ userId, login: 'petr' }], nextCursor: 'd2' }),
    });
    await expect(listAdminDirectory('  petr  ')).resolves.toEqual({ kind: 'ok', items: [{ userId, login: 'petr' }], nextCursor: 'd2' });
    expect(fake.requests[0].query.get('q')).toBe('petr');
    expect(fake.requests[0].query.get('limit')).toBe('20');
    expect(fake.requests[0].query.get('cursor')).toBeNull();

    await listAdminDirectory('petr', 'c1');
    expect(fake.requests[1].query.get('q')).toBe('petr');
    expect(fake.requests[1].query.get('cursor')).toBe('c1');
  });

  it('sends no q at all for a blank search, so the directory lists from the start', async () => {
    const fake = installFakeServer({ [directoryPath]: json(200, { items: [], nextCursor: null }) });
    await expect(listAdminDirectory('   ')).resolves.toEqual({ kind: 'ok', items: [], nextCursor: null });
    expect(fake.requests[0].query.has('q')).toBe(false);
  });

  it('reads the missing grant as forbidden and a household refusal as rejected', async () => {
    installFakeServer({ [directoryPath]: problem(403, 'instance_admin_required') });
    await expect(listAdminDirectory('')).resolves.toEqual({ kind: 'forbidden' });
    installFakeServer({ [directoryPath]: problem(403, 'admin_required') });
    await expect(listAdminDirectory('')).resolves.toEqual({ kind: 'rejected' });
    installFakeServer({ [directoryPath]: problem(401) });
    await expect(listAdminDirectory('')).resolves.toEqual({ kind: 'unauthenticated' });
  });

  it('separates a refused search text from a search that simply failed', async () => {
    installFakeServer({ [directoryPath]: problem(400, 'invalid_query') });
    await expect(listAdminDirectory('x'.repeat(65))).resolves.toEqual({ kind: 'invalidQuery' });
    // A bad request that is not about the search text stays the common failure the dialog offers a retry for.
    installFakeServer({ [directoryPath]: problem(400, 'invalid_forwarded_request') });
    await expect(listAdminDirectory('')).resolves.toEqual({ kind: 'error' });
    installFakeServer({ [directoryPath]: problem(503) });
    await expect(listAdminDirectory('')).resolves.toEqual({ kind: 'error' });
  });
});

describe('installation household account actions', () => {
  it('reads one membership only with its strong version tag', async () => {
    installFakeServer({ [`GET ${accountPath}`]: json(200, account) });
    await expect(readAdminHouseholdAccount(HOUSEHOLD_ID, userId)).resolves.toEqual({ kind: 'error' });

    installFakeServer({ [`GET ${accountPath}`]: json(200, account, { ETag: '"4"' }) });
    await expect(readAdminHouseholdAccount(HOUSEHOLD_ID, userId)).resolves.toEqual({ kind: 'ok', account, etag: '"4"' });
  });

  it('assigns with PUT and separates a creation from the idempotent replay and the conflicts', async () => {
    const fake = installFakeServer({ [`PUT ${accountPath}`]: json(201, account, { ETag: '"1"' }) });
    await expect(assignAdminHouseholdAccount(HOUSEHOLD_ID, userId, 'member')).resolves.toEqual({ kind: 'created', account });
    expect(fake.requests[0].body).toBe('{"role":"member"}');

    installFakeServer({ [`PUT ${accountPath}`]: json(200, account) });
    await expect(assignAdminHouseholdAccount(HOUSEHOLD_ID, userId, 'member')).resolves.toEqual({ kind: 'unchanged', account });

    const refuse = (status: number, code: string) => installFakeServer({ [`PUT ${accountPath}`]: problem(status, code) });
    refuse(409, 'already_member');
    await expect(assignAdminHouseholdAccount(HOUSEHOLD_ID, userId, 'member')).resolves.toEqual({ kind: 'alreadyMember' });
    refuse(409, 'household_requires_admin');
    await expect(assignAdminHouseholdAccount(HOUSEHOLD_ID, userId, 'member')).resolves.toEqual({ kind: 'requiresAdmin' });
    refuse(409, 'member_person_changed');
    await expect(assignAdminHouseholdAccount(HOUSEHOLD_ID, userId, 'member')).resolves.toEqual({ kind: 'rejected' });
    refuse(404, 'not_found');
    await expect(assignAdminHouseholdAccount(HOUSEHOLD_ID, userId, 'member')).resolves.toEqual({ kind: 'notFound' });
  });

  it('sends the exact If-Match on a role change and separates last admin from a stale tag', async () => {
    const fake = installFakeServer({ [`PATCH ${accountPath}`]: json(200, { ...account, role: 'admin' }, { ETag: '"2"' }) });
    await expect(changeAdminHouseholdAccountRole(HOUSEHOLD_ID, userId, 'admin', '"4"'))
      .resolves.toEqual({ kind: 'ok', account: { ...account, role: 'admin' } });
    expect(fake.requests[0].headers.get('If-Match')).toBe('"4"');
    expect(fake.requests[0].body).toBe('{"role":"admin"}');

    const refuse = (status: number, code: string) => installFakeServer({ [`PATCH ${accountPath}`]: problem(status, code) });
    refuse(409, 'last_admin');
    await expect(changeAdminHouseholdAccountRole(HOUSEHOLD_ID, userId, 'member', '"4"')).resolves.toEqual({ kind: 'lastAdmin' });
    refuse(409, 'member_person_changed');
    await expect(changeAdminHouseholdAccountRole(HOUSEHOLD_ID, userId, 'member', '"4"')).resolves.toEqual({ kind: 'rejected' });
    refuse(412, 'precondition_failed');
    await expect(changeAdminHouseholdAccountRole(HOUSEHOLD_ID, userId, 'member', '"4"')).resolves.toEqual({ kind: 'stale' });
    refuse(404, 'not_found');
    await expect(changeAdminHouseholdAccountRole(HOUSEHOLD_ID, userId, 'member', '"4"')).resolves.toEqual({ kind: 'notFound' });
  });

  it('sends the exact If-Match on a removal and separates last admin from a stale tag', async () => {
    const fake = installFakeServer({ [`DELETE ${accountPath}`]: json(204) });
    await expect(removeAdminHouseholdAccount(HOUSEHOLD_ID, userId, '"7"')).resolves.toEqual({ kind: 'ok' });
    expect(fake.requests[0].headers.get('If-Match')).toBe('"7"');

    const refuse = (status: number, code: string) => installFakeServer({ [`DELETE ${accountPath}`]: problem(status, code) });
    refuse(409, 'last_admin');
    await expect(removeAdminHouseholdAccount(HOUSEHOLD_ID, userId, '"7"')).resolves.toEqual({ kind: 'lastAdmin' });
    refuse(409, 'member_person_changed');
    await expect(removeAdminHouseholdAccount(HOUSEHOLD_ID, userId, '"7"')).resolves.toEqual({ kind: 'rejected' });
    refuse(412, 'precondition_failed');
    await expect(removeAdminHouseholdAccount(HOUSEHOLD_ID, userId, '"7"')).resolves.toEqual({ kind: 'stale' });
    refuse(404, 'not_found');
    await expect(removeAdminHouseholdAccount(HOUSEHOLD_ID, userId, '"7"')).resolves.toEqual({ kind: 'notFound' });
  });

  it('maps the missing installation grant to forbidden on every action, and other refusals to rejected', async () => {
    const methods = ['GET', 'PUT', 'PATCH', 'DELETE'] as const;
    const call = {
      GET: () => readAdminHouseholdAccount(HOUSEHOLD_ID, userId),
      PUT: () => assignAdminHouseholdAccount(HOUSEHOLD_ID, userId, 'member'),
      PATCH: () => changeAdminHouseholdAccountRole(HOUSEHOLD_ID, userId, 'member', '"1"'),
      DELETE: () => removeAdminHouseholdAccount(HOUSEHOLD_ID, userId, '"1"'),
    };
    for (const method of methods) {
      installFakeServer({ [`${method} ${accountPath}`]: problem(403, 'instance_admin_required') });
      await expect(call[method]()).resolves.toEqual({ kind: 'forbidden' });
      installFakeServer({ [`${method} ${accountPath}`]: problem(403, 'admin_required') });
      await expect(call[method]()).resolves.toEqual({ kind: 'rejected' });
      installFakeServer({ [`${method} ${accountPath}`]: problem(401) });
      await expect(call[method]()).resolves.toEqual({ kind: 'unauthenticated' });
    }
  });

  it('reports an unreachable server as the common unavailable state on every action', async () => {
    const offline = () => Promise.reject(new Error('offline'));
    installFakeServer({ [`GET ${accountPath}`]: offline, [`PUT ${accountPath}`]: offline, [`PATCH ${accountPath}`]: offline, [`DELETE ${accountPath}`]: offline });
    await expect(readAdminHouseholdAccount(HOUSEHOLD_ID, userId)).resolves.toEqual({ kind: 'error' });
    await expect(assignAdminHouseholdAccount(HOUSEHOLD_ID, userId, 'member')).resolves.toEqual({ kind: 'error' });
    await expect(changeAdminHouseholdAccountRole(HOUSEHOLD_ID, userId, 'member', '"1"')).resolves.toEqual({ kind: 'error' });
    await expect(removeAdminHouseholdAccount(HOUSEHOLD_ID, userId, '"1"')).resolves.toEqual({ kind: 'error' });
  });
});
