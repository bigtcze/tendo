import { useCallback, useEffect, useRef, type Dispatch, type RefObject, type SetStateAction } from 'react';
import type { MessageKey } from '../i18n';
import type { Session } from '../features/auth/sessionApi';
import { clearPendingCompletions } from '../features/items/pendingCompletions';
import { clearPendingItemCreates } from '../features/items/pendingItemCreate';
import { clearPendingSubjectCreates } from '../features/subjects/pendingSubjectCreate';
import { INVITE_PATH, LOGIN_PATH, type Route } from './routes';
import type { OidcEntryActions } from './useOidcEntry';
import type { BootState } from './useAppSession';

export function clearPendingStores(): void {
  clearPendingCompletions();
  clearPendingSubjectCreates();
  clearPendingItemCreates();
}

function preserveInviteHistoryEntry(): void {
  const inviteVisit = window.history.state?.inviteVisit;
  window.history.replaceState(typeof inviteVisit === 'string' ? { inviteVisit } : null, '', INVITE_PATH);
}

type Deps = {
  setRoute: Dispatch<SetStateAction<Route>>;
  transitionedRef: RefObject<boolean>;
  setOidcOutcome: (outcome: null) => void;
  entry: OidcEntryActions;
  setState: Dispatch<SetStateAction<BootState>>;
  advanceSessionGeneration: () => void;
  /** True while the callback still belongs to the live signed-in session. */
  isCurrentSession: () => boolean;
  setAccountReadyNotice: (notice: boolean) => void;
  setSessionEndedNotice: (notice: boolean) => void;
  setOidcContinueExpired: (expired: boolean) => void;
  setInviteFocusLogin: (focus: boolean) => void;
  setLoginPrefill: (login: string | null) => void;
  setPasswordResetNotice: (notice: boolean) => void;
};

type LeaveOptions = { path: string; route?: Route; expired?: boolean; sessionEnded?: boolean };

/**
 * Every way out of the signed-in shell clears the same transient state before the sign-in screen
 * owns the page: drafts, callback fragments, OIDC entry state and the history entry.
 */
function leaveSignedIn(deps: Deps, options: LeaveOptions): void {
  deps.advanceSessionGeneration();
  clearPendingStores();
  deps.entry.clearContinue();
  deps.entry.clearNotice();
  deps.setOidcOutcome(null);
  deps.setSessionEndedNotice(Boolean(options.sessionEnded));
  deps.setOidcContinueExpired(Boolean(options.expired));
  deps.transitionedRef.current = true;
  if (window.location.pathname !== options.path) window.history.replaceState(null, '', options.path);
  deps.setRoute(options.route ?? { kind: 'home' });
  deps.setState({ kind: 'signedOut' });
}

/** All session transitions in one place so every entry/exit clears the same transient state. */
export function useSessionTransitions(deps: Deps) {
  const {
    setRoute, transitionedRef, setOidcOutcome, entry, setState, advanceSessionGeneration,
    setAccountReadyNotice, setSessionEndedNotice, setOidcContinueExpired, setInviteFocusLogin,
    setLoginPrefill, setPasswordResetNotice,
  } = deps;
  const { clearContinue, requestContinue, clearNotice, showNotice } = entry;
  // Latest dependencies without making every callback change identity on each render.
  const depsRef = useRef(deps);
  useEffect(() => { depsRef.current = deps; });

  const leave = useCallback((options: LeaveOptions) => leaveSignedIn(depsRef.current, options), []);
  const signedOut = useCallback(() => leave({ path: '/' }), [leave]);

  const inviteSignedOut = useCallback(() => {
    advanceSessionGeneration();
    clearPendingStores();
    clearContinue();
    clearNotice();
    transitionedRef.current = true;
    preserveInviteHistoryEntry();
    setRoute({ kind: 'invite' });
    setInviteFocusLogin(true);
    setState({ kind: 'signedOut' });
  }, [advanceSessionGeneration, clearContinue, clearNotice, setInviteFocusLogin, setRoute, setState, transitionedRef]);

  const accountReady = useCallback(() => {
    advanceSessionGeneration();
    clearContinue();
    clearNotice();
    transitionedRef.current = true;
    window.history.replaceState(null, '', '/');
    setRoute({ kind: 'home' });
    setOidcOutcome(null);
    setSessionEndedNotice(false);
    setAccountReadyNotice(true);
    setState({ kind: 'signedOut' });
  }, [advanceSessionGeneration, clearContinue, clearNotice, setAccountReadyNotice, setOidcOutcome, setRoute, setSessionEndedNotice, setState, transitionedRef]);

  const sessionEnded = useCallback(() => {
    if (depsRef.current.isCurrentSession()) leave({ path: '/', sessionEnded: true });
  }, [leave]);

  /** The account was discarded server-side; a fresh continuation waits behind the pending cookie. */
  const pendingSignedOut = useCallback(() => {
    leave({ path: LOGIN_PATH, route: { kind: 'login' } });
    requestContinue();
  }, [leave, requestContinue]);

  /** The continuation was cancelled: the ordinary sign-in screen, without a notice. */
  const continueCancelled = useCallback(() => {
    leave({ path: LOGIN_PATH, route: { kind: 'login' } });
  }, [leave]);

  /** The continuation is gone: the ordinary sign-in screen explains the expiry. */
  const continueExpired = useCallback(() => {
    leave({ path: LOGIN_PATH, route: { kind: 'login' }, expired: true });
  }, [leave]);

  /** The link replaced the password and deleted that account's sessions, so the only next step is sign-in. */
  const passwordResetCompleted = useCallback((login: string) => {
    leave({ path: LOGIN_PATH, route: { kind: 'login' } });
    setLoginPrefill(login);
    setPasswordResetNotice(true);
  }, [leave, setLoginPrefill, setPasswordResetNotice]);

  const signedIn = useCallback((session: Session, notice: MessageKey | null = null) => {
    advanceSessionGeneration();
    clearContinue();
    showNotice(notice);
    setAccountReadyNotice(false);
    setSessionEndedNotice(false);
    setInviteFocusLogin(false);
    setOidcContinueExpired(false);
    setLoginPrefill(null);
    setPasswordResetNotice(false);
    transitionedRef.current = true;
    if (window.location.pathname !== '/') window.history.replaceState(null, '', '/');
    setRoute({ kind: 'home' });
    setOidcOutcome(null);
    setState({ kind: 'signedIn', session });
  }, [advanceSessionGeneration, clearContinue, setAccountReadyNotice, setOidcContinueExpired, setOidcOutcome, setRoute, setSessionEndedNotice, setState, setInviteFocusLogin, setLoginPrefill, setPasswordResetNotice, showNotice, transitionedRef]);

  return {
    inviteSignedOut, accountReady, signedOut, sessionEnded, pendingSignedOut, continueCancelled, continueExpired,
    passwordResetCompleted, signedIn,
  };
}
