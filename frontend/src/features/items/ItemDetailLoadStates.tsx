import { Heading } from '../../app/Heading';
import { NavLink } from '../../app/NavLink';
import { Button } from '../../components/ui/button';
import { LoadingLine } from '../../components/ui/loading-line';
import type { Translate } from '../../i18n';
import { DETAIL_BACK_LINK_CLASS, DETAIL_TITLE_CLASS } from './itemDetailStyles';
import type { DetailLoadState } from './useItemDetail';

/**
 * Loading, failed-load and gone states, laid out as the same page the ready item is: the shared page title,
 * one quiet paragraph and, when the screen was not reached from All items, a way back to the overview.
 */
export function ItemDetailLoadStates({ state, t, onRetry, onOverview }: {
  state: DetailLoadState; t: Translate; onRetry: () => void; onOverview: (() => void) | null;
}) {
  if (state === 'loading') {
    return <LoadingLine className="mt-4">{t('items.loading')}</LoadingLine>;
  }
  if (state === 'error') {
    return <div className="mt-4">
      <Heading className={DETAIL_TITLE_CLASS}>{t('app.error.title')}</Heading>
      <p className="mt-3 max-w-prose text-base text-muted">{t('app.error.body')}</p>
      <Button type="button" className="mt-6" onClick={onRetry}>{t('app.error.retry')}</Button>
    </div>;
  }
  if (state === 'gone') {
    return <div className="mt-4">
      <Heading className={DETAIL_TITLE_CLASS}>{t('itemDetail.gone')}</Heading>
      <p className="mt-3 max-w-prose text-base text-muted">{t('itemDetail.goneBody')}</p>
      {onOverview
        ? <NavLink href="/" onNavigate={onOverview} className={`${DETAIL_BACK_LINK_CLASS} mt-4`}>{t('itemDetail.backOverview')}</NavLink>
        : null}
    </div>;
  }
  return null;
}
