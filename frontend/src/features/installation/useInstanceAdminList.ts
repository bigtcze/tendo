import { useCallback } from 'react';
import { useCursorList, type CursorList } from '../../app/useCursorList';
import {
  listAdminHouseholdAccounts, listAdminHouseholds,
  type AdminHousehold, type AdminHouseholdAccount,
} from './instanceAdminApi';

type Handlers = { onSignedOut: () => void; onForbidden?: () => void };

/**
 * Both installation lists: the shared keyset paging with quiet paging, a page retry that keeps the rows already on
 * screen, plus the quiet reload, row replacement and forbidden marking a screen needs after an edit. An
 * authoritative refusal empties the list; `notFound` says the household itself is gone, and `forbidden` runs the
 * caller's session re-read so a stale navigation hint corrects itself. `load` must be stable: it restarts the list
 * when the target changes.
 */
export type InstanceList<T> = CursorList<T>;

export function useAdminHouseholdList(handlers: Handlers): InstanceList<AdminHousehold> {
  const load = useCallback((cursor?: string) => listAdminHouseholds(cursor), []);
  return useCursorList<AdminHousehold>({
    load,
    onSignedOut: handlers.onSignedOut,
    onForbidden: handlers.onForbidden,
    notFound: 'authoritative',
    keyOf: (household) => household.id,
  });
}

export function useAdminHouseholdAccounts(householdId: string, handlers: Handlers): InstanceList<AdminHouseholdAccount> {
  const load = useCallback((cursor?: string) => listAdminHouseholdAccounts(householdId, cursor), [householdId]);
  return useCursorList<AdminHouseholdAccount>({
    load,
    onSignedOut: handlers.onSignedOut,
    onForbidden: handlers.onForbidden,
    notFound: 'authoritative',
    keyOf: (account) => account.userId,
  });
}
