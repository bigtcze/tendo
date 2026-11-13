import { useCallback, useEffect, useRef, useState } from 'react';
import type { Session } from '../features/auth/sessionApi';
import { switchHousehold, type HouseholdSummary } from './householdsApi';

export type SwitchFailure = 'gone' | 'unavailable';

type Options = {
  onSwitched: (session: Session, household: HouseholdSummary) => void;
  onSignedOut: () => void;
  /** The household is no longer one of the caller's memberships, so the offered list has to be read again. */
  onGone: () => void;
};

/**
 * Choosing another household: one request at a time, never optimistic. Nothing changes on screen until the server
 * returns the session it stores the choice on; a refusal keeps the panel open with a calm explanation.
 */
export function useHouseholdSwitch({ onSwitched, onSignedOut, onGone }: Options) {
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<SwitchFailure | null>(null);
  const inFlight = useRef(false);
  const mounted = useRef(true);
  const callbacks = useRef({ onSwitched, onSignedOut, onGone });

  useEffect(() => { callbacks.current = { onSwitched, onSignedOut, onGone }; });
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  const choose = useCallback(async (household: HouseholdSummary) => {
    // A second click while the first request is in flight changes nothing at all.
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setFailure(null);
    const outcome = await switchHousehold(household.id);
    if (!mounted.current) return;
    inFlight.current = false;
    setBusy(false);
    if (outcome.kind === 'unauthenticated') { callbacks.current.onSignedOut(); return; }
    if (outcome.kind === 'ok') { callbacks.current.onSwitched(outcome.session, household); return; }
    if (outcome.kind === 'gone') { setFailure('gone'); callbacks.current.onGone(); return; }
    setFailure('unavailable');
  }, []);

  const dismiss = useCallback(() => setFailure(null), []);

  return { busy, failure, choose, dismiss };
}
