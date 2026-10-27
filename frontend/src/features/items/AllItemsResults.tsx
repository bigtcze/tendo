import type { RefObject } from 'react';
import { NavLink } from '../../app/NavLink';
import { Button } from '../../components/ui/button';
import { useI18n, type MessageKey, type Translate } from '../../i18n';
import type { Member } from '../members/membersApi';
import { formatDate } from './formatDate';
import { repeatSummary } from './repeatSummary';
import type { Item } from './itemsApi';
import type { PageState } from './allItemsPageState';
import { countFor, countKey, cursorStack, hasFilters, type Context } from './allItemsUrl';
import type { AllItemsActions } from './useAllItems';

const emptyCard = 'rounded-3xl border border-line bg-white/70 p-6 sm:p-8';
const focusRing = 'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent';
const titleLink = 'inline-flex min-h-11 max-w-full [overflow-wrap:anywhere] text-lg leading-snug underline decoration-line underline-offset-4 '
  + `hover:decoration-accent ${focusRing}`;
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

const stateLabels: Partial<Record<Item['workflowState'], MessageKey>> = {
  paused: 'items.group.paused', in_progress: 'items.group.inProgress', waiting: 'items.group.waiting',
};

function stateLabel(item: Item): MessageKey {
  if (item.archived) return 'allItems.status.archived';
  if (item.done) return 'itemDetail.state.done';
  return stateLabels[item.workflowState] ?? (item.attention === 'upcoming' ? 'items.group.upcoming' : 'items.group.needs');
}

// Same wording as Home rows: only an open item can be due on or overdue since its date.
function attentionKey(item: Item): MessageKey {
  if (item.archived || item.done || item.workflowState !== 'open') return 'items.rowAttentionDate';
  return item.attention === 'upcoming' ? 'items.attentionOn' : 'items.needsSince';
}

function ItemRow({ item, subjectName, members, onOpenItem }: {
  item: Item; subjectName: string | undefined; members: Member[]; onOpenItem: (itemId: string, returnTo: string) => void;
}) {
  const { t, locale } = useI18n();
  const responsible = members.find((member) => member.userId === item.responsibleUserId)?.login ?? t('items.responsible.unknown');
  return <li data-item-id={item.id} className="p-4">
    <NavLink href={`/items/${encodeURIComponent(item.id)}`} onNavigate={() => onOpenItem(item.id, `/items${window.location.search}`)}
      className={titleLink}>{item.title}</NavLink>
    <p className="mt-1 text-sm text-muted">{subjectName ?? t('items.subject.unknown')}</p>
    <p className="text-sm font-medium text-accent-strong">{t(stateLabel(item))}</p>
    {item.attentionOn ? <p className="text-sm text-muted">{t(attentionKey(item), { date: formatDate(item.attentionOn, locale) })}</p> : null}
    {item.recurrence ? <p className="text-sm text-muted">{repeatSummary(t, item.recurrence, locale)}</p> : null}
    {item.responsibleUserId ? <p className="text-sm text-muted">{t('items.responsible.read', { name: responsible })}</p> : null}
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
    <ul className="mt-3 divide-y divide-line border-y border-line bg-white/70">
      {page.items.map((item) =>
        <ItemRow key={item.id} item={item} subjectName={subjectNames.get(item.subjectId)} members={members} onOpenItem={onOpenItem} />)}
    </ul>
    <Pager context={context} nextCursor={page.nextCursor} goToCursor={actions.goToCursor} />
  </section>;
}
