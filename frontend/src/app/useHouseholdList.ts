import { useCallback, useEffect, useRef, useState } from 'react';
import { listHouseholds, type HouseholdSummary } from './householdsApi';

export type HouseholdListState = 'idle' | 'loading' | 'ready' | 'error';

/**
 * The households the top-bar panel offers to switch between. The first page is read only while the panel is open
 * — the Home screen's request budget stays untouched — and a `100`-bounded page is followed only when the reader
 * asks for more. A `401` ends the session the same way every other screen does.
 */
export function useHouseholdList({ open, onSignedOut }: { open: boolean; onSignedOut: () => void }) {
  const [state, setState] = useState<HouseholdListState>('idle');
  const [items, setItems] = useState<HouseholdSummary[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreFailed, setMoreFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const generation = useRef(0);
  const onSignedOutRef = useRef(onSignedOut);

  useEffect(() => { onSignedOutRef.current = onSignedOut; }, [onSignedOut]);

  useEffect(() => {
    if (!open) return;
    const token = ++generation.current;
    let cancelled = false;
    // Reset inside the async start so the effect never sets state during render commit. A household list already on
    // screen stays there while it is read again; only the first read waits in the open panel.
    (async () => {
      setState((current) => (current === 'ready' ? current : 'loading'));
      setMoreFailed(false);
      setLoadingMore(false);
      const page = await listHouseholds();
      if (cancelled || generation.current !== token) return;
      if (page.kind === 'unauthenticated') { onSignedOutRef.current(); return; }
      if (page.kind === 'error') { setState('error'); return; }
      setItems(page.items);
      setNextCursor(page.nextCursor);
      setState('ready');
    })();
    return () => { cancelled = true; };
  }, [open, attempt]);

  const more = useCallback(async () => {
    if (!nextCursor) return;
    const token = generation.current;
    setLoadingMore(true);
    setMoreFailed(false);
    const page = await listHouseholds(nextCursor);
    if (generation.current !== token) return;
    setLoadingMore(false);
    if (page.kind === 'unauthenticated') { onSignedOutRef.current(); return; }
    if (page.kind !== 'ok') { setMoreFailed(true); return; }
    setItems((list) => [...list, ...page.items]);
    setNextCursor(page.nextCursor);
  }, [nextCursor]);

  /** Read the first page again, e.g. after a household turned out to be gone. */
  const reload = useCallback(() => setAttempt((n) => n + 1), []);
  const retryMore = useCallback(() => { void more(); }, [more]);

  return { state, items, nextCursor, loadingMore, moreFailed, more, reload, retryMore };
}

export type HouseholdList = ReturnType<typeof useHouseholdList>;
