import type { Translate } from '../../i18n';
import type { Member } from '../members/membersApi';
import { formatAttentionDate, formatDate } from './formatDate';
import type { Item, ItemGroup } from './itemsApi';
import { repeatSummary } from './repeatSummary';

export function responsibleLabels(
  item: Item, members: Member[], membersLoaded: boolean, userId: string, t: Translate,
): { responsibleLabel: string | null; assigneeLabel: string | null } {
  if (!item.responsibleUserId || !membersLoaded) return { responsibleLabel: null, assigneeLabel: null };
  const member = members.find((entry) => entry.userId === item.responsibleUserId);
  if (!member) {
    const unknown = t('items.responsible.unknown');
    return { responsibleLabel: unknown, assigneeLabel: unknown };
  }
  if (member.userId === userId) return { responsibleLabel: t('items.responsible.you'), assigneeLabel: t('items.assignee.you') };
  return { responsibleLabel: t('items.responsible.read', { name: member.login }), assigneeLabel: member.login };
}

type FactInput = {
  item: Item;
  group: ItemGroup;
  evaluatedOn: string;
  locale: string;
  t: Translate;
  responsibleLabel: string | null;
  assigneeLabel: string | null;
};

export type RowFactKind = 'attention' | 'responsible' | 'details';

/**
 * One real fact of a Home row, written for each width. On a phone the attention fact may drop the words the
 * group heading already says ("Since May 12" under "Needs attention"); the desktop status column keeps the
 * full wording. `column` marks the fact that becomes that column from `xl` rather than staying in the line.
 */
export type RowFact = {
  kind: RowFactKind;
  /** Words used on the metadata line below `xl`. */
  phone: string;
  /** Words used in the status column, or in the metadata line, from `xl`. */
  wide: string;
  tone: 'attention' | 'muted';
  icon: 'alert' | 'calendar' | null;
  column: boolean;
};

/** The needs group is already headed "Needs attention", so its row only names the date on a phone. */
function attentionFact(group: ItemGroup, date: string, t: Translate): RowFact {
  const needs = group === 'needs';
  const phone = needs ? t('items.needsSinceCompact', { date }) : t('items.rowAttentionDate', { date });
  const wide = needs
    ? t('items.needsSince', { date })
    : group === 'upcoming' ? date : t('items.rowAttentionDate', { date });
  return {
    kind: 'attention',
    phone,
    wide,
    tone: needs ? 'attention' : 'muted',
    icon: needs ? 'alert' : 'calendar',
    column: needs || group === 'upcoming',
  };
}

/** The desktop column names a bare login in the upcoming group, where the date is its neighbour. */
function responsibleFact(group: ItemGroup, label: string, assignee: string | null): RowFact {
  return {
    kind: 'responsible',
    phone: label,
    wide: group === 'upcoming' ? assignee ?? label : label,
    tone: 'muted',
    icon: null,
    column: group !== 'needs',
  };
}

function detailsFact(item: Item, locale: string, t: Translate): RowFact | null {
  const details = [
    item.lastCompletedOn ? t('itemDetail.lastDone', { date: formatDate(item.lastCompletedOn, locale) }) : null,
    item.recurrence ? repeatSummary(t, item.recurrence, locale) : null,
  ].filter(Boolean).join(' · ');
  return details ? { kind: 'details', phone: details, wide: details, tone: 'muted', icon: null, column: false } : null;
}

/**
 * Independent facts, never substituted for one another: attention status, responsible member, then last
 * completion plus recurrence summary. The facts that form the desktop status column come first so the
 * phone line and the desktop columns read from the same element per fact.
 */
export function rowFacts({ item, group, evaluatedOn, locale, t, responsibleLabel, assigneeLabel }: FactInput): RowFact[] {
  const attention = item.attentionOn ? attentionFact(group, formatAttentionDate(item.attentionOn, evaluatedOn, locale), t) : null;
  const responsible = responsibleLabel === null ? null : responsibleFact(group, responsibleLabel, assigneeLabel);
  const facts = [attention, responsible, detailsFact(item, locale, t)].filter((fact): fact is RowFact => fact !== null);
  return [...facts.filter((fact) => fact.column), ...facts.filter((fact) => !fact.column)];
}
