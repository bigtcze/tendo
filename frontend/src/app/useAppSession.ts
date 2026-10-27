import { useCallback, useEffect, useRef, useState } from 'react';
import type { Dispatch, RefObject, SetStateAction } from 'react';
import { fetchSession, fetchSetupRequired, type Session } from '../features/auth/sessionApi';
import { clearPendingCompletions } from '../features/items/pendingCompletions';
import { clearPendingItemCreates } from '../features/items/pendingItemCreate';
import { clearPendingSubjectCreates } from '../features/subjects/pendingSubjectCreate';
import { INVITE_PATH, type Route } from './routes';

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
  clearPendingStores();
  return { kind: 'signedOut' };
}

function completeSignedInTransition(
  session: Session,
  transitionedRef: RefObject<boolean>,
  setRoute: Dispatch<SetStateAction<Route>>,
  setOidcOutcome: (outcome: null) => void,
  setAccountReadyNotice: (notice: boolean) => void,
  setSessionEndedNotice: (notice: boolean) => void,
  setInviteFocusLogin: (focus: boolean) => void,
  setState: Dispatch<SetStateAction<BootState>>,
): void {
  transitionedRef.current = true;
  if (window.location.pathname !== '/') window.history.replaceState(null, '', '/');
  setRoute({ kind: 'home' });
  setOidcOutcome(null);
  setAccountReadyNotice(false);
  setSessionEndedNotice(false);
  setInviteFocusLogin(false);
  setState({ kind: 'signedIn', session });
}

function preserveInviteHistoryEntry(): void {
  const inviteVisit = window.history.state?.inviteVisit;
  window.history.replaceState(typeof inviteVisit === 'string' ? { inviteVisit } : null, '', INVITE_PATH);
}

export function clearPendingStores(): void {
  clearPendingCompletions();
  clearPendingSubjectCreates();
  clearPendingItemCreates();
}

type Options = {
  setRoute: Dispatch<SetStateAction<Route>>;
  transitionedRef: RefObject<boolean>;
  setOidcOutcome: (outcome: null) => void;
};

export function useAppSession({ setRoute, transitionedRef, setOidcOutcome }: Options) {
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
  const [inviteFocusLogin, setInviteFocusLogin] = useState(false);

  useEffect(() => {
    let cancelled = false;
    boot().then((next) => {
      if (!cancelled) {
        if (next.kind === 'signedIn') advanceSessionGeneration();
        setState(next);
      }
    });
    return () => { cancelled = true; };
  }, [attempt, advanceSessionGeneration]);

  const retry = useCallback(() => {
    transitionedRef.current = true;
    setState({ kind: 'loading' });
    setAttempt((n) => n + 1);
  }, [transitionedRef]);
  const inviteSignedOut = useCallback(() => {
    advanceSessionGeneration();
    clearPendingStores();
    transitionedRef.current = true;
    preserveInviteHistoryEntry();
    setRoute({ kind: 'invite' });
    setInviteFocusLogin(true);
    setState({ kind: 'signedOut' });
  }, [advanceSessionGeneration, setRoute, transitionedRef]);
  const accountReady = useCallback(() => {
    advanceSessionGeneration();
    transitionedRef.current = true;
    window.history.replaceState(null, '', '/');
    setRoute({ kind: 'home' });
    setOidcOutcome(null);
    setSessionEndedNotice(false);
    setAccountReadyNotice(true);
    setState({ kind: 'signedOut' });
  }, [advanceSessionGeneration, setOidcOutcome, setRoute, transitionedRef]);
  const signedOut = useCallback(() => {
    advanceSessionGeneration();
    clearPendingStores();
    setOidcOutcome(null);
    setSessionEndedNotice(false);
    transitionedRef.current = true;
    if (window.location.pathname !== '/') window.history.replaceState(null, '', '/');
    setRoute({ kind: 'home' });
    setState({ kind: 'signedOut' });
  }, [advanceSessionGeneration, setOidcOutcome, setRoute, transitionedRef]);
  const sessionEnded = useCallback(() => {
    if (sessionGenerationRef.current !== sessionGeneration || state.kind !== 'signedIn') return;
    advanceSessionGeneration();
    clearPendingStores();
    setOidcOutcome(null);
    transitionedRef.current = true;
    if (window.location.pathname !== '/') window.history.replaceState(null, '', '/');
    setRoute({ kind: 'home' });
    setState({ kind: 'signedOut' });
    setSessionEndedNotice(true);
  }, [advanceSessionGeneration, sessionGeneration, setOidcOutcome, setRoute, state.kind, transitionedRef]);
  const signedIn = useCallback((session: Session) => {
    advanceSessionGeneration();
    completeSignedInTransition(session, transitionedRef, setRoute, setOidcOutcome,
      setAccountReadyNotice, setSessionEndedNotice, setInviteFocusLogin, setState);
  }, [advanceSessionGeneration, setOidcOutcome, setRoute, transitionedRef]);

  return { state, retry, inviteSignedOut, accountReady, signedOut, sessionEnded, signedIn,
    accountReadyNotice, sessionEndedNotice, inviteFocusLogin };
}


