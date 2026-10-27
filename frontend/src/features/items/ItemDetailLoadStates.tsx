import { Heading } from '../../app/Heading';
import { Button } from '../../components/ui/button';
import type { Translate } from '../../i18n';
import type { DetailLoadState } from './useItemDetail';

/** Loading, failed-load and gone states; the ready state renders the item itself. */
export function ItemDetailLoadStates({ state, t, onRetry }: { state: DetailLoadState; t: Translate; onRetry: () => void }) {
  if (state === 'loading') return <p role="status" className="mt-6 text-muted">{t('items.loading')}</p>;
  if (state === 'error') {
    return <div className="mt-6 space-y-3">
      <Heading className="font-display text-3xl leading-tight sm:text-4xl">{t('app.error.title')}</Heading>
      <p className="text-muted">{t('app.error.body')}</p>
      <Button type="button" onClick={onRetry}>{t('app.error.retry')}</Button>
    </div>;
  }
  if (state === 'gone') {
    return <div className="mt-6 space-y-3">
      <Heading className="font-display text-h1">{t('itemDetail.gone')}</Heading>
      <p className="text-muted">{t('itemDetail.goneBody')}</p>
    </div>;
  }
  return null;
}
