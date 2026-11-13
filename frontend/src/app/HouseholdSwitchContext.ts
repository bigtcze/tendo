import { createContext } from 'react';
import type { Session } from '../features/auth/sessionApi';

export type HouseholdSwitchActions = {
  /**
   * Adopt a household chosen in the top bar: the returned session becomes the live one, the address returns to
   * Home and the household's own screens load again. `name` is only what the change is announced as.
   */
  switchTo: (session: Session, name: string) => void;
  /** A household request was refused as signed out: end the session the way every other screen does. */
  sessionEnded: () => void;
};

export const HouseholdSwitchContext = createContext<HouseholdSwitchActions | null>(null);
