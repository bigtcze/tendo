import type { FormEvent } from 'react';
import { fetchSession, login, logout, type Session } from '../auth/sessionApi';
import { acceptInvitationAsCurrentUser, acceptNewAccount } from './membersApi';
import {
  mapNewAccountResult, mapSignedInResult, type InviteCallbacks,
} from './inviteAcceptModel';
import { useInviteAcceptState } from './useInviteAcceptState';

type Props = InviteCallbacks & { token: string | null; session?: Session; focusLogin: boolean };
type Mounted = { current: boolean };

async function refreshSession(mounted: Mounted, callbacks: InviteCallbacks): Promise<'handled' | 'retry' | 'unmounted'> {
  const refreshed = await fetchSession();
  if (!mounted.current) return 'unmounted';
  if (refreshed.kind === 'signedIn') {
    callbacks.onSignedIn(refreshed.session);
    return 'handled';
  }
  if (refreshed.kind === 'signedOut') {
    callbacks.onJoinedSignedOut();
    return 'handled';
  }
  return 'retry';
}

export function useInviteAccept({ token, session, focusLogin, ...callbacks }: Props) {
  const state = useInviteAcceptState(token, session, focusLogin);
  const { loginName, setLoginName, password, setPassword, error, setError, fieldError,
    pending, setPending, setSignedOutMode, invalidLink, accepted, setAccepted,
    mounted, loginRef, passwordRef, noticeRef, valid, currentSession, fail } = state;

  async function retrySession() {
    if (pending || !accepted || !mounted.current) return;
    setPending(true);
    const result = await refreshSession(mounted, callbacks);
    if (result === 'unmounted') return;
    setPending(false);
    if (result === 'retry') noticeRef.current?.focus();
  }

  async function submitNewAccount(validToken: string) {
    if (!loginName) { fail('login'); return; }
    if (!password) { fail('password'); return; }
    const result = await acceptNewAccount(validToken, loginName, password);
    if (!mounted.current) return;
    const outcome = mapNewAccountResult(result);
    if (outcome.kind === 'error') { fail(outcome.error); return; }
    const signedIn = await login(loginName, password);
    if (!mounted.current) return;
    if (signedIn.kind === 'ok') callbacks.onSignedIn(signedIn.session);
    else callbacks.onAccountReady();
  }

  async function submitSignedIn(validToken: string) {
    const result = await acceptInvitationAsCurrentUser(validToken);
    if (!mounted.current) return;
    const outcome = mapSignedInResult(result);
    if (outcome.kind === 'unauthenticated') {
      setSignedOutMode(true);
      setPending(false);
      callbacks.onSignedOut();
      return;
    }
    if (outcome.kind === 'error') {
      if (outcome.error === 'householdConflict') {
        setError(outcome.error);
        setPending(false);
      } else fail(outcome.error);
      return;
    }
    const refreshed = await refreshSession(mounted, callbacks);
    if (refreshed !== 'retry') return;
    setAccepted(true);
    setPending(false);
    setError(null);
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (pending || !valid || !token) return;
    setPending(true);
    setError(null);
    if (currentSession) await submitSignedIn(token);
    else await submitNewAccount(token);
  }

  async function signOutAndContinue() {
    if (pending) return;
    setPending(true);
    const result = await logout();
    if (!mounted.current) return;
    if (result === 'ok') {
      setSignedOutMode(true);
      setError(null);
      setPending(false);
      callbacks.onSignedOut();
    } else fail('unavailable');
  }

  return {
    loginName, setLoginName, password, setPassword, error, fieldError,
    pending, currentSession, valid, invalidLink, accepted,
    loginRef, passwordRef, noticeRef, submit, retrySession, signOutAndContinue,
  };
}
