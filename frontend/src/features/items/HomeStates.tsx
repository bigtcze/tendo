import { EmptyState } from '../../components/ui/empty-state';
import { LoadError } from '../../components/ui/load-error';
import { Skeleton } from '../../components/ui/skeleton';
import { useI18n } from '../../i18n';

export function HomeLoadState({ loading, error, onRetry }: { loading: boolean; error: boolean; onRetry: () => void }) {
  const { t } = useI18n();
  return <>
    {error ? <LoadError message={t('app.error.body')} retryLabel={t('app.error.retry')} onRetry={onRetry} /> : null}
    {loading ? <div role="status" aria-busy="true" className="space-y-3">
      <span className="sr-only">{t('items.loading')}</span>
      <Skeleton className="h-10 w-2/5" />
      <Skeleton className="h-16 w-full" />
      <Skeleton className="h-16 w-11/12" />
    </div> : null}
  </>;
}

export function HomeEmptyState() {
  const { t } = useI18n();
  return <EmptyState title={t('home.empty.title')}>
    {t('home.empty.body')} {t('home.empty.addHint')}
  </EmptyState>;
}
