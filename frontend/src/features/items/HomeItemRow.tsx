import { Fragment } from 'react';
import { NavLink } from '../../app/NavLink';
import { useI18n } from '../../i18n';
import type { Member } from '../members/membersApi';
import type { SubjectType } from '../subjects/subjectsApi';
import { SubjectTypeIcon } from '../subjects/SubjectTypeIcon';
import { AlertIcon, CalendarIcon, CheckIcon } from './HomeIcons';
import { responsibleLabels, rowFacts, type RowFact } from './homeRowMeta';
import type { Item, ItemGroup } from './itemsApi';

// One reading line per row: the completion control in its own 44 px target, then the title and a
// single quiet metadata line. Nothing is aligned into columns, so long Czech names and dates wrap
// without clipping and a 320 px screen keeps the same order as a wide one.
const TITLE_LINK = 'flex min-h-6 min-w-0 items-center rounded text-row-title font-semibold text-ink '
  + '[overflow-wrap:anywhere] transition-colors hover:text-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus';
const META = 'mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-meta leading-snug text-muted';
const FACT = 'inline max-w-full [overflow-wrap:anywhere]';
const SEPARATOR = 'text-muted select-none';

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

/** One calm row: a round completion control, the title, then subject, date, responsibility and cadence. */
export function HomeItemRow(props: Props) {
  const { item, group, evaluatedOn, subjectName, subjectType, locale, userId, members, membersLoaded, busy, rowRef, onDone, onOpenItem } = props;
  const { t } = useI18n();
  const { responsibleLabel } = responsibleLabels(item, members, membersLoaded, userId, t);
  const facts = rowFacts({ item, group, evaluatedOn, locale, t, responsibleLabel });
  return (
    <li ref={rowRef} data-item-id={item.id} className="flex items-start gap-3 border-b border-line/80 py-3 pr-1 last:border-b-0">
      <button
        type="button"
        data-action="done"
        aria-label={t('items.done.label', { title: item.title })}
        disabled={busy}
        onClick={onDone}
        className="group/done relative -ml-2 mt-0.5 grid size-7 shrink-0 place-items-center rounded-full border-[1.5px] border-field bg-raised text-muted transition-colors after:absolute after:-inset-2 after:rounded-full hover:border-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:cursor-not-allowed disabled:opacity-50"
      >
        <CheckIcon className="size-3.5 text-accent opacity-0 transition-opacity group-hover/done:opacity-70 group-focus-visible/done:opacity-70" />
      </button>
      <div className="min-w-0 flex-1">
        <NavLink
          href={`/items/${encodeURIComponent(item.id)}`}
          onNavigate={() => onOpenItem(item.id)}
          aria-label={t('itemDetail.open', { title: item.title })}
          className={TITLE_LINK}
        >
          <span data-item-title className="[overflow-wrap:anywhere]">{item.title}</span>
        </NavLink>
        <div data-item-meta className={META}>
          <span data-subject-pill className="inline-flex max-w-full items-center gap-1 [overflow-wrap:anywhere]">
            <span data-subject-icon={subjectType ?? 'custom'} className="inline-flex">
              <SubjectTypeIcon type={subjectType ?? 'custom'} className="size-3.5 shrink-0 text-muted" />
            </span>
            {subjectName}
          </span>
          {facts.map((fact) => (
            <Fragment key={fact.kind}>
              {' '}<span aria-hidden="true" className={SEPARATOR}>·</span>{' '}
              <Fact fact={fact} />
            </Fragment>
          ))}
        </div>
      </div>
    </li>
  );
}

/** One real fact with its semantic icon; meaning never depends on colour alone. */
function Fact({ fact }: { fact: RowFact }) {
  const Icon = fact.icon === 'alert' ? AlertIcon : fact.icon === 'calendar' ? CalendarIcon : null;
  const [before, after] = fact.emphasis ? fact.text.split(fact.emphasis) : [fact.text, ''];
  return (
    <span data-item-fact={fact.kind} className={`${FACT} ${fact.tone === 'attention' ? '' : 'text-muted'}`}>
      {Icon ? <Icon className={`mr-1 inline-block size-3.5 align-[-0.15em] ${fact.tone === 'attention' ? 'text-needs-attention' : ''}`} /> : null}
      {before}
      {fact.emphasis ? <span className="text-needs-attention">{fact.emphasis}</span> : null}
      {after}
    </span>
  );
}
