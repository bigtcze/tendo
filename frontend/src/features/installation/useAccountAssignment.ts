import { useEffect, useRef, useState, type FormEvent } from 'react';
import { codePointLength } from '../../lib/textLength';
import type { NoticeSpec } from '../admin/useRoleChange';
import { nextPage, previousPage } from '../members/membersModel';
import {
  assignAdminHouseholdAccount, listAdminDirectory,
  type AdminAccountRole, type AdminAssignment, type DirectoryAccount,
} from './instanceAdminApi';

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

/** A refusal that keeps the dialog open and says where the reader has to look for the answer. */
export type AssignmentFailure = 'alreadyMember' | 'requiresAdmin' | 'failed';

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
 * The account directory behind the dialog: one keyset list of its own, which never reads or reloads the household's
 * member list. A search only ever runs when the reader asks for it, so typing never searches by itself; the empty
 * search is a search like any other and the server answers it with everyone.
 */
function useDirectorySearch(handlers: DirectoryHandlers) {
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

type Outcomes = {
  viewerId: string;
  onSignedOut: () => void;
  /** An authoritative refusal of the installation grant: the screen drops its rows and the shell re-reads. */
  onForbidden: () => void;
  onNotice: (notice: NoticeSpec) => void;
  /** Read the screen's list again: the server, not this dialog, decides who belongs to the household now. */
  onReload: () => void;
  /** The viewer added themselves, so the server may have made this their household: the shell reads the session. */
  onSelfAssigned: (notice: NoticeSpec) => void;
};

/** Why the dialog is still open after the server answered: null means the add landed and the dialog closes. */
function settleAssignment(
  result: AdminAssignment, target: DirectoryAccount, role: AdminAccountRole, out: Outcomes,
): AssignmentFailure | null {
  const self = target.userId === out.viewerId;
  switch (result.kind) {
    case 'created':
      out.onReload();
      // The server only makes this household the viewer's own when they had none, so the shell has to look again.
      if (self) out.onSelfAssigned(selfNotice(role));
      else out.onNotice({ key: 'install.notice.added', params: { login: target.login }, tone: 'success' });
      return null;
    case 'unchanged':
      out.onReload();
      // Their own membership reads in the second person; anyone else's needs the login to say who it is about.
      out.onNotice(self
        ? { key: 'install.notice.alreadyAssignedSelf', tone: 'neutral' }
        : { key: 'install.notice.alreadyAssigned', params: { login: target.login }, tone: 'neutral' });
      return null;
    case 'notFound':
      out.onReload();
      out.onNotice({ key: 'install.notice.assignmentGone', tone: 'neutral' });
      return null;
    case 'forbidden':
      out.onForbidden();
      return null;
    case 'unauthenticated':
      out.onSignedOut();
      return null;
    case 'alreadyMember':
      return 'alreadyMember';
    case 'requiresAdmin':
      return 'requiresAdmin';
    default:
      return 'failed';
  }
}

/** The viewer's own new membership, said in the second person instead of their own login. */
function selfNotice(role: AdminAccountRole): NoticeSpec {
  return { key: role === 'admin' ? 'install.notice.selfPromoted' : 'install.notice.selfDemoted', tone: 'success' };
}

/**
 * The screen's one way of putting an existing account into this household: search the account directory, choose an
 * account, choose the role, then let the server answer.
 *
 * Opening the dialog runs the empty search, which lists everyone: the choices are there at once, so a reader who
 * knows the login can type over them and one who does not is never asked to guess a query first.
 */
export function useAccountAssignment(householdId: string, outcomes: Outcomes) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [chosen, setChosen] = useState<DirectoryAccount | null>(null);
  const [role, setRole] = useState<AdminAccountRole>('member');
  const [failure, setFailure] = useState<AssignmentFailure | null>(null);
  const [busy, setBusy] = useState(false);
  const triggerRef = useRef<HTMLElement | null>(null);
  const busyRef = useRef(false);
  const outcomesRef = useRef(outcomes);
  const mountedRef = useRef(false);
  const directory = useDirectorySearch({
    onSignedOut: () => outcomesRef.current.onSignedOut(),
    // A refusal of the grant ends the dialog with the screen: there is nothing left to add an account to here.
    onForbidden: () => { setOpen(false); outcomesRef.current.onForbidden(); },
    // Any new search, page or retry replaces the choices that were on screen, so a stale choice is dropped with them
    // rather than left selectable: adding an account that is no longer in front of the reader is never one click away.
    onReadStart: () => { setChosen(null); setFailure(null); },
  });

  useEffect(() => { outcomesRef.current = outcomes; });
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  /** Opens the dialog for one trigger, on a clean slate: no account chosen, the member role, no refusal left over. */
  function start(trigger: HTMLElement) {
    if (busyRef.current) return;
    triggerRef.current = trigger;
    setOpen(true);
    setQuery('');
    setChosen(null);
    setRole('member');
    setFailure(null);
    directory.reset();
    directory.start('');
  }

  function submitSearch(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (busyRef.current) return;
    const needle = query.trim();
    // The server counts code points, so a search of 65 astral characters is refused here, not by the server.
    if (codePointLength(needle) > MAX_QUERY) {
      directory.refuse(needle);
      return;
    }
    setFailure(null);
    directory.start(needle);
  }

  function choose(account: DirectoryAccount) {
    setChosen(account);
    setFailure(null);
  }

  function chooseRole(option: AdminAccountRole) {
    setRole(option);
    setFailure(null);
  }

  function cancel() {
    if (busyRef.current) return;
    setOpen(false);
  }

  async function submit() {
    if (!chosen || busyRef.current) return;
    const out = outcomesRef.current;
    const target = chosen;
    busyRef.current = true;
    setBusy(true);
    setFailure(null);
    const result = await assignAdminHouseholdAccount(householdId, target.userId, role);
    busyRef.current = false;
    if (!mountedRef.current) return;
    setBusy(false);
    const refused = settleAssignment(result, target, role, out);
    if (refused) { setFailure(refused); return; }
    setOpen(false);
  }

  return {
    open, busy, triggerRef, query, chosen, role, failure,
    search: directory.search, queryError: directory.queryError, refusals: directory.refusals,
    start, editQuery: setQuery, submitSearch, next: directory.next, previous: directory.previous,
    retry: directory.retry, choose, chooseRole, cancel, submit,
  };
}

export type Assignment = ReturnType<typeof useAccountAssignment>;
