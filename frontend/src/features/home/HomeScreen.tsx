import { useCallback, useEffect, useState } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Layout } from '../../app/Layout';
import { ErrorScreen, LoadingScreen } from '../../app/StatusScreens';
import { useI18n, type MessageKey } from '../../i18n';
import { api } from '../../lib/api';
import type { Session } from '../auth/sessionApi';
import { ItemsScreen } from '../items/ItemsScreen';
import { Button } from '../../components/ui/button';
import { Notice } from '../../components/ui/notice';
import { NoHouseholdScreen } from './NoHouseholdScreen';

type HouseholdState =
  | { kind: 'loading' }
  | { kind: 'ready'; name: string }
  | { kind: 'none' }
  | { kind: 'error' };

/** One-time calm note about the result of a provider sign-in; dismissible so it never blocks the screen. */
function HomeNotice({ notice, onDismiss }: { notice: MessageKey; onDismiss: () => void }) {
  const { t } = useI18n();
  return (
    <div className="mb-5 flex max-w-3xl flex-wrap items-start gap-2">
      <Notice className="min-w-0 flex-1">{t(notice)}</Notice>
      <Button variant="quiet" size="small" onClick={onDismiss}>{t('notice.dismiss')}</Button>
    </div>
  );
}

export function HomeScreen({
  session,
  onSignedOut,
  onOpenPeople,
  onOpenItem,
  onOpenAllItems,
  homeNotice,
  onDismissNotice,
  onContinuePending,
}: {
  session: Session;
  onSignedOut: () => void;
  onOpenPeople: () => void;
  onOpenItem: (itemId: string) => void;
  onOpenAllItems: (status: import('../items/itemsApi').ItemGroup) => void;
  homeNotice?: MessageKey | null;
  onDismissNotice?: () => void;
  onContinuePending?: () => void;
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
      {homeNotice && onDismissNotice ? <HomeNotice notice={homeNotice} onDismiss={onDismissNotice} /> : null}
      {household.kind === 'loading' ? <LoadingScreen message={t('home.loading')} /> : null}
      {household.kind === 'error' ? <ErrorScreen onRetry={retry} /> : null}
      {household.kind === 'none' ? (
        <NoHouseholdScreen onSignedOut={onSignedOut} onDiscarded={onContinuePending ?? onSignedOut} />
      ) : null}
      {household.kind === 'ready' ? (
        <section className="settle">
          <ItemsScreen
            key={`${session.userId}:${householdId}`}
            userId={session.userId}
            householdId={householdId!}
            householdName={household.name}
            onOpenPeople={onOpenPeople}
            onOpenItem={onOpenItem}
            onOpenAllItems={onOpenAllItems}
            onSignedOut={onSignedOut}
          />
        </section>
      ) : null}
    </Layout>
  );
}
