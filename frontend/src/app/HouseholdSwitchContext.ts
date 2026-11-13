import { createContext } from 'react';
import type { HouseholdSummary } from './householdsApi';
import type { HouseholdList } from './useHouseholdList';
import type { SwitchFailure } from './useHouseholdSwitch';

export type HouseholdSwitchActions = {
  /** The households the panel offers; read on the first open and kept for this session and household; a switch starts afresh. */
  list: HouseholdList;
  /** The panel opened: read the list now, or read a list already known again without taking it off screen. */
  openList: () => void;
  /** A switch is on its way; rows stay focusable, but pressing one again does nothing until it settles. */
  busy: boolean;
  failure: SwitchFailure | null;
  /** Resolves `true` once the server's session was adopted, so the panel closes behind a real change only. */
  choose: (household: HouseholdSummary) => Promise<boolean>;
  dismiss: () => void;
  /** A household request was refused as signed out: end the session the way every other screen does. */
  sessionEnded: () => void;
};

export const HouseholdSwitchContext = createContext<HouseholdSwitchActions | null>(null);
