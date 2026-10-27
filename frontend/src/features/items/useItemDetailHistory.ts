import { useCallback, useRef, useState, type RefObject } from 'react';
import { listCompletions, type Completion } from './itemsApi';

const HISTORY_PAGE_SIZE = 25;

type Options = {
  headingRef: RefObject<HTMLHeadingElement | null>; sectionRef: RefObject<HTMLElement | null>;
  householdId: string; itemId: string; onSignedOut: () => void;
  /** Whether a detail load generation is still the live one. */
  current: (token: number) => boolean;
  /** The live detail load generation, read when the user pages. */
  liveToken: () => number;
};

/** Item history as replacement pages of 25, newest first, with a cursor stack for Older / Newer / Latest. */
export function useItemDetailHistory({ headingRef, sectionRef, householdId, itemId, onSignedOut, current, liveToken }: Options) {
  const [history, setHistory] = useState<Completion[]>([]);
  const [state, setState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [cursors, setCursors] = useState<(string | null)[]>([null]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const generation = useRef(0);
    const focusAfterPage = useRef(false);

  const load = useCallback(async (token: number, cursor: string | null = null) => {
    const historyToken = ++generation.current;
    setState('loading');
    const result = await listCompletions(householdId, itemId, cursor ?? undefined, { order: 'newest', limit: HISTORY_PAGE_SIZE });
    if (!current(token) || generation.current !== historyToken) return;
    if (result.kind === 'unauthenticated') { onSignedOut(); return; }
    const moveFocus = focusAfterPage.current; focusAfterPage.current = false;
    if (result.kind !== 'ok') { setState('error'); return; }
    setHistory(result.completions); setState('ready'); setNextCursor(result.nextCursor);
    // The pressed paging control unmounts while loading; only reclaim focus if it was left there (on body or in History).
    const active = document.activeElement;
    if (moveFocus && (!active || active === document.body || sectionRef.current?.contains(active))) headingRef.current?.focus();
  }, [householdId, itemId, current, onSignedOut, headingRef, sectionRef]);

  /** Invalidates any in-flight page and returns to an empty first page; called when the item reloads. */
  const reset = useCallback(() => {
    generation.current += 1;
    focusAfterPage.current = false;
    setHistory([]); setState('loading'); setCursors([null]); setNextCursor(null);
  }, []);

  function showPage(cursor: string | null) {
    focusAfterPage.current = true;
    void load(liveToken(), cursor);
  }
  function older() {
    if (!nextCursor || state === 'loading') return;
    const cursor = nextCursor;
    setCursors((stack) => [...stack, cursor]);
    showPage(cursor);
  }
  function newer() {
    if (cursors.length <= 1 || state === 'loading') return;
    const next = cursors.slice(0, -1);
    setCursors(next);
    showPage(next[next.length - 1] ?? null);
  }
  function latest() {
    if (cursors.length <= 1 || state === 'loading') return;
    setCursors([null]);
    showPage(null);
  }
  function retry() {
    showPage(cursors.at(-1) ?? null);
  }

  return { history, state, cursors, nextCursor, load, reset, older, newer, latest, retry };
}

export type ItemDetailHistoryState = ReturnType<typeof useItemDetailHistory>;
