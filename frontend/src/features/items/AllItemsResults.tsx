import type { ReactNode, RefObject } from 'react';
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

// Same compact rhythm as a Home row: one text line in layout, a 44 px pointer target through compensated padding.
// The focus outline is drawn inside the target box (-2px offset) so it never paints over the row divider, while
// staying a real outline that focus-indicator tooling can measure.
const titleLink = 'relative z-10 flex min-h-11 max-w-full items-center rounded-lg py-[11px] -my-[11px] '
  + '[overflow-wrap:anywhere] text-row-title font-semibold text-ink hover:text-accent '
  + 'focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus';
// The subject reads as on Home: its type icon beside its name, no filled pill or separate tile column.
const subjectClass = 'inline-flex min-w-0 max-w-full items-center gap-1 font-medium text-ink [overflow-wrap:anywhere]';

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
// plain attention date, so the words always agree with the state icon.
function attentionKey(item: Item): 'items.needsSince' | 'items.rowAttentionDate' {
  const open = item.workflowState === 'open' && !item.done && !item.archived;
  return open && item.attention === 'needs_attention' ? 'items.needsSince' : 'items.rowAttentionDate';
}

/** Home's wording: the viewer reads "you", another member their login, a departed account "Unknown member". */
function responsibleText(item: Item, members: Member[], userId: string, t: Translate): string | null {
  if (!item.responsibleUserId) return null;
  if (item.responsibleUserId === userId) return t('items.responsible.you');
  const login = members.find((member) => member.userId === item.responsibleUserId)?.login ?? t('items.responsible.unknown');
  return t('items.responsible.read', { name: login });
}

/**
 * One fact with Home's decorative middle dot in a fixed 1rem box before it. Dot and fact wrap as one unit, and the
 * metadata line's -1rem shift into a clipped parent hides whichever dot starts a line.
 */
function Parted({ children }: { children: ReactNode }) {
  return <span data-item-meta-unit className="inline-flex min-w-0 max-w-full items-center">
    <span aria-hidden="true" data-item-separator className="w-4 shrink-0 select-none text-center text-muted">·</span>
    {children}
  </span>;
}

function ItemRow({ item, subjectName, subjectType, members, userId, onOpenItem }: {
  item: Item; subjectName: string | undefined; subjectType: SubjectType | undefined; members: Member[]; userId: string;
  onOpenItem: (itemId: string, returnTo: string) => void;
}) {
  const { t, locale } = useI18n();
  const responsible = responsibleText(item, members, userId, t);
  const attention = item.attentionOn ? t(attentionKey(item), { date: formatDate(item.attentionOn, locale) }) : null;
  const state = itemState(item);
  const tone = itemStateTone[state];
  const status = attention && item.workflowState === 'open' && !item.done && !item.archived
    ? attention
    : [t(itemStateLabel(item)), attention].filter(Boolean).join(' · ');
  const kind = subjectType ?? 'custom';
  return <li data-item-id={item.id} className="px-4 pb-2 pt-[11px] xl:pb-0 xl:pt-2">
    <NavLink href={`/items/${encodeURIComponent(item.id)}`} onNavigate={() => onOpenItem(item.id, `/items${window.location.search}`)}
      className={titleLink}>{item.title}</NavLink>
    {/* Every fact carries its own leading dot; the row shifts left by one dot box and clips it, so the dot that
        would start any line (the first fact's, or one that wrapped) is hidden and none is left dangling. */}
    <div className="overflow-hidden">
      <div data-item-meta className="-ml-4 mt-0.5 flex flex-wrap items-center gap-y-1 text-meta leading-snug">
        <Parted>
          <span data-subject-pill className={subjectClass}>
            <span aria-hidden="true" data-subject-icon={kind} className="inline-flex shrink-0 text-muted">
              <SubjectTypeIcon type={kind} className="size-3.5" />
            </span>
            {subjectName ?? t('items.subject.unknown')}
          </span>
        </Parted>
        <Parted>
          <span data-item-state data-item-state-kind={state} className={`inline-flex min-w-0 items-center gap-1.5 font-semibold ${tone.text}`}>
            <ItemStateIcon state={state} className="size-4 shrink-0" />
            <span className="[overflow-wrap:anywhere]">{status}</span>
          </span>
        </Parted>
        {responsible
          ? <Parted><span data-item-responsible className="min-w-0 text-muted [overflow-wrap:anywhere]">{responsible}</span></Parted>
          : null}
        {item.recurrence
          ? <Parted><span data-item-recurrence className="min-w-0 text-muted [overflow-wrap:anywhere]">{repeatSummary(t, item.recurrence, locale)}</span></Parted>
          : null}
      </div>
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

export function AllItemsResults({ view, context, page, subjectNames, subjectTypes, members, userId, actions, resultRef, onOpenItem }: {
  view: ResultsView; context: Context; page: PageState; subjectNames: Map<string, string>; subjectTypes: Map<string, SubjectType>;
  members: Member[]; userId: string; actions: AllItemsActions;
  resultRef: RefObject<HTMLElement | null>; onOpenItem: (itemId: string, returnTo: string) => void;
}) {
  const { t, locale } = useI18n();
  // Add item sits on the title line above; the empty list points at it, like the empty Home.
  if (view === 'none') return <EmptyState title={t('allItems.none')}>{t('home.empty.addHint')}</EmptyState>;
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
        <ItemRow key={item.id} item={item} subjectName={subjectNames.get(item.subjectId)} members={members} userId={userId}
          onOpenItem={onOpenItem}
          subjectType={subjectTypes.get(item.subjectId)} />)}
    </ul>
    <Pager context={context} nextCursor={page.nextCursor} goToCursor={actions.goToCursor} />
  </section>;
}
