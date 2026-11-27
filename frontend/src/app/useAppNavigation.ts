import { useCallback, useEffect, useState } from 'react';
import type { Dispatch, RefObject, SetStateAction } from 'react';
import type { ItemGroup } from '../features/items/itemsApi';
import type { BootState } from './useAppSession';
import {
  ADMIN_ACCOUNTS_PATH, ADMIN_AI_APPS_PATH, ADMIN_HOUSEHOLDS_PATH, adminAccountAiAppsPath, readRoute, type Route,
} from './routes';

/** Where an open item detail was reached from, so the navigation highlights the route of origin. */
export type ItemOrigin = 'home' | 'allItems' | 'archive' | null;

/**
 * The name a drilled-down installation list showed on the way in: a household's name, or an account's login. It is
 * display state for that visit only: it keeps the detail heading truthful without a second read, is never written to
 * history or storage, and names the account it belongs to so it can never leak into another session's screen.
 */
export type AdminTargetName = { userId: string; targetId: string; name: string };

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
 * results; in both cases the address bar is corrected to the destination the screen really shows. The route is
 * corrected with it, so the navigation highlights the screen that is actually open instead of the address it left.
 */
function useCanonicalUrl(route: Route, state: BootState, setRoute: Dispatch<SetStateAction<Route>>) {
  const noHousehold = state.kind === 'signedIn' && !state.session.defaultHouseholdId;
  // /people and /admin without a household have nothing to show; the installation administrator's Admin center is
  // the household list, so that is where their /admin really points.
  const householdLessInstallation = noHousehold && state.kind === 'signedIn' && state.session.instanceAdmin
    && route.kind === 'admin';
  const householdLessAdmin = noHousehold && route.kind === 'admin'
    ? (householdLessInstallation ? ADMIN_HOUSEHOLDS_PATH : '/')
    : null;
  const noHouseholdScreen = noHousehold && (route.kind === 'people' || route.kind === 'admin');
  const signedInOnLogin = state.kind === 'signedIn' && route.kind === 'login';
  useEffect(() => {
    if (householdLessAdmin) window.history.replaceState(null, '', householdLessAdmin);
    else if (noHouseholdScreen) window.history.replaceState(null, '', '/');
    if (signedInOnLogin) window.history.replaceState(null, '', '/');
    // A replaced address must also replace the route: the Admin center stays highlighted for a household-less
    // installation administrator, and the list is no longer a household route that only knows how to fall back Home.
    if (householdLessInstallation) setRoute({ kind: 'adminHouseholds' });
  }, [householdLessAdmin, householdLessInstallation, noHouseholdScreen, setRoute, signedInOnLogin]);
}

/** Every way out of a screen: one place that records the destination, the address and the list it belongs to. */
function useNavigationActions({ setRoute, transitionedRef, setAllItemsVisit, setItemOrigin, setArchivedView, clearTransient, userId, setAdminTargetName }: {
  setRoute: Dispatch<SetStateAction<Route>>;
  transitionedRef: RefObject<boolean>;
  setAllItemsVisit: Dispatch<SetStateAction<number>>;
  setItemOrigin: Dispatch<SetStateAction<ItemOrigin>>;
  setArchivedView: Dispatch<SetStateAction<boolean>>;
  clearTransient: () => void;
  userId: string | null;
  setAdminTargetName: Dispatch<SetStateAction<AdminTargetName | null>>;
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
    setAdminTargetName(null);
    if (transient) clearTransient();
    window.history.pushState(null, '', path);
    setRoute(route);
  }, [clearTransient, setAdminTargetName, setItemOrigin, setRoute, transitionedRef]);
  const goHome = useCallback(() => simple('/', { kind: 'home' }, true), [simple]);
  const openPeople = useCallback(() => simple('/people', { kind: 'people' }, false), [simple]);
  const openAccount = useCallback(() => simple('/account', { kind: 'account' }, true), [simple]);
  const openMembers = useCallback(() => simple('/members', { kind: 'members' }, false), [simple]);
  const openAdmin = useCallback(() => simple('/admin', { kind: 'admin' }, true), [simple]);
  const openAdminHouseholds = useCallback(
    () => simple(ADMIN_HOUSEHOLDS_PATH, { kind: 'adminHouseholds' }, true), [simple],
  );
  const openAdminAccounts = useCallback(
    () => simple(ADMIN_ACCOUNTS_PATH, { kind: 'adminAccounts' }, true), [simple],
  );
  const openAdminAiApps = useCallback(
    () => simple(ADMIN_AI_APPS_PATH, { kind: 'adminAiApps' }, true), [simple],
  );
  /** The list row already knows the household's name; the detail screen shows it instead of a generic heading. */
  const openAdminHousehold = useCallback((householdId: string, name: string) => {
    simple(`${ADMIN_HOUSEHOLDS_PATH}/${encodeURIComponent(householdId)}`, { kind: 'adminHousehold', householdId }, true);
    setAdminTargetName(userId ? { userId, targetId: householdId, name } : null);
  }, [setAdminTargetName, simple, userId]);
  /** The directory row already knows the login; the account's AI apps heading names it. */
  const openAdminAccountAiApps = useCallback((accountId: string, login: string) => {
    simple(adminAccountAiAppsPath(accountId), { kind: 'adminAccountAiApps', userId: accountId }, true);
    setAdminTargetName(userId ? { userId, targetId: accountId, name: login } : null);
  }, [setAdminTargetName, simple, userId]);
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
    openAdminHouseholds, openAdminAccounts, openAdminAiApps, openAdminHousehold, openAdminAccountAiApps,
    goHome, goBackFromItem, openItemFromAllItems, openItemFromHome, reportItemsView };
}

