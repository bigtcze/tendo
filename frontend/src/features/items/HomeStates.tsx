import { Button } from '../../components/ui/button';
import { EmptyState } from '../../components/ui/empty-state';
import { Notice } from '../../components/ui/notice';
import { Skeleton } from '../../components/ui/skeleton';
import { useI18n } from '../../i18n';

export function HomeLoadState({ loading, error, onRetry }: { loading: boolean; error: boolean; onRetry: () => void }) {
  const { t } = useI18n();
  return <>
    {error ? <Notice tone="error" className="flex flex-wrap items-center justify-between gap-3">
      <span>{t('app.error.body')}</span>
      <Button type="button" variant="secondary" onClick={onRetry}>{t('app.error.retry')}</Button>
    </Notice> : null}
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
    <p>{t('home.empty.body')} {t('home.empty.addHint')}</p>
  </EmptyState>;
}
