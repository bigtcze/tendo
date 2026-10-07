import { useCallback, useEffect, useRef, useState } from 'react';
import { LoginScreen } from '../features/auth/LoginScreen';
import { OnboardingScreen } from '../features/auth/OnboardingScreen';
import { fetchSession, fetchSetupRequired, type Session } from '../features/auth/sessionApi';
import { HomeScreen } from '../features/home/HomeScreen';
import { SubjectsScreen } from '../features/subjects/SubjectsScreen';
import { useI18n } from '../i18n';
import { ScreenTransitionContext } from './Heading';
import { Layout } from './Layout';
import { ErrorScreen, LoadingScreen } from './StatusScreens';

const PEOPLE_PATH = '/people';

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
  return { kind: 'signedOut' };
}

export function App() {
  const { t } = useI18n();
  const [state, setState] = useState<BootState>({ kind: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const [people, setPeople] = useState(() => window.location.pathname === PEOPLE_PATH);
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
    transitioned.current = true;
    if (window.location.pathname !== '/') window.history.replaceState(null, '', '/');
    setPeople(false);
    setState({ kind: 'signedOut' });
  }, []);

  // Two screens only, so a path flag plus popstate is enough for Back to work; no router.
  const openPeople = useCallback(() => {
    transitioned.current = true;
    window.history.pushState(null, '', PEOPLE_PATH);
    setPeople(true);
  }, []);
  const closePeople = useCallback(() => {
    transitioned.current = true;
    window.history.pushState(null, '', '/');
    setPeople(false);
  }, []);
  useEffect(() => {
    const onPop = () => {
      transitioned.current = true;
      setPeople(window.location.pathname === PEOPLE_PATH);
    };
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, []);
  // Signing in from onboarding or login always lands on home, even when the page was opened at /people.
  const signedIn = useCallback((session: Session) => {
    transitioned.current = true;
    if (window.location.pathname !== '/') window.history.replaceState(null, '', '/');
    setPeople(false);
    setState({ kind: 'signedIn', session });
  }, []);

  if (state.kind === 'signedIn') {
    const householdId = state.session.defaultHouseholdId;
    return (
      <ScreenTransitionContext value={transitioned}>
        {people && householdId ? (
          <SubjectsScreen householdId={householdId} login={state.session.login} onBack={closePeople} onSignedOut={signedOut} />
        ) : (
          <HomeScreen session={state.session} onSignedOut={signedOut} onOpenPeople={openPeople} />
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
