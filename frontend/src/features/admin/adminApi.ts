import type { components } from '../../../../api/generated/api';
import { api } from '../../lib/api';

export type Account = components['schemas']['Account'];
export type AccountRole = components['schemas']['AccountRole'];

const PAGE_SIZE = 50;
const accountsPath = '/api/v1/households/{householdId}/accounts';
const accountPath = '/api/v1/households/{householdId}/accounts/{userId}';

function problemCode(error: unknown): string | undefined {
  return (error as { code?: string } | undefined)?.code;
}
function isAdminRequired(code: string | undefined): boolean {
  return code === 'admin_required' || code === 'owner_required';
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

export function signInMethod(account: Account): 'password' | 'oidc' | 'both' | 'none' {
  const { password, oidc } = account.signInMethods;
  if (password && oidc) return 'both';
  if (password) return 'password';
  if (oidc) return 'oidc';
  return 'none';
}
