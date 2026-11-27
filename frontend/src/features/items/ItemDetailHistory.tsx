import type { Ref } from 'react';
import { Button } from '../../components/ui/button';
import type { Translate } from '../../i18n';
import type { Completion } from './itemsApi';
import { formatDate } from './formatDate';
import type { ItemDetailHistoryState } from './useItemDetailHistory';
import { LoadingLine } from '../../components/ui/loading-line';

function HistoryRow({ completion, t, locale }: { completion: Completion; t: Translate; locale: string }) {
  const doneKey = completion.undoneAt ? 'itemDetail.historyUndone' : 'itemDetail.historyDone';
  return <li className={`py-3 text-small ${completion.undoneAt ? 'text-muted' : 'text-ink'}`}>
    <p>{t(doneKey, { date: formatDate(completion.completedOn, locale) })}</p>
    {completion.cycleAttentionOn
      ? <p className="mt-1 text-muted">{t('itemDetail.historyPlanned', { date: formatDate(completion.cycleAttentionOn, locale) })}</p> : null}
    {completion.nextAttentionOn
      ? <p className="text-muted">{t('itemDetail.historyNext', { date: formatDate(completion.nextAttentionOn, locale) })}</p> : null}
  </li>;
}

/**
 * Paged history controls (UX "History": Previous and Next with an accessible current page). The list is newest
 * first, so Next page goes to earlier completions. Previous page and Next page are always shown and go disabled at
 * the first and last page, so each boundary stays visible; from page 3 on, First page returns to the newest page in
 * one step (on page 2 Previous page already does that). The current page is stated last, below the controls.
 *
 * The navigation stays mounted while a page loads, including the load back to a lone first page from the last
 * one: moving the cursor stack happens on click, before the request,
 * so the page number changes inside an existing live region and assistive tech announces it. The buttons only go
 * disabled for that load, which also keeps a second page change out of the in-flight one.
 * A failed page keeps just the error and its Retry action, so recovery stays the only thing to do next.
 */
function HistoryPaging({ history, t }: { history: ItemDetailHistoryState; t: Translate }) {
  if (history.state === 'error') return null;
  const page = history.cursors.length;
  if (!history.nextCursor && page === 1 && !history.paging) return null;
  const loading = history.state === 'loading';
  // Same layout as the other list pagers (CursorPager): backward controls left, Next page right, and the current page
  // on its own line below. When narrow widths or long Czech labels wrap Next page onto its own row, ml-auto keeps it
  // on the right, so the forward direction never reads as one more backward control.
  return <nav aria-label={t('itemDetail.historyPages')} className="mt-3 flex flex-wrap items-center justify-between gap-2">
    <div className="flex flex-wrap gap-2">
      {page > 2 ? <Button type="button" variant="quiet" disabled={loading} onClick={history.first}>{t('itemDetail.historyFirstPage')}</Button> : null}
      <Button type="button" variant="quiet" disabled={loading || page === 1} onClick={history.previous}>{t('itemDetail.historyPreviousPage')}</Button>
    </div>
    <Button type="button" variant="quiet" className="ml-auto" disabled={loading || !history.nextCursor} onClick={history.next}>{t('itemDetail.historyNextPage')}</Button>
    <p role="status" className="w-full text-center text-small text-muted">
      {t('itemDetail.historyPage', { page: String(page) })}
    </p>
  </nav>;
}

/** The list area of the history: the page rows, or what replaced them. The paging navigation is its sibling. */
function HistoryList({ history, t, locale }: { history: ItemDetailHistoryState; t: Translate; locale: string }) {
  if (history.state === 'loading') return <LoadingLine className="mt-3">{t('itemDetail.historyLoading')}</LoadingLine>;
  if (history.state === 'error') {
    return <div className="mt-3 space-y-2">
      <p className="text-small text-muted">{t('itemDetail.historyError')}</p>
      <Button type="button" variant="quiet" onClick={history.retry}>{t('app.error.retry')}</Button>
    </div>;
  }
  return history.history.length
    ? <ol className="mt-3 divide-y divide-line border-t border-line">
      {history.history.map((completion) => <HistoryRow key={completion.id} completion={completion} t={t} locale={locale} />)}
    </ol>
    : <p className="mt-3 text-small text-muted">{t('itemDetail.historyEmpty')}</p>;
}

function HistoryBody({ history, t, locale }: { history: ItemDetailHistoryState; t: Translate; locale: string }) {
  return <>
    <HistoryList history={history} t={t} locale={locale} />
    <HistoryPaging history={history} t={t} />
  </>;
}

export function ItemDetailHistory({ history, sectionRef, headingRef, t, locale }: {
  history: ItemDetailHistoryState; sectionRef: Ref<HTMLElement>; headingRef: Ref<HTMLHeadingElement>; t: Translate; locale: string;
}) {
  return <section ref={sectionRef} className="mt-8 border-t border-line pt-6">
    <h2 ref={headingRef} tabIndex={-1} className="text-h2 font-bold text-ink outline-none">{t('itemDetail.history')}</h2>
    <HistoryBody history={history} t={t} locale={locale} />
  </section>;
}
