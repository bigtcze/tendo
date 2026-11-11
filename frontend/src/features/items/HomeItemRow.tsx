import { NavLink } from '../../app/NavLink';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import type { Member } from '../members/membersApi';
import type { SubjectType } from '../subjects/subjectsApi';
import { SubjectTypeIcon } from '../subjects/SubjectTypeIcon';
import { AlertIcon, CalendarIcon, CheckIcon } from './HomeIcons';
import { groupTone } from './homeGroupTone';
import { responsibleLabels, rowFacts, rowStatus } from './homeRowMeta';
import type { Item, ItemGroup } from './itemsApi';

const SUBJECT_PILL = 'inline-flex max-w-full items-center rounded-full bg-upcoming-bg px-2.5 py-0.5 '
  + 'text-meta font-semibold text-ink [overflow-wrap:anywhere] xl:col-start-2 xl:row-span-2 xl:row-start-1 xl:justify-self-start xl:self-center';
// One text line in layout, 44 px pointer target: 12 px above and 10 px below the title line, both
// compensated by negative margins so the row keeps its compact height. The link is raised above the
// metadata hit area; only the small lower band overlaps, and the metadata text stays selectable below it.
const TITLE_LINK = 'relative z-10 flex min-w-0 items-center rounded-lg pt-[11px] pb-[11px] -my-[11px] '
  + 'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus xl:col-start-1 xl:row-start-1';

type Props = {
  item: Item;
  group: ItemGroup;
  evaluatedOn: string;
  subjectName: string;
  subjectType: SubjectType | undefined;
  locale: string;
  userId: string;
  members: Member[];
  membersLoaded: boolean;
  busy: boolean;
  onDone: () => void;
  onOpenItem: (itemId: string) => void;
  rowRef?: (node: HTMLLIElement | null) => void;
};

/** Compact grid row: real subject-type tile, selectable title link, every real fact, subject pill and Done. */
export function HomeItemRow(props: Props) {
  const { item, group, evaluatedOn, subjectName, subjectType, locale, userId, members, membersLoaded, busy, rowRef, onDone, onOpenItem } = props;
  const { t } = useI18n();
  const { responsibleLabel, assigneeLabel } = responsibleLabels(item, members, membersLoaded, userId, t);
  const facts = rowFacts({ item, group, evaluatedOn, locale, t, responsibleLabel });
  const status = rowStatus({ item, group, evaluatedOn, locale, t, responsibleLabel, assigneeLabel });
  const tone = groupTone[group];
  const attentionHiddenOnDesktop = group === 'needs' || group === 'upcoming';
  const detailsHasDesktopPredecessor = group === 'upcoming' ? false : group === 'needs' ? Boolean(facts.responsible) : Boolean(facts.attention);
  return (
    <li
      ref={rowRef}
      data-item-id={item.id}
      className="grid grid-cols-[2.25rem_minmax(0,1fr)_auto] items-center gap-x-3 px-4 pb-2 pt-[11px] xl:grid-cols-[2.25rem_minmax(0,1fr)_14rem_auto] xl:gap-x-4 xl:pb-0 xl:pt-[11px]"
    >
      <span
        aria-hidden="true"
        data-subject-icon={subjectType ?? 'unknown'}
        className={`grid size-9 place-items-center rounded-xl ${tone.tile} xl:col-start-1 xl:row-start-1 xl:self-center`}
      >
        <SubjectTypeIcon type={subjectType ?? 'custom'} />
      </span>
      <div className="min-w-0 xl:col-start-2 xl:row-start-1 xl:grid xl:grid-cols-[minmax(0,1fr)_8rem] xl:items-start xl:gap-x-4">
        <NavLink
          href={`/items/${encodeURIComponent(item.id)}`}
          onNavigate={() => onOpenItem(item.id)}
          className={TITLE_LINK}
        >
          <span className="sr-only">{t('itemDetail.open', { title: item.title })}</span>
          <span aria-hidden="true" data-item-title className="text-row-title font-semibold leading-snug text-ink [overflow-wrap:anywhere]">{item.title}</span>
        </NavLink>
        <div className="mt-0.5 flex flex-col items-start gap-0.5 xl:contents">
          <span data-subject-pill className={SUBJECT_PILL}>{subjectName}</span>
          <div className="flex flex-col gap-0.5 text-meta leading-snug xl:col-start-1 xl:row-start-2 xl:block">
            {facts.attention ? (
              <span className={`flex items-center gap-1.5 ${group === 'needs' ? 'text-needs-attention' : 'text-muted'} ${
                attentionHiddenOnDesktop ? 'xl:hidden' : 'xl:inline-flex'
              }`}>
                {group === 'needs' ? <AlertIcon className="size-4 shrink-0" /> : <CalendarIcon className="size-4 shrink-0" />}
                <span className="[overflow-wrap:anywhere]">{facts.attention}</span>
              </span>
            ) : null}
            {facts.responsible ? (
              <span className={`text-muted [overflow-wrap:anywhere] ${group === 'needs' ? '' : 'xl:hidden'}`}>{facts.responsible}</span>
            ) : null}
            {facts.details ? (
              <span className="text-muted [overflow-wrap:anywhere]">
                {detailsHasDesktopPredecessor ? <span aria-hidden="true" className="hidden xl:inline">{' · '}</span> : null}
                {facts.details}
              </span>
            ) : null}
          </div>
        </div>
      </div>
      <RowStatusColumn status={status} />
      <Button
        type="button"
        variant="secondary"
        data-action="done"
        aria-label={t('items.done.label', { title: item.title })}
        disabled={busy}
        onClick={onDone}
        className="h-11 w-11 shrink-0 gap-2 p-0 lg:w-auto lg:px-3 xl:col-start-4 xl:row-start-1 xl:px-4"
      >
        <CheckIcon />
        <span className="hidden lg:inline">{t('items.done')}</span>
      </Button>
    </li>
  );
}

function RowStatusColumn({ status }: { status: ReturnType<typeof rowStatus> }) {
  if (!status) return null;
  return (
    <p className={`hidden min-w-0 items-center gap-1.5 text-meta leading-snug xl:col-start-3 xl:row-start-1 xl:flex ${
      status.kind === 'attention' ? 'text-needs-attention' : 'text-muted'
    }`}>
      {status.kind === 'attention' ? <AlertIcon className="size-4 shrink-0" /> : null}
      {status.kind === 'date' ? <CalendarIcon className="size-4 shrink-0" /> : null}
      <span className="[overflow-wrap:anywhere]">{status.text}</span>
    </p>
  );
}
