import { Button } from '../../components/ui/button';
import { NavLink } from '../../app/NavLink';
import { useI18n } from '../../i18n';
import type { Member } from '../members/membersApi';
import { repeatSummary } from './repeatSummary';
import { formatDate } from './formatDate';
import type { Item, ItemGroup } from './itemsApi';
import { HOME_GROUP_ROW_CAP, groups, type GroupData } from './homeGroups';

function getAttentionLine(item: Item, group: ItemGroup, locale: string, t: ReturnType<typeof useI18n>['t']) {
  if (!item.attentionOn) return null;
  const key = group === 'upcoming' ? 'items.attentionOn' : group === 'needs' ? 'items.needsSince' : 'items.rowAttentionDate';
  return t(key, { date: formatDate(item.attentionOn, locale) });
}

export function ItemRow({ item, group, subjectName, locale, userId, members, membersLoaded, busy, onDone, onOpenItem, rowRef, subjectHeading }: {
  item: Item;
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
  subjectHeading?: string;
}) {
  const { t } = useI18n();
  const responsible = item.responsibleUserId ? members.find((member) => member.userId === item.responsibleUserId) : undefined;
  const attentionLine = getAttentionLine(item, group, locale, t);
  const historyLine = [
    item.lastCompletedOn
      ? t('itemDetail.lastDone', {
          date: formatDate(item.lastCompletedOn, locale),
        })
      : null,
    item.responsibleUserId && membersLoaded
      ? responsible
        ? t(
            responsible.userId === userId ? 'items.responsible.you' : 'items.responsible.read',
            { name: responsible.login },
          )
        : t('items.responsible.unknown')
      : null,
  ]
    .filter(Boolean)
    .join(' · ');
  return (
    <li ref={rowRef} data-item-id={item.id} className='home-item-row group flex flex-wrap items-center justify-between gap-3 border-b border-line py-3 last:border-0'>
      {subjectHeading ? <h3 className="home-subject-heading mb-2 w-full font-display text-lg">{subjectHeading}</h3> : null}
      <div className='min-w-0 flex-1 basis-48'>
        <NavLink
          href={`/items/${encodeURIComponent(item.id)}`}
          onNavigate={() => onOpenItem(item.id)}
          className={[
            'inline-flex min-h-11 min-w-0 max-w-full [overflow-wrap:anywhere] text-left text-lg leading-snug',
            'underline decoration-line underline-offset-4 hover:decoration-accent',
            'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent',
          ].join(' ')}
        >
          <span aria-hidden='true'>{item.title}</span>
          <span className='sr-only'>{t('itemDetail.open', { title: item.title })}</span>
        </NavLink>
        <p className='mt-1 break-words text-base text-muted'>
          {group === 'needs' ? null : <span>{subjectName}</span>}
          {attentionLine ? `${group === 'needs' ? '' : ' · '}${attentionLine}` : null}
          {historyLine ? ` · ${historyLine}` : null}
        </p>
        {item.recurrence ? <p className='mt-1 hidden break-words text-sm text-muted group-hover:block group-focus-within:block'>{repeatSummary(t, item.recurrence, locale)}</p> : null}
      </div>
      <Button type='button' variant='secondary' data-action='done' aria-label={t('items.done.label', { title: item.title })} disabled={busy} onClick={onDone}>
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
    <section id={`group-${group.id}`} className={`home-group-section ${group.quiet ? 'border-t border-line pt-5' : ''}`}>
      <h2 ref={headingRef} data-group-heading={group.id} tabIndex={-1} className={`font-display ${group.quiet ? 'text-xl text-muted' : 'text-2xl'}`}>
        {t(group.key)} <span className='text-base font-normal text-muted'>{countLabel}</span>
      </h2>
      <ul className='mt-3'>{children}</ul>
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
