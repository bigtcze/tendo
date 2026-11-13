import { useCallback, useEffect, useRef, useState } from 'react';
import type { Session } from '../features/auth/sessionApi';
import { switchHousehold, type HouseholdSummary } from './householdsApi';

export type SwitchFailure = 'gone' | 'unavailable';

type Options = {
  /** The server accepted the choice: its session is adopted, and the shell opens Home in that household. */
  onAdopted: (session: Session, household: HouseholdSummary) => void;
  onSignedOut: () => void;
  /** The household is no longer one of the caller's memberships, so the offered list has to be read again. */
  onGone: () => void;
};

/**
 * Choosing another household: one request at a time, never optimistic. Nothing changes on screen until the server
 * returns the session it stores the choice on; a refusal keeps the panel open with a calm explanation.
 *
 * It lives above the screens, in HouseholdSwitchProvider: the menu that asked may unmount because the reader moved
 * on, and the answer is still adopted instead of being silently dropped.
 */
export function useHouseholdSwitch({ onAdopted, onSignedOut, onGone }: Options) {
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<SwitchFailure | null>(null);
  const inFlight = useRef(false);
  const mounted = useRef(true);
  const callbacks = useRef({ onAdopted, onSignedOut, onGone });

  useEffect(() => { callbacks.current = { onAdopted, onSignedOut, onGone }; });
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  /** Resolves `true` once the returned session was adopted, so only a real change closes the panel. */
  const choose = useCallback(async (household: HouseholdSummary): Promise<boolean> => {
    // A second press while the first request is in flight changes nothing at all.
    if (inFlight.current) return false;
    inFlight.current = true;
    setBusy(true);
    setFailure(null);
    const outcome = await switchHousehold(household.id);
    if (!mounted.current) return false;
    inFlight.current = false;
    setBusy(false);
    if (outcome.kind === 'unauthenticated') { callbacks.current.onSignedOut(); return false; }
    if (outcome.kind === 'ok') {
      callbacks.current.onAdopted(outcome.session, household);
      return true;
    }
    if (outcome.kind === 'gone') { setFailure('gone'); callbacks.current.onGone(); return false; }
    setFailure('unavailable');
    return false;
  }, []);

  const dismiss = useCallback(() => setFailure(null), []);

  return { busy, failure, choose, dismiss };
}
