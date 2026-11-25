import { useCallback, useEffect, useRef, useState, type RefObject } from 'react';

/** `loading` only before the first attempt settles; a failed first load stays `error` so the list can offer Retry. */
export type ShowMoreListState = 'loading' | 'ready' | 'error';

type Page<T> = { kind: 'ok'; items: T[]; nextCursor: string | null };
/** One page of an append-only list; any other answer is the reader's `F`. */
export type ShowMorePage<T, F> = Page<T> | F;

const isPage = <T, F>(answer: ShowMorePage<T, F>): answer is Page<T> => (answer as { kind: string }).kind === 'ok';

export type ShowMoreListOptions<T, F> = {
  read: (cursor?: string) => Promise<ShowMorePage<T, F>>;
  /**
   * Takes the answers that end the list's own work (a session that ended, a grant that is gone) and says whether it
   * took this one. `duringAction` is false only for the first read on arrival; any later answer follows something
   * the reader did, so the screen moves focus for it.
   */
  handOff: (failure: F, duringAction: boolean) => boolean;
};

/**
 * A newest-first list read by cursor: the first page loads on mount and after every `reload`, and Show more appends
 * the next page below the rows already shown. A failed refresh or Show more keeps those rows and offers Try again.
 * Each read takes a generation number, so an answer overtaken by a newer read or by unmount never writes stale rows
 * back. The options are read when used, so callers need not keep them stable.
 */
export function useShowMoreList<T, F extends { kind: string }>(options: ShowMoreListOptions<T, F>) {
  const [state, setState] = useState<ShowMoreListState>('loading');
  const [items, setItems] = useState<T[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  /** A refresh failed: shown rows stay, and the list offers Try again. */
  const [refreshFailed, setRefreshFailed] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreFailed, setMoreFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const generation = useRef(0);
  const optionsRef = useRef(options);

  useEffect(() => {
    optionsRef.current = options;
  });

  // The first page is read in the effect, not synchronously in a callback: `reload` only bumps `attempt`.
  useEffect(() => {
    const current = ++generation.current;
    (async () => {
      const page = await optionsRef.current.read();
      if (current !== generation.current) return;
      if (!isPage(page)) {
        // Only the first read (attempt 0) is on arrival; a retry or a re-read after an action follows one.
        if (optionsRef.current.handOff(page, attempt > 0)) return;
        setState((previous) => (previous === 'loading' ? 'error' : previous));
        setRefreshFailed(true);
        return;
      }
      setItems(page.items);
      setNextCursor(page.nextCursor);
      setMoreFailed(false);
      setRefreshFailed(false);
      setState('ready');
      // A Show more sent before this page arrived names a cursor from the rows it replaced; its page would leave a gap.
      generation.current += 1;
    })();
    return () => { generation.current += 1; };
  }, [attempt]);

  const reload = useCallback(() => setAttempt((n) => n + 1), []);

  async function loadMore() {
    if (!nextCursor || loadingMore) return;
    const current = generation.current;
    setLoadingMore(true);
    const page = await optionsRef.current.read(nextCursor);
    // Cleared even when a newer read overtook this page, so Show more can never stay stuck while loading.
    setLoadingMore(false);
    if (current !== generation.current) return;
    if (!isPage(page)) {
      if (!optionsRef.current.handOff(page, true)) setMoreFailed(true);
      return;
    }
    setItems((existing) => [...existing, ...page.items]);
    setNextCursor(page.nextCursor);
    setMoreFailed(false);
  }

  /** Drops rows the server no longer has without reading again. */
  const remove = useCallback((keep: (item: T) => boolean) => setItems((existing) => existing.filter(keep)), []);

  /** Every row is gone on the server: they leave at once and the list is read again to match it. */
  const cleared = useCallback(() => {
    setItems([]);
    setNextCursor(null);
    setMoreFailed(false);
    reload();
  }, [reload]);

  return { state, items, nextCursor, refreshFailed, loadingMore, moreFailed, reload, loadMore, remove, cleared };
}

export type ShowMoreList<T> = ReturnType<typeof useShowMoreList<T, { kind: string }>>;


/**
 * Keeps focus on the list after Show more: the button stays focusable while its page loads (`aria-disabled`), and
 * when the last page takes the button away, focus moves to the first row that page added, or its first control.
 * Call `mark` with the row count just before Show more.
 */
export function useAppendedRowFocus(listRef: RefObject<HTMLUListElement | null>, count: number, loadingMore: boolean) {
  const marked = useRef<number | null>(null);
  useEffect(() => {
    if (marked.current === null || loadingMore) return;
    const first = marked.current;
    marked.current = null;
    const lost = !document.activeElement || document.activeElement === document.body;
    if (!lost || count <= first) return;
    const row = listRef.current?.children[first] as HTMLElement | undefined;
    (row?.querySelector<HTMLElement>('button, a[href]') ?? row)?.focus();
  }, [count, loadingMore, listRef]);
  return useCallback((rows: number) => { marked.current = rows; }, []);
}
