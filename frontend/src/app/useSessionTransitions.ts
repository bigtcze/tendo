import { useCallback, useEffect, useRef, type Dispatch, type RefObject, type SetStateAction } from 'react';
import type { MessageKey } from '../i18n';
import type { NoticeMessage } from './notices';
import type { Session } from '../features/auth/sessionApi';
import { clearPendingCompletions } from '../features/items/pendingCompletions';
import { clearPendingItemCreates } from '../features/items/pendingItemCreate';
import { clearPendingSubjectCreates } from '../features/subjects/pendingSubjectCreate';
import { CONNECT_PATH, INVITE_PATH, LOGIN_PATH, type Route } from './routes';
import type { OidcEntryActions } from './useOidcEntry';
import type { BootState } from './useAppSession';
import { markConnectReturn, takeConnectReturn } from '../features/mcp/connectEntry';

export function clearPendingStores(): void {
  clearPendingCompletions();
  clearPendingSubjectCreates();
  clearPendingItemCreates();
}

function preserveInviteHistoryEntry(): void {
  const inviteVisit = window.history.state?.inviteVisit;
  window.history.replaceState(typeof inviteVisit === 'string' ? { inviteVisit } : null, '', INVITE_PATH);
}

/** Every membership change lands on Home, with the transition recorded so the fresh screen fades in. */
function enterHome(transitionedRef: RefObject<boolean>, setRoute: Dispatch<SetStateAction<Route>>): void {
  transitionedRef.current = true;
  if (window.location.pathname !== '/') window.history.replaceState(null, '', '/');
  setRoute({ kind: 'home' });
}

/** The consent page after a sign-in that began there: the same transition as Home, but a different destination. */
function enterConnect(transitionedRef: RefObject<boolean>, setRoute: Dispatch<SetStateAction<Route>>): void {
  transitionedRef.current = true;
  if (window.location.pathname !== CONNECT_PATH) window.history.replaceState(null, '', CONNECT_PATH);
  setRoute({ kind: 'connect' });
}

/** A sign-in that began on the consent page returns there once; Home never renders in between. */
function enterSignedIn(transitionedRef: RefObject<boolean>, setRoute: Dispatch<SetStateAction<Route>>): void {
  if (takeConnectReturn()) enterConnect(transitionedRef, setRoute);
  else enterHome(transitionedRef, setRoute);
}

/** Leaving and being removed read differently; so does having a household left and having none. */
function leftNoticeKey(remaining: boolean, alreadyGone: boolean): MessageKey {
  return alreadyGone
    ? (remaining ? 'home.notice.removed.households' : 'home.notice.removed.none')
    : (remaining ? 'home.notice.left.households' : 'home.notice.left.none');
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
  setHouseholdLeftNotice: (notice: NoticeMessage | null) => void;
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
    setLoginPrefill, setPasswordResetNotice, setHouseholdLeftNotice,
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
    enterHome(transitionedRef, setRoute);
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

  /**
   * The consent session ended (expired, or "Not you? Sign out"). The request handle stays stored and the next sign-in
   * is marked to return to the consent page, so the person finishes the same consent.
   */
  const connectSignedOut = useCallback(() => {
    leave({ path: CONNECT_PATH, route: { kind: 'connect' } });
    markConnectReturn();
  }, [leave]);

  /** The link replaced the password and deleted that account's sessions, so the only next step is sign-in. */
  const passwordResetCompleted = useCallback((login: string) => {
    leave({ path: LOGIN_PATH, route: { kind: 'login' } });
    setLoginPrefill(login);
    setPasswordResetNotice(true);
  }, [leave, setLoginPrefill, setPasswordResetNotice]);

  /**
   * The viewer's own membership is gone: they left, or an administrator removed them meanwhile. The server has already
   * moved their default membership, so the fresh session — never the old one — decides where they land: Home in a
   * remaining household, or the no-household state when none is left. Home carries the explanation.
   */
  const householdLeft = useCallback((session: Session, household: string, alreadyGone = false) => {
    const key = leftNoticeKey(Boolean(session.defaultHouseholdId), alreadyGone);
    advanceSessionGeneration();
    setHouseholdLeftNotice({ key, params: { household } });
    setState({ kind: 'signedIn', session });
    enterHome(transitionedRef, setRoute);
  }, [advanceSessionGeneration, setHouseholdLeftNotice, setRoute, setState, transitionedRef]);

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
    enterSignedIn(transitionedRef, setRoute);
    setOidcOutcome(null);
    setState({ kind: 'signedIn', session });
  }, [advanceSessionGeneration, clearContinue, setAccountReadyNotice, setOidcContinueExpired, setOidcOutcome, setRoute, setSessionEndedNotice, setState, setInviteFocusLogin, setLoginPrefill, setPasswordResetNotice, showNotice, transitionedRef]);

  return {
    inviteSignedOut, accountReady, signedOut, sessionEnded, pendingSignedOut, continueCancelled, continueExpired,
    passwordResetCompleted, signedIn, householdLeft, connectSignedOut,
  };
}
