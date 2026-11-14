import type { ReactNode } from 'react';
import { Heading } from '../../app/Heading';
import { Button } from '../../components/ui/button';
import type { Translate } from '../../i18n';
import type { DetailLoadState } from './useItemDetail';

/** White surface for the loading, failed-load and gone states, the same rhythm as the full-page secondary states. */
function StatePanel({ children }: { children: ReactNode }) {
  return <div className="mt-4 w-full rounded-2xl border border-line bg-raised p-6 sm:p-8">{children}</div>;
}

/** Loading, failed-load and gone states; the ready state renders the item itself. */
export function ItemDetailLoadStates({ state, t, onRetry }: { state: DetailLoadState; t: Translate; onRetry: () => void }) {
  if (state === 'loading') {
    return <StatePanel>
      <p role="status" className="text-lg text-ink">{t('items.loading')}</p>
    </StatePanel>;
  }
  if (state === 'error') {
    return <StatePanel>
      <Heading className="text-h1 font-semibold text-ink">{t('app.error.title')}</Heading>
      <p className="mt-3 max-w-prose text-base text-muted">{t('app.error.body')}</p>
      <Button type="button" className="mt-6" onClick={onRetry}>{t('app.error.retry')}</Button>
    </StatePanel>;
  }
  if (state === 'gone') {
    return <StatePanel>
      <Heading className="text-h1 leading-tight text-ink">{t('itemDetail.gone')}</Heading>
      <p className="mt-3 max-w-prose text-base text-muted">{t('itemDetail.goneBody')}</p>
    </StatePanel>;
  }
  return null;
}
