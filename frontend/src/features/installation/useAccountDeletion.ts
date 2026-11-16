import { useEffect, useRef, useState, type RefObject } from 'react';
import type { NoticeSpec } from '../admin/useRoleChange';
import { deleteAdminAccount, type DirectoryAccount } from './instanceAdminApi';

/**
 * Why the dialog is still open after the server answered. `mismatch` belongs on the confirmation field; every other
 * one is a calm sentence under the consequences, and the reader decides whether to try again.
 */
export type DeletionFailure =
  | { kind: 'mismatch' | 'lastAdmin' | 'lastInstanceAdmin' | 'self' | 'rejected' | 'error' };

type Outcomes = {
  onSignedOut: () => void;
  /** An authoritative refusal of the installation grant: the screen drops its rows and the shell re-reads the session. */
  onForbidden: () => void;
  onNotice: (notice: NoticeSpec) => void;
  /** Read the list again: the server, not this dialog, decides which accounts exist now. */
  onReload: () => void;
  /** Where the focus lands once the account is gone: its row, and the control that opened the dialog, go with it. */
  landingRef: RefObject<HTMLElement | null>;
};

/**
 * One installation account on its way out of Tendo. Deleting is not removing a membership: the account disappears
 * from every household at once, so the dialog asks the reader to repeat the login and the screen then believes only
 * the server. A deletion that landed, and one whose account is already gone, both close the dialog, speak, and read
 * the list again; every refusal that changed nothing keeps the dialog and everything typed in it.
 *
 * No local row is removed before the server answers `204`, and no trigger is disabled by the reload this starts
 * afterwards. A refusal or a cancel hands the focus back to the control that opened the dialog; once the account is
 * gone that control leaves with its row, so the focus goes to the list heading instead of falling to the page. When
 * the grant itself is refused, the dialog goes with the screen — the calm refusal has no trigger left to point at, so
 * the screen places the focus on its heading.
 */
export function useAccountDeletion(outcomes: Outcomes) {
  const [target, setTarget] = useState<DirectoryAccount | null>(null);
  const [confirmation, setConfirmation] = useState('');
  const [failure, setFailure] = useState<DeletionFailure | null>(null);
  const [busy, setBusy] = useState(false);
  const triggerRef = useRef<HTMLElement | null>(null);
  /** Set only when the account is gone; otherwise the dialog falls back to the trigger. */
  const returnFocusRef = useRef<HTMLElement | null>(null);
  const busyRef = useRef(false);
  const outcomesRef = useRef(outcomes);
  const mountedRef = useRef(false);

  useEffect(() => {
    outcomesRef.current = outcomes;
  });
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  /** Opens the dialog for one trigger on a clean slate: nothing typed, no refusal left over. */
  function request(account: DirectoryAccount, trigger: HTMLElement) {
    if (busyRef.current) return;
    triggerRef.current = trigger;
    returnFocusRef.current = null;
    setTarget(account);
    setConfirmation('');
    setFailure(null);
  }

  /**
   * The repeat is corrected here, so an answer about the text goes as soon as it is no longer true. Every other
   * refusal stays until the next attempt: it explains why the deletion was refused, which typing does not change.
   */
  function edit(value: string) {
    setConfirmation(value);
    if (failure?.kind === 'mismatch') setFailure(null);
  }

  function cancel() {
    if (busyRef.current) return;
    setTarget(null);
  }

  async function confirm() {
    if (!target || busyRef.current) return;
    const account = target;
    busyRef.current = true;
    setBusy(true);
    setFailure(null);
    const result = await deleteAdminAccount(account.userId, confirmation);
    busyRef.current = false;
    if (!mountedRef.current) return;
    // Read after the await: the screen may have handed over new outcomes while the request was running.
    const out = outcomesRef.current;
    setBusy(false);
    if (result.kind === 'ok' || result.kind === 'notFound') returnFocusRef.current = out.landingRef.current;
    if (result.kind === 'ok') {
      setTarget(null);
      out.onNotice({ key: 'install.notice.deleted', params: { login: account.login }, tone: 'success' });
      out.onReload();
      return;
    }
    if (result.kind === 'notFound') {
      setTarget(null);
      out.onNotice({ key: 'install.notice.deletedGone', tone: 'neutral' });
      out.onReload();
      return;
    }
    if (result.kind === 'unauthenticated') { out.onSignedOut(); return; }
    // The grant is gone: the dialog cannot stay over a screen that has nothing left to administer.
    if (result.kind === 'forbidden') { setTarget(null); out.onForbidden(); return; }
    setFailure({ kind: result.kind });
  }

  return { target, confirmation, failure, busy, triggerRef, returnFocusRef, request, edit, cancel, confirm };
}

export type AccountDeletion = ReturnType<typeof useAccountDeletion>;
