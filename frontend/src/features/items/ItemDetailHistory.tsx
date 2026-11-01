import type { Ref } from 'react';
import { Button } from '../../components/ui/button';
import type { Translate } from '../../i18n';
import type { Completion } from './itemsApi';
import { formatDate } from './formatDate';
import type { ItemDetailHistoryState } from './useItemDetailHistory';

function HistoryRow({ completion, t, locale }: { completion: Completion; t: Translate; locale: string }) {
  const doneKey = completion.undoneAt ? 'itemDetail.historyUndone' : 'itemDetail.historyDone';
  return <li className={`rounded-xl bg-white/50 px-4 py-3 text-sm ${completion.undoneAt ? 'text-muted' : ''}`}>
    <p>{t(doneKey, { date: formatDate(completion.completedOn, locale) })}</p>
    {completion.cycleAttentionOn
      ? <p className="mt-1 text-muted">{t('itemDetail.historyPlanned', { date: formatDate(completion.cycleAttentionOn, locale) })}</p> : null}
    {completion.nextAttentionOn
      ? <p className="text-muted">{t('itemDetail.historyNext', { date: formatDate(completion.nextAttentionOn, locale) })}</p> : null}
  </li>;
}

function HistoryPaging({ history, t }: { history: ItemDetailHistoryState; t: Translate }) {
  const hasNewer = history.cursors.length > 1;
  if (!history.nextCursor && !hasNewer) return null;
  return <div className="mt-4 flex flex-wrap gap-2">
    {hasNewer ? <Button type="button" variant="quiet" onClick={history.newer}>{t('itemDetail.historyNewer')}</Button> : null}
    {history.nextCursor ? <Button type="button" variant="quiet" onClick={history.older}>{t('itemDetail.historyOlder')}</Button> : null}
    {hasNewer ? <Button type="button" variant="quiet" onClick={history.latest}>{t('itemDetail.historyLatest')}</Button> : null}
  </div>;
}

function HistoryBody({ history, t, locale }: { history: ItemDetailHistoryState; t: Translate; locale: string }) {
  if (history.state === 'loading') return <p role="status" className="mt-3 text-sm text-muted">{t('itemDetail.historyLoading')}</p>;
  if (history.state === 'error') {
    return <div className="mt-3 space-y-2">
      <p className="text-sm text-muted">{t('itemDetail.historyError')}</p>
      <Button type="button" variant="quiet" onClick={history.retry}>{t('app.error.retry')}</Button>
    </div>;
  }
  return <>
    {history.history.length
      ? <ol className="mt-3 space-y-2">
        {history.history.map((completion) => <HistoryRow key={completion.id} completion={completion} t={t} locale={locale} />)}
      </ol>
      : <p className="mt-3 text-sm text-muted">{t('itemDetail.historyEmpty')}</p>}
    <HistoryPaging history={history} t={t} />
  </>;
}

export function ItemDetailHistory({ history, sectionRef, headingRef, t, locale }: {
  history: ItemDetailHistoryState; sectionRef: Ref<HTMLElement>; headingRef: Ref<HTMLHeadingElement>; t: Translate; locale: string;
}) {
  return <section ref={sectionRef} className="item-history mt-8 border-t border-line pt-6">
    <h2 ref={headingRef} tabIndex={-1} className="font-display text-2xl outline-none">{t('itemDetail.history')}</h2>
    <HistoryBody history={history} t={t} locale={locale} />
  </section>;
}
