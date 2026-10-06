import { useCallback, useEffect, useState } from 'react';
import { Layout } from '../../app/Layout';
import { ErrorScreen, LoadingScreen } from '../../app/StatusScreens';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import { api } from '../../lib/api';
import { logout, type Session } from '../auth/sessionApi';
import { Heading } from '../../app/Heading';

type HouseholdState =
  | { kind: 'loading' }
  | { kind: 'ready'; name: string }
  | { kind: 'none' }
  | { kind: 'error' };

export function HomeScreen({ session, onSignedOut }: { session: Session; onSignedOut: () => void }) {
  const { t } = useI18n();
  const householdId = session.defaultHouseholdId;
  const [household, setHousehold] = useState<HouseholdState>(
    householdId ? { kind: 'loading' } : { kind: 'none' },
  );
  const [attempt, setAttempt] = useState(0);
  const [signingOut, setSigningOut] = useState(false);
  const [signOutFailed, setSignOutFailed] = useState(false);

  useEffect(() => {
    if (!householdId) return;
    let cancelled = false;
    (async () => {
      let next: HouseholdState = { kind: 'error' };
      try {
        const { data, response } = await api.GET('/api/v1/households/{householdId}', {
          params: { path: { householdId } },
        });
        if (response.status === 401) {
          if (!cancelled) onSignedOut();
          return;
        }
        if (data && response.status === 200) next = { kind: 'ready', name: data.name };
        else if (response.status === 404) next = { kind: 'none' };
      } catch {
        // keep error state
      }
      if (!cancelled) setHousehold(next);
    })();
    return () => {
      cancelled = true;
    };
  }, [householdId, attempt, onSignedOut]);

  const retry = useCallback(() => {
    setHousehold({ kind: 'loading' });
    setAttempt((n) => n + 1);
  }, []);

  async function signOut() {
    setSigningOut(true);
    setSignOutFailed(false);
    const result = await logout();
    if (result === 'ok') {
      onSignedOut();
      return;
    }
    setSigningOut(false);
    setSignOutFailed(true);
  }

  const actions = (
    <div className="flex items-center gap-1 text-sm text-muted">
      <span className="max-w-[10rem] truncate sm:max-w-xs">{t('header.signedInAs', { login: session.login })}</span>
      <Button type="button" variant="quiet" size="small" onClick={signOut} disabled={signingOut}>
        {signingOut ? t('header.signingOut') : t('header.signOut')}
      </Button>
    </div>
  );

  return (
    <Layout actions={actions}>
      {signOutFailed ? (
        <p role="alert" className="mb-6 text-danger">
          {t('header.signOutError')}
        </p>
      ) : null}
      {household.kind === 'loading' ? <LoadingScreen message={t('home.loading')} /> : null}
      {household.kind === 'error' ? <ErrorScreen onRetry={retry} /> : null}
      {household.kind === 'none' ? (
        <section className="settle space-y-4">
          <Heading className="font-display text-3xl leading-tight sm:text-4xl">{t('home.noHousehold.title')}</Heading>
          <p className="text-lg text-muted">{t('home.noHousehold.body')}</p>
        </section>
      ) : null}
      {household.kind === 'ready' ? (
        <section className="settle">
          <Heading className="font-display text-4xl leading-tight sm:text-5xl">{household.name}</Heading>
          <div className="mt-10 rounded-3xl border border-line bg-white/70 p-8 sm:p-12">
            <h2 className="font-display text-2xl leading-snug">{t('home.empty.title')}</h2>
            <p className="mt-3 text-muted">{t('home.empty.body')}</p>
          </div>
        </section>
      ) : null}
    </Layout>
  );
}
