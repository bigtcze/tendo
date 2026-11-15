import { useEffect, useRef, useState } from 'react';
import type { MessageKey } from '../../i18n';
import type { AccountRole } from './adminApi';

export type NoticeTone = 'neutral' | 'success';
export type NoticeSpec = { key: MessageKey; params?: Record<string, string>; tone: NoticeTone };

/** What a role change and a removal need to know about one membership row, in both Admin centers. */
export type MemberTarget = { userId: string; login: string; role: AccountRole };
export type PendingChange<A extends MemberTarget = MemberTarget> = { account: A; nextRole: AccountRole };

export type MembershipRead =
  | { kind: 'ok'; etag: string }
  | { kind: 'unauthenticated' | 'forbidden' | 'notFound' | 'rejected' | 'error' };

export type RoleChangeOutcome<A extends MemberTarget> =
  | { kind: 'ok'; account: A }
  | { kind: 'lastAdmin' | 'stale' | 'unauthenticated' | 'forbidden' | 'notFound' | 'rejected' | 'error' };

type Outcomes<A extends MemberTarget> = {
  viewerId: string;
  onSignedOut: () => void;
  onNotice: (notice: NoticeSpec) => void;
  onReload: () => void;
  onAccount: (account: A) => void;
  onForbidden: () => void;
  onSelfDemoted: () => void;
  /** A fresh membership version for one target; the caller owns which route reads it. */
  read: (userId: string) => Promise<MembershipRead>;
  /** Applies the role with exactly the tag the read returned. */
  change: (userId: string, role: AccountRole, etag: string) => Promise<RoleChangeOutcome<A>>;
};

/**
 * One role change: read the account for a fresh membership version, then quote it exactly in the PATCH.
 * Every outcome ends the dialog and speaks through the screen's live region; nothing changes locally on a
 * refusal. The routes themselves are injected, so the household and the installation Admin centers share the
 * same state machine.
 */
export function useRoleChange<A extends MemberTarget = MemberTarget>(outcomes: Outcomes<A>) {
  const [pending, setPending] = useState<PendingChange<A> | null>(null);
  const [busy, setBusy] = useState(false);
  const triggerRef = useRef<HTMLElement | null>(null);
  const busyRef = useRef(false);
  const outcomesRef = useRef(outcomes);
  const mountedRef = useRef(false);

  useEffect(() => {
    outcomesRef.current = outcomes;
  }, [outcomes]);
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  function request(account: A, trigger: HTMLElement) {
    if (busyRef.current) return;
    triggerRef.current = trigger;
    setPending({ account, nextRole: account.role === 'admin' ? 'member' : 'admin' });
  }
  function cancel() {
    if (busyRef.current) return;
    setPending(null);
  }

  async function confirm() {
    if (!pending || busyRef.current) return;
    const { account, nextRole } = pending;
    const { viewerId, onSignedOut, onNotice, onReload, onAccount, onForbidden, onSelfDemoted, read, change } = outcomesRef.current;
    busyRef.current = true;
    setBusy(true);
    const finish = () => {
      busyRef.current = false;
      if (!mountedRef.current) return;
      setBusy(false);
      setPending(null);
    };
    const self = account.userId === viewerId;
    const current = await read(account.userId);
    if (current.kind !== 'ok') {
      finish();
      if (current.kind === 'unauthenticated') onSignedOut();
      else if (current.kind === 'forbidden') onForbidden();
      else if (current.kind === 'notFound') { onNotice({ key: 'admin.error.noLongerMember', tone: 'neutral' }); onReload(); }
      else onNotice({ key: 'admin.error.action', tone: 'neutral' });
      return;
    }
    const result = await change(account.userId, nextRole, current.etag);
    finish();
    const login = account.login;
    if (result.kind === 'ok') {
      if (self) {
        onSelfDemoted();
        onNotice({ key: 'admin.notice.selfDemoted', tone: 'success' });
        return;
      }
      onAccount(result.account);
      onNotice({
        key: nextRole === 'admin' ? 'admin.notice.promoted' : 'admin.notice.demoted',
        params: { login },
        tone: 'success',
      });
      return;
    }
    if (result.kind === 'lastAdmin') onNotice({ key: 'admin.error.lastAdmin', tone: 'neutral' });
    else if (result.kind === 'stale') { onNotice({ key: 'admin.error.changedElsewhere', tone: 'neutral' }); onReload(); }
    else if (result.kind === 'notFound') { onNotice({ key: 'admin.error.noLongerMember', tone: 'neutral' }); onReload(); }
    else if (result.kind === 'forbidden') onForbidden();
    else if (result.kind === 'unauthenticated') onSignedOut();
    else onNotice({ key: 'admin.error.action', tone: 'neutral' });
  }

  return { pending, busy, triggerRef, request, cancel, confirm };
}

export type RoleChange = ReturnType<typeof useRoleChange<MemberTarget>>;
