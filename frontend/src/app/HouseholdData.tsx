import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import type { components } from '../../../api/generated/api';
import { api } from '../lib/api';

export type Household = components['schemas']['Household'];

export type HouseholdData =
  | { kind: 'loading' }
  | { kind: 'ready'; household: Household }
  | { kind: 'none' }
  | { kind: 'error' };

type HouseholdDataValue = {
  data: HouseholdData;
  /** Load again from the start, showing the loading state. */
  retry: () => void;
  /** Re-read quietly, e.g. after the viewer's own role changed; keeps the screen in place. */
  refresh: () => void;
};

const HouseholdDataContext = createContext<HouseholdDataValue | null>(null);

/** Household name and the viewer's role, loaded once per signed-in household for the whole shell. */
export function useHouseholdData(): HouseholdDataValue | null {
  return useContext(HouseholdDataContext);
}

/** The viewer's own membership role, or null until the household is known. A navigation hint only. */
export function useViewerRole(): Household['viewerRole'] | null {
  const value = useHouseholdData();
  return value?.data.kind === 'ready' ? value.data.household.viewerRole : null;
}

async function readHousehold(householdId: string): Promise<HouseholdData | 'unauthenticated'> {
  try {
    const { data, response } = await api.GET('/api/v1/households/{householdId}', { params: { path: { householdId } } });
    if (data && response.status === 200) return { kind: 'ready', household: data };
    if (response.status === 401) return 'unauthenticated';
    if (response.status === 404) return { kind: 'none' };
  } catch {
    // Network failures fall through to the retryable error state.
  }
  return { kind: 'error' };
}

/**
 * One household request serves the navigation role, the Home heading and the Account admin link.
 * A quiet refresh never clears the screen, and both paths ignore results that a newer request replaced.
 */
export function HouseholdDataProvider({ householdId, onSignedOut, children }: {
  householdId: string | null;
  onSignedOut: () => void;
  children: ReactNode;
}) {
  const [data, setData] = useState<HouseholdData>(householdId ? { kind: 'loading' } : { kind: 'none' });
  const [attempt, setAttempt] = useState(0);
  const generation = useRef(0);
  const onSignedOutRef = useRef(onSignedOut);

  useEffect(() => {
    onSignedOutRef.current = onSignedOut;
  }, [onSignedOut]);

  useEffect(() => {
    const token = ++generation.current;
    let cancelled = false;
    (async () => {
      if (!householdId) {
        setData({ kind: 'none' });
        return;
      }
      setData({ kind: 'loading' });
      const next = await readHousehold(householdId);
      if (cancelled || generation.current !== token) return;
      if (next === 'unauthenticated') { onSignedOutRef.current(); return; }
      setData(next);
    })();
    return () => { cancelled = true; };
  }, [householdId, attempt]);

  const retry = useCallback(() => {
    setData({ kind: 'loading' });
    setAttempt((n) => n + 1);
  }, []);

  const refresh = useCallback(() => {
    if (!householdId) return;
    const token = ++generation.current;
    void readHousehold(householdId).then((next) => {
      if (generation.current !== token) return;
      if (next === 'unauthenticated') { onSignedOutRef.current(); return; }
      // A transient failure keeps the last known name and role instead of emptying the shell.
      if (next.kind !== 'error') setData(next);
    });
  }, [householdId]);

  return <HouseholdDataContext value={{ data, retry, refresh }}>{children}</HouseholdDataContext>;
}
