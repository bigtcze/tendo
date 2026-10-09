import { useCallback, useEffect, useRef, useState } from 'react';
import { AccountScreen } from '../features/auth/AccountScreen';
import { LoginScreen } from '../features/auth/LoginScreen';
import { parseOidcFragment, type OidcOutcome } from '../features/auth/oidcApi';
import { OnboardingScreen } from '../features/auth/OnboardingScreen';
import { fetchSession, fetchSetupRequired, type Session } from '../features/auth/sessionApi';
import { HomeScreen } from '../features/home/HomeScreen';
import { ItemDetailScreen } from '../features/items/ItemDetailScreen';
import { clearPendingCompletions } from '../features/items/pendingCompletions';
import { SubjectsScreen } from '../features/subjects/SubjectsScreen';
import { MembersScreen } from '../features/members/MembersScreen';
import { InviteScreen } from '../features/members/InviteScreen';
import { useI18n } from '../i18n';
import { ScreenTransitionContext } from './Heading';
import { Layout } from './Layout';
import { ErrorScreen, LoadingScreen } from './StatusScreens';

const PEOPLE_PATH = '/people';
const MEMBERS_PATH = '/members';
const INVITE_PATH = '/invite';
const LOGIN_PATH = '/login';
const ACCOUNT_PATH = '/account';
type Route = { kind: 'home' | 'people' | 'members' | 'invite' | 'login' | 'account' } | { kind: 'item'; itemId: string } | { kind: 'invalidItem' };
function readRoute(): Route {
  const path = window.location.pathname;
  if (path === PEOPLE_PATH) return { kind: 'people' };
  if (path === MEMBERS_PATH) return { kind: 'members' };
  if (path === INVITE_PATH) return { kind: 'invite' };
  if (path === LOGIN_PATH) return { kind: 'login' };
  if (path === ACCOUNT_PATH) return { kind: 'account' };
  if (path === '/') return { kind: 'home' };
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
  const [attempt, setAttempt] = useState(0);
  const [route, setRoute] = useState(() => readRoute());
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
      if (!cancelled) setState(next);
    });
    return () => {
      cancelled = true;
    };
  }, [attempt]);

  const retry = useCallback(() => {
    transitioned.current = true;
    setState({ kind: 'loading' });
    setAttempt((n) => n + 1);
  }, []);

  const inviteSignedOut = useCallback(() => {
    clearPendingCompletions();
    transitioned.current = true;
    const inviteVisit = window.history.state?.inviteVisit;
    window.history.replaceState(typeof inviteVisit === 'string' ? { inviteVisit } : null, '', INVITE_PATH);
    setRoute({ kind: 'invite' });
    setInviteFocusLogin(true);
    setState({ kind: 'signedOut' });
  }, []);
  const accountReady = useCallback(() => {
    transitioned.current = true;
    window.history.replaceState(null, '', '/');
    setRoute({ kind: 'home' });
    setOidcOutcome(null);
    setAccountReadyNotice(true);
    setState({ kind: 'signedOut' });
  }, []);
  const signedOut = useCallback(() => {
    clearPendingCompletions();
    setOidcOutcome(null);
    transitioned.current = true;
    if (window.location.pathname !== '/') window.history.replaceState(null, '', '/');
    setRoute({ kind: 'home' });
    setState({ kind: 'signedOut' });
  }, []);

  // A small route value plus popstate keeps these few app screens navigable without a router.
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
    transitioned.current = true;
    if (window.location.pathname !== '/') window.history.replaceState(null, '', '/');
    setRoute({ kind: 'home' });
    setOidcOutcome(null);
    setAccountReadyNotice(false);
    setInviteFocusLogin(false);
    setState({ kind: 'signedIn', session });
  }, []);

  if (state.kind === 'signedIn') {
    const householdId = state.session.defaultHouseholdId;
    return (
      <ScreenTransitionContext value={transitioned}>
        {route.kind === 'invite' ? (
          <InviteScreen key={inviteVisit} token={inviteToken} session={state.session} onSignedIn={signedIn} onSignedOut={inviteSignedOut} onAccountReady={accountReady} onJoinedSignedOut={signedOut} />
        ) : route.kind === 'account' ? (
          <AccountScreen login={state.session.login} outcome={oidcOutcome} onBack={goHome} onSignedOut={signedOut} />
        ) : route.kind === 'people' && householdId ? (
          <SubjectsScreen householdId={householdId} login={state.session.login} onBack={goHome} onSignedOut={signedOut} />
        ) : route.kind === 'members' && householdId ? (
          <MembersScreen householdId={householdId} userId={state.session.userId} login={state.session.login} onBack={goHome} onSignedOut={signedOut} />
        ) : route.kind === 'item' && householdId ? (
          <ItemDetailScreen key={`${householdId}:${route.itemId}`} householdId={householdId} itemId={route.itemId} login={state.session.login} onBack={goHome} onSignedOut={signedOut} />
        ) : route.kind === 'invalidItem' && householdId ? (
          <ItemDetailScreen key={`${householdId}:invalid`} householdId={householdId} itemId="invalid" login={state.session.login} onBack={goHome} onSignedOut={signedOut} />
        ) : (
          <HomeScreen session={state.session} onSignedOut={signedOut} onOpenPeople={openPeople} onOpenMembers={openMembers} onOpenAccount={openAccount} onOpenItem={(itemId) => { transitioned.current = true; window.history.pushState(null, '', `/items/${encodeURIComponent(itemId)}`); setRoute({ kind: 'item', itemId }); }} />
        )}
      </ScreenTransitionContext>
    );
  }

  return (
    <ScreenTransitionContext value={transitioned}>
    {state.kind === 'signedOut' && route.kind === 'invite' ? <InviteScreen key={inviteVisit} token={inviteToken} onSignedIn={signedIn} onSignedOut={inviteSignedOut} onAccountReady={accountReady} onJoinedSignedOut={signedOut} focusLogin={inviteFocusLogin} /> : <Layout>
      {state.kind === 'loading' ? <LoadingScreen message={t('app.loading')} /> : null}
      {state.kind === 'error' ? <ErrorScreen onRetry={retry} /> : null}
      {state.kind === 'setupRequired' ? <OnboardingScreen onSignedIn={signedIn} onSetupComplete={signedOut} /> : null}
      {state.kind === 'signedOut' ? <LoginScreen onSignedIn={signedIn} notice={accountReadyNotice ? t('invite.error.accountReady') : oidcOutcome?.kind === 'error' ? t(`oidc.error.${oidcOutcome.code}`) : undefined} /> : null}
    </Layout>}
    </ScreenTransitionContext>
  );
}
