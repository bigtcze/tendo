import type { components } from '../../../api/generated/api';
import type { Session } from '../features/auth/sessionApi';
import { api } from '../lib/api';

export type HouseholdSummary = components['schemas']['Household'];

/** One page of the households this account belongs to; the list never contains another account's household. */
export type HouseholdPage =
  | { kind: 'ok'; items: HouseholdSummary[]; nextCursor: string | null }
  | { kind: 'unauthenticated' }
  | { kind: 'error' };

/** The caller's own memberships, in stable order; the next page is only read when the reader asks for it. */
export async function listHouseholds(cursor?: string): Promise<HouseholdPage> {
  try {
    const { data, response } = await api.GET('/api/v1/households', { params: { query: cursor ? { cursor } : {} } });
    if (data && response.status === 200) return { kind: 'ok', items: data.items, nextCursor: data.nextCursor };
    if (response.status === 401) return { kind: 'unauthenticated' };
  } catch {
    // Network failures fall through to the retryable error state.
  }
  return { kind: 'error' };
}

export type SwitchOutcome =
  | { kind: 'ok'; session: Session }
  /** 422: the household is unknown, malformed, or no longer one of the caller's memberships. */
  | { kind: 'gone' }
  | { kind: 'unauthenticated' }
  | { kind: 'error' };

/**
 * Records the household the account wants to work in next. The server stores it on the session as a navigation
 * hint; access is still decided by membership on every household request.
 */
export async function switchHousehold(householdId: string): Promise<SwitchOutcome> {
  try {
    // The browser adds the canonical Origin itself on a state-changing request, as it does for every other mutation.
    const { data, response } = await api.PATCH('/api/v1/session', {
      body: { defaultHouseholdId: householdId },
    });
    if (data && response.status === 200) return { kind: 'ok', session: data };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 422) return { kind: 'gone' };
  } catch {
    // Network failures and 503 fall through to the retryable error state.
  }
  return { kind: 'error' };
}
