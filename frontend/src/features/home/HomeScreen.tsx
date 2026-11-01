import { useCallback, useEffect, useState } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Layout } from '../../app/Layout';
import { ErrorScreen, LoadingScreen } from '../../app/StatusScreens';
import { useI18n } from '../../i18n';
import { api } from '../../lib/api';
import type { Session } from '../auth/sessionApi';
import { Heading } from '../../app/Heading';
import { ItemsScreen } from '../items/ItemsScreen';

type HouseholdState =
  | { kind: 'loading' }
  | { kind: 'ready'; name: string }
  | { kind: 'none' }
  | { kind: 'error' };

export function HomeScreen({
  session,
  onSignedOut,
  onOpenPeople,
  onOpenItem,
  onOpenAllItems,
}: {
  session: Session;
  onSignedOut: () => void;
  onOpenPeople: () => void;
  onOpenItem: (itemId: string) => void;
  onOpenAllItems: (status: import('../items/itemsApi').ItemGroup) => void;
}) {
  const { t, locale } = useI18n();
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
        if (data && response.status === 200) {
          next = { kind: 'ready', name: data.name };
        }
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
          <div className="home-heading"><p className="text-base font-medium text-accent">{new Intl.DateTimeFormat(locale === 'cs' ? 'cs-CZ' : 'en-US', { weekday: 'long', month: 'long', day: 'numeric' }).format(new Date())}</p><Heading className="mt-1 font-display text-display leading-tight">{household.name}</Heading></div>
          <p className="home-today-label text-base font-medium text-accent">{t('items.group.needs')}</p>
          <div className="mt-3">
            <ItemsScreen key={`${session.userId}:${householdId}`} userId={session.userId} householdId={householdId!} onOpenPeople={onOpenPeople} onOpenItem={onOpenItem} onOpenAllItems={onOpenAllItems} onSignedOut={onSignedOut} locale={locale} />
          </div>
        </section>
      ) : null}
    </Layout>
  );
}
