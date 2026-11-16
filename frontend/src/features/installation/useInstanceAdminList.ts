import { useCallback, useState } from 'react';
import { useCursorList, type CursorList } from '../../app/useCursorList';
import {
  listAdminDirectory, listAdminHouseholdAccounts, listAdminHouseholds,
  type AdminHousehold, type AdminHouseholdAccount, type DirectoryAccount,
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

/** The account directory list, with the server's refusal of the search text kept apart from a read that failed. */
export type DirectoryList = CursorList<DirectoryAccount> & {
  /** The search text itself was refused: the field says what to fix, and there is nothing to retry. */
  invalidQuery: boolean;
  /**
   * The exact submitted search the server refused, or null. Each press submits a new object, so a repeated identical
   * refusal is a new object too and can put the reader back on the field again.
   */
  refused: { needle: string } | null;
};

/**
 * The installation-wide account directory as a paged list. `search` is the search that was submitted: its identity
 * changes with every press, so pressing Search always reads again even for the same text, and the list restarts.
 *
 * A refused search text is not a failed read: it answers on the field, so this hook reports it as `invalidQuery`
 * while the list itself holds no rows. Every other refusal keeps the shared list behavior — including the same code
 * on a page request, which is about the cursor the list made up and never about what the reader typed.
 */
export function useAdminAccountDirectory(search: { needle: string }, handlers: Handlers): DirectoryList {
  const [refused, setRefused] = useState<{ needle: string } | null>(null);
  const load = useCallback(async (cursor?: string) => {
    const page = await listAdminDirectory(search.needle, cursor);
    if (page.kind === 'invalidQuery') {
      if (cursor !== undefined) return { kind: 'error' } as const;
      setRefused(search);
      return { kind: 'error' } as const;
    }
    // Only a success of the same search retires its refusal: another search's answer says nothing about this field.
    setRefused((current) => (current === search ? null : current));
    return page;
  }, [search]);
  const list = useCursorList<DirectoryAccount>({
    load,
    onSignedOut: handlers.onSignedOut,
    onForbidden: handlers.onForbidden,
    keyOf: (account) => account.userId,
  });
  return { ...list, invalidQuery: refused === search, refused };
}
