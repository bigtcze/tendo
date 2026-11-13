import { useCallback, useEffect, useRef, useState } from 'react';
import { listHouseholds, type HouseholdSummary } from './householdsApi';

export type HouseholdListState = 'idle' | 'loading' | 'ready' | 'error';

/** The page just appended: its households are the rows the reader was handed when the "more" row disappeared. Its
 * `ids` are empty when that last page added nothing, so the menu hands focus back to the trigger instead. */
export type AddedHouseholds = { ids: string[]; key: number } | null;

/**
 * The households the top-bar panel offers to switch between. The list is read when the panel first opens — the
 * Home screen's request budget stays untouched — and a page (the server's default of 50; no `limit` is sent) is
 * followed only when the reader asks for more. Later opens reuse the households already on screen and read them
 * again quietly, so a known list never flashes a loading line. A `401` ends the session the way every screen does.
 *
 * It lives above the screens, in HouseholdSwitchProvider, so moving between screens never forgets the list.
 */
export function useHouseholdList({ openedAt, onSignedOut }: { openedAt: number; onSignedOut: () => void }) {
  const [state, setState] = useState<HouseholdListState>('idle');
  const [items, setItems] = useState<HouseholdSummary[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreFailed, setMoreFailed] = useState(false);
  const [added, setAdded] = useState<AddedHouseholds>(null);
  const [attempt, setAttempt] = useState(0);
  const addedKey = useRef(0);
  const loadingRef = useRef(false);
  const generation = useRef(0);
  const onSignedOutRef = useRef(onSignedOut);

  useEffect(() => { onSignedOutRef.current = onSignedOut; }, [onSignedOut]);

  useEffect(() => {
    if (!openedAt) return;
    const token = ++generation.current;
    let cancelled = false;
    // Reset inside the async start so the effect never sets state during render commit. A household list already on
    // screen stays there while it is read again; only the first read waits in the open panel.
    (async () => {
      setState((current) => (current === 'ready' ? current : 'loading'));
      setMoreFailed(false);
      setLoadingMore(false);
      loadingRef.current = false;
      const page = await listHouseholds();
      if (cancelled || generation.current !== token) return;
      if (page.kind === 'unauthenticated') { onSignedOutRef.current(); return; }
      if (page.kind === 'error') { setState('error'); return; }
      setItems(page.items);
      setNextCursor(page.nextCursor);
      setState('ready');
    })();
    return () => { cancelled = true; };
  }, [openedAt, attempt]);

  const more = useCallback(async () => {
    // A second press while the page is on its way asks for nothing more.
    if (!nextCursor || loadingRef.current) return;
    const token = generation.current;
    loadingRef.current = true;
    setLoadingMore(true);
    setMoreFailed(false);
    const page = await listHouseholds(nextCursor);
    loadingRef.current = false;
    if (generation.current !== token) return;
    setLoadingMore(false);
    if (page.kind === 'unauthenticated') { onSignedOutRef.current(); return; }
    if (page.kind !== 'ok') { setMoreFailed(true); return; }
    setItems((list) => [...list, ...page.items]);
    setNextCursor(page.nextCursor);
    // The last page takes the "more" row with it, so the reader's focus is handed to the first household it added —
    // or, when that last page added nothing, back to the trigger, so focus never falls to the top of the document.
    if (!page.nextCursor) setAdded({ ids: page.items.map((item) => item.id), key: ++addedKey.current });
  }, [nextCursor]);

  /** Read the first page again, e.g. after a household turned out to be gone. */
  const reload = useCallback(() => setAttempt((n) => n + 1), []);
  const retryMore = useCallback(() => { void more(); }, [more]);

  return { state, items, nextCursor, loadingMore, moreFailed, added, more, reload, retryMore };
}

export type HouseholdList = ReturnType<typeof useHouseholdList>;
