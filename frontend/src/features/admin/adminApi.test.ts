import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  cancelPasswordReset, changeAccountRole, issuePasswordReset, listAccounts, readAccount, signInMethod, type Account,
} from './adminApi';
import { HOUSEHOLD_ID, installFakeServer, json } from '../../test/fakeServer';

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const account: Account = { userId: 'u-1', login: 'petr', role: 'member', signInMethods: { password: true, oidc: false }, passwordReset: null };
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

  it('accepts only the admin_required code the account routes answer, not the legacy owner_required', async () => {
    installFakeServer({ [`GET ${base}/accounts`]: problem(403, 'owner_required') });
    await expect(listAccounts(HOUSEHOLD_ID)).resolves.toEqual({ kind: 'rejected' });

    installFakeServer({ [`GET ${base}/accounts/u-1`]: problem(403, 'owner_required') });
    await expect(readAccount(HOUSEHOLD_ID, 'u-1')).resolves.toEqual({ kind: 'rejected' });

    installFakeServer({ [`PATCH ${base}/accounts/u-1`]: problem(403, 'owner_required') });
    await expect(changeAccountRole(HOUSEHOLD_ID, 'u-1', 'member', '"4"')).resolves.toEqual({ kind: 'rejected' });

    installFakeServer({ [`GET ${base}/accounts`]: problem(403, 'admin_required') });
    await expect(listAccounts(HOUSEHOLD_ID)).resolves.toEqual({ kind: 'forbidden' });
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

  it('issues a reset link with an empty body and separates the specific conflicts', async () => {
    const reset = { token: 'T'.repeat(43), createdAt: '2026-11-10T10:00:00Z', expiresAt: '2026-11-11T10:00:00Z' };
    const fake = installFakeServer({ [`POST ${base}/accounts/u-1/password-reset`]: json(201, reset) });
    await expect(issuePasswordReset(HOUSEHOLD_ID, 'u-1')).resolves.toEqual({ kind: 'issued', reset });
    expect(fake.requests[0].body).toBe('{}');

    const refuse = (status: number, code: string) => installFakeServer({ [`POST ${base}/accounts/u-1/password-reset`]: problem(status, code) });
    refuse(409, 'cannot_reset_self');
    await expect(issuePasswordReset(HOUSEHOLD_ID, 'u-1')).resolves.toEqual({ kind: 'self' });
    refuse(409, 'no_local_password');
    await expect(issuePasswordReset(HOUSEHOLD_ID, 'u-1')).resolves.toEqual({ kind: 'noLocalPassword' });
    refuse(409, 'password_reset_not_permitted');
    await expect(issuePasswordReset(HOUSEHOLD_ID, 'u-1')).resolves.toEqual({ kind: 'notPermitted' });
    refuse(409, 'member_person_changed');
    await expect(issuePasswordReset(HOUSEHOLD_ID, 'u-1')).resolves.toEqual({ kind: 'rejected' });
    refuse(403, 'admin_required');
    await expect(issuePasswordReset(HOUSEHOLD_ID, 'u-1')).resolves.toEqual({ kind: 'forbidden' });
    refuse(403, 'forbidden_origin');
    await expect(issuePasswordReset(HOUSEHOLD_ID, 'u-1')).resolves.toEqual({ kind: 'rejected' });
    refuse(404, 'not_found');
    await expect(issuePasswordReset(HOUSEHOLD_ID, 'u-1')).resolves.toEqual({ kind: 'notFound' });
    refuse(429, 'rate_limited');
    await expect(issuePasswordReset(HOUSEHOLD_ID, 'u-1')).resolves.toEqual({ kind: 'rateLimited' });
    refuse(503, 'unavailable');
    await expect(issuePasswordReset(HOUSEHOLD_ID, 'u-1')).resolves.toEqual({ kind: 'unavailable' });
  });

  it('cancels the outstanding link and treats an already empty reset as done', async () => {
    installFakeServer({ [`DELETE ${base}/accounts/u-1/password-reset`]: json(204) });
    await expect(cancelPasswordReset(HOUSEHOLD_ID, 'u-1')).resolves.toEqual({ kind: 'ok' });

    installFakeServer({ [`DELETE ${base}/accounts/u-1/password-reset`]: problem(403, 'admin_required') });
    await expect(cancelPasswordReset(HOUSEHOLD_ID, 'u-1')).resolves.toEqual({ kind: 'forbidden' });
    installFakeServer({ [`DELETE ${base}/accounts/u-1/password-reset`]: problem(409, 'cannot_reset_self') });
    await expect(cancelPasswordReset(HOUSEHOLD_ID, 'u-1')).resolves.toEqual({ kind: 'self' });
    installFakeServer({ [`DELETE ${base}/accounts/u-1/password-reset`]: problem(429, 'rate_limited') });
    await expect(cancelPasswordReset(HOUSEHOLD_ID, 'u-1')).resolves.toEqual({ kind: 'rateLimited' });
  });
});
