import { useCallback, useEffect, useRef, useState } from 'react';
import { AccountScreen } from '../features/auth/AccountScreen';
import { LoginScreen } from '../features/auth/LoginScreen';
import { parseOidcFragment, type OidcOutcome } from '../features/auth/oidcApi';
import { OnboardingScreen } from '../features/auth/OnboardingScreen';
import { fetchSession, fetchSetupRequired, type Session } from '../features/auth/sessionApi';
import { HomeScreen } from '../features/home/HomeScreen';
import { ItemDetailScreen } from '../features/items/ItemDetailScreen';
import { AllItemsScreen } from '../features/items/AllItemsScreen';
import type { ItemGroup } from '../features/items/itemsApi';
import { clearPendingCompletions } from '../features/items/pendingCompletions';
import { clearPendingItemCreates } from '../features/items/pendingItemCreate';
import { clearPendingSubjectCreates } from '../features/subjects/pendingSubjectCreate';
import { SubjectsScreen } from '../features/subjects/SubjectsScreen';
import { MembersScreen } from '../features/members/MembersScreen';
import { InviteScreen } from '../features/members/InviteScreen';
import { useI18n } from '../i18n';
import { ScreenTransitionContext } from './Heading';
import { SignOutContext } from './SignOutContext';
import { HouseholdNavigationContext, type HouseholdRoute } from './HouseholdNavigation';
import { Layout } from './Layout';
import { ErrorScreen, LoadingScreen } from './StatusScreens';

const PEOPLE_PATH = '/people';
const MEMBERS_PATH = '/members';
const INVITE_PATH = '/invite';
const LOGIN_PATH = '/login';
const ACCOUNT_PATH = '/account';
type Route = { kind: 'home' | 'people' | 'members' | 'invite' | 'login' | 'account' | 'allItems' } | { kind: 'item'; itemId: string } | { kind: 'invalidItem' };
function readRoute(): Route {
  const path = window.location.pathname;
  if (path === PEOPLE_PATH) return { kind: 'people' };
  if (path === MEMBERS_PATH) return { kind: 'members' };
  if (path === INVITE_PATH) return { kind: 'invite' };
  if (path === LOGIN_PATH) return { kind: 'login' };
  if (path === ACCOUNT_PATH) return { kind: 'account' };
  if (path === '/') return { kind: 'home' };
  if (path === '/items') return { kind: 'allItems' };
  const match = /^\/items\/([^/]+)$/.exec(path);
  if (match) return /^[0-9a-f-]{36}$/i.test(match[1]!) ? { kind: 'item', itemId: match[1]! } : { kind: 'invalidItem' };
  return { kind: 'home' };
}

type BootState =
  | { kind: 'loading' }
  | { kind: 'error' }
  | { kind: 'setupRequired' }
  | { kind: 'signedOut' }
  | { kind: 'signedIn'; session: Session };

async function boot(): Promise<BootState> {
  const result = await fetchSession();
  if (result.kind === 'signedIn') return { kind: 'signedIn', session: result.session };
  if (result.kind === 'error') return { kind: 'error' };
  const setup = await fetchSetupRequired();
  if (setup === 'required') return { kind: 'setupRequired' };
  if (setup === 'error') return { kind: 'error' };
  clearPendingCompletions();
  clearPendingSubjectCreates();
  clearPendingItemCreates();
  return { kind: 'signedOut' };
}

