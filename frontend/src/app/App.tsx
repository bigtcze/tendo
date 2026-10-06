import { useCallback, useEffect, useRef, useState } from 'react';
import { LoginScreen } from '../features/auth/LoginScreen';
import { SetupPendingScreen } from '../features/auth/SetupPendingScreen';
import { fetchSession, fetchSetupRequired, type Session } from '../features/auth/sessionApi';
import { HomeScreen } from '../features/home/HomeScreen';
import { useI18n } from '../i18n';
import { ScreenTransitionContext } from './Heading';
import { Layout } from './Layout';
import { ErrorScreen, LoadingScreen } from './StatusScreens';

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
    setState({ kind: 'signedOut' });
  }, []);
  const signedIn = useCallback((session: Session) => {
    transitioned.current = true;
    setState({ kind: 'signedIn', session });
  }, []);

  if (state.kind === 'signedIn') {
    return (
      <ScreenTransitionContext value={transitioned}>
        <HomeScreen session={state.session} onSignedOut={signedOut} />
      </ScreenTransitionContext>
    );
  }

  return (
    <ScreenTransitionContext value={transitioned}>
    <Layout>
      {state.kind === 'loading' ? <LoadingScreen message={t('app.loading')} /> : null}
      {state.kind === 'error' ? <ErrorScreen onRetry={retry} /> : null}
      {state.kind === 'setupRequired' ? <SetupPendingScreen /> : null}
      {state.kind === 'signedOut' ? <LoginScreen onSignedIn={signedIn} /> : null}
    </Layout>
    </ScreenTransitionContext>
  );
}
