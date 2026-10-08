import { useCallback, useEffect, useRef, useState } from 'react';
import { LoginScreen } from '../features/auth/LoginScreen';
import { OnboardingScreen } from '../features/auth/OnboardingScreen';
import { fetchSession, fetchSetupRequired, type Session } from '../features/auth/sessionApi';
import { HomeScreen } from '../features/home/HomeScreen';
import { ItemDetailScreen } from '../features/items/ItemDetailScreen';
import { clearPendingCompletions } from '../features/items/pendingCompletions';
import { SubjectsScreen } from '../features/subjects/SubjectsScreen';
import { useI18n } from '../i18n';
import { ScreenTransitionContext } from './Heading';
import { Layout } from './Layout';
import { ErrorScreen, LoadingScreen } from './StatusScreens';

const PEOPLE_PATH = '/people';
type Route = { kind: 'home' | 'people' } | { kind: 'item'; itemId: string } | { kind: 'invalidItem' };
function readRoute(): Route {
  const path = window.location.pathname;
  if (path === PEOPLE_PATH) return { kind: 'people' };
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

export function App() {
  const { t } = useI18n();
  const [state, setState] = useState<BootState>({ kind: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const [route, setRoute] = useState(() => readRoute());
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

  const signedOut = useCallback(() => {
    clearPendingCompletions();
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
  // Back links from People and item details push home so browser Back still returns to them.
  const goHome = useCallback(() => {
    transitioned.current = true;
    window.history.pushState(null, '', '/');
    setRoute({ kind: 'home' });
  }, []);
  useEffect(() => {
    const onPop = () => {
      transitioned.current = true;
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
  const signedIn = useCallback((session: Session) => {
    transitioned.current = true;
    if (window.location.pathname !== '/') window.history.replaceState(null, '', '/');
    setRoute({ kind: 'home' });
    setState({ kind: 'signedIn', session });
  }, []);

  if (state.kind === 'signedIn') {
    const householdId = state.session.defaultHouseholdId;
    return (
      <ScreenTransitionContext value={transitioned}>
        {route.kind === 'people' && householdId ? (
          <SubjectsScreen householdId={householdId} login={state.session.login} onBack={goHome} onSignedOut={signedOut} />
        ) : route.kind === 'item' && householdId ? (
          <ItemDetailScreen key={`${householdId}:${route.itemId}`} householdId={householdId} itemId={route.itemId} login={state.session.login} onBack={goHome} onSignedOut={signedOut} />
        ) : route.kind === 'invalidItem' && householdId ? (
          <ItemDetailScreen key={`${householdId}:invalid`} householdId={householdId} itemId="invalid" login={state.session.login} onBack={goHome} onSignedOut={signedOut} />
        ) : (
          <HomeScreen session={state.session} onSignedOut={signedOut} onOpenPeople={openPeople} onOpenItem={(itemId) => { transitioned.current = true; window.history.pushState(null, '', `/items/${encodeURIComponent(itemId)}`); setRoute({ kind: 'item', itemId }); }} />
        )}
      </ScreenTransitionContext>
    );
  }

  return (
    <ScreenTransitionContext value={transitioned}>
    <Layout>
      {state.kind === 'loading' ? <LoadingScreen message={t('app.loading')} /> : null}
      {state.kind === 'error' ? <ErrorScreen onRetry={retry} /> : null}
      {state.kind === 'setupRequired' ? <OnboardingScreen onSignedIn={signedIn} onSetupComplete={signedOut} /> : null}
      {state.kind === 'signedOut' ? <LoginScreen onSignedIn={signedIn} /> : null}
    </Layout>
    </ScreenTransitionContext>
  );
}
