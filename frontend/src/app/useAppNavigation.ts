import { useCallback, useEffect } from 'react';
import type { Dispatch, RefObject, SetStateAction } from 'react';
import type { ItemGroup } from '../features/items/itemsApi';
import type { BootState } from './useAppSession';
import { readRoute, type Route } from './routes';

type Options = {
  route: Route;
  setRoute: Dispatch<SetStateAction<Route>>;
  transitionedRef: RefObject<boolean>;
  setAllItemsVisit: Dispatch<SetStateAction<number>>;
  setOidcOutcome: (outcome: null) => void;
  state: BootState;
};

export function useAppNavigation({ route, setRoute, transitionedRef, setAllItemsVisit, setOidcOutcome, state }: Options) {
  const openAllItems = useCallback(() => {
    transitionedRef.current = true;
    window.history.pushState({ allItemsCursors: [] }, '', '/items');
    setAllItemsVisit((visit) => visit + 1);
    setRoute({ kind: 'allItems' });
  }, [setAllItemsVisit, setRoute, transitionedRef]);
  const openAllItemsForStatus = useCallback((status: ItemGroup) => {
    transitionedRef.current = true;
    window.history.pushState({ allItemsCursors: [] }, '', `/items?status=${status}`);
    setAllItemsVisit((visit) => visit + 1);
    setRoute({ kind: 'allItems' });
  }, [setAllItemsVisit, setRoute, transitionedRef]);
  const openPeople = useCallback(() => {
    transitionedRef.current = true;
    window.history.pushState(null, '', '/people');
    setRoute({ kind: 'people' });
  }, [setRoute, transitionedRef]);
  const openAccount = useCallback(() => {
    transitionedRef.current = true;
    setOidcOutcome(null);
    window.history.pushState(null, '', '/account');
    setRoute({ kind: 'account' });
  }, [setOidcOutcome, setRoute, transitionedRef]);
  const openMembers = useCallback(() => {
    transitionedRef.current = true;
    window.history.pushState(null, '', '/members');
    setRoute({ kind: 'members' });
  }, [setRoute, transitionedRef]);
  const goHome = useCallback(() => {
    transitionedRef.current = true;
    setOidcOutcome(null);
    window.history.pushState(null, '', '/');
    setRoute({ kind: 'home' });
  }, [setOidcOutcome, setRoute, transitionedRef]);
  const goBackFromItem = useCallback(() => {
    const returnTo = window.history.state?.returnTo;
    if (typeof returnTo === 'string' && returnTo.startsWith('/items')) {
      transitionedRef.current = true;
      if (window.history.length > 1) window.history.back();
      else { window.history.pushState(null, '', returnTo); setRoute({ kind: 'allItems' }); }
      return;
    }
    goHome();
  }, [goHome, setRoute, transitionedRef]);
  const openItemFromAllItems = useCallback((itemId: string, returnTo: string) => {
    transitionedRef.current = true;
    window.history.pushState({ ...(window.history.state ?? {}), returnTo, fromAllItems: true }, '', `/items/${encodeURIComponent(itemId)}`);
    setRoute({ kind: 'item', itemId });
  }, [setRoute, transitionedRef]);
  const openItemFromHome = useCallback((itemId: string) => {
    transitionedRef.current = true;
    window.history.pushState(null, '', `/items/${encodeURIComponent(itemId)}`);
    setRoute({ kind: 'item', itemId });
  }, [setRoute, transitionedRef]);

  useEffect(() => {
    const onPop = () => {
      transitionedRef.current = true;
      setOidcOutcome(null);
      setRoute(readRoute());
    };
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, [setRoute, setOidcOutcome, transitionedRef]);
  // /people without a household has nothing to show; keep the address bar honest and land on home.
  const noHouseholdOnPeople =
    state.kind === 'signedIn' && route.kind === 'people' && !state.session.defaultHouseholdId;
  useEffect(() => {
    if (noHouseholdOnPeople) window.history.replaceState(null, '', '/');
  }, [noHouseholdOnPeople]);
  // /login is only a landing path for sign-in results; a signed-in visitor belongs on home.
  const signedInOnLogin = state.kind === 'signedIn' && route.kind === 'login';
  useEffect(() => {
    if (signedInOnLogin) window.history.replaceState(null, '', '/');
  }, [signedInOnLogin]);

  return { openAllItems, openAllItemsForStatus, openPeople, openAccount, openMembers,
    goHome, goBackFromItem, openItemFromAllItems, openItemFromHome };
}
