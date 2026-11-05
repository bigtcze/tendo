import { Button } from '../../components/ui/button';
import { Notice } from '../../components/ui/notice';
import type { ListState } from './subjectsModel';
import type { MessageKey } from '../../i18n';

export function SubjectsStatus({ list, t, onRetry }: { list: ListState; t: (key: MessageKey) => string; onRetry: () => void }) {
  return <>
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
