import { useCallback, useEffect, useRef, useState, type MouseEvent } from 'react';
import { listAccounts, type Account, type AccountPage } from './adminApi';
import { nextPage, previousPage } from '../members/membersModel';
import { usePagedHeadingFocus } from '../../app/usePagedHeadingFocus';

export type AdminListState = 'loading' | 'ready' | 'forbidden' | 'error';
type FailedPage = Exclude<AccountPage, { kind: 'ok' }>;
/** The page a pager click could not load; its rows stay on screen until the retry succeeds. */
type PageTarget = { cursor: string | undefined; stack: string[] };

function nextTarget(stack: string[], cursor: string | undefined, nextCursor: string): PageTarget {
  const page = nextPage(stack, cursor, nextCursor);
  return { cursor: page.cursor, stack: page.stack };
}
function previousTarget(stack: string[]): PageTarget {
  const page = previousPage(stack);
  return { cursor: page.cursor, stack: page.stack };
}

/**
 * The account list is the screen's only authorization probe: a member's `403 admin_required` settles the calm
 * only-administrators state and no account data is kept. Paging and a quiet reload share one request guard.
 * A paging failure keeps the current page and offers an inline retry instead of dropping to the first page.
 * `onForbidden` runs after an authoritative refusal so the caller can correct the stale viewerRole hint.
 */
export function useAccountList(householdId: string, onSignedOut: () => void, onForbidden?: () => void) {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [stack, setStack] = useState<string[]>([]);
  const [cursor, setCursor] = useState<string | undefined>();
  const [state, setState] = useState<AdminListState>('loading');
  const [pageFailure, setPageFailure] = useState<PageTarget | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [busy, setBusy] = useState(false);
  const mountedRef = useRef(false);
  const generationRef = useRef(0);
  const pageGenerationRef = useRef(0);
  const headingRef = useRef<HTMLHeadingElement>(null);
  const pageFocusRef = useRef<HTMLElement | null>(null);
  const onSignedOutRef = useRef(onSignedOut);
  const onForbiddenRef = useRef(onForbidden);
  useEffect(() => { onSignedOutRef.current = onSignedOut; onForbiddenRef.current = onForbidden; }, [onSignedOut, onForbidden]);
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; generationRef.current += 1; pageGenerationRef.current += 1; };
  }, []);

  const fail = useCallback((page: FailedPage) => {
    if (page.kind === 'unauthenticated') { onSignedOutRef.current(); return; }
    if (page.kind !== 'forbidden') { setState('error'); return; }
    setAccounts([]); setNextCursor(null); setPageFailure(null); setState('forbidden');
    onForbiddenRef.current?.();
  }, []);
  useEffect(() => {
    const token = ++generationRef.current;
    pageGenerationRef.current += 1;
    let cancelled = false;
    // Reset inside the async start, like the members screen, so the effect never sets state during render commit.
    (async () => {
      setState('loading');
      setPageFailure(null);
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

  async function loadPage(target: PageTarget, activated?: HTMLElement) {
    const token = generationRef.current;
    const pageToken = ++pageGenerationRef.current;
    if (activated) pageFocusRef.current = activated;
    setPageFailure(null);
    setBusy(true);
    const page = await listAccounts(householdId, target.cursor);
    if (!mountedRef.current || generationRef.current !== token || pageGenerationRef.current !== pageToken) return;
    setBusy(false);
    if (page.kind !== 'ok') {
      // An authoritative refusal still ends the screen; anything else keeps the current page and offers a retry.
      if (page.kind === 'forbidden' || page.kind === 'unauthenticated') { pageFocusRef.current = null; fail(page); } else { setPageFailure(target); }
      return;
    }
    setAccounts(page.items);
    setNextCursor(page.nextCursor);
    setCursor(target.cursor);
    setStack(target.stack);
    setState('ready');
  }

  const next = (event: MouseEvent<HTMLButtonElement>) => { if (nextCursor && !busy) void loadPage(nextTarget(stack, cursor, nextCursor), event.currentTarget); };
  const previous = (event: MouseEvent<HTMLButtonElement>) => { void loadPage(previousTarget(stack), event.currentTarget); };
  const retry = () => setAttempt((n) => n + 1);
  /** Retries the page a pager click could not load, keeping the rows the screen already shows. */
  const retryPage = () => { if (pageFailure) void loadPage(pageFailure); };
  /** After 412 or 404: show the same page as the server holds now, with a busy state so no second change lands. */
  const reload = () => { void loadPage({ cursor, stack }); };
  const replaceAccount = (account: Account) => setAccounts((list) => list.map((row) => (row.userId === account.userId ? account : row)));
  const markForbidden = () => { setAccounts([]); setNextCursor(null); setPageFailure(null); setState('forbidden'); };

  return {
    accounts, nextCursor, stack, state, busy, pageFailure, headingRef, pageFocusRef,
    next, previous, retry, retryPage, reload, replaceAccount, markForbidden,
  };
}

export type AccountList = ReturnType<typeof useAccountList>;
