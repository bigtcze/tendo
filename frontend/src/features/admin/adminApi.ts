import type { components } from '../../../../api/generated/api';
import { api } from '../../lib/api';

export type Account = components['schemas']['Account'];
export type AccountRole = components['schemas']['AccountRole'];
export type PasswordResetCreated = components['schemas']['PasswordResetCreated'];

const PAGE_SIZE = 50;
const accountsPath = '/api/v1/households/{householdId}/accounts';
const accountPath = '/api/v1/households/{householdId}/accounts/{userId}';
const passwordResetPath = '/api/v1/households/{householdId}/accounts/{userId}/password-reset';

function problemCode(error: unknown): string | undefined {
  return (error as { code?: string } | undefined)?.code;
}
/** The account routes answer `admin_required` for a member; the legacy `owner_required` belongs to invitations only. */
function isAdminRequired(code: string | undefined): boolean {
  return code === 'admin_required';
}

export type AccountPage = { kind: 'ok'; items: Account[]; nextCursor: string | null } | { kind: 'unauthenticated' | 'forbidden' | 'rejected' | 'notFound' | 'error' };

export async function listAccounts(householdId: string, cursor?: string): Promise<AccountPage> {
  try {
    const { data, error, response } = await api.GET(accountsPath, {
      params: { path: { householdId }, query: { limit: PAGE_SIZE, ...(cursor ? { cursor } : {}) } },
    });
    if (data && response.status === 200) return { kind: 'ok', items: data.items, nextCursor: data.nextCursor };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 403) return isAdminRequired(problemCode(error)) ? { kind: 'forbidden' } : { kind: 'rejected' };
    if (response.status === 404) return { kind: 'notFound' };
  } catch { /* show the common unavailable state */ }
  return { kind: 'error' };
}

export type AccountRead = { kind: 'ok'; account: Account; etag: string } | { kind: 'unauthenticated' | 'forbidden' | 'notFound' | 'rejected' | 'error' };

/** Reads one account for a fresh membership version; the role change must quote that exact strong tag. */
export async function readAccount(householdId: string, userId: string): Promise<AccountRead> {
  try {
    const { data, error, response } = await api.GET(accountPath, { params: { path: { householdId, userId } } });
    const etag = response.headers.get('ETag');
    if (data && response.status === 200 && etag) return { kind: 'ok', account: data, etag };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 403) return isAdminRequired(problemCode(error)) ? { kind: 'forbidden' } : { kind: 'rejected' };
    if (response.status === 404) return { kind: 'notFound' };
  } catch { /* fall through */ }
  return { kind: 'error' };
}

export type RoleChange =
  | { kind: 'ok'; account: Account }
  | { kind: 'lastAdmin' }
  | { kind: 'stale' }
  | { kind: 'unauthenticated' | 'forbidden' | 'notFound' | 'rejected' | 'error' };

export async function changeAccountRole(householdId: string, userId: string, role: AccountRole, etag: string): Promise<RoleChange> {
  try {
    const { data, error, response } = await api.PATCH(accountPath, {
      params: { path: { householdId, userId }, header: { 'If-Match': etag } },
      body: { role },
    });
    if (data && response.status === 200) return { kind: 'ok', account: data };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 403) return isAdminRequired(problemCode(error)) ? { kind: 'forbidden' } : { kind: 'rejected' };
    if (response.status === 404) return { kind: 'notFound' };
    if (response.status === 409 && problemCode(error) === 'last_admin') return { kind: 'lastAdmin' };
    if (response.status === 409) return { kind: 'rejected' };
    if (response.status === 412) return { kind: 'stale' };
  } catch { /* fall through */ }
  return { kind: 'error' };
}

export type PasswordResetIssue =
  | { kind: 'issued'; reset: PasswordResetCreated }
  | { kind: 'self' | 'noLocalPassword' | 'notPermitted' }
  | { kind: 'unauthenticated' | 'forbidden' | 'notFound' | 'rateLimited' | 'unavailable' | 'rejected' };

/** Issues a single-use link; the raw token appears only in this 201 response and is never stored by the client. */
export async function issuePasswordReset(householdId: string, userId: string): Promise<PasswordResetIssue> {
  try {
    const { data, error, response } = await api.POST(passwordResetPath, {
      params: { path: { householdId, userId } },
      body: {},
    });
    if (data && response.status === 201) return { kind: 'issued', reset: data };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 403) return isAdminRequired(problemCode(error)) ? { kind: 'forbidden' } : { kind: 'rejected' };
    if (response.status === 404) return { kind: 'notFound' };
    if (response.status === 409) {
      const code = problemCode(error);
      if (code === 'cannot_reset_self') return { kind: 'self' };
      if (code === 'no_local_password') return { kind: 'noLocalPassword' };
      if (code === 'password_reset_not_permitted') return { kind: 'notPermitted' };
      return { kind: 'rejected' };
    }
    if (response.status === 429) return { kind: 'rateLimited' };
  } catch { /* fall through */ }
  return { kind: 'unavailable' };
}

export type PasswordResetCancel =
  | { kind: 'ok' }
  | { kind: 'self' }
  | { kind: 'unauthenticated' | 'forbidden' | 'notFound' | 'rateLimited' | 'unavailable' | 'rejected' };

/** Cancels the outstanding link; the server also answers 204 when there was nothing left to cancel. */
export async function cancelPasswordReset(householdId: string, userId: string): Promise<PasswordResetCancel> {
  try {
    const { error, response } = await api.DELETE(passwordResetPath, { params: { path: { householdId, userId } } });
    if (response.status === 204) return { kind: 'ok' };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 403) return isAdminRequired(problemCode(error)) ? { kind: 'forbidden' } : { kind: 'rejected' };
    if (response.status === 404) return { kind: 'notFound' };
    if (response.status === 409 && problemCode(error) === 'cannot_reset_self') return { kind: 'self' };
    if (response.status === 409) return { kind: 'rejected' };
    if (response.status === 429) return { kind: 'rateLimited' };
  } catch { /* fall through */ }
  return { kind: 'unavailable' };
}

export function signInMethod(account: Account): 'password' | 'oidc' | 'both' | 'none' {
  const { password, oidc } = account.signInMethods;
  if (password && oidc) return 'both';
  if (password) return 'password';
  if (oidc) return 'oidc';
  return 'none';
}
