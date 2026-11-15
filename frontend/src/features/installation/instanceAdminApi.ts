import type { components } from '../../../../api/generated/api';
import { api } from '../../lib/api';

export type AdminHousehold = components['schemas']['AdminHousehold'];
export type AdminHouseholdAccount = components['schemas']['AdminAccount'];
export type AdminAccountRole = components['schemas']['AccountRole'];
export type DirectoryAccount = components['schemas']['DirectoryAccount'];

const PAGE_SIZE = 50;
const DIRECTORY_PAGE_SIZE = 20;
const householdsPath = '/api/v1/admin/households';
const directoryPath = '/api/v1/admin/accounts';
const accountsPath = '/api/v1/admin/households/{householdId}/accounts';
const accountPath = '/api/v1/admin/households/{householdId}/accounts/{userId}';

/**
 * One page of an installation-wide list. `forbidden` means the caller does not hold the installation grant right
 * now (a stale navigation hint); `rejected` is anything else refused, for example a mismatched request origin.
 */
export type InstancePage<T> =
  | { kind: 'ok'; items: T[]; nextCursor: string | null }
  | { kind: 'unauthenticated' | 'forbidden' | 'rejected' | 'notFound' | 'error' };

/** A refusal every installation route can answer; `forbidden` is only the missing grant, never a household role. */
type Refusal =
  | { kind: 'unauthenticated' }
  | { kind: 'forbidden' }
  | { kind: 'notFound' }
  | { kind: 'rejected' };

function problemCode(error: unknown): string | undefined {
  return (error as { code?: string } | undefined)?.code;
}

/** The installation routes answer the same refusals; only the grant code makes one of them the calm refusal. */
function refusal(status: number, code: string | undefined): Refusal | null {
  if (status === 401) return { kind: 'unauthenticated' };
  if (status === 403) return code === 'instance_admin_required' ? { kind: 'forbidden' } : { kind: 'rejected' };
  if (status === 404) return { kind: 'notFound' };
  return null;
}

export async function listAdminHouseholds(cursor?: string): Promise<InstancePage<AdminHousehold>> {
  try {
    const { data, error, response } = await api.GET(householdsPath, {
      params: { query: { limit: PAGE_SIZE, ...(cursor ? { cursor } : {}) } },
    });
    if (data && response.status === 200) return { kind: 'ok', items: data.items, nextCursor: data.nextCursor };
    return refusal(response.status, problemCode(error)) ?? { kind: 'error' };
  } catch { /* the common unavailable state */ }
  return { kind: 'error' };
}

/** The account directory an assignment picks from: identifier and login only, filtered by a login substring. */
export async function listAdminDirectory(q: string, cursor?: string): Promise<InstancePage<DirectoryAccount>> {
  const needle = q.trim();
  try {
    const { data, error, response } = await api.GET(directoryPath, {
      params: { query: { limit: DIRECTORY_PAGE_SIZE, ...(needle ? { q: needle } : {}), ...(cursor ? { cursor } : {}) } },
    });
    if (data && response.status === 200) return { kind: 'ok', items: data.items, nextCursor: data.nextCursor };
    return refusal(response.status, problemCode(error)) ?? { kind: 'error' };
  } catch { /* the common unavailable state */ }
  return { kind: 'error' };
}

export async function listAdminHouseholdAccounts(householdId: string, cursor?: string): Promise<InstancePage<AdminHouseholdAccount>> {
  try {
    const { data, error, response } = await api.GET(accountsPath, {
      params: { path: { householdId }, query: { limit: PAGE_SIZE, ...(cursor ? { cursor } : {}) } },
    });
    if (data && response.status === 200) return { kind: 'ok', items: data.items, nextCursor: data.nextCursor };
    return refusal(response.status, problemCode(error)) ?? { kind: 'error' };
  } catch { /* the common unavailable state */ }
  return { kind: 'error' };
}

export type AdminAccountRead =
  | { kind: 'ok'; account: AdminHouseholdAccount; etag: string }
  | { kind: 'unauthenticated' | 'forbidden' | 'notFound' | 'rejected' | 'error' };

