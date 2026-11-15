import { useCallback, useEffect, useRef, useState, type MouseEvent, type RefObject } from 'react';
import { usePagedHeadingFocus } from '../../app/usePagedHeadingFocus';
import { nextPage, previousPage } from '../members/membersModel';
import {
  listAdminHouseholdAccounts, listAdminHouseholds,
  type AdminHousehold, type AdminHouseholdAccount, type InstancePage,
} from './instanceAdminApi';

export type InstanceListState = 'loading' | 'ready' | 'forbidden' | 'notFound' | 'error';
type Failed = Exclude<InstancePage<unknown>, { kind: 'ok' }>;
/** The page a pager click could not load; its rows stay on screen until the retry succeeds. */
type PageTarget = { cursor: string | undefined; stack: string[] };

type Handlers = { onSignedOut: () => void; onForbidden?: () => void };

/** Both request guards advance when the screen goes away, so nothing late lands in a list nobody is looking at. */
function useRequestGuards(mountedRef: RefObject<boolean>, generationRef: RefObject<number>, pageGenerationRef: RefObject<number>) {
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; generationRef.current += 1; pageGenerationRef.current += 1; };
  }, [generationRef, mountedRef, pageGenerationRef]);
}

/**
 * The keyset pager both installation lists share: one request guard for the first page, quiet paging and a page
 * retry that keeps the rows already on screen. An authoritative refusal empties the list; `notFound` says the
 * household itself is gone, and `forbidden` runs the caller's session re-read so a stale navigation hint corrects
 * itself. `load` must be stable (a `useCallback`): it is what restarts the list when the target changes.
 */
function useInstancePage<T>(load: (cursor?: string) => Promise<InstancePage<T>>, { onSignedOut, onForbidden }: Handlers) {
  const [items, setItems] = useState<T[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [stack, setStack] = useState<string[]>([]);
  const [cursor, setCursor] = useState<string | undefined>();
  const [state, setState] = useState<InstanceListState>('loading');
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
  useEffect(() => {
    onSignedOutRef.current = onSignedOut;
    onForbiddenRef.current = onForbidden;
  }, [onSignedOut, onForbidden]);
  useRequestGuards(mountedRef, generationRef, pageGenerationRef);

  const fail = useCallback((page: Failed) => {
    if (page.kind === 'unauthenticated') { onSignedOutRef.current(); return; }
    if (page.kind !== 'forbidden' && page.kind !== 'notFound') { setState('error'); return; }
    setItems([]); setNextCursor(null); setPageFailure(null);
    setState(page.kind);
    if (page.kind === 'forbidden') onForbiddenRef.current?.();
  }, []);
  useEffect(() => {
    const token = ++generationRef.current;
    pageGenerationRef.current += 1;
    let cancelled = false;
    // Reset inside the async start, so the effect never sets state during render commit.
    (async () => {
      setState('loading');
      setPageFailure(null);
      setItems([]); setNextCursor(null); setStack([]); setCursor(undefined); setBusy(false);
      const page = await load();
      if (cancelled || generationRef.current !== token) return;
      if (page.kind === 'ok') {
        setItems(page.items);
        setNextCursor(page.nextCursor);
        setState('ready');
        return;
      }
      fail(page);
    })();
    return () => { cancelled = true; };
  }, [load, attempt, fail]);

  usePagedHeadingFocus(pageFocusRef, headingRef, items, busy);

  async function loadPage(target: PageTarget, activated?: HTMLElement) {
    const token = generationRef.current;
    const pageToken = ++pageGenerationRef.current;
    if (activated) pageFocusRef.current = activated;
    setPageFailure(null);
    setBusy(true);
    const page = await load(target.cursor);
    if (!mountedRef.current || generationRef.current !== token || pageGenerationRef.current !== pageToken) return;
    setBusy(false);
    if (page.kind !== 'ok') {
      // An authoritative refusal still ends the screen; anything else keeps the current page and offers a retry.
      if (page.kind === 'forbidden' || page.kind === 'unauthenticated' || page.kind === 'notFound') { pageFocusRef.current = null; fail(page); } else { setPageFailure(target); }
      return;
    }
    setItems(page.items);
    setNextCursor(page.nextCursor);
    setCursor(target.cursor);
    setStack(target.stack);
    setState('ready');
  }

  const next = (event: MouseEvent<HTMLButtonElement>) => {
    if (nextCursor && !busy) void loadPage(nextPage(stack, cursor, nextCursor), event.currentTarget);
  };
  const previous = (event: MouseEvent<HTMLButtonElement>) => { void loadPage(previousPage(stack), event.currentTarget); };
  const retry = () => setAttempt((n) => n + 1);
  /** Retries the page a pager click could not load, keeping the rows the screen already shows. */
  const retryPage = () => { if (pageFailure) void loadPage(pageFailure); };

  return {
    items, nextCursor, stack, state, busy, pageFailure, headingRef, pageFocusRef,
    next, previous, retry, retryPage,
  };
}

export type InstanceList<T> = ReturnType<typeof useInstancePage<T>>;

export function useAdminHouseholdList(handlers: Handlers): InstanceList<AdminHousehold> {
  const load = useCallback((cursor?: string) => listAdminHouseholds(cursor), []);
  return useInstancePage(load, handlers);
}

export function useAdminHouseholdAccounts(householdId: string, handlers: Handlers): InstanceList<AdminHouseholdAccount> {
  const load = useCallback((cursor?: string) => listAdminHouseholdAccounts(householdId, cursor), [householdId]);
  return useInstancePage(load, handlers);
}
