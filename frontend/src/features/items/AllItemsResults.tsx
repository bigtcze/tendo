import type { RefObject } from 'react';
import { NavLink } from '../../app/NavLink';
import { Button } from '../../components/ui/button';
import { useI18n, type MessageKey, type Translate } from '../../i18n';
import type { Member } from '../members/membersApi';
import { repeatSummary } from './repeatSummary';
import { formatDate } from './formatDate';
import type { Item } from './itemsApi';
import type { PageState } from './allItemsPageState';
import { countFor, countKey, cursorStack, hasFilters, type Context } from './allItemsUrl';
import type { AllItemsActions } from './useAllItems';

const emptyCard = 'rounded-3xl border border-line bg-white/70 p-6 sm:p-8';
const focusRing = 'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent';
const addLink = `mt-3 inline-flex min-h-11 items-center rounded-lg text-accent underline underline-offset-4 ${focusRing}`;

export type ResultsView = 'hidden' | 'none' | 'noMatch' | 'rows';

/** Which settled result to show; nothing while loading, after an error, or while rows belong to another context. */
export function resultsView(context: Context, page: PageState, query: string): ResultsView {
  if (page.loading || page.error || page.loadedQuery !== query || !page.summary) return 'hidden';
  if (page.items.length) return 'rows';
  return page.summary.total === 0 && !hasFilters(context) ? 'none' : 'noMatch';
}

/** "Items 26–50 of 80" when the page position is known; after a reload on a later page the earlier cursors are gone, so only the count. */
export function rangeLabel(context: Context, page: PageState, t: Translate, locale: string): string {
  const stack = cursorStack();
  const count = page.summary ? countFor(context.status, page.summary) : 0;
  if (context.cursor && stack[0] !== '') return t(countKey(count, locale), { count: String(count) });
  const offset = context.cursor ? stack.length * 25 : 0;
  return t('allItems.range', { from: String(offset + 1), to: String(offset + page.items.length), total: String(count) });
}

/** WCAG 4.1.3: one persistent polite region reports each settled outcome while focus stays on the control. */
export function outcomeText(view: ResultsView, context: Context, page: PageState, t: Translate, locale: string): string {
  if (page.error) return t('app.error.body');
  if (view === 'none') return t('allItems.none');
  if (view === 'noMatch') return t('allItems.noMatch');
  return view === 'rows' ? rangeLabel(context, page, t, locale) : '';
}

function relativeDay(value: string, locale: string) {
  const date = new Date(`${value}T00:00:00Z`);
  const today = new Date();
  const delta = Math.round((date.getTime() - Date.UTC(today.getUTCFullYear(), today.getUTCMonth(), today.getUTCDate())) / 86_400_000);
  if (!delta) return locale === 'cs' ? 'dnes' : 'today';
  if (delta > 0) return locale === 'cs' ? `za ${delta} dní` : `in ${delta} days`;
  return locale === 'cs' ? `od ${date.getUTCDate()}. ${new Intl.DateTimeFormat('cs-CZ', { month: 'short', timeZone: 'UTC' }).format(date)}` : `since ${date.getUTCDate()} ${new Intl.DateTimeFormat('en-GB', { month: 'short', timeZone: 'UTC' }).format(date)}`;
}

function allItemsAttentionDescription(item: Item, date: string | null, locale: string, t: Translate) {
  if (!date) return undefined;
  const key: MessageKey = item.done || item.archived || item.workflowState !== 'open'
    ? 'items.rowAttentionDate'
    : item.attention === 'upcoming' ? 'items.attentionOn' : 'items.needsSince';
  const lastDone = item.lastCompletedOn ? ` · ${t('itemDetail.lastDone', { date: formatDate(item.lastCompletedOn, locale) })}` : '';
  return `${t(key, { date })}${lastDone}`;
}

