import { useEffect, useRef, useState } from 'react';
import { readAccount, readViewerMembership, removeAccount, type AccountRemoval as RemovalResult, type AccountRole } from './adminApi';
import type { NoticeSpec } from './useRoleChange';

/** One membership on its way out: enough to name it and to tell the viewer's own row from someone else's. */
export type RemovalTarget = { userId: string; login: string; role: AccountRole };

type Outcomes = {
  householdId: string;
  viewerId: string;
  onSignedOut: () => void;
  onNotice: (notice: NoticeSpec) => void;
  /** Read the screen's data again: the server, not this dialog, decides what the list holds now. */
  onReload: () => void;
  onForbidden: () => void;
  /** The viewer's own membership is gone; the shell reads the fresh session and opens Home. */
  onSelfLeft: (alreadyGone: boolean) => void;
};

/** The target left or was removed elsewhere: they are gone, so the screen shows what the server holds now. */
function announceGone(out: Outcomes, self: boolean): void {
  if (self) { out.onSelfLeft(true); return; }
  out.onNotice({ key: 'admin.error.noLongerMember', tone: 'neutral' });
  out.onReload();
}

/** A refusal before anything was sent: nothing changed, so only the live region speaks. */
function announceReadFailure(read: { kind: 'unauthenticated' | 'forbidden' | 'notFound' | 'rejected' | 'error' }, out: Outcomes, self: boolean): void {
  if (read.kind === 'unauthenticated') { out.onSignedOut(); return; }
  if (read.kind === 'forbidden') { out.onForbidden(); return; }
  if (read.kind === 'notFound') { announceGone(out, self); return; }
  out.onNotice({ key: 'admin.error.action', tone: 'neutral' });
}

/** The server's answer, spoken after the dialog closes: success reloads, a refusal explains, nothing is guessed. */
function announceRemoval(result: RemovalResult, out: Outcomes, self: boolean, login: string): void {
  if (result.kind === 'ok') {
    if (self) { out.onSelfLeft(false); return; }
    out.onNotice({ key: 'admin.notice.removed', params: { login }, tone: 'success' });
    out.onReload();
    return;
  }
  if (result.kind === 'lastAdmin') {
    out.onNotice({ key: self ? 'admin.error.lastAdminSelf' : 'admin.error.lastAdmin', tone: 'neutral' });
    return;
  }
  if (result.kind === 'stale') {
    out.onNotice({ key: self ? 'admin.error.changedSelf' : 'admin.error.changedElsewhere', tone: 'neutral' });
    out.onReload();
    return;
  }
  if (result.kind === 'notFound') { announceGone(out, self); return; }
  if (result.kind === 'forbidden') { out.onForbidden(); return; }
  if (result.kind === 'unauthenticated') { out.onSignedOut(); return; }
  out.onNotice({ key: 'admin.error.action', tone: 'neutral' });
}

/**
 * One removal, or the viewer's own leave: read a fresh membership version, quote it exactly in the
 * DELETE, and only then change anything on screen. An admin removing someone else reads that account
 * (an admin-only route); leaving reads the household's own viewerMembershipVersion, because a plain
 * member may not call the account route. Every refusal ends the dialog and speaks through the screen's
 * live region; nothing is removed locally before the server answers 204.
 */
export function useAccountRemoval(outcomes: Outcomes) {
  const [pending, setPending] = useState<RemovalTarget | null>(null);
  const [busy, setBusy] = useState(false);
  const triggerRef = useRef<HTMLElement | null>(null);
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

  function request(target: RemovalTarget, trigger: HTMLElement) {
    if (busyRef.current) return;
    triggerRef.current = trigger;
    setPending(target);
  }
  function cancel() {
    if (busyRef.current) return;
    setPending(null);
  }

  async function confirm() {
    if (!pending || busyRef.current) return;
    const target = pending;
    const out = outcomesRef.current;
    const self = target.userId === out.viewerId;
    busyRef.current = true;
    setBusy(true);
    const finish = () => {
      busyRef.current = false;
      if (!mountedRef.current) return;
      setBusy(false);
      setPending(null);
    };
    const read = self
      ? await readViewerMembership(out.householdId)
      : await readAccount(out.householdId, target.userId);
    if (read.kind !== 'ok') {
      finish();
      announceReadFailure(read, out, self);
      return;
    }
    const etag = 'etag' in read ? read.etag : `"${read.version}"`;
    const result = await removeAccount(out.householdId, target.userId, etag);
    finish();
    announceRemoval(result, out, self, target.login);
  }

  return { pending, busy, triggerRef, request, cancel, confirm };
}

export type AccountRemoval = ReturnType<typeof useAccountRemoval>;
