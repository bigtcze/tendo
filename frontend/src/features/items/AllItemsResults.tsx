import type { RefObject } from 'react';
import { NavLink } from '../../app/NavLink';
import { Button } from '../../components/ui/button';
import { EmptyState } from '../../components/ui/empty-state';
import { useI18n, type Translate } from '../../i18n';
import type { Member } from '../members/membersApi';
import { SubjectTypeIcon } from '../subjects/SubjectTypeIcon';
import type { SubjectType } from '../subjects/subjectsApi';
import { formatDate } from './formatDate';
import { ItemStateIcon, itemState, itemStateLabel, itemStateTone } from './itemStateTone';
import { repeatSummary } from './repeatSummary';
import type { Item } from './itemsApi';
import type { PageState } from './allItemsPageState';
import { countFor, countKey, cursorStack, hasFilters, type Context } from './allItemsUrl';
import type { AllItemsActions } from './useAllItems';

const focusRing = 'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent';
// Same compact rhythm as a Home row: one text line in layout, a 44 px pointer target through compensated padding.
// The focus outline is drawn inside the target box (-2px offset) so it never paints over the row divider, while
// staying a real outline that focus-indicator tooling can measure.
const titleLink = 'relative z-10 flex min-h-11 max-w-full items-center rounded-lg py-[11px] -my-[11px] '
  + '[overflow-wrap:anywhere] text-row-title font-semibold text-ink hover:text-accent '
  + 'focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus';
const addLink = `inline-flex min-h-11 items-center rounded-lg text-accent underline underline-offset-4 ${focusRing}`;
const subjectPill = 'inline-flex max-w-full items-center rounded-full bg-upcoming-bg px-2.5 py-0.5 text-meta font-semibold text-ink [overflow-wrap:anywhere]';

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

// Same wording as Home rows: only an open needs-attention item is "since" a date; every other state names a
// plain attention date, so the words always agree with the tile and icon.
function attentionKey(item: Item): 'items.needsSince' | 'items.rowAttentionDate' {
  const open = item.workflowState === 'open' && !item.done && !item.archived;
  return open && item.attention === 'needs_attention' ? 'items.needsSince' : 'items.rowAttentionDate';
}

function ItemRow({ item, subjectName, subjectType, members, onOpenItem }: {
  item: Item; subjectName: string | undefined; subjectType: SubjectType | undefined; members: Member[];
  onOpenItem: (itemId: string, returnTo: string) => void;
}) {
  const { t, locale } = useI18n();
  const responsible = members.find((member) => member.userId === item.responsibleUserId)?.login ?? t('items.responsible.unknown');
  const attention = item.attentionOn ? t(attentionKey(item), { date: formatDate(item.attentionOn, locale) }) : null;
  const state = itemState(item);
  const tone = itemStateTone[state];
  const status = attention && item.workflowState === 'open' && !item.done && !item.archived
    ? attention
    : [t(itemStateLabel(item)), attention].filter(Boolean).join(' · ');
  return <li data-item-id={item.id} className="grid grid-cols-[2.25rem_minmax(0,1fr)] items-center gap-x-3 px-4 pb-2 pt-[11px] xl:pb-0 xl:pt-2">
    <span aria-hidden="true" data-subject-icon={subjectType ?? 'custom'}
      className={`grid size-9 place-items-center rounded-xl ${tone.tile}`}>
      <SubjectTypeIcon type={subjectType ?? 'custom'} />
    </span>
    <div className="min-w-0">
      <NavLink href={`/items/${encodeURIComponent(item.id)}`} onNavigate={() => onOpenItem(item.id, `/items${window.location.search}`)}
        className={titleLink}>{item.title}</NavLink>
      <div data-item-meta className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-meta leading-snug">
        <span data-subject-pill className={subjectPill}>{subjectName ?? t('items.subject.unknown')}</span>
        <span data-item-state data-item-state-kind={state} className={`inline-flex items-center gap-1.5 font-semibold ${tone.text}`}>
          <ItemStateIcon state={state} className="size-4 shrink-0" />
          <span className="[overflow-wrap:anywhere]">{status}</span>
        </span>
        {item.responsibleUserId
          ? <span data-item-responsible className="text-muted [overflow-wrap:anywhere]">{t('items.responsible.read', { name: responsible })}</span>
          : null}
      </div>
      {item.recurrence
        ? <p data-item-recurrence className="mt-0.5 text-meta text-muted">{repeatSummary(t, item.recurrence, locale)}</p>
        : null}
    </div>
  </li>;
}

type PagerProps = { context: Context; nextCursor: string | null; goToCursor: AllItemsActions['goToCursor'] };

function Pager({ context, nextCursor, goToCursor }: PagerProps) {
  const { t } = useI18n();
  const previous = () => {
    const prior = cursorStack();
    const cursor = prior.pop();
    goToCursor(cursor || undefined, prior);
  };
  // '' marks the first page (no cursor) so Previous and the range line stay truthful from page 2 on.
  const next = () => goToCursor(nextCursor ?? undefined, [...cursorStack(), context.cursor ?? '']);
  const stack = cursorStack();
  return <nav aria-label={t('allItems.pages')} className="flex flex-wrap items-center justify-between gap-2">
    {context.cursor ? <Button data-all-items-page type="button" variant="quiet" onClick={previous}>
      {stack.length ? t('allItems.previous') : t('allItems.first')}
    </Button> : null}
    {nextCursor ? <Button data-all-items-page type="button" variant="quiet" onClick={next}>{t('allItems.next')}</Button> : null}
  </nav>;
}

export function AllItemsResults({ view, context, page, subjectNames, subjectTypes, members, actions, resultRef, onBack, onOpenItem }: {
  view: ResultsView; context: Context; page: PageState; subjectNames: Map<string, string>; subjectTypes: Map<string, SubjectType>;
  members: Member[]; actions: AllItemsActions;
  resultRef: RefObject<HTMLElement | null>; onBack: () => void; onOpenItem: (itemId: string, returnTo: string) => void;
}) {
  const { t, locale } = useI18n();
  if (view === 'none') return <EmptyState title={t('allItems.none')} action={
    <NavLink href="/" onNavigate={onBack} className={addLink}>{t('items.add')}</NavLink>
  } />;
  if (view === 'noMatch') return <EmptyState title={t('allItems.noMatch')} action={
    // Secondary, like Retry under a failed load: its edge lines up with the message above, where a quiet button's
    // padded label would float inset with no visible edge.
    (hasFilters(context) || context.status !== 'active') ? <Button data-all-items-clear type="button" variant="secondary"
      onClick={actions.clearFilters}>{t('allItems.clear')}</Button> : null
  } />;
  if (view !== 'rows') return null;
  const range = rangeLabel(context, page, t, locale);
  return <section ref={resultRef} tabIndex={-1} aria-label={t('allItems.results')} className="min-w-0 space-y-3 outline-none">
    <h2 className="text-row-title font-semibold text-ink">{range}</h2>
    <ul className="divide-y divide-line overflow-hidden rounded-2xl border border-line bg-raised">
      {page.items.map((item) =>
        <ItemRow key={item.id} item={item} subjectName={subjectNames.get(item.subjectId)} members={members} onOpenItem={onOpenItem}
          subjectType={subjectTypes.get(item.subjectId)} />)}
    </ul>
    <Pager context={context} nextCursor={page.nextCursor} goToCursor={actions.goToCursor} />
  </section>;
}