function ItemRow({ item, subjectName, members, onOpenItem }: {
  item: Item; subjectName: string | undefined; members: Member[]; onOpenItem: (itemId: string, returnTo: string) => void;
}) {
  const { t, locale } = useI18n();
  const responsible = members.find((member) => member.userId === item.responsibleUserId)?.login ?? t('items.responsible.unknown');
  const attentionDate = item.attentionOn ? formatDate(item.attentionOn, locale) : null;
  const attentionDescription = allItemsAttentionDescription(item, attentionDate, locale, t);
  const attentionText = attentionDate ? relativeDay(item.attentionOn!, locale) : null;
  const recurrenceText = item.recurrence ? repeatSummary(t, item.recurrence, locale) : null;
  const visibleSecondary = [subjectName ?? t('items.subject.unknown'), attentionText, item.responsibleUserId ? t('items.responsible.read', { name: responsible }) : null].filter(Boolean).join(' · ');
  return <li data-item-id={item.id} className="item-row flex items-center gap-3 border-b border-line px-3 py-2">
    <div className="row-copy min-w-0 flex-1">
      <NavLink href={`/items/${encodeURIComponent(item.id)}`} onNavigate={() => onOpenItem(item.id, `/items${window.location.search}`)}
        className="row-title inline-flex min-h-11 max-w-full text-left text-lg font-medium leading-snug focus-visible:outline-2 focus-visible:outline-accent">{item.title}</NavLink>
      <p className="row-secondary text-base text-muted" aria-label={attentionDescription}>
        <span>{visibleSecondary}</span>
        {recurrenceText ? <span className="sr-only"> {recurrenceText}</span> : null}
        {attentionDescription ? <span className="sr-only"> {attentionDescription}</span> : null}
      </p>
    </div>
    {item.recurrence ? <span className="repeat-mark" aria-hidden="true"><svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round"><path d="M20 7v5h-5M4 17v-5h5M5.6 9a7 7 0 0 1 11.7-2L20 12M4 12l2.7 5a7 7 0 0 0 11.7-2"/></svg></span> : null}
  </li>;
}

function Pager({ context, nextCursor, goToCursor }: { context: Context; nextCursor: string | null; goToCursor: AllItemsActions['goToCursor'] }) {
  const { t } = useI18n();
  const previous = () => {
    const prior = cursorStack();
    const cursor = prior.pop();
    goToCursor(cursor || undefined, prior);
  };
  // '' marks the first page (no cursor) so Previous and the range line stay truthful from page 2 on.
  const next = () => goToCursor(nextCursor ?? undefined, [...cursorStack(), context.cursor ?? '']);
  return <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
    {context.cursor
      ? <Button data-all-items-page type="button" variant="quiet" onClick={previous}>
        {cursorStack().length ? t('allItems.previous') : t('allItems.first')}
      </Button>
      : null}
    {nextCursor ? <Button data-all-items-page type="button" variant="quiet" onClick={next}>{t('allItems.next')}</Button> : null}
  </div>;
}

export function AllItemsResults({ view, context, page, subjectNames, members, actions, resultRef, onBack, onOpenItem }: {
  view: ResultsView; context: Context; page: PageState; subjectNames: Map<string, string>; members: Member[]; actions: AllItemsActions;
  resultRef: RefObject<HTMLElement | null>; onBack: () => void; onOpenItem: (itemId: string, returnTo: string) => void;
}) {
  const { t, locale } = useI18n();
  if (view === 'none') return <div className={emptyCard}><p className="font-display text-xl">{t('allItems.none')}</p>
    <NavLink href="/" onNavigate={onBack}
      className={addLink}>{t('items.add')}</NavLink></div>;
  if (view === 'noMatch') return <div className={emptyCard}><p className="font-display text-xl">{t('allItems.noMatch')}</p>
    <Button type="button" variant="quiet" className="mt-2 -ml-3" onClick={actions.clearFilters}>{t('allItems.clear')}</Button></div>;
  if (view !== 'rows') return null;
  return <section ref={resultRef} tabIndex={-1} aria-label={t('allItems.results')} className="outline-none">
    <h2 className="font-display text-xl leading-snug">{rangeLabel(context, page, t, locale)}</h2>
    <ul className="all-items-list mt-3 divide-y divide-line border-y border-line bg-white/70">
      {page.items.map((item) =>
        <ItemRow key={item.id} item={item} subjectName={subjectNames.get(item.subjectId)} members={members} onOpenItem={onOpenItem} />)}
    </ul>
    <Pager context={context} nextCursor={page.nextCursor} goToCursor={actions.goToCursor} />
  </section>;
}
