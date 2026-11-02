import { Button } from '../../components/ui/button';
import { NavLink } from '../../app/NavLink';
import { useI18n } from '../../i18n';
import type { Member } from '../members/membersApi';
import { repeatSummary } from './repeatSummary';
import type { Item, ItemGroup } from './itemsApi';
import { formatAttentionDate, formatDate } from './formatDate';
import { HOME_GROUP_ROW_CAP, groups, type GroupData } from './homeGroups';

const ROW_TITLE_CLASS = [
  'inline-flex min-h-11 w-fit max-w-full items-center py-1 text-left text-row-title font-medium leading-snug text-ink',
  '[overflow-wrap:anywhere] hover:text-accent focus-visible:rounded-sm focus-visible:outline-2',
  'focus-visible:outline-offset-2 focus-visible:outline-accent',
].join(' ');
const ROW_CLASS = 'flex items-start gap-3 px-4 py-3';
const ROW_CONTENT_CLASS = 'min-w-0 flex-1';
const DONE_CLASS = 'mt-1 shrink-0';
const GROUP_HEADING_CLASS = [
  'flex items-baseline justify-between border-b border-line pb-2 text-h2 font-medium text-ink',
  'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
].join(' ');
const GROUP_LIST_CLASS = 'mt-3 divide-y divide-line rounded-2xl border border-line bg-raised';

export function ItemRow({ item, group, evaluatedOn, subjectName, locale, userId, members, membersLoaded, busy, onDone, onOpenItem, rowRef }: {
  item: Item;
  evaluatedOn: string;
  group: ItemGroup;
  subjectName: string;
  locale: string;
  userId: string;
  members: Member[];
  membersLoaded: boolean;
  busy: boolean;
  onDone: () => void;
  onOpenItem: (itemId: string) => void;
  rowRef?: (node: HTMLLIElement | null) => void;
}) {
  const { t } = useI18n();
  const responsible = item.responsibleUserId ? members.find((member) => member.userId === item.responsibleUserId) : undefined;
  const attentionLine = item.attentionOn
    ? t(
        group === 'upcoming'
          ? 'items.attentionOn'
          : group === 'needs'
            ? 'items.needsSince'
            : 'items.rowAttentionDate',
        { date: formatAttentionDate(item.attentionOn, evaluatedOn, locale) },
      )
    : null;
  const responsibleLine = item.responsibleUserId && membersLoaded
    ? responsible
      ? t(responsible.userId === userId ? 'items.responsible.you' : 'items.responsible.read', { name: responsible.login })
      : t('items.responsible.unknown')
    : null;
  const scanLine = [subjectName, attentionLine, responsibleLine].filter(Boolean).join(' · ');
  const additionalDetails = [
    item.lastCompletedOn ? t('itemDetail.lastDone', { date: formatDate(item.lastCompletedOn, locale) }) : null,
    item.recurrence ? repeatSummary(t, item.recurrence, locale) : null,
  ].filter(Boolean).join(' · ');
  return (
    <li ref={rowRef} data-item-id={item.id} className={ROW_CLASS}>
      <div className={ROW_CONTENT_CLASS}>
        <NavLink
          href={`/items/${encodeURIComponent(item.id)}`}
          onNavigate={() => onOpenItem(item.id)}
          className={ROW_TITLE_CLASS}
        >
          <span aria-hidden='true'>{item.title}</span>
          <span className='sr-only'>{t('itemDetail.open', { title: item.title })}</span>
        </NavLink>
        <p className='mt-2 break-words text-meta text-muted'>
          {scanLine}
        </p>
        {additionalDetails ? <p className='mt-1 break-words text-meta text-muted'>{additionalDetails}</p> : null}
      </div>
      <Button
        className={DONE_CLASS}
        type='button'
        variant='secondary'
        data-action='done'
        aria-label={t('items.done.label', { title: item.title })}
        disabled={busy}
        onClick={onDone}
      >
        {t('items.done')}
      </Button>
    </li>
  );
}

export function HomeGroupSection({ group, summary, state, countLabel, children, headingRef, moreRef, onShowMore, onOpenAll }: {
  group: (typeof groups)[number];
  summary: number;
  state: GroupData;
  countLabel: string;
  children: React.ReactNode;
  headingRef: (node: HTMLHeadingElement | null) => void;
  moreRef: (node: HTMLButtonElement | null) => void;
  onShowMore: () => void;
  onOpenAll: () => void;
}) {
  const { t } = useI18n();
  if (!state.items.length) return null;
  return (
    <section id={`group-${group.id}`} className={group.quiet ? 'border-t border-line pt-5' : ''}>
      <h2 ref={headingRef} data-group-heading={group.id} tabIndex={-1} className={GROUP_HEADING_CLASS}>
        <span>{t(group.key)}</span>{' '}<span className='text-meta font-normal text-muted'>{countLabel}</span>
      </h2>
      <ul className={GROUP_LIST_CLASS}>{children}</ul>
      {state.cursor && state.items.length < HOME_GROUP_ROW_CAP ? (
        <Button ref={moreRef} type='button' data-group-more={group.id} aria-disabled={state.loadingMore} variant='quiet' onClick={onShowMore}>
          {state.loadingMore
            ? t('items.moreLoading')
            : t('items.showMore', {
                count: String(Math.max(0, summary - state.items.length)),
              })}
        </Button>
      ) : null}
      {state.items.length >= HOME_GROUP_ROW_CAP && (state.cursor || state.overflow || summary > state.items.length) ? (
        <NavLink
          href={`/items?status=${group.id}`}
          data-group-all={group.id}
          onNavigate={onOpenAll}
          className={[
            'inline-flex min-h-11 items-center underline decoration-line underline-offset-4 hover:decoration-accent',
            'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
          ].join(' ')}
        >
          {summary > state.items.length ? t('items.group.seeAll', { count: String(summary) }) : t('items.group.seeAllNoCount')}
        </NavLink>
      ) : null}
    </section>
  );
}
