import { useCallback, useEffect, useState } from 'react';
import type { Dispatch, RefObject, SetStateAction } from 'react';
import type { ItemGroup } from '../features/items/itemsApi';
import type { BootState } from './useAppSession';
import { readRoute, type Route } from './routes';

/** Where an open item detail was reached from, so the navigation highlights the route of origin. */
export type ItemOrigin = 'home' | 'allItems' | 'archive' | null;

type Options = {
  route: Route;
  setRoute: Dispatch<SetStateAction<Route>>;
  transitionedRef: RefObject<boolean>;
  setAllItemsVisit: Dispatch<SetStateAction<number>>;
  setOidcOutcome: (outcome: null) => void;
  clearHomeNotice: () => void;
  state: BootState;
};

/** The archived filter is part of the All items address, so its link state follows that address. */
function isArchivedSearch(search: string) {
  return new URLSearchParams(search).get('status') === 'archived';
}

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

/** Every way out of a screen: one place that records the destination, the address and the list it belongs to. */
function useNavigationActions({ setRoute, transitionedRef, setAllItemsVisit, setItemOrigin, setArchivedView, clearTransient }: {
  setRoute: Dispatch<SetStateAction<Route>>;
  transitionedRef: RefObject<boolean>;
  setAllItemsVisit: Dispatch<SetStateAction<number>>;
  setItemOrigin: Dispatch<SetStateAction<ItemOrigin>>;
  setArchivedView: Dispatch<SetStateAction<boolean>>;
  clearTransient: () => void;
}) {
  // All items destinations differ only in their query: subject, group status or the archive filter.
  const openItemsPath = useCallback((search = '') => {
    transitionedRef.current = true;
    setArchivedView(isArchivedSearch(search));
    setItemOrigin(null);
    window.history.pushState({ allItemsCursors: [] }, '', `/items${search}`);
    setAllItemsVisit((visit) => visit + 1);
    setRoute({ kind: 'allItems' });
  }, [setAllItemsVisit, setArchivedView, setItemOrigin, setRoute, transitionedRef]);
  const openAllItems = useCallback((subjectId?: string) => {
    openItemsPath(subjectId ? `?subject=${encodeURIComponent(subjectId)}` : '');
  }, [openItemsPath]);
  const openArchive = useCallback(() => openItemsPath('?status=archived'), [openItemsPath]);
  const openAllItemsForStatus = useCallback((status: ItemGroup) => openItemsPath(`?status=${status}`), [openItemsPath]);
  /** A plain page has no origin: the navigation highlights the page itself. */
  const simple = useCallback((path: string, route: Route, transient: boolean) => {
    transitionedRef.current = true;
    setItemOrigin(null);
    if (transient) clearTransient();
    window.history.pushState(null, '', path);
    setRoute(route);
  }, [clearTransient, setItemOrigin, setRoute, transitionedRef]);
  const goHome = useCallback(() => simple('/', { kind: 'home' }, true), [simple]);
  const openPeople = useCallback(() => simple('/people', { kind: 'people' }, false), [simple]);
  const openAccount = useCallback(() => simple('/account', { kind: 'account' }, true), [simple]);
  const openMembers = useCallback(() => simple('/members', { kind: 'members' }, false), [simple]);
  const openAdmin = useCallback(() => simple('/admin', { kind: 'admin' }, true), [simple]);
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
    setItemOrigin(returnTo.includes('status=archived') ? 'archive' : 'allItems');
    window.history.pushState({ ...(window.history.state ?? {}), returnTo, fromAllItems: true }, '', `/items/${encodeURIComponent(itemId)}`);
    setRoute({ kind: 'item', itemId });
  }, [setItemOrigin, setRoute, transitionedRef]);
  const openItemFromHome = useCallback((itemId: string) => {
    transitionedRef.current = true;
    setItemOrigin('home');
    window.history.pushState({ fromHome: true }, '', `/items/${encodeURIComponent(itemId)}`);
    setRoute({ kind: 'item', itemId });
  }, [setItemOrigin, setRoute, transitionedRef]);
  /** The All items screen owns the filter; it reports back so the Archive link matches what is on screen. */
  const reportItemsView = useCallback((archived: boolean) => setArchivedView(archived), [setArchivedView]);

  return { openAllItems, openAllItemsForStatus, openArchive, openPeople, openAccount, openMembers, openAdmin,
    goHome, goBackFromItem, openItemFromAllItems, openItemFromHome, reportItemsView };
}

/** Back and forward recompute everything the address says, including the list an item belongs to. */
function usePopstateNavigation({ setRoute, transitionedRef, setArchivedView, setItemOrigin, clearTransient }: {
  setRoute: Dispatch<SetStateAction<Route>>;
  transitionedRef: RefObject<boolean>;
  setArchivedView: Dispatch<SetStateAction<boolean>>;
  setItemOrigin: Dispatch<SetStateAction<ItemOrigin>>;
  clearTransient: () => void;
}) {
  useEffect(() => {
    const onPop = () => {
      transitionedRef.current = true;
      clearTransient();
      const next = readRoute();
      setArchivedView(next.kind === 'allItems' && isArchivedSearch(window.location.search));
      setItemOrigin(next.kind === 'item' ? historyOrigin() : null);
      setRoute(next);
    };
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, [clearTransient, setArchivedView, setItemOrigin, setRoute, transitionedRef]);
}

export function useAppNavigation({ route, setRoute, transitionedRef, setAllItemsVisit, setOidcOutcome, clearHomeNotice, state }: Options) {
  // Both are presentation state for the navigation only: which list an item was opened from, and whether that
  // list is showing the archive. They are set by the actions above; a reload keeps them only because the history
  // entry an item was pushed with still names where it was opened from.
  const [itemOrigin, setItemOrigin] = useState<ItemOrigin>(() => (route.kind === 'item' ? historyOrigin() : null));
  const [archivedView, setArchivedView] = useState(false);
  // A notice belongs to the screen that received it; any move away retires it together with callback fragments.
  const clearTransient = useCallback(() => {
    setOidcOutcome(null);
    clearHomeNotice();
  }, [clearHomeNotice, setOidcOutcome]);
  const actions = useNavigationActions({ setRoute, transitionedRef, setAllItemsVisit, setItemOrigin, setArchivedView, clearTransient });
  usePopstateNavigation({ setRoute, transitionedRef, setArchivedView, setItemOrigin, clearTransient });
  useCanonicalUrl(route, state);
  return { ...actions, itemOrigin, archivedView };
}

/** A deep link has no history to read, so an item reached without one belongs to the All items list. */
function historyOrigin(): ItemOrigin {
  const state = window.history.state ?? {};
  if (state.fromHome === true) return 'home';
  const returnTo = state.returnTo;
  if (typeof returnTo !== 'string') return 'allItems';
  return returnTo.includes('status=archived') ? 'archive' : 'allItems';
}
