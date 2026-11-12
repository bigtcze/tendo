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
  clearHomeNotice: () => void;
  state: BootState;
};

/**
 * /people and /admin without a household have nothing to show, and /login is only a landing path for sign-in
 * results; in both cases the address bar is corrected to the destination the screen really shows.
 */
function useCanonicalUrl(route: Route, state: BootState) {
  const noHouseholdScreen =
    state.kind === 'signedIn' && (route.kind === 'people' || route.kind === 'admin') && !state.session.defaultHouseholdId;
  const signedInOnLogin = state.kind === 'signedIn' && route.kind === 'login';
  useEffect(() => {
    if (noHouseholdScreen) window.history.replaceState(null, '', '/');
    if (signedInOnLogin) window.history.replaceState(null, '', '/');
  }, [noHouseholdScreen, signedInOnLogin]);
}

export function useAppNavigation({ route, setRoute, transitionedRef, setAllItemsVisit, setOidcOutcome, clearHomeNotice, state }: Options) {
  // A notice belongs to the screen that received it; any move away retires it together with callback fragments.
  const clearTransient = useCallback(() => {
    setOidcOutcome(null);
    clearHomeNotice();
  }, [clearHomeNotice, setOidcOutcome]);
  // All items destinations differ only in their query: subject, group status or the archive filter.
  const openItemsPath = useCallback((search = '') => {
    transitionedRef.current = true;
    window.history.pushState({ allItemsCursors: [] }, '', `/items${search}`);
    setAllItemsVisit((visit) => visit + 1);
    setRoute({ kind: 'allItems' });
  }, [setAllItemsVisit, setRoute, transitionedRef]);
  const openAllItems = useCallback((subjectId?: string) => {
    openItemsPath(subjectId ? `?subject=${encodeURIComponent(subjectId)}` : '');
  }, [openItemsPath]);
  const openArchive = useCallback(() => openItemsPath('?status=archived'), [openItemsPath]);
  const openAllItemsForStatus = useCallback((status: ItemGroup) => openItemsPath(`?status=${status}`), [openItemsPath]);
  const openPeople = useCallback(() => {
    transitionedRef.current = true;
    window.history.pushState(null, '', '/people');
    setRoute({ kind: 'people' });
  }, [setRoute, transitionedRef]);
  const openAccount = useCallback(() => {
    transitionedRef.current = true;
    clearTransient();
    window.history.pushState(null, '', '/account');
    setRoute({ kind: 'account' });
  }, [clearTransient, setRoute, transitionedRef]);
  const openMembers = useCallback(() => {
    transitionedRef.current = true;
    window.history.pushState(null, '', '/members');
    setRoute({ kind: 'members' });
  }, [setRoute, transitionedRef]);
  const openAdmin = useCallback(() => {
    transitionedRef.current = true;
    clearTransient();
    window.history.pushState(null, '', '/admin');
    setRoute({ kind: 'admin' });
  }, [clearTransient, setRoute, transitionedRef]);
  const goHome = useCallback(() => {
    transitionedRef.current = true;
    clearTransient();
    window.history.pushState(null, '', '/');
    setRoute({ kind: 'home' });
  }, [clearTransient, setRoute, transitionedRef]);
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
      clearTransient();
      setRoute(readRoute());
    };
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, [clearTransient, setRoute, transitionedRef]);
  useCanonicalUrl(route, state);

  return { openAllItems, openAllItemsForStatus, openArchive, openPeople, openAccount, openMembers, openAdmin,
    goHome, goBackFromItem, openItemFromAllItems, openItemFromHome };
}