/** Back and forward recompute everything the address says, including the list an item belongs to. */
function usePopstateNavigation({ setRoute, transitionedRef, setArchivedView, setItemOrigin, clearTransient, setAdminTargetName }: {
  setRoute: Dispatch<SetStateAction<Route>>;
  transitionedRef: RefObject<boolean>;
  setArchivedView: Dispatch<SetStateAction<boolean>>;
  setItemOrigin: Dispatch<SetStateAction<ItemOrigin>>;
  clearTransient: () => void;
  setAdminTargetName: Dispatch<SetStateAction<AdminTargetName | null>>;
}) {
  useEffect(() => {
    const onPop = () => {
      transitionedRef.current = true;
      clearTransient();
      // A history entry never carried the name, so a detail reached by Back or Forward keeps the generic heading.
      setAdminTargetName(null);
      const next = readRoute();
      setArchivedView(next.kind === 'allItems' && isArchivedSearch(window.location.search));
      setItemOrigin(next.kind === 'item' ? historyOrigin() : null);
      setRoute(next);
    };
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, [clearTransient, setAdminTargetName, setArchivedView, setItemOrigin, setRoute, transitionedRef]);
}

export function useAppNavigation({ route, setRoute, transitionedRef, setAllItemsVisit, setOidcOutcome, clearHomeNotice, state }: Options) {
  // Both are presentation state for the navigation only: which list an item was opened from, and whether that
  // list is showing the archive. They are set by the actions above; a reload keeps them only because the history
  // entry an item was pushed with still names where it was opened from.
  const [itemOrigin, setItemOrigin] = useState<ItemOrigin>(() => (route.kind === 'item' ? historyOrigin() : null));
  // A deep link or reload of the archive opens with Archive (and phone More) current from the first render.
  const [archivedView, setArchivedView] = useState(() => route.kind === 'allItems' && isArchivedSearch(window.location.search));
  const [adminTargetName, setAdminTargetName] = useState<AdminTargetName | null>(null);
  // A one-shot request from another screen to open Add item on Home. It is screen state, never history state, so Back
  // or Forward to Home (and any other move) never reopens the form.
  const [addItemRequested, setAddItemRequested] = useState(false);
  const userId = state.kind === 'signedIn' ? state.session.userId : null;
  // A notice belongs to the screen that received it; any move away retires it together with callback fragments.
  const clearTransient = useCallback(() => {
    setOidcOutcome(null);
    clearHomeNotice();
    setAddItemRequested(false);
  }, [clearHomeNotice, setOidcOutcome]);
  const actions = useNavigationActions({
    setRoute, transitionedRef, setAllItemsVisit, setItemOrigin, setArchivedView, clearTransient,
    userId, setAdminTargetName,
  });
  usePopstateNavigation({ setRoute, transitionedRef, setArchivedView, setItemOrigin, clearTransient, setAdminTargetName });
  useCanonicalUrl(route, state, setRoute);
  /** The name this visit carried in for one target; dropped when it belongs to another account or page. */
  const adminTargetNameFor = useCallback((targetId: string) => (
    adminTargetName?.userId === userId && adminTargetName.targetId === targetId ? adminTargetName.name : null
  ), [adminTargetName, userId]);
  const { goHome } = actions;
  /** Add item lives on Home: go there and ask it to open the form once it is ready. */
  const openAddItem = useCallback(() => { goHome(); setAddItemRequested(true); }, [goHome]);
  const addItemHandled = useCallback(() => setAddItemRequested(false), []);
  // Leaving Home before it was ready spends the request too: it never opens the form on a later visit.
  if (addItemRequested && route.kind !== 'home') setAddItemRequested(false);
  return { ...actions, itemOrigin, archivedView, adminTargetNameFor, openAddItem, addItemRequested, addItemHandled };
}

/** A deep link has no history to read, so an item reached without one belongs to the All items list. */
function historyOrigin(): ItemOrigin {
  const state = window.history.state ?? {};
  if (state.fromHome === true) return 'home';
  const returnTo = state.returnTo;
  if (typeof returnTo !== 'string') return 'allItems';
  return returnTo.includes('status=archived') ? 'archive' : 'allItems';
}
