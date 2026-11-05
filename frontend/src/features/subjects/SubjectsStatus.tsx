import { Button } from '../../components/ui/button';
import { Notice } from '../../components/ui/notice';
import type { ListState } from './subjectsModel';
import type { MessageKey } from '../../i18n';

export function SubjectsStatus({ list, t, onRetry }: { list: ListState; t: (key: MessageKey) => string; onRetry: () => void }) {
  // WCAG 4.1.3: a persistent polite host announces a failed list load; the visible Notice stays presentational.
  // It has no status/alert role so it never competes with the screen's mutation notice or action alert.
  return <>
    <p aria-live="polite" aria-atomic="true" className="sr-only">
      {list.kind === 'error' ? `${t('app.error.title')} ${t('app.error.body')}` : ''}
    </p>
    {list.kind === 'loading' ? <p role="status" className="mt-8 text-muted">{t('subjects.loading')}</p> : null}
    {list.kind === 'error' ? <Notice tone="error" role={null} className="mt-8 space-y-3">
      <p className="text-lg">{t('app.error.title')}</p>
      <p>{t('app.error.body')}</p>
      <Button type="button" onClick={onRetry}>{t('app.error.retry')}</Button>
    </Notice> : null}
    {list.kind === 'noHousehold' ? <div className="mt-8 space-y-2">
      <h2 className="font-display text-2xl leading-snug">{t('home.noHousehold.title')}</h2>
      <p className="text-muted">{t('home.noHousehold.body')}</p>
    </div> : null}
  </>;
}
