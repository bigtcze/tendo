import { LoadError } from '../../components/ui/load-error';
import { LoadingLine } from '../../components/ui/loading-line';
import type { ListState } from './subjectsModel';
import type { MessageKey } from '../../i18n';

export function SubjectsStatus({ list, t, onRetry }: { list: ListState; t: (key: MessageKey) => string; onRetry: () => void }) {
  // WCAG 4.1.3: a persistent polite host announces a failed list load; the visible Notice stays presentational.
  // It has no status/alert role so it never competes with the screen's mutation notice or action alert.
  return <>
    <p aria-live="polite" aria-atomic="true" className="sr-only">
      {list.kind === 'error' ? `${t('app.error.title')} ${t('app.error.body')}` : ''}
    </p>
    {list.kind === 'loading' ? <LoadingLine className="mt-6">{t('subjects.loading')}</LoadingLine> : null}
    {list.kind === 'error' ? <LoadError role={null} className="mt-6" message={t('app.error.body')}
      retryLabel={t('app.error.retry')} onRetry={onRetry} /> : null}
    {list.kind === 'noHousehold' ? <div className="mt-6 w-full">
      <h2 className="text-h2 text-ink">{t('home.noHousehold.title')}</h2>
      <p className="mt-1 max-w-prose text-body text-muted">{t('home.noHousehold.body')}</p>
    </div> : null}
  </>;
}
