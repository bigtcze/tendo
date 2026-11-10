import { Heading } from '../../app/Heading';
import { Button } from '../../components/ui/button';
import type { Translate } from '../../i18n';
import type { DetailLoadState } from './useItemDetail';

/** Eyebrow + bold message + white surface, the same rhythm as the full-page secondary states. */
function StatePanel({ eyebrow, children, tone = 'neutral' }: { eyebrow?: string; children: React.ReactNode; tone?: 'neutral' | 'danger' }) {
  return <div className="mt-4 w-full rounded-2xl border border-line bg-raised p-6 sm:p-8">
    {eyebrow ? <p className={`text-[0.68rem] font-semibold uppercase tracking-[0.18em] ${tone === 'danger' ? 'text-danger' : 'text-muted'}`}>{eyebrow}</p> : null}
    {children}
  </div>;
}

/** Loading, failed-load and gone states; the ready state renders the item itself. */
export function ItemDetailLoadStates({ state, t, onRetry }: { state: DetailLoadState; t: Translate; onRetry: () => void }) {
  if (state === 'loading') return <p role="status" className="mt-6 text-muted">{t('items.loading')}</p>;
  if (state === 'error') {
    return <StatePanel eyebrow={t('app.error.eyebrow')} tone="danger">
      <Heading className="mt-2 text-h1 leading-tight text-ink">{t('app.error.title')}</Heading>
      <p className="mt-3 max-w-prose text-base text-muted">{t('app.error.body')}</p>
      <Button type="button" className="mt-5" onClick={onRetry}>{t('app.error.retry')}</Button>
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
