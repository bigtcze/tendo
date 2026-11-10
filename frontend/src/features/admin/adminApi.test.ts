import { afterEach, describe, expect, it, vi } from 'vitest';
import { changeAccountRole, listAccounts, readAccount, signInMethod, type Account } from './adminApi';
import { HOUSEHOLD_ID, installFakeServer, json } from '../../test/fakeServer';

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const account: Account = { userId: 'u-1', login: 'petr', role: 'member', signInMethods: { password: true, oidc: false } };
const problem = (status: number, code?: string) => json(status, { type: 'about:blank', title: 'x', status, ...(code ? { code } : {}) });

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('adminApi', () => {
  it('pages accounts with the fixed page size and reports the admin-required refusal as forbidden', async () => {
    const fake = installFakeServer({
      [`GET ${base}/accounts`]: json(200, { items: [account], nextCursor: 'a1' }),
    });
    await expect(listAccounts(HOUSEHOLD_ID)).resolves.toEqual({ kind: 'ok', items: [account], nextCursor: 'a1' });
    expect(fake.requests[0].query.get('limit')).toBe('50');

    installFakeServer({ [`GET ${base}/accounts`]: problem(403, 'admin_required') });
    await expect(listAccounts(HOUSEHOLD_ID)).resolves.toEqual({ kind: 'forbidden' });
  });

  it('does not mistake an address-origin refusal for the only-administrators state', async () => {
    installFakeServer({ [`GET ${base}/accounts`]: problem(403, 'forbidden_origin') });
    await expect(listAccounts(HOUSEHOLD_ID)).resolves.toEqual({ kind: 'rejected' });
  });

  it('requires the strong version tag before a change can quote it', async () => {
    installFakeServer({ [`GET ${base}/accounts/u-1`]: json(200, account) });
    await expect(readAccount(HOUSEHOLD_ID, 'u-1')).resolves.toEqual({ kind: 'error' });

    installFakeServer({ [`GET ${base}/accounts/u-1`]: json(200, account, { ETag: '"4"' }) });
    await expect(readAccount(HOUSEHOLD_ID, 'u-1')).resolves.toEqual({ kind: 'ok', account, etag: '"4"' });
  });

  it('separates the last-admin refusal from other conflicts and from a stale version', async () => {
    const routes = (status: number, code: string) => installFakeServer({ [`PATCH ${base}/accounts/u-1`]: problem(status, code) });
    routes(409, 'last_admin');
    await expect(changeAccountRole(HOUSEHOLD_ID, 'u-1', 'member', '"4"')).resolves.toEqual({ kind: 'lastAdmin' });
    routes(409, 'member_person_changed');
    await expect(changeAccountRole(HOUSEHOLD_ID, 'u-1', 'member', '"4"')).resolves.toEqual({ kind: 'rejected' });
    routes(412, 'precondition_failed');
    await expect(changeAccountRole(HOUSEHOLD_ID, 'u-1', 'member', '"4"')).resolves.toEqual({ kind: 'stale' });
  });

  it('names the sign-in methods a person actually has', () => {
    expect(signInMethod(account)).toBe('password');
    expect(signInMethod({ ...account, signInMethods: { password: false, oidc: true } })).toBe('oidc');
    expect(signInMethod({ ...account, signInMethods: { password: true, oidc: true } })).toBe('both');
    expect(signInMethod({ ...account, signInMethods: { password: false, oidc: false } })).toBe('none');
  });
});
