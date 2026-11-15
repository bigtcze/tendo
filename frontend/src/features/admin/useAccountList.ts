import { useCallback } from 'react';
import { useCursorList } from '../../app/useCursorList';
import { listAccounts, type Account } from './adminApi';

/**
 * The account list is the screen's only authorization probe: a member's `403 admin_required` settles the calm
 * only-administrators state and no account data is kept. Paging, a quiet reload and a paging retry that keeps the
 * current page all come from the shared cursor list; a missing household reads as a generic failure here, because
 * an account list is never simply absent. `onForbidden` runs after an authoritative refusal so the caller can
 * correct the stale viewerRole hint.
 */
export function useAccountList(householdId: string, onSignedOut: () => void, onForbidden?: () => void) {
  const load = useCallback((cursor?: string) => listAccounts(householdId, cursor), [householdId]);
  const list = useCursorList<Account>({
    load, onSignedOut, onForbidden, notFound: 'retryable', keyOf: (account) => account.userId,
  });
  return { ...list, accounts: list.items };
}
