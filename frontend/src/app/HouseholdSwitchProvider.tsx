import { useCallback, useState, type ReactNode } from 'react';
import type { Session } from '../features/auth/sessionApi';
import { HouseholdSwitchContext } from './HouseholdSwitchContext';
import { useHouseholdList } from './useHouseholdList';
import { useHouseholdSwitch } from './useHouseholdSwitch';

/**
 * Owns both of the top bar's household requests above every screen. Each screen paints its own shell, so a menu
 * that unmounted mid-request would otherwise drop a switch the server had already stored.
 *
 * SignedInApp keys this provider by the session, so a switch starts again with an empty list and a late list
 * response from the session that was left behind can never land in the one that replaced it.
 */
export function HouseholdSwitchProvider({ onAdopt, onSignedOut, children }: {
  onAdopt: (session: Session, householdName: string) => void;
  onSignedOut: () => void;
  children: ReactNode;
}) {
  const [openedAt, setOpenedAt] = useState(0);
  const list = useHouseholdList({ openedAt, onSignedOut });
  const chooser = useHouseholdSwitch({
    onAdopted: (session, household) => onAdopt(session, household.name),
    onSignedOut,
    onGone: list.reload,
  });
  const openList = useCallback(() => setOpenedAt((count) => count + 1), []);

  return <HouseholdSwitchContext value={{
    list,
    openList,
    busy: chooser.busy,
    failure: chooser.failure,
    choose: chooser.choose,
    dismiss: chooser.dismiss,
    sessionEnded: onSignedOut,
  }}>
    {children}
  </HouseholdSwitchContext>;
}
