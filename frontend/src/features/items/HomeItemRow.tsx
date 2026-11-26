import { Fragment } from 'react';
import { NavLink } from '../../app/NavLink';
import { useI18n } from '../../i18n';
import type { Member } from '../members/membersApi';
import type { SubjectType } from '../subjects/subjectsApi';
import { SubjectTypeIcon } from '../subjects/SubjectTypeIcon';
import { AlertIcon, CalendarIcon, CheckIcon } from './HomeIcons';
import { responsibleLabel as labelFor, rowFacts, type RowFact } from './homeRowMeta';
import type { Item, ItemGroup } from './itemsApi';

// One reading line per row: the completion control in its own column (at least 44 px each way), then the title and
// a single quiet metadata line. Nothing is aligned into columns, so long Czech names and dates wrap instead of
// clipping and a 320 px screen keeps the same order as a wide one.
//
// The link's own box is the text block, but its stretched pseudo-element covers the whole row: the row — not a
// minimum height on the title — is the target, so a two-line row stays as short as its content honestly needs.
const ROW_LINK = 'block rounded text-row-title font-semibold text-ink transition-colors hover:text-accent '
  + 'after:absolute after:inset-0 after:rounded '
  + 'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus';
const META = 'mt-0.5 block text-meta leading-snug text-muted';
const SEPARATOR = 'select-none text-muted';
const FACT = 'inline max-w-full [overflow-wrap:anywhere]';
// Above the stretched row link, so the completion box keeps its own clicks and its own focus ring. The action is
// always named in words: below lg the check circle carries "Done" under it (a 56 px wide, 44 px+ tall column, like
// the phone tabs); from lg the row has the room for one outlined pill with the check and the word side by side.
const DONE = 'group relative z-10 flex w-14 min-h-11 shrink-0 flex-col items-center justify-center gap-0.5 rounded-lg text-muted '
  + 'transition-colors enabled:hover:text-accent focus-visible:outline-2 focus-visible:outline-offset-2 '
  + 'focus-visible:outline-focus disabled:cursor-not-allowed disabled:opacity-60 '
  + 'lg:w-auto lg:flex-row lg:gap-2 lg:rounded-full lg:border-[1.5px] lg:border-field lg:bg-raised lg:px-3 '
  + 'lg:enabled:hover:border-accent';
// The check's own outer <svg> draws the phone circle (border and padding apply to an outer svg box), so the row keeps
// its element count; from lg the pill's border takes over and the check is a plain 16 px glyph.
const DONE_MARK = 'size-9 rounded-full border-[1.5px] border-field bg-raised p-2.5 transition-colors '
  + 'group-enabled:group-hover:border-accent lg:size-4 lg:rounded-none lg:border-0 lg:bg-transparent lg:p-0';
const DONE_WORD = 'max-w-full text-micro font-medium [overflow-wrap:anywhere] max-lg:leading-tight lg:text-small';

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
    <li ref={rowRef} data-item-id={item.id} className="relative flex items-start gap-2 py-2 pr-1">
      <button
        type="button"
        data-action="done"
        aria-label={t('items.done.label', { title: item.title })}
        disabled={busy}
        onClick={onDone}
        className={DONE}
      >
        <CheckIcon className={DONE_MARK} />
        <span data-done-word className={DONE_WORD}>{t('items.done')}</span>
      </button>
      <div className="min-w-0 flex-1 py-0.5">
        <NavLink
          href={`/items/${encodeURIComponent(item.id)}`}
          onNavigate={() => onOpenItem(item.id)}
          aria-label={t('itemDetail.open', { title: item.title })}
          className={ROW_LINK}
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
