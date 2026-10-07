import { useCallback, useEffect, useState } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Layout } from '../../app/Layout';
import { NavLink } from '../../app/NavLink';
import { ErrorScreen, LoadingScreen } from '../../app/StatusScreens';
import { useI18n } from '../../i18n';
import { api } from '../../lib/api';
import type { Session } from '../auth/sessionApi';
import { Heading } from '../../app/Heading';

type HouseholdState =
  | { kind: 'loading' }
  | { kind: 'ready'; name: string }
  | { kind: 'none' }
  | { kind: 'error' };

export function HomeScreen({
  session,
  onSignedOut,
  onOpenPeople,
}: {
  session: Session;
  onSignedOut: () => void;
  onOpenPeople: () => void;
}) {
  const { t } = useI18n();
  const householdId = session.defaultHouseholdId;
  const [household, setHousehold] = useState<HouseholdState>(
    householdId ? { kind: 'loading' } : { kind: 'none' },
  );
  const [attempt, setAttempt] = useState(0);

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

  return (
    <Layout actions={<AccountActions login={session.login} onSignedOut={onSignedOut} />}>
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
          <NavLink
            href="/people"
            onNavigate={onOpenPeople}
            className="mt-6 -ml-3 inline-flex min-h-11 items-center rounded-xl px-3 text-muted hover:bg-sand hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
          >
            {t('home.people')}
          </NavLink>
        </section>
      ) : null}
    </Layout>
  );
}
