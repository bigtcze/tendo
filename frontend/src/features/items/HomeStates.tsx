import { Button } from '../../components/ui/button';
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
      <Skeleton className="h-4 w-28" />
      <Skeleton className="h-12 w-full" />
      <Skeleton className="h-12 w-full" />
      <Skeleton className="h-12 w-11/12" />
    </div> : null}
  </>;
}

/** The empty home keeps the same left-aligned reading column as the list, without a decorative card. */
export function HomeEmptyState() {
  const { t } = useI18n();
  return <div data-home-empty className="border-t border-line pt-6">
    <p className="text-body font-medium text-ink">{t('home.empty.title')}</p>
    <p className="mt-1 max-w-prose text-body text-muted">{t('home.empty.body')} {t('home.empty.addHint')}</p>
  </div>;
}
