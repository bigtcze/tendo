import { Button } from '../../components/ui/button';
import { NavLink } from '../../app/NavLink';
import { useI18n } from '../../i18n';
import type { Member } from '../members/membersApi';
import { repeatSummary } from './repeatSummary';
import { formatDate } from './formatDate';
import type { Completion, Item, ItemGroup } from './itemsApi';
import { HOME_GROUP_ROW_CAP, groups, type GroupData } from './homeGroups';

export type Notice = {
  text: string;
  completion?: Completion;
  itemId?: string;
  retryKey?: string;
  retryKind?: 'complete' | 'undo';
  undoAvailable?: boolean;
};
export function NoticeView({ notice, busy, onUndo, onRetry, onRetryFocus }: {
  notice: Notice | null;
  busy: boolean;
  onUndo: () => void;
  onRetry: () => void;
  onRetryFocus: () => void;
}) {
  const { t } = useI18n();
  if (!notice) return null;
  return (
    <div role='status' aria-live='polite' className='flex flex-wrap items-center justify-between gap-2 rounded-xl bg-sand px-4 py-2'>
      <p>{notice.text}</p>
      {notice.undoAvailable && notice.completion?.id ? (
        <Button type='button' variant='quiet' disabled={busy} onClick={onUndo}>
          {t('items.undo')}
        </Button>
      ) : null}
      {notice.retryKey && notice.retryKind ? (
        <Button type='button' data-action='completion-trigger' variant='quiet' disabled={busy} onFocus={onRetryFocus} onClick={onRetry}>
          {t('items.retry')}
        </Button>
      ) : null}
    </div>
  );
}

export function ItemRow({ item, group, subjectName, locale, userId, members, membersLoaded, busy, onDone, onOpenItem, rowRef }: {
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
        { date: formatDate(item.attentionOn, locale) },
      )
    : null;
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
    <li ref={rowRef} data-item-id={item.id} className='flex flex-wrap items-center justify-between gap-3 p-4'>
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
        <p className='mt-1 break-words text-sm text-muted'>
          {subjectName}
          {attentionLine ? ` · ${attentionLine}` : ''}
        </p>
        {historyLine ? <p className='mt-1 break-words text-sm text-muted'>{historyLine}</p> : null}
        {item.recurrence ? <p className='mt-1 break-words text-sm text-muted'>{repeatSummary(t, item.recurrence, locale)}</p> : null}
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
    <section id={`group-${group.id}`} className={group.quiet ? 'border-t border-line pt-5' : ''}>
      <h2 ref={headingRef} data-group-heading={group.id} tabIndex={-1} className={`font-display ${group.quiet ? 'text-xl text-muted' : 'text-2xl'}`}>
        {t(group.key)} <span className='text-base font-normal text-muted'>{countLabel}</span>
      </h2>
      <ul className='mt-3 divide-y divide-line rounded-2xl border border-line bg-white/70'>{children}</ul>
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
