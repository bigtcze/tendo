import { useEffect, useRef, useState } from 'react';
import type { MessageKey } from '../../i18n';
import {
  cancelOidcPending, confirmOidcPendingLink, createOidcPendingAccount,
  type OidcInvitationOutcome,
} from './oidcPendingApi';
import { useOidcPendingStatus } from './useOidcPendingStatus';
import type { Session } from './sessionApi';

export type { ContinueLoad } from './useOidcPendingStatus';
export type ContinueFailure = 'invalid' | 'rateLimited' | 'unavailable' | 'conflict' | null;
export type ContinueAction = 'create' | 'link' | 'cancel' | null;

type Callbacks = {
  signedIn: (session: Session, notice: MessageKey | null) => void;
  expired: () => void;
  cancelled: () => void;
};

/**
 * One notice at most, matching the callback's `#oidcNotice`: invitation trouble first, then the
 * "email wasn't saved" note, then the confirmation that a carried invitation was used.
 */
function signedInNotice(invitation: OidcInvitationOutcome, emailSaved: boolean): MessageKey | null {
  if (invitation === 'invalid') return 'home.notice.invitationInvalid';
  if (!emailSaved) return 'oidc.continue.emailNotSaved';
  return invitation === 'accepted' ? 'home.notice.invitationAccepted' : null;
}

/**
 * Drives the OIDC continue interstitial: read the pending continuation, then either create a new
 * account or bind it to an existing one. Expiry and cancellation leave the screen to its owners.
 */
export function useOidcContinue(callbacks: Callbacks) {
  const [linkOpen, setLinkOpen] = useState(false);
  const [action, setAction] = useState<ContinueAction>(null);
  const [failure, setFailure] = useState<ContinueFailure>(null);
  const [loginName, setLoginName] = useState('');
  const [password, setPassword] = useState('');
  const actionRef = useRef<ContinueAction>(null);
  const mountedRef = useRef(false);
  const callbacksRef = useRef(callbacks);
  const pending = useOidcPendingStatus(() => callbacksRef.current.expired());
  const { setLoad } = pending;

  useEffect(() => { callbacksRef.current = callbacks; });
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  function begin(next: Exclude<ContinueAction, null>): boolean {
    if (actionRef.current) return false;
    actionRef.current = next;
    setAction(next);
    setFailure(null);
    return true;
  }

  function finish() {
    actionRef.current = null;
    if (mountedRef.current) setAction(null);
  }

  async function createAccount() {
    if (!begin('create')) return;
    const result = await createOidcPendingAccount();
    if (!mountedRef.current) return;
    if (result.kind === 'ok') { callbacksRef.current.signedIn(result.session, signedInNotice(result.invitation, true)); return; }
    if (result.kind === 'expired') { callbacksRef.current.expired(); return; }
    finish();
    if (result.kind === 'alreadySignedIn') { setLoad('alreadySignedIn'); return; }
    setFailure(result.kind === 'conflict' ? 'conflict' : result.kind);
  }

  async function submitLink() {
    if (!begin('link')) return;
    const result = await confirmOidcPendingLink(loginName, password);
    if (!mountedRef.current) return;
    if (result.kind === 'ok') { callbacksRef.current.signedIn(result.session, signedInNotice(result.invitation, result.emailSaved)); return; }
    if (result.kind === 'expired') { callbacksRef.current.expired(); return; }
    finish();
    if (result.kind === 'alreadySignedIn') { setLoad('alreadySignedIn'); return; }
    if (result.kind === 'invalidCredentials') setPassword('');
    setFailure(result.kind === 'invalidCredentials' ? 'invalid' : result.kind);
  }

  async function cancel() {
    if (!begin('cancel')) return;
    const result = await cancelOidcPending();
    if (!mountedRef.current) return;
    finish();
    if (result === 'ok') { callbacksRef.current.cancelled(); return; }
    setFailure(result);
  }

  /**
   * A conflict consumes the continuation server-side, so there is nothing left to cancel:
   * leave the local entry state and let the ordinary sign-in screen take over.
   */
  function backToSignIn() {
    actionRef.current = null;
    callbacksRef.current.cancelled();
  }

  return {
    ...pending,
    linkOpen, action, failure, loginName, password,
    setLoginName, setPassword,
    openLink: () => { setLinkOpen(true); setFailure(null); },
    closeLink: () => { setLinkOpen(false); setFailure(null); },
    createAccount: () => void createAccount(),
    submitLink: () => void submitLink(),
    cancel: () => void cancel(),
    backToSignIn,
  };
}
