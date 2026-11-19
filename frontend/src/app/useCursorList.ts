import { useCallback, useEffect, useRef, useState, type MouseEvent, type RefObject } from 'react';
import { nextPage, previousPage } from '../features/members/membersModel';
import { usePagedHeadingFocus } from './usePagedHeadingFocus';

/** One page of any cursor list; only `ok` carries rows. */
export type PageResult<T> =
  | { kind: 'ok'; items: T[]; nextCursor: string | null }
  | { kind: 'unauthenticated' | 'forbidden' | 'notFound' | 'rejected' | 'error' };

export type CursorListState = 'loading' | 'ready' | 'forbidden' | 'notFound' | 'error';
type Failed = Exclude<PageResult<unknown>, { kind: 'ok' }>;
/** A page to read: the target of a pager click, of a quiet reload, or of a reload's step back. */
type PageTarget = { cursor: string | undefined; stack: string[] };
/**
 * The page a pager click or a reload could not load; its rows stay on screen until the retry succeeds. A retry
 * keeps the read's own mode, so a retried reload still steps back from a page that has emptied meanwhile.
 */
type PageFailure = PageTarget & { stepBack: boolean };

/**
 * How a `404` reads. `retryable` is the household Admin center's account list, where a missing household is a
 * generic failure on the first page and a page retry while paging. `authoritative` is the installation lists,
 * where a missing household is a state of its own and ends the screen.
 */
export type NotFoundPolicy = 'retryable' | 'authoritative';

export type CursorListOptions<T> = {
  /** Reads one page for the cursor; must be stable (a `useCallback`), because a new function restarts the list. */
  load: (cursor?: string) => Promise<PageResult<T>>;
  onSignedOut: () => void;
  onForbidden?: () => void;
  notFound?: NotFoundPolicy;
  /** The row identity a replacement matches; only rows the list itself already holds are replaced. */
  keyOf: (item: T) => string;
};

/** Both request guards advance when the screen goes away, so nothing late lands in a list nobody is looking at. */
function useRequestGuards(mountedRef: RefObject<boolean>, generationRef: RefObject<number>, pageGenerationRef: RefObject<number>) {
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; generationRef.current += 1; pageGenerationRef.current += 1; };
  }, [generationRef, mountedRef, pageGenerationRef]);
}

/**
 * The loaded pages themselves: a request guard for the first page, quiet paging, a page retry that keeps the rows
 * already on screen, and the quiet reload of the current page (or of the nearest earlier page that still has rows). An authoritative refusal empties the list;
 * `forbidden` then runs the caller's session re-read so a stale navigation hint corrects itself.
 */
function useCursorPages<T>({ load, onSignedOut, onForbidden, notFound = 'retryable', keyOf }: CursorListOptions<T>) {
  const [items, setItems] = useState<T[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [stack, setStack] = useState<string[]>([]);
  const [cursor, setCursor] = useState<string | undefined>();
  const [state, setState] = useState<CursorListState>('loading');
  const [pageFailure, setPageFailure] = useState<PageFailure | null>(null);
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
    const endsScreen = page.kind === 'forbidden' || (page.kind === 'notFound' && notFound === 'authoritative');
    if (!endsScreen) { setState('error'); return; }
    setItems([]); setNextCursor(null); setPageFailure(null);
    setState(page.kind === 'forbidden' ? 'forbidden' : 'notFound');
    if (page.kind === 'forbidden') onForbiddenRef.current?.();
  }, [notFound]);
  // Reset inside the async start, so the effect never sets state during render commit.
  useEffect(() => {
    const token = ++generationRef.current;
    pageGenerationRef.current += 1;
    let cancelled = false;
    (async () => {
      setState('loading'); setPageFailure(null);
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

  /**
   * Reads one page. With `stepBack`, a later page that comes back empty and ends the list is not shown: its rows
   * went away since it was read (deleted, removed, left), so the list steps back page by page to the nearest one
   * that still has rows, or to the first page. A pager click never steps back — it shows the page the reader asked for.
   */
  async function loadPage(target: PageTarget, activated?: HTMLElement, stepBack = false): Promise<void> {
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
      const endsScreen = page.kind === 'forbidden' || page.kind === 'unauthenticated'
        || (page.kind === 'notFound' && notFound === 'authoritative');
      if (endsScreen) { pageFocusRef.current = null; fail(page); } else { setPageFailure({ ...target, stepBack }); }
      return;
    }
    if (stepBack && page.items.length === 0 && page.nextCursor === null && target.stack.length > 0) {
      return loadPage(previousPage(target.stack), undefined, true);
    }
    setItems(page.items);
    setNextCursor(page.nextCursor);
    setCursor(target.cursor);
    setStack(target.stack);
    setState('ready');
  }

  const replaceItem = (item: T) => setItems((list) => list.map((row) => (keyOf(row) === keyOf(item) ? item : row)));
  const markForbidden = () => { setItems([]); setNextCursor(null); setPageFailure(null); setState('forbidden'); };

  return {
    items, nextCursor, stack, cursor, state, busy, pageFailure, headingRef, pageFocusRef,
    loadPage, retry: () => setAttempt((n) => n + 1), replaceItem, markForbidden,
  };
}

/**
 * The pager the screens render: page steps, the inline page retry, and a quiet reload of the current page that
 * steps back to the nearest earlier page with rows when the current one has emptied.
 */
export function useCursorList<T>(options: CursorListOptions<T>) {
  const pages = useCursorPages(options);
  const { cursor, stack, nextCursor, busy, pageFailure, loadPage, ...rest } = pages;
  const next = (event: MouseEvent<HTMLButtonElement>) => {
    if (nextCursor && !busy) void loadPage(nextPage(stack, cursor, nextCursor), event.currentTarget);
  };
  const previous = (event: MouseEvent<HTMLButtonElement>) => { void loadPage(previousPage(stack), event.currentTarget); };
  const retryPage = () => { if (pageFailure) void loadPage(pageFailure, undefined, pageFailure.stepBack); };
  /** Reads the current page again after a change; an emptied later page steps back to one that still has rows. */
  const reload = () => { void loadPage({ cursor, stack }, undefined, true); };
  return { ...rest, stack, nextCursor, busy, pageFailure, next, previous, retryPage, reload };
}

export type CursorList<T> = ReturnType<typeof useCursorList<T>>;
