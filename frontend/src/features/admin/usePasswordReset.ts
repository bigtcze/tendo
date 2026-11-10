import { useEffect, useRef, useState } from 'react';
import {
  cancelPasswordReset, issuePasswordReset, type Account, type PasswordResetCancel, type PasswordResetIssue,
} from './adminApi';
import type { NoticeSpec } from './useRoleChange';

export type ResetMode = 'issue' | 'renew';
/** The confirmation still to be answered; a row with an outstanding link offers a replacement. */
export type PendingReset = { account: Account; mode: ResetMode };
/** The one-time link, held in memory only: closing the dialog drops it for good. */
export type IssuedReset = { account: Account; link: string; expiresAt: string };

type Outcomes = {
  householdId: string;
  onSignedOut: () => void;
  onNotice: (notice: NoticeSpec) => void;
  onReload: () => void;
  onAccount: (account: Account) => void;
  onForbidden: () => void;
};
type Announcements = Pick<Outcomes, 'onSignedOut' | 'onNotice' | 'onReload' | 'onForbidden'>;
type IssueFailure = Exclude<PasswordResetIssue['kind'], 'issued'>;
type CancelFailure = Exclude<PasswordResetCancel['kind'], 'ok'>;

/** Every refusal is calm and specific; a lost admin role re-reads the household like a role change does. */
function announceIssueFailure(kind: IssueFailure, login: string, out: Announcements) {
  if (kind === 'self') out.onNotice({ key: 'admin.error.resetSelf', tone: 'neutral' });
  else if (kind === 'noLocalPassword') {
    out.onNotice({ key: 'admin.error.resetNoPassword', params: { login }, tone: 'neutral' });
    out.onReload();
  } else if (kind === 'notFound') {
    out.onNotice({ key: 'admin.error.noLongerMember', tone: 'neutral' });
    out.onReload();
  } else if (kind === 'forbidden') out.onForbidden();
  else if (kind === 'unauthenticated') out.onSignedOut();
  else if (kind === 'rateLimited') out.onNotice({ key: 'admin.error.resetRateLimited', tone: 'neutral' });
  else out.onNotice({ key: 'admin.error.resetUnavailable', tone: 'neutral' });
}

function announceCancelFailure(kind: CancelFailure, out: Announcements) {
  if (kind === 'self') out.onNotice({ key: 'admin.error.resetSelf', tone: 'neutral' });
  else if (kind === 'notFound') {
    out.onNotice({ key: 'admin.error.noLongerMember', tone: 'neutral' });
    out.onReload();
  } else if (kind === 'forbidden') out.onForbidden();
  else if (kind === 'unauthenticated') out.onSignedOut();
  else if (kind === 'rateLimited') out.onNotice({ key: 'admin.error.resetRateLimited', tone: 'neutral' });
  else out.onNotice({ key: 'admin.error.resetCancel', tone: 'neutral' });
}

/** Admin-issued reset links. A refusal changes nothing locally and is announced by the screen's live region. */
export function usePasswordReset(outcomes: Outcomes) {
  const [pending, setPending] = useState<PendingReset | null>(null);
  const [issued, setIssued] = useState<IssuedReset | null>(null);
  const [busy, setBusy] = useState(false);
  const triggerRef = useRef<HTMLElement | null>(null);
  const busyRef = useRef(false);
  const mountedRef = useRef(false);
  const outcomesRef = useRef(outcomes);

  useEffect(() => { outcomesRef.current = outcomes; });
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  function request(account: Account, trigger: HTMLElement) {
    if (busyRef.current) return;
    triggerRef.current = trigger;
    setPending({ account, mode: account.passwordReset ? 'renew' : 'issue' });
  }
  function cancelRequest() {
    if (busyRef.current) return;
    setPending(null);
  }
  /** Closing the link is deliberate: the token leaves component state and cannot be shown again. */
  function closeLink() {
    setIssued(null);
  }

  async function confirm() {
    if (!pending || busyRef.current) return;
    const { account, mode } = pending;
    busyRef.current = true;
    setBusy(true);
    const result = await issuePasswordReset(outcomesRef.current.householdId, account.userId);
    busyRef.current = false;
    if (!mountedRef.current) return;
    setBusy(false);
    setPending(null);
    if (result.kind === 'issued') {
      const { token, createdAt, expiresAt } = result.reset;
      outcomesRef.current.onAccount({ ...account, passwordReset: { createdAt, expiresAt } });
      setIssued({ account, link: `${window.location.origin}/reset-password#${token}`, expiresAt });
      if (mode === 'renew') {
        outcomesRef.current.onNotice({ key: 'admin.notice.resetReplaced', params: { login: account.login }, tone: 'success' });
      }
      return;
    }
    announceIssueFailure(result.kind, account.login, outcomesRef.current);
  }

  async function cancelLink(account: Account) {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    const result = await cancelPasswordReset(outcomesRef.current.householdId, account.userId);
    busyRef.current = false;
    if (!mountedRef.current) return;
    setBusy(false);
    if (result.kind === 'ok') {
      outcomesRef.current.onAccount({ ...account, passwordReset: null });
      outcomesRef.current.onNotice({ key: 'admin.notice.resetCancelled', params: { login: account.login }, tone: 'success' });
      return;
    }
    announceCancelFailure(result.kind, outcomesRef.current);
  }

  return { pending, issued, busy, triggerRef, request, cancelRequest, closeLink, confirm, cancelLink };
}

export type PasswordReset = ReturnType<typeof usePasswordReset>;