/** Reads one membership for a fresh version tag; an assignee's role change must quote that exact strong tag. */
export async function readAdminHouseholdAccount(householdId: string, userId: string): Promise<AdminAccountRead> {
  try {
    const { data, error, response } = await api.GET(accountPath, { params: { path: { householdId, userId } } });
    const etag = response.headers.get('ETag');
    if (data && response.status === 200 && etag) return { kind: 'ok', account: data, etag };
    return refusal(response.status, problemCode(error)) ?? { kind: 'error' };
  } catch { /* fall through */ }
  return { kind: 'error' };
}

export type AdminAssignment =
  | { kind: 'created'; account: AdminHouseholdAccount }
  | { kind: 'unchanged'; account: AdminHouseholdAccount }
  | { kind: 'alreadyMember' }
  | { kind: 'requiresAdmin' }
  | { kind: 'unauthenticated' | 'forbidden' | 'notFound' | 'rejected' | 'error' };

/**
 * Puts an existing account into a household. `created` is a fresh membership and `unchanged` the server's
 * idempotent replay of the same role; an existing membership with another role needs the role change instead.
 */
export async function assignAdminHouseholdAccount(
  householdId: string, userId: string, role: AdminAccountRole,
): Promise<AdminAssignment> {
  try {
    const { data, error, response } = await api.PUT(accountPath, {
      params: { path: { householdId, userId } },
      body: { role },
    });
    if (data && response.status === 201) return { kind: 'created', account: data };
    if (data && response.status === 200) return { kind: 'unchanged', account: data };
    const refused = refusal(response.status, problemCode(error));
    if (refused) return refused;
    if (response.status === 409) {
      const code = problemCode(error);
      if (code === 'already_member') return { kind: 'alreadyMember' };
      if (code === 'household_requires_admin') return { kind: 'requiresAdmin' };
      return { kind: 'rejected' };
    }
  } catch { /* fall through */ }
  return { kind: 'error' };
}

export type AdminRoleChange =
  | { kind: 'ok'; account: AdminHouseholdAccount }
  | { kind: 'lastAdmin' }
  | { kind: 'stale' }
  | { kind: 'unauthenticated' | 'forbidden' | 'notFound' | 'rejected' | 'error' };

/** Applies a role with the required `If-Match`; a last admin and a stale tag change nothing. */
export async function changeAdminHouseholdAccountRole(
  householdId: string, userId: string, role: AdminAccountRole, etag: string,
): Promise<AdminRoleChange> {
  try {
    const { data, error, response } = await api.PATCH(accountPath, {
      params: { path: { householdId, userId }, header: { 'If-Match': etag } },
      body: { role },
    });
    if (data && response.status === 200) return { kind: 'ok', account: data };
    const refused = refusal(response.status, problemCode(error));
    if (refused) return refused;
    if (response.status === 409 && problemCode(error) === 'last_admin') return { kind: 'lastAdmin' };
    if (response.status === 409) return { kind: 'rejected' };
    if (response.status === 412) return { kind: 'stale' };
  } catch { /* fall through */ }
  return { kind: 'error' };
}

export type AdminAccountRemoval =
  | { kind: 'ok' }
  | { kind: 'lastAdmin' }
  | { kind: 'stale' }
  | { kind: 'unauthenticated' | 'forbidden' | 'notFound' | 'rejected' | 'error' };

/** Removes one membership with the required `If-Match`; nothing but the status comes back. */
export async function removeAdminHouseholdAccount(
  householdId: string, userId: string, etag: string,
): Promise<AdminAccountRemoval> {
  try {
    const { error, response } = await api.DELETE(accountPath, {
      params: { path: { householdId, userId }, header: { 'If-Match': etag } },
    });
    if (response.status === 204) return { kind: 'ok' };
    const refused = refusal(response.status, problemCode(error));
    if (refused) return refused;
    if (response.status === 409 && problemCode(error) === 'last_admin') return { kind: 'lastAdmin' };
    if (response.status === 409) return { kind: 'rejected' };
    if (response.status === 412) return { kind: 'stale' };
  } catch { /* fall through */ }
  return { kind: 'error' };
}
