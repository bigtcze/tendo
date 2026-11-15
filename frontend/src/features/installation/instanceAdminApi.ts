import type { components } from '../../../../api/generated/api';
import { api } from '../../lib/api';

export type AdminHousehold = components['schemas']['AdminHousehold'];
export type AdminHouseholdAccount = components['schemas']['AdminAccount'];

const PAGE_SIZE = 50;
const householdsPath = '/api/v1/admin/households';
const accountsPath = '/api/v1/admin/households/{householdId}/accounts';

/**
 * One page of an installation-wide list. `forbidden` means the caller does not hold the installation grant right
 * now (a stale navigation hint); `rejected` is anything else refused, for example a mismatched request origin.
 */
export type InstancePage<T> =
  | { kind: 'ok'; items: T[]; nextCursor: string | null }
  | { kind: 'unauthenticated' | 'forbidden' | 'rejected' | 'notFound' | 'error' };

function problemCode(error: unknown): string | undefined {
  return (error as { code?: string } | undefined)?.code;
}

/** The three installation routes answer the same refusals; only the grant code makes one of them the calm refusal. */
function refusal(status: number, code: string | undefined): InstancePage<never> | null {
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
