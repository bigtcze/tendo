import { useEffect, useRef, useState } from 'react';
import { codePointLength } from '../../lib/textLength';
import type { NoticeSpec } from '../admin/useRoleChange';
import {
  assignAdminHouseholdAccount,
  type AdminAccountRole, type AdminAssignment, type DirectoryAccount,
} from './instanceAdminApi';
import { MAX_QUERY, useDirectorySearch } from './useDirectorySearch';

/** A refusal that keeps the dialog open and says where the reader has to look for the answer. */
export type AssignmentFailure = 'alreadyMember' | 'requiresAdmin' | 'failed';

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

  function searchDirectory() {
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
    start, editQuery: setQuery, searchDirectory, next: directory.next, previous: directory.previous,
    retry: directory.retry, choose, chooseRole, cancel, submit,
  };
}

export type Assignment = ReturnType<typeof useAccountAssignment>;
