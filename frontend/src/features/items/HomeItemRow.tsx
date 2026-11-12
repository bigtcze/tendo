import { Fragment } from 'react';
import { NavLink } from '../../app/NavLink';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import type { Member } from '../members/membersApi';
import type { SubjectType } from '../subjects/subjectsApi';
import { SubjectTypeIcon } from '../subjects/SubjectTypeIcon';
import { AlertIcon, CalendarIcon, CheckIcon } from './HomeIcons';
import { groupTone } from './homeGroupTone';
import { responsibleLabels, rowFacts, type RowFact } from './homeRowMeta';
import type { Item, ItemGroup } from './itemsApi';

// Phone rows flow: subject pill and every fact share one wrapping line. From `xl` the same elements move
// into the aligned columns through `display: contents`, so no fact is rendered twice.
const ROW = [
  'grid grid-cols-[2.25rem_minmax(0,1fr)_auto] items-center gap-x-3 px-2.5 py-2.5',
  'sm:px-4',
  'xl:grid-cols-[2.25rem_minmax(0,1fr)_8rem_14rem_auto] xl:gap-x-4 xl:py-0 xl:pt-[11px]',
].join(' ');
// Direction B: the subject is plain metadata text, not a pill on every row, so the status colour stays rare.
const SUBJECT_PILL = 'mr-2 inline align-top font-semibold text-ink [overflow-wrap:anywhere] '
  + 'xl:col-start-3 xl:row-span-2 xl:mr-0 xl:self-center xl:justify-self-start';
// One text line in layout, 44 px pointer target: 11 px above and below the title line, both compensated by
// negative margins so the row keeps its compact height.
const TITLE_LINK = 'relative z-10 flex min-w-0 items-center rounded-lg pt-[11px] pb-[11px] -my-[11px] '
  + 'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus xl:col-start-2 xl:row-start-1';
/** Phone metadata line; from `xl` its children are placed in the row grid directly. */
const META = 'mt-0.5 text-meta leading-snug xl:contents';
/** The one status column from `xl`: the fact the desktop reference puts beside the title. */
const STATUS_COLUMN = 'contents xl:col-start-4 xl:row-span-2 xl:flex xl:min-w-0 xl:flex-wrap xl:items-center xl:self-center';
/** A single status fact needs no wrapper: it is placed in the column itself. */
const STATUS_FACT = 'xl:col-start-4 xl:row-span-2 xl:self-center';
/** The remaining facts, still under the title from `xl`. */
const META_LINE = 'contents xl:col-start-2 xl:row-start-2 xl:flex xl:min-w-0 xl:flex-wrap xl:items-center';
// Every phone fact is one atomic inline-level box, so the line breaks only between facts; a fact wraps
// internally only when it is wider than the whole line. Ordinary spaces (not just margins) separate the
// units so copied text keeps its word boundaries; the 8 px margin plus that space keeps the ~12 px rhythm
// of the line. From `xl` the same spans are flex items separated by a quiet middle dot, where the metadata
// line and the status column keep facts beside each other (whitespace-only nodes are not rendered there).
const FACT = 'mr-2 inline-block max-w-full align-top [overflow-wrap:anywhere] xl:mr-0';
const FACT_DOT = "xl:after:content-['·'] xl:after:mx-1.5";

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

/** Compact row: real subject-type tile, selectable title link, every real fact inline and Done. */
export function HomeItemRow(props: Props) {
  const { item, group, evaluatedOn, subjectName, subjectType, locale, userId, members, membersLoaded, busy, rowRef, onDone, onOpenItem } = props;
  const { t } = useI18n();
  const { responsibleLabel, assigneeLabel } = responsibleLabels(item, members, membersLoaded, userId, t);
  const facts = rowFacts({ item, group, evaluatedOn, locale, t, responsibleLabel, assigneeLabel });
  const column = facts.filter((fact) => fact.column);
  const inline = facts.filter((fact) => !fact.column);
  const tone = groupTone[group];
  return (
    <li ref={rowRef} data-item-id={item.id} className={ROW}>
      <span
        aria-hidden="true"
        data-subject-icon={subjectType ?? 'unknown'}
        className={`grid size-9 place-items-center rounded-xl ${tone.tile} xl:row-span-2 xl:self-center`}
      >
        <SubjectTypeIcon type={subjectType ?? 'custom'} />
      </span>
      <div className="min-w-0 xl:contents">
        <NavLink
          href={`/items/${encodeURIComponent(item.id)}`}
          onNavigate={() => onOpenItem(item.id)}
          aria-label={t('itemDetail.open', { title: item.title })}
          className={TITLE_LINK}
        >
          <span data-item-title className="text-row-title font-semibold leading-snug text-ink [overflow-wrap:anywhere]">{item.title}</span>
        </NavLink>
        <div data-item-meta className={META}>
          <span data-subject-pill className={SUBJECT_PILL}>{subjectName}</span>
          {facts.length ? ' ' : null}
          {column.length > 1 ? <div className={STATUS_COLUMN}>
            {column.map((fact, index) => (
              <Fragment key={fact.kind}>{index > 0 ? ' ' : null}<Fact fact={fact} last={index === column.length - 1} /></Fragment>
            ))}
          </div> : column.map((fact) => (
            <Fragment key={fact.kind}><Fact fact={fact} last placement={STATUS_FACT} /></Fragment>
          ))}
          {column.length && inline.length ? ' ' : null}
          {inline.length ? <div className={META_LINE}>
            {inline.map((fact, index) => (
              <Fragment key={fact.kind}>{index > 0 ? ' ' : null}<Fact fact={fact} last={index === inline.length - 1} /></Fragment>
            ))}
          </div> : null}
        </div>
      </div>
      <Button
        type="button"
        variant="secondary"
        data-action="done"
        aria-label={t('items.done.label', { title: item.title })}
        disabled={busy}
        onClick={onDone}
        className="h-11 w-11 shrink-0 gap-2 p-0 lg:w-auto lg:px-3 xl:col-start-5 xl:row-span-2 xl:self-center xl:px-4"
      >
        <CheckIcon />
        <span className="hidden lg:inline">{t('items.done')}</span>
      </Button>
    </li>
  );
}

/** One real fact with its semantic icon; the words differ per width, never the meaning. */
function Fact({ fact, last, placement = '' }: { fact: RowFact; last: boolean; placement?: string }) {
  const Icon = fact.icon === 'alert' ? AlertIcon : fact.icon === 'calendar' ? CalendarIcon : null;
  return (
    <span
      data-item-fact={fact.kind}
      className={[
        placement,
        FACT,
        last ? '' : FACT_DOT,
        fact.tone === 'attention' ? 'text-needs-attention' : 'text-muted',
      ].filter(Boolean).join(' ')}
    >
      {Icon ? <Icon className="mr-1 inline-block size-4 align-[-0.15em]" /> : null}
      {fact.phone === fact.wide
        ? fact.phone
        : <>
          <span className="xl:hidden">{fact.phone}</span>
          <span className="hidden xl:inline">{fact.wide}</span>
        </>}
    </span>
  );
}
