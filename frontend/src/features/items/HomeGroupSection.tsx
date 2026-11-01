import { Button } from '../../components/ui/button';
import { NavLink } from '../../app/NavLink';
import { useI18n } from '../../i18n';
import type { Member } from '../members/membersApi';
import { repeatSummary } from './repeatSummary';
import { relativeAttention } from './relativeAttention';
import { formatDate } from './formatDate';
import type { Item, ItemGroup } from './itemsApi';
import { HOME_GROUP_ROW_CAP, groups, type GroupData } from './homeGroups';

function rowMetadata(item: Item, group: ItemGroup, subjectName: string, locale: string, members: Member[], membersLoaded: boolean, userId: string, t: ReturnType<typeof useI18n>['t']) {
  const responsible = item.responsibleUserId ? members.find((member) => member.userId === item.responsibleUserId) : undefined;
  const accessibleDate = item.attentionOn ? new Intl.DateTimeFormat(locale === 'cs' ? 'cs-CZ' : 'en-GB', { day: 'numeric', month: 'short', year: 'numeric', timeZone: 'UTC' }).format(new Date(`${item.attentionOn}T00:00:00Z`)) : null;
  const attention = accessibleDate ? t(group === 'needs' ? 'items.needsSince' : group === 'upcoming' ? 'items.attentionOn' : 'items.rowAttentionDate', { date: accessibleDate }) : null;
  const relativeDate = item.attentionOn ? relativeAttention(item.attentionOn, locale, nowFromPage()) : null;
  const assigned = item.responsibleUserId && membersLoaded
    ? responsible?.userId === userId ? t('items.responsible.you') : responsible ? t('items.responsible.read', { name: responsible.login }) : t('items.responsible.unknown')
    : null;
  return { secondary: [subjectName, relativeDate, assigned].filter(Boolean).join(' · '), fullAttention: attention, lastDone: item.lastCompletedOn ? t('itemDetail.lastDone', { date: formatDate(item.lastCompletedOn, locale) }) : null, recurrence: item.recurrence ? repeatSummary(t, item.recurrence, locale) : null };
}

function nowFromPage() {
  const testDate = document.documentElement.dataset.testDate;
  return testDate ? new Date(`${testDate}T12:00:00Z`) : new Date();
}

export function ItemRow({ item, group, subjectName, locale, userId, members, membersLoaded, busy, onDone, onOpenItem, rowRef }: {
  item: Item; group: ItemGroup; subjectName: string; locale: string; userId: string; members: Member[]; membersLoaded: boolean;
  busy: boolean; onDone: () => void; onOpenItem: (itemId: string) => void; rowRef?: (node: HTMLLIElement | null) => void;
}) {
  const { t } = useI18n();
  const meta = rowMetadata(item, group, subjectName, locale, members, membersLoaded, userId, t);
  return <li ref={rowRef} data-item-id={item.id} className='item-row flex flex-wrap items-center justify-between gap-3 px-3 py-1 sm:px-4'>
    <Button type='button' variant='secondary' className='row-done' data-action='done' aria-label={t('items.done.label', { title: item.title })} disabled={busy} onClick={onDone}>
      <span className='done-control' aria-hidden='true' /><span className='sr-only'>{t('items.done')}</span>
    </Button>
    <div className='row-copy min-w-0 flex-1 basis-48'>
      <NavLink href={`/items/${encodeURIComponent(item.id)}`} onNavigate={() => onOpenItem(item.id)} className='row-title inline-flex min-h-11 min-w-0 max-w-full [overflow-wrap:anywhere] text-left text-lg font-medium leading-snug hover:text-accent hover:decoration-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent'>
        <span aria-hidden='true'>{item.title}</span><span className='sr-only'>{t('itemDetail.open', { title: item.title })}</span>
      </NavLink>
      <p className='row-secondary truncate text-base text-muted' aria-label={meta.fullAttention ? `${meta.fullAttention}${meta.lastDone ? ` ${meta.lastDone}` : ''}` : undefined}><span aria-hidden='true'>{meta.secondary}</span><span className='sr-only'>{meta.fullAttention} {meta.lastDone}</span></p>
      {meta.recurrence ? <span className='repeat-mark' title={meta.recurrence}><svg aria-hidden='true' viewBox='0 0 24 24' width='20' height='20' fill='none' stroke='currentColor' strokeWidth='1.7' strokeLinecap='round' strokeLinejoin='round'><path d='M20 7v5h-5M4 17v-5h5' /><path d='M5.6 9a7 7 0 0 1 11.7-2L20 12M4 12l2.7 5a7 7 0 0 0 11.7-2' /></svg><span className='sr-only'>{meta.recurrence}</span></span> : null}
    </div>
  </li>;
}

export function HomeGroupSection({ group, summary, state, countLabel, children, headingRef, moreRef, onShowMore, onOpenAll }: {
  group: (typeof groups)[number]; summary: number; state: GroupData; countLabel: string; children: React.ReactNode;
  headingRef: (node: HTMLHeadingElement | null) => void; moreRef: (node: HTMLButtonElement | null) => void; onShowMore: () => void; onOpenAll: () => void;
}) {
  const { t } = useI18n();
  if (!state.items.length) return null;
  return <section id={`group-${group.id}`} className={`home-group home-group-${group.id} ${group.quiet ? 'border-t border-line pt-5' : ''}`}>
    <h2 ref={headingRef} data-group-heading={group.id} tabIndex={-1} className={`font-display ${group.quiet ? 'text-xl text-muted' : 'text-2xl'}`}>
      {t(group.key)} <span className='text-base font-normal text-muted'>{countLabel}</span>
    </h2>
    <ul className='mt-3 divide-y divide-line border-y border-line bg-white/50'>{children}</ul>
    {state.cursor && state.items.length < HOME_GROUP_ROW_CAP ? <Button ref={moreRef} type='button' data-group-more={group.id} aria-disabled={state.loadingMore} variant='quiet' onClick={onShowMore}>
      {state.loadingMore ? t('items.moreLoading') : t('items.showMore', { count: String(Math.max(0, summary - state.items.length)) })}
    </Button> : null}
    {state.items.length >= HOME_GROUP_ROW_CAP && (state.cursor || state.overflow || summary > state.items.length) ? <NavLink href={`/items?status=${group.id}`} data-group-all={group.id} onNavigate={onOpenAll} className='inline-flex min-h-11 items-center underline decoration-line underline-offset-4 hover:decoration-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent'>
      {summary > state.items.length ? t('items.group.seeAll', { count: String(summary) }) : t('items.group.seeAllNoCount')}
    </NavLink> : null}
  </section>;
}
