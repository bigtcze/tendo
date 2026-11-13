import { AccountActions } from '../../app/AccountActions';
import { useHouseholdData } from '../../app/HouseholdData';
import { Layout } from '../../app/Layout';
import { ErrorScreen, LoadingScreen } from '../../app/StatusScreens';
import { useI18n, type MessageKey } from '../../i18n';
import type { Session } from '../auth/sessionApi';
import { ItemsScreen } from '../items/ItemsScreen';
import { Button } from '../../components/ui/button';
import { Notice } from '../../components/ui/notice';
import { NoHouseholdScreen } from './NoHouseholdScreen';

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
  const household = useHouseholdData();
  if (!household) throw new Error('HomeScreen must render inside the HouseholdData provider');

  return (
    <Layout actions={<AccountActions login={session.login} onSignedOut={onSignedOut} />}>
      {homeNotice && onDismissNotice ? <HomeNotice notice={homeNotice} onDismiss={onDismissNotice} /> : null}
      {household.data.kind === 'loading' ? <LoadingScreen message={t('home.loading')} /> : null}
      {household.data.kind === 'error' ? <ErrorScreen onRetry={household.retry} /> : null}
      {household.data.kind === 'none' ? (
        <NoHouseholdScreen onSignedOut={onSignedOut} onDiscarded={onContinuePending ?? onSignedOut} />
      ) : null}
      {household.data.kind === 'ready' ? (
        <section className="settle">
          <ItemsScreen
            key={`${session.userId}:${household.data.household.id}`}
            userId={session.userId}
            householdId={household.data.household.id}
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
