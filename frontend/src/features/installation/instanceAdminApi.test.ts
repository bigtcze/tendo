import { afterEach, describe, expect, it, vi } from 'vitest';
import { listAdminHouseholdAccounts, listAdminHouseholds, type AdminHousehold, type AdminHouseholdAccount } from './instanceAdminApi';
import { HOUSEHOLD_ID, installFakeServer, json } from '../../test/fakeServer';

const householdsPath = 'GET /api/v1/admin/households';
const accountsPath = `GET /api/v1/admin/households/${HOUSEHOLD_ID}/accounts`;
const household: AdminHousehold = {
  id: HOUSEHOLD_ID, name: 'Veselí', timezone: 'Europe/Prague', createdAt: '2026-10-06T10:00:00Z',
};
const account: AdminHouseholdAccount = {
  userId: '0198a2f0-0000-7000-8000-000000000002', login: 'petr', role: 'member', signInMethods: { password: true, oidc: false },
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
