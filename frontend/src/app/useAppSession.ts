import { useCallback, useEffect, useRef, useState } from 'react';
import type { Dispatch, RefObject, SetStateAction } from 'react';
import { fetchSession, fetchSetupRequired, type Session } from '../features/auth/sessionApi';
import type { NoticeMessage } from './notices';
import type { Route } from './routes';
import type { OidcEntryActions } from './useOidcEntry';
import { clearPendingStores, useSessionTransitions } from './useSessionTransitions';

export { clearPendingStores } from './useSessionTransitions';

export type BootState =
  | { kind: 'loading' | 'error' | 'setupRequired' | 'signedOut' }
  | { kind: 'signedIn'; session: Session };

async function boot(): Promise<BootState> {
  const result = await fetchSession();
  if (result.kind === 'signedIn') return { kind: 'signedIn', session: result.session };
  if (result.kind === 'error') return { kind: 'error' };
  const setup = await fetchSetupRequired();
  if (setup === 'required') return { kind: 'setupRequired' };
  if (setup === 'error') return { kind: 'error' };
  return { kind: 'signedOut' };
}

type Options = {
  setRoute: Dispatch<SetStateAction<Route>>;
  transitionedRef: RefObject<boolean>;
  setOidcOutcome: (outcome: null) => void;
  entry: OidcEntryActions;
};

export function useAppSession({ setRoute, transitionedRef, setOidcOutcome, entry }: Options) {
  const [state, setState] = useState<BootState>({ kind: 'loading' });
  const [sessionGeneration, setSessionGeneration] = useState(0);
  const sessionGenerationRef = useRef(0);
  const advanceSessionGeneration = useCallback(() => {
    const next = sessionGenerationRef.current + 1;
    sessionGenerationRef.current = next;
    setSessionGeneration(next);
  }, []);
  const [attempt, setAttempt] = useState(0);
  const [accountReadyNotice, setAccountReadyNotice] = useState(false);
  const [sessionEndedNotice, setSessionEndedNotice] = useState(false);
  const [oidcContinueExpired, setOidcContinueExpired] = useState(false);
  const [inviteFocusLogin, setInviteFocusLogin] = useState(false);
  const [loginPrefill, setLoginPrefill] = useState<string | null>(null);
  const [passwordResetNotice, setPasswordResetNotice] = useState(false);
  const [householdLeftNotice, setHouseholdLeftNotice] = useState<NoticeMessage | null>(null);

  const isCurrentSession = useCallback(
    () => sessionGenerationRef.current === sessionGeneration && state.kind === 'signedIn',
    [sessionGeneration, state.kind],
  );
  const { clearContinue } = entry;

  useEffect(() => {
    let cancelled = false;
    boot().then((next) => {
      if (cancelled) return;
      if (next.kind === 'signedIn') {
        advanceSessionGeneration();
        clearContinue();
      }
      // Nothing is pending for a signed-out visitor: drop drafts owned by the previous signed-in session.
      if (next.kind === 'signedOut') clearPendingStores();
      setState(next);
    });
    return () => { cancelled = true; };
  }, [attempt, advanceSessionGeneration, clearContinue]);

  const retry = useCallback(() => {
    transitionedRef.current = true;
    setState({ kind: 'loading' });
    setAttempt((n) => n + 1);
  }, [transitionedRef]);

  /**
   * A fresh session for the same person and household, not a new sign-in: the password change rotates the cookie and
   * returns the new expiry, so the shell keeps reading the live session instead of the one it booted with. Nothing
   * else is reset — the route, the drafts and the household all stay where the person left them.
   */
  const updateSession = useCallback((session: Session) => {
    setState((previous) => (previous.kind === 'signedIn' ? { kind: 'signedIn', session } : previous));
  }, []);

  const transitions = useSessionTransitions({
    setRoute, transitionedRef, setOidcOutcome, entry, setState, advanceSessionGeneration, isCurrentSession,
    setAccountReadyNotice, setSessionEndedNotice, setOidcContinueExpired, setInviteFocusLogin,
    setLoginPrefill, setPasswordResetNotice, setHouseholdLeftNotice,
  });

  const dismissHouseholdLeft = useCallback(() => setHouseholdLeftNotice(null), []);

  return {
    state, retry, updateSession, householdLeftNotice, dismissHouseholdLeft,
    ...transitions,
    accountReadyNotice, sessionEndedNotice, oidcContinueExpired, inviteFocusLogin,
    loginPrefill, passwordResetNotice,
  };
}
