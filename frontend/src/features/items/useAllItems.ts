import { useCallback, useEffect, useReducer, useRef, useState } from 'react';
import type { Dispatch, FormEvent, MutableRefObject, RefObject } from 'react';
import { codePointLength } from '../../lib/textLength';
import { fetchAllItemsPage } from './allItemsLoad';
import { draftFrom, initialPage, pageReducer, type Draft, type PageAction, type PageState } from './allItemsPageState';
import { buildQuery, clearedFilters, readContext, writeLocation, type Context } from './allItemsUrl';

type Generation = MutableRefObject<number>;

/**
 * Loads the page for each context change (after a zero-delay timer, so rapid changes coalesce).
 * Every navigation bumps `generationRef` synchronously; a load applies results only while its token is current.
 * A stale cursor restarts once from the first page, replacing the context without triggering a second load.
 */
function useContextLoader(householdId: string, onSignedOut: () => void, context: Context, setContext: (next: Context) => void,
  dispatch: Dispatch<PageAction>, generationRef: Generation) {
  const [attempt, setAttempt] = useState(0);
  const mountedRef = useRef(false);
  const skipNextContextLoadRef = useRef(false);
  useEffect(() => {
    mountedRef.current = true;
    const generation = generationRef;
    return () => { mountedRef.current = false; generation.current++; };
  }, [generationRef]);

  const loadPage = useCallback(async (nextContext: Context) => {
    const token = ++generationRef.current;
    dispatch({ type: 'start' });
    async function run(pageContext: Context, allowRestart: boolean, requestToken: number): Promise<void> {
      const query = buildQuery(pageContext);
      const current = () => mountedRef.current && requestToken === generationRef.current;
      const outcome = await fetchAllItemsPage(householdId, pageContext, current);
      // Navigation can land between the loader's last check and this continuation; recheck before applying anything.
      if (outcome.kind === 'superseded' || !current()) return;
      if (outcome.kind === 'unauthenticated') { onSignedOut(); return; }
      if (outcome.kind === 'stale' && allowRestart) {
        generationRef.current++;
        dispatch({ type: 'notice', shown: true });
        const firstPage = { ...pageContext, cursor: undefined };
        writeLocation(firstPage, []);
        skipNextContextLoadRef.current = true;
        setContext(firstPage);
        await run(firstPage, false, generationRef.current);
        return;
      }
      if (outcome.kind === 'ok') dispatch({ type: 'loaded', query, summary: outcome.summary, items: outcome.items, nextCursor: outcome.nextCursor });
      else dispatch({ type: outcome.kind === 'invalidQuery' ? 'invalidQuery' : 'failed', query });
    }
    await run(nextContext, true, token);
  }, [householdId, onSignedOut, dispatch, generationRef, setContext]);

  useEffect(() => {
    if (skipNextContextLoadRef.current) {
      skipNextContextLoadRef.current = false;
      return;
    }
    const timer = window.setTimeout(() => { void loadPage(context); }, 0);
    return () => window.clearTimeout(timer);
  }, [loadPage, context, attempt]);
  return useCallback(() => setAttempt((value) => value + 1), []);
}

/** Moves focus to the results after paging, unless the user already moved focus elsewhere while the page loaded. */
function useFocusResultsAfterPaging(page: PageState, focusResultsRef: MutableRefObject<boolean>, resultRef: RefObject<HTMLElement | null>) {
  const { loading, error, items } = page;
  useEffect(() => {
    if (loading || error || !focusResultsRef.current) return;
    const active = document.activeElement;
    const pagingControl = active instanceof HTMLElement && active.matches('[data-all-items-page]');
    if (pagingControl || active === document.body || active === null) resultRef.current?.focus();
    focusResultsRef.current = false;
  }, [loading, error, items, focusResultsRef, resultRef]);
}

function usePopstateSync(generationRef: Generation, setContext: (next: Context) => void, setDraft: (draft: Draft) => void,
  dispatch: Dispatch<PageAction>) {
  useEffect(() => {
    const sync = () => {
      if (window.location.pathname !== '/items') return;
      generationRef.current++;
      const next = readContext();
      setContext(next);
      setDraft(draftFrom(next));
      dispatch({ type: 'notice', shown: false });
    };
    window.addEventListener('popstate', sync);
    return () => window.removeEventListener('popstate', sync);
  }, [generationRef, setContext, setDraft, dispatch]);
}

/** All items state: URL context, loaded page, unsubmitted form draft, and the actions that move between them. */
export function useAllItems(householdId: string, onSignedOut: () => void) {
  const [context, setContext] = useState(readContext);
  const [draft, setDraft] = useState(() => draftFrom(context));
  const [page, dispatch] = useReducer(pageReducer, initialPage);
  const generationRef = useRef(0);
  const focusResultsRef = useRef(false);
  const resultRef = useRef<HTMLElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const retry = useContextLoader(householdId, onSignedOut, context, setContext, dispatch, generationRef);
  useFocusResultsAfterPaging(page, focusResultsRef, resultRef);
  usePopstateSync(generationRef, setContext, setDraft, dispatch);
  const onNoHousehold = useCallback(() => dispatch({ type: 'facetHouseholdMissing' }), []);

  function navigate(next: Context, cursors: string[]) {
    generationRef.current++;
    writeLocation(next, cursors);
    dispatch({ type: 'notice', shown: false });
    setContext(next);
  }
  const update = (changes: Partial<Context>) => navigate({ ...context, ...changes, cursor: undefined }, []);
  const actions = {
    retry,
    update,
    setDraft: (changes: Partial<Draft>) => setDraft((current) => ({ ...current, ...changes })),
    editSearch(search: string) {
      setDraft((current) => ({ ...current, search }));
      dispatch({ type: 'searchError', shown: false });
    },
    submit(event: FormEvent) {
      event.preventDefault();
      const q = draft.search.trim();
      if (codePointLength(q) > 200) {
        dispatch({ type: 'searchError', shown: true, announce: true });
        searchRef.current?.focus();
        return;
      }
      dispatch({ type: 'searchError', shown: false });
      update({ q: q || undefined });
    },
    clearFilters() {
      setDraft(draftFrom({ status: 'active' }));
      dispatch({ type: 'searchError', shown: false });
      update(clearedFilters);
    },
    goToCursor(cursor: string | undefined, cursors: string[]) {
      focusResultsRef.current = true;
      navigate({ ...context, cursor }, cursors);
    },
  };
  return { context, draft, page, actions, resultRef, searchRef, onNoHousehold };
}

export type AllItemsActions = ReturnType<typeof useAllItems>['actions'];