let inviteVisitFallbackCounter = 0;
function createInviteVisitId(): string {
  const cryptoApi = globalThis.crypto;
  if (typeof cryptoApi?.randomUUID === 'function') return cryptoApi.randomUUID();
  if (typeof cryptoApi?.getRandomValues === 'function') {
    const bytes = cryptoApi.getRandomValues(new Uint8Array(16));
    return Array.from(bytes, (byte) => byte.toString(16).padStart(2, '0')).join('');
  }
  inviteVisitFallbackCounter += 1;
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}-${inviteVisitFallbackCounter.toString(36)}`;
}

export function App() {
  const { t } = useI18n();
  const [state, setState] = useState<BootState>({ kind: 'loading' });
  const [sessionGeneration, setSessionGeneration] = useState(0);
  const sessionGenerationRef = useRef(0);
  const advanceSessionGeneration = useCallback(() => {
    const next = sessionGenerationRef.current + 1;
    sessionGenerationRef.current = next;
    setSessionGeneration(next);
  }, []);
  const [attempt, setAttempt] = useState(0);
  const [route, setRoute] = useState(() => readRoute());
  const [allItemsVisit, setAllItemsVisit] = useState(0);
  const [inviteVisit, setInviteVisit] = useState<string | null>(() => {
    const initialVisit = window.history.state?.inviteVisit;
    return typeof initialVisit === 'string' ? initialVisit : null;
  });
  const inviteTokens = useRef(new Map<string, string>());
  const [inviteToken, setInviteToken] = useState<string | null>(() => {
    if (window.location.pathname !== INVITE_PATH) return null;
    const hash = window.location.hash;
    return hash.startsWith('#') ? hash.slice(1) || null : null;
  });
  const [accountReadyNotice, setAccountReadyNotice] = useState(false);
  const [sessionEndedNotice, setSessionEndedNotice] = useState(false);
  // The OIDC callback reports its result in a fixed fragment on /login or /account. Read it once,
  // keep it in memory only, and scrub it from the address bar so reload/share never repeats it.
  const [oidcOutcome, setOidcOutcome] = useState<OidcOutcome | null>(() => {
    const path = window.location.pathname;
    return path === LOGIN_PATH || path === ACCOUNT_PATH ? parseOidcFragment(window.location.hash) : null;
  });
  useEffect(() => {
    const path = window.location.pathname;
    if ((path === LOGIN_PATH || path === ACCOUNT_PATH) && window.location.hash) window.history.replaceState(window.history.state, '', path);
  }, []);
  const [inviteFocusLogin, setInviteFocusLogin] = useState(false);

  useEffect(() => {
    const captureInviteVisit = () => {
      if (window.location.pathname !== INVITE_PATH) return;
      if (window.location.hash) {
        const token = window.location.hash.slice(1);
        const visit = createInviteVisitId();
        inviteTokens.current.set(visit, token);
        window.history.replaceState({ inviteVisit: visit }, '', INVITE_PATH);
        setInviteToken(token || null);
        setInviteVisit(visit);
        return;
      }
      const visit = window.history.state?.inviteVisit;
      if (typeof visit === 'string') {
        setInviteVisit(visit);
        setInviteToken(inviteTokens.current.get(visit) ?? null);
      } else {
        setInviteVisit(null);
        setInviteToken(null);
      }
    };
    if (window.location.pathname === INVITE_PATH && window.location.hash) {
      const token = window.location.hash.slice(1);
      const visit = createInviteVisitId();
      inviteTokens.current.set(visit, token);
      window.history.replaceState({ inviteVisit: visit }, '', INVITE_PATH);
    }
    window.addEventListener('hashchange', captureInviteVisit);
    window.addEventListener('popstate', captureInviteVisit);
    return () => {
      window.removeEventListener('hashchange', captureInviteVisit);
      window.removeEventListener('popstate', captureInviteVisit);
    };
  }, []);
  // Flipped on the first screen change so later headings take focus; initial load keeps natural focus.
  const transitioned = useRef(false);

  useEffect(() => {
    let cancelled = false;
    boot().then((next) => {
      if (!cancelled) {
        if (next.kind === 'signedIn') advanceSessionGeneration();
        setState(next);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [attempt, advanceSessionGeneration]);

  const retry = useCallback(() => {
    transitioned.current = true;
    setState({ kind: 'loading' });
    setAttempt((n) => n + 1);
  }, []);

  const inviteSignedOut = useCallback(() => {
    advanceSessionGeneration();
    clearPendingCompletions();
    clearPendingSubjectCreates();
    clearPendingItemCreates();
    transitioned.current = true;
    const inviteVisit = window.history.state?.inviteVisit;
    window.history.replaceState(typeof inviteVisit === 'string' ? { inviteVisit } : null, '', INVITE_PATH);
    setRoute({ kind: 'invite' });
    setInviteFocusLogin(true);
    setState({ kind: 'signedOut' });
  }, [advanceSessionGeneration]);
  const accountReady = useCallback(() => {
    advanceSessionGeneration();
    transitioned.current = true;
    window.history.replaceState(null, '', '/');
    setRoute({ kind: 'home' });
    setOidcOutcome(null);
    setSessionEndedNotice(false);
    setAccountReadyNotice(true);
    setState({ kind: 'signedOut' });
  }, [advanceSessionGeneration]);
  const signedOut = useCallback(() => {
    advanceSessionGeneration();
    clearPendingCompletions();
    clearPendingSubjectCreates();
    clearPendingItemCreates();
    setOidcOutcome(null);
    setSessionEndedNotice(false);
    transitioned.current = true;
    if (window.location.pathname !== '/') window.history.replaceState(null, '', '/');
    setRoute({ kind: 'home' });
    setState({ kind: 'signedOut' });
  }, [advanceSessionGeneration]);
  const sessionEnded = useCallback(() => {
    if (sessionGenerationRef.current !== sessionGeneration || state.kind !== 'signedIn') return;
    advanceSessionGeneration();
    clearPendingCompletions();
    clearPendingSubjectCreates();
    clearPendingItemCreates();
    setOidcOutcome(null);
    transitioned.current = true;
    if (window.location.pathname !== '/') window.history.replaceState(null, '', '/');
    setRoute({ kind: 'home' });
    setState({ kind: 'signedOut' });
    setSessionEndedNotice(true);
  }, [advanceSessionGeneration, sessionGeneration, state.kind]);

  // A small route value plus popstate keeps these few app screens navigable without a router.
  const openAllItems = useCallback(() => {
    transitioned.current = true;
    window.history.pushState({ allItemsCursors: [] }, '', '/items');
    setAllItemsVisit((visit) => visit + 1);
    setRoute({ kind: 'allItems' });
  }, []);
  const openAllItemsForStatus = useCallback((status: ItemGroup) => {
    transitioned.current = true;
    window.history.pushState({ allItemsCursors: [] }, '', `/items?status=${status}`);
    setAllItemsVisit((visit) => visit + 1);
    setRoute({ kind: 'allItems' });
  }, []);
  const openPeople = useCallback(() => {
    transitioned.current = true;
    window.history.pushState(null, '', PEOPLE_PATH);
    setRoute({ kind: 'people' });
  }, []);
  const openAccount = useCallback(() => {
    transitioned.current = true;
    setOidcOutcome(null);
    window.history.pushState(null, '', ACCOUNT_PATH);
    setRoute({ kind: 'account' });
  }, []);
  const openMembers = useCallback(() => {
    transitioned.current = true;
    window.history.pushState(null, '', MEMBERS_PATH);
    setRoute({ kind: 'members' });
  }, []);
  // Back links from People and item details push home so browser Back still returns to them.
  const goHome = useCallback(() => {
    transitioned.current = true;
    setOidcOutcome(null);
    window.history.pushState(null, '', '/');
    setRoute({ kind: 'home' });
  }, []);
  const goBackFromItem = useCallback(() => {
    const returnTo = window.history.state?.returnTo;
    if (typeof returnTo === 'string' && returnTo.startsWith('/items')) {
      transitioned.current = true;
      if (window.history.length > 1) window.history.back();
      else { window.history.pushState(null, '', returnTo); setRoute({ kind: 'allItems' }); }
      return;
    }
    goHome();
  }, [goHome]);
  useEffect(() => {
    const onPop = () => {
      transitioned.current = true;
      setOidcOutcome(null);
      setRoute(readRoute());
    };
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, []);
  // Signing in from onboarding or login always lands on home, even when the page was opened at /people.
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
  const signedIn = useCallback((session: Session) => {
    advanceSessionGeneration();
    transitioned.current = true;
    if (window.location.pathname !== '/') window.history.replaceState(null, '', '/');
    setRoute({ kind: 'home' });
    setOidcOutcome(null);
    setAccountReadyNotice(false);
    setSessionEndedNotice(false);
    setInviteFocusLogin(false);
    setState({ kind: 'signedIn', session });
  }, [advanceSessionGeneration]);

  if (state.kind === 'signedIn') {
    const householdId = state.session.defaultHouseholdId;
    const navRoute: HouseholdRoute = route.kind === 'account' ? 'account' : !householdId ? 'home' : route.kind === 'allItems' ? 'allItems' : route.kind === 'people' ? 'people' : route.kind === 'members' ? 'members' : route.kind === 'item' || route.kind === 'invalidItem' ? 'item' : 'home';
    const navigationActions = { home: goHome, allItems: openAllItems, people: openPeople, members: openMembers, account: openAccount };
    if (route.kind === 'invite') {
      return <ScreenTransitionContext value={transitioned}>
        <InviteScreen key={inviteVisit} token={inviteToken} session={state.session} onSignedIn={signedIn} onSignedOut={inviteSignedOut} onAccountReady={accountReady} onJoinedSignedOut={sessionEnded} />
      </ScreenTransitionContext>;
    }
    return (
      <SignOutContext value={signedOut}>
      <HouseholdNavigationContext value={{ route: navRoute, actions: navigationActions, household: Boolean(householdId) }}>
      <ScreenTransitionContext value={transitioned}>
        {route.kind === 'account' ? (
          <AccountScreen login={state.session.login} outcome={oidcOutcome} onSignedOut={sessionEnded} />
        ) : route.kind === 'people' && householdId ? (
          <SubjectsScreen userId={state.session.userId} householdId={householdId} login={state.session.login} onSignedOut={sessionEnded} />
        ) : route.kind === 'members' && householdId ? (
          <MembersScreen householdId={householdId} userId={state.session.userId} login={state.session.login} onSignedOut={sessionEnded} />
        ) : route.kind === 'allItems' && householdId ? (
          <AllItemsScreen key={allItemsVisit} householdId={householdId} login={state.session.login} userId={state.session.userId} onBack={goHome} onSignedOut={sessionEnded} onOpenItem={(itemId, returnTo) => { transitioned.current = true; window.history.pushState({ ...(window.history.state ?? {}), returnTo, fromAllItems: true }, '', `/items/${encodeURIComponent(itemId)}`); setRoute({ kind: 'item', itemId }); }} />
        ) : route.kind === 'item' && householdId ? (
          <ItemDetailScreen key={`${householdId}:${route.itemId}`} householdId={householdId} itemId={route.itemId} login={state.session.login} onBack={goBackFromItem} onSignedOut={sessionEnded} />
        ) : route.kind === 'invalidItem' && householdId ? (
          <ItemDetailScreen key={`${householdId}:invalid`} householdId={householdId} itemId="invalid" login={state.session.login} onBack={goHome} onSignedOut={sessionEnded} />
        ) : (
          <HomeScreen session={state.session} onSignedOut={sessionEnded} onOpenPeople={openPeople} onOpenAllItems={openAllItemsForStatus} onOpenItem={(itemId) => { transitioned.current = true; window.history.pushState(null, '', `/items/${encodeURIComponent(itemId)}`); setRoute({ kind: 'item', itemId }); }} />
        )}
      </ScreenTransitionContext>
      </HouseholdNavigationContext>
      </SignOutContext>
    );
  }

  return (
    <ScreenTransitionContext value={transitioned}>
    {state.kind === 'signedOut' && route.kind === 'invite' ? <InviteScreen key={inviteVisit} token={inviteToken} onSignedIn={signedIn} onSignedOut={inviteSignedOut} onAccountReady={accountReady} onJoinedSignedOut={signedOut} focusLogin={inviteFocusLogin} /> : <Layout>
      {state.kind === 'loading' ? <LoadingScreen message={t('app.loading')} /> : null}
      {state.kind === 'error' ? <ErrorScreen onRetry={retry} /> : null}
      {state.kind === 'setupRequired' ? <OnboardingScreen onSignedIn={signedIn} onSetupComplete={signedOut} /> : null}
      {state.kind === 'signedOut' ? <LoginScreen onSignedIn={signedIn} notice={accountReadyNotice ? t('invite.error.accountReady') : oidcOutcome?.kind === 'error' ? t(`oidc.error.${oidcOutcome.code}`) : sessionEndedNotice ? t('login.sessionEnded') : undefined} /> : null}
    </Layout>}
    </ScreenTransitionContext>
  );
}
