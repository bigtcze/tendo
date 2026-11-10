import { useCallback, useEffect, useRef, useState, type MouseEvent } from 'react';
import { listAccounts, type Account, type AccountPage } from './adminApi';
import { nextPage, previousPage } from '../members/membersModel';
import { usePagedHeadingFocus } from './usePagedHeadingFocus';

export type AdminListState = 'loading' | 'ready' | 'forbidden' | 'error';
type FailedPage = Exclude<AccountPage, { kind: 'ok' }>;

/**
 * The account list is the screen's only authorization probe: a member's `403 admin_required` settles the calm
 * only-administrators state and no account data is kept. Paging and a quiet reload share one request guard.
 */
export function useAccountList(householdId: string, onSignedOut: () => void) {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [stack, setStack] = useState<string[]>([]);
  const [cursor, setCursor] = useState<string | undefined>();
  const [state, setState] = useState<AdminListState>('loading');
  const [attempt, setAttempt] = useState(0);
  const [busy, setBusy] = useState(false);
  const mountedRef = useRef(false);
  const generationRef = useRef(0);
  const pageGenerationRef = useRef(0);
  const headingRef = useRef<HTMLHeadingElement>(null);
  const pageFocusRef = useRef<HTMLElement | null>(null);
  const onSignedOutRef = useRef(onSignedOut);
  useEffect(() => { onSignedOutRef.current = onSignedOut; }, [onSignedOut]);
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; generationRef.current += 1; pageGenerationRef.current += 1; };
  }, []);

  const fail = useCallback((page: FailedPage) => {
    if (page.kind === 'unauthenticated') { onSignedOutRef.current(); return; }
    if (page.kind === 'forbidden') { setAccounts([]); setNextCursor(null); setState('forbidden'); return; }
    setState('error');
  }, []);
  useEffect(() => {
    const token = ++generationRef.current;
    pageGenerationRef.current += 1;
    let cancelled = false;
    // Reset inside the async start, like the members screen, so the effect never sets state during render commit.
    (async () => {
      setState('loading');
      setAccounts([]); setNextCursor(null); setStack([]); setCursor(undefined); setBusy(false);
      const page = await listAccounts(householdId);
      if (cancelled || generationRef.current !== token) return;
      if (page.kind === 'ok') {
        setAccounts(page.items);
        setNextCursor(page.nextCursor);
        setState('ready');
        return;
      }
      fail(page);
    })();
    return () => { cancelled = true; };
  }, [householdId, attempt, fail]);

  usePagedHeadingFocus(pageFocusRef, headingRef, accounts, busy);

  async function loadPage(cursorValue: string | undefined, nextStack: string[], activated?: HTMLElement) {
    const token = generationRef.current;
    const pageToken = ++pageGenerationRef.current;
    if (activated) pageFocusRef.current = activated;
    setBusy(true);
    const page = await listAccounts(householdId, cursorValue);
    if (!mountedRef.current || generationRef.current !== token || pageGenerationRef.current !== pageToken) return;
    setBusy(false);
    if (page.kind !== 'ok') { fail(page); return; }
    setAccounts(page.items);
    setNextCursor(page.nextCursor);
    setCursor(cursorValue);
    setStack(nextStack);
    setState('ready');
  }

  const next = (event: MouseEvent<HTMLButtonElement>) => {
    if (!nextCursor || busy) return;
    const page = nextPage(stack, cursor, nextCursor);
    void loadPage(page.cursor, page.stack, event.currentTarget);
  };
  const previous = (event: MouseEvent<HTMLButtonElement>) => {
    const page = previousPage(stack);
    void loadPage(page.cursor, page.stack, event.currentTarget);
  };
  const retry = () => setAttempt((n) => n + 1);
  /** After 412 or 404: show the same page as the server holds now, with a busy state so no second change lands. */
  const reload = () => { void loadPage(cursor, stack); };
  const replaceAccount = (account: Account) => setAccounts((list) => list.map((row) => (row.userId === account.userId ? account : row)));
  const markForbidden = () => { setAccounts([]); setNextCursor(null); setState('forbidden'); };

  return {
    accounts, nextCursor, stack, state, busy, headingRef, pageFocusRef,
    next, previous, retry, reload, replaceAccount, markForbidden,
  };
}

export type AccountList = ReturnType<typeof useAccountList>;
