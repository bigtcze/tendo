import { useEffect, useRef, useState } from 'react';
import type { MessageKey } from '../../i18n';
import { changeAccountRole, readAccount, type Account, type AccountRole } from './adminApi';

export type NoticeTone = 'neutral' | 'success';
export type NoticeSpec = { key: MessageKey; params?: Record<string, string>; tone: NoticeTone };
export type PendingChange = { account: Account; nextRole: AccountRole };

type Outcomes = {
  householdId: string;
  viewerId: string;
  onSignedOut: () => void;
  onNotice: (notice: NoticeSpec) => void;
  onReload: () => void;
  onAccount: (account: Account) => void;
  onForbidden: () => void;
  onSelfDemoted: () => void;
};

/**
 * One role change: read the account for a fresh membership version, then quote it exactly in the PATCH.
 * Every outcome ends the dialog and speaks through the screen's live region; nothing changes locally on a refusal.
 */
export function useRoleChange(outcomes: Outcomes) {
  const [pending, setPending] = useState<PendingChange | null>(null);
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

  function request(account: Account, trigger: HTMLElement) {
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
    const { householdId, viewerId, onSignedOut, onNotice, onReload, onAccount, onForbidden, onSelfDemoted } = outcomesRef.current;
    busyRef.current = true;
    setBusy(true);
    const finish = () => {
      busyRef.current = false;
      if (!mountedRef.current) return;
      setBusy(false);
      setPending(null);
    };
    const self = account.userId === viewerId;
    const read = await readAccount(householdId, account.userId);
    if (read.kind !== 'ok') {
      finish();
      if (read.kind === 'unauthenticated') onSignedOut();
      else if (read.kind === 'forbidden') onForbidden();
      else if (read.kind === 'notFound') { onNotice({ key: 'admin.error.noLongerMember', tone: 'neutral' }); onReload(); }
      else onNotice({ key: 'admin.error.action', tone: 'neutral' });
      return;
    }
    const result = await changeAccountRole(householdId, account.userId, nextRole, read.etag);
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

export type RoleChange = ReturnType<typeof useRoleChange>;
