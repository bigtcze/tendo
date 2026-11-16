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
const deletionsPath = '/api/v1/admin/account-deletions';
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

/**
 * One page of the account directory. `invalidQuery` is the server refusing the search text itself — a login substring
 * longer than 64 characters, for example — which the add dialog answers on its own field instead of offering a retry
 * that could never succeed.
 */
export type DirectoryPage =
  | { kind: 'ok'; items: DirectoryAccount[]; nextCursor: string | null }
  | { kind: 'invalidQuery' }
  | { kind: 'unauthenticated' | 'forbidden' | 'notFound' | 'rejected' | 'error' };

/** The account directory an assignment picks from: identifier and login only, filtered by a login substring. */
export async function listAdminDirectory(q: string, cursor?: string): Promise<DirectoryPage> {
  const needle = q.trim();
  try {
    const { data, error, response } = await api.GET(directoryPath, {
      params: { query: { limit: DIRECTORY_PAGE_SIZE, ...(needle ? { q: needle } : {}), ...(cursor ? { cursor } : {}) } },
    });
    if (data && response.status === 200) return { kind: 'ok', items: data.items, nextCursor: data.nextCursor };
    if (response.status === 400 && problemCode(error) === 'invalid_query') return { kind: 'invalidQuery' };
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

/** The fields `createAdminHousehold` validates, in the order the server answers them. */
const createFields: readonly string[] = ['name', 'timezone', 'adminUserId'];
const createCodes: readonly string[] = ['invalid_length', 'invalid_characters', 'invalid_iana_timezone', 'invalid_reference'];

export type AdminHouseholdField = 'name' | 'timezone' | 'adminUserId';
export type AdminHouseholdCode = 'invalid_length' | 'invalid_characters' | 'invalid_iana_timezone' | 'invalid_reference';

export type AdminHouseholdCreate =
  | { kind: 'created'; household: AdminHousehold }
  /** One field the server refused; `code` says why and the field it names is where the reader has to answer. */
  | { kind: 'invalid'; field: AdminHouseholdField; code: AdminHouseholdCode }
  /** The key was already used for a different name, timezone or account: the caller must start a new intent. */
  | { kind: 'keyReused' }
  /** Anything else refused as a whole; a `404` is not in this route's contract but reads as a plain failure. */
  | { kind: 'unauthenticated' | 'forbidden' | 'notFound' | 'rejected' | 'error' };

/** A `422` whose field this client knows; anything else is not a field answer and stays a plain failure. */
function invalidHousehold(error: unknown): Extract<AdminHouseholdCreate, { kind: 'invalid' }> | null {
  const { field, code } = (error ?? {}) as { field?: unknown; code?: unknown };
  if (typeof field !== 'string' || !createFields.includes(field)) return null;
  if (typeof code !== 'string' || !createCodes.includes(code)) return null;
  return { kind: 'invalid', field: field as AdminHouseholdField, code: code as AdminHouseholdCode };
}

/**
 * Creates a household and its first admin membership in one call. The `Idempotency-Key` is the caller's promise that
 * a retry of the same intent returns the household already created; a key reused for another intent is refused
 * rather than answered with an unrelated household.
 */
export async function createAdminHousehold(
  body: { name: string; timezone: string; adminUserId: string }, idempotencyKey: string,
): Promise<AdminHouseholdCreate> {
  try {
    const { data, error, response } = await api.POST(householdsPath, {
      params: { header: { 'Idempotency-Key': idempotencyKey } },
      body,
    });
    if (data && response.status === 201) return { kind: 'created', household: data };
    if (response.status === 422) {
      if (problemCode(error) === 'idempotency_key_reused') return { kind: 'keyReused' };
      return invalidHousehold(error) ?? { kind: 'error' };
    }
    const refused = refusal(response.status, problemCode(error));
    if (refused) return refused;
    if (response.status >= 400 && response.status < 500) return { kind: 'rejected' };
  } catch { /* the common unavailable state */ }
  return { kind: 'error' };
}

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

export type AdminAccountDeletion =
  | { kind: 'ok' }
  /** A household would be left without an admin, or the installation without a live instance administrator. */
  | { kind: 'lastAdmin' }
  | { kind: 'lastInstanceAdmin' }
  | { kind: 'self' }
  /** The repeated login did not match the account's current login: the confirmation field answers it. */
  | { kind: 'mismatch' }
  | { kind: 'unauthenticated' | 'forbidden' | 'notFound' | 'rejected' | 'error' };

/** The refusals a `409` carries; anything else refused as a whole is not one of them. */
const deletionConflicts: Readonly<Record<string, AdminAccountDeletion>> = {
  last_admin: { kind: 'lastAdmin' },
  last_instance_admin: { kind: 'lastInstanceAdmin' },
  cannot_delete_self: { kind: 'self' },
};

/** The one `422` this client answers on its own field; a bad account identifier is a plain failure here. */
function deletionValidation(error: unknown): AdminAccountDeletion | null {
  const { field, code } = (error ?? {}) as { field?: unknown; code?: unknown };
  return field === 'confirmLogin' && code === 'mismatch' ? { kind: 'mismatch' } : null;
}

/**
 * Deletes an installation account everywhere: it can never sign in again and leaves every household. The repeated
 * login guards against deleting the wrong account from a stale list; the server compares it exactly and changes
 * nothing on any refusal. There is no `If-Match` and no `Idempotency-Key`: a repeat finds no live account and is a
 * plain `404`, which reads as an account that is already gone.
 */
export async function deleteAdminAccount(userId: string, confirmLogin: string): Promise<AdminAccountDeletion> {
  try {
    const { error, response } = await api.POST(deletionsPath, { body: { userId, confirmLogin } });
    if (response.status === 204) return { kind: 'ok' };
    const refused = refusal(response.status, problemCode(error));
    if (refused) return refused;
    if (response.status === 409) return deletionConflicts[problemCode(error) ?? ''] ?? { kind: 'rejected' };
    if (response.status === 422) return deletionValidation(error) ?? { kind: 'rejected' };
    if (response.status >= 400 && response.status < 500) return { kind: 'rejected' };
  } catch { /* the common unavailable state */ }
  return { kind: 'error' };
}
