import { Fragment } from 'react';
import { NavLink } from '../../app/NavLink';
import { useI18n } from '../../i18n';
import type { Member } from '../members/membersApi';
import type { SubjectType } from '../subjects/subjectsApi';
import { SubjectTypeIcon } from '../subjects/SubjectTypeIcon';
import { AlertIcon, CalendarIcon, CheckIcon } from './HomeIcons';
import { responsibleLabel as labelFor, rowFacts, type RowFact } from './homeRowMeta';
import type { Item, ItemGroup } from './itemsApi';

// One reading line per row: the completion control in its own 44 px box, then the title and a single quiet
// metadata line. Nothing is aligned into columns, so long Czech names and dates wrap instead of clipping and a
// 320 px screen keeps the same order as a wide one.
const TITLE_LINK = 'flex min-h-11 min-w-0 items-center rounded text-row-title font-semibold text-ink '
  + 'transition-colors hover:text-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus';
const META = 'mt-0.5 block text-meta leading-snug text-muted';
const SEPARATOR = 'select-none text-muted';
const FACT = 'inline max-w-full [overflow-wrap:anywhere]';

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

/** One calm row: a completion box, the title, then subject, date, responsibility and cadence. */
export function HomeItemRow(props: Props) {
  const { item, group, evaluatedOn, subjectName, subjectType, locale, userId, members, membersLoaded, busy, rowRef, onDone, onOpenItem } = props;
  const { t } = useI18n();
  const responsibleLabel = labelFor(item, members, membersLoaded, userId, t);
  const facts = rowFacts({ item, group, evaluatedOn, locale, t, responsibleLabel });
  return (
    <li ref={rowRef} data-item-id={item.id} className="flex items-start gap-2 py-2 pr-1">
      <button
        type="button"
        data-action="done"
        data-variant="secondary"
        aria-label={t('items.done.label', { title: item.title })}
        disabled={busy}
        onClick={onDone}
        className="grid size-11 shrink-0 place-items-center rounded-full border-[1.5px] border-field bg-raised text-muted
          transition-colors hover:border-accent hover:text-accent focus-visible:outline-2 focus-visible:outline-offset-2
          focus-visible:outline-focus disabled:cursor-not-allowed disabled:opacity-60"
      >
        <CheckIcon className="size-4" />
      </button>
      <div className="min-w-0 flex-1 py-0.5">
        <NavLink
          href={`/items/${encodeURIComponent(item.id)}`}
          onNavigate={() => onOpenItem(item.id)}
          aria-label={t('itemDetail.open', { title: item.title })}
          className={TITLE_LINK}
        >
          <span data-item-title className="[overflow-wrap:anywhere]">{item.title}</span>
        </NavLink>
        <div data-item-meta className={META}>
          <span data-subject-pill className="inline-flex min-w-0 max-w-full items-center gap-1 align-[-0.15em] [overflow-wrap:anywhere]">
            <span aria-hidden="true" data-subject-icon={subjectType ?? 'custom'} className="inline-flex shrink-0">
              <SubjectTypeIcon type={subjectType ?? 'custom'} className="size-3.5" />
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
    <span data-item-fact={fact.kind} className={FACT}>
      {Icon ? <Icon className={`mr-1 inline-block size-3.5 align-[-0.15em] ${fact.tone === 'attention' ? 'text-needs-attention' : 'text-muted'}`} /> : null}
      {before}
      {fact.emphasis ? <span className={fact.tone === 'attention' ? 'font-semibold text-needs-attention' : 'font-semibold text-ink'}>{fact.emphasis}</span> : null}
      {after}
    </span>
  );
}
