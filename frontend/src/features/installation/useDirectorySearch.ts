import { useEffect, useRef, useState } from 'react';
import { nextPage, previousPage } from '../members/membersModel';
import { listAdminDirectory, type DirectoryAccount } from './instanceAdminApi';

/** The longest login search the server accepts; anything longer is answered on the field itself, with no request. */
export const MAX_QUERY = 64;

/** Where the directory pane is: no search worth showing, one in flight, a page in hand, or a failure to retry. */
export type DirectoryState = 'idle' | 'searching' | 'ready' | 'failed';

/** One page of the directory: which search it answers, and where in it the reader is. */
type PageTarget = { needle: string; cursor: string | undefined; stack: string[] };

/** One search of the account directory and the page it answers, or the page a reader moved to. */
export type DirectorySearch = {
  state: DirectoryState;
  /** The login substring this page answers; empty means every account. */
  needle: string;
  items: DirectoryAccount[];
  nextCursor: string | null;
  cursor: string | undefined;
  stack: string[];
  /** A page is on its way under the choices already on screen. */
  busy: boolean;
  /** The page a read could not load, so a retry asks for that page and not whichever one is now on screen. */
  failed: PageTarget | null;
};

const NO_SEARCH: DirectorySearch = {
  state: 'idle', needle: '', items: [], nextCursor: null, cursor: undefined, stack: [], busy: false, failed: null,
};

type DirectoryHandlers = {
  onSignedOut: () => void;
  onForbidden: () => void;
  /** A read is starting: its page is a new set of choices, so a choice made against the previous ones is gone. */
  onReadStart: () => void;
};

/**
 * The account directory behind a picker: one keyset list of its own, which never reads or reloads a household's
 * member list. A search only ever runs when the reader asks for it, so typing never searches by itself; the empty
 * search is a search like any other and the server answers it with everyone.
 *
 * Two admin dialogs pick an existing account from here — adding one to a household, and choosing a new household's
 * first administrator — so the paging, refusal and stale-answer behavior lives in one place.
 */
export function useDirectorySearch(handlers: DirectoryHandlers) {
  const [search, setSearch] = useState<DirectorySearch>(NO_SEARCH);
  const [queryError, setQueryError] = useState(false);
  /** Counts refusals, so every one of them can put the reader back on the field even when it was refused before. */
  const [refusals, setRefusals] = useState(0);
  /** Every new search invalidates the answers still in flight, so a slow page can never land over a newer one. */
  const requestRef = useRef(0);
  const mountedRef = useRef(false);
  const handlersRef = useRef(handlers);

  useEffect(() => { handlersRef.current = handlers; });
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  /** One page of the directory; `search` starts again at the first page, `page` keeps the rows that are on screen. */
  async function read(target: PageTarget, mode: 'search' | 'page') {
    const token = ++requestRef.current;
    handlersRef.current.onReadStart();
    setQueryError(false);
    setSearch((current) => (mode === 'search'
      ? { ...NO_SEARCH, state: 'searching', needle: target.needle }
      : { ...current, state: 'searching', busy: true, failed: null }));
    const page = await listAdminDirectory(target.needle, target.cursor);
    if (!mountedRef.current || token !== requestRef.current) return;
    if (page.kind === 'ok') {
      setSearch({
        state: 'ready', needle: target.needle, items: page.items, nextCursor: page.nextCursor,
        cursor: target.cursor, stack: target.stack, busy: false, failed: null,
      });
      return;
    }
    if (page.kind === 'invalidQuery') {
      // The search itself was refused, so there is nothing to retry: the field says what to fix.
      refuse(target.needle);
      return;
    }
    if (page.kind === 'unauthenticated') { handlersRef.current.onSignedOut(); return; }
    if (page.kind === 'forbidden') { handlersRef.current.onForbidden(); return; }
    setSearch((current) => ({ ...current, state: 'failed', busy: false, failed: target }));
  }

  function start(needle: string) { void read({ needle, cursor: undefined, stack: [] }, 'search'); }

  function next() {
    if (search.busy || !search.nextCursor) return;
    const target = nextPage(search.stack, search.cursor, search.nextCursor);
    void read({ needle: search.needle, cursor: target.cursor, stack: target.stack }, 'page');
  }

  function previous() {
    if (search.busy || !search.stack.length) return;
    const target = previousPage(search.stack);
    void read({ needle: search.needle, cursor: target.cursor, stack: target.stack }, 'page');
  }

  /**
   * The page that failed, not the one that is on screen: the rows of the page in hand stay exactly where they were
   * while the page that could not be read is asked for again.
   */
  function retry() {
    if (!search.failed) return;
    void read(search.failed, 'page');
  }

  function reset() {
    requestRef.current += 1;
    setSearch(NO_SEARCH);
    setQueryError(false);
    setRefusals(0);
  }

  /** The search was refused before it could answer: the field says what to fix and no rows are left behind. */
  function refuse(needle: string) {
    requestRef.current += 1;
    setSearch({ ...NO_SEARCH, needle });
    setQueryError(true);
    setRefusals((count) => count + 1);
  }

  return { search, queryError, refusals, start, next, previous, retry, reset, refuse };
}

export type Directory = ReturnType<typeof useDirectorySearch>;
