import type { Translate } from '../../i18n';
import type { Member } from '../members/membersApi';
import { formatAttentionDate, formatDate } from './formatDate';
import type { Item, ItemGroup } from './itemsApi';
import { repeatSummary } from './repeatSummary';

/** The responsibility wording, or null until the member list can name the person. */
export function responsibleLabel(
  item: Item, members: Member[], membersLoaded: boolean, userId: string, t: Translate,
): string | null {
  if (!item.responsibleUserId || !membersLoaded) return null;
  const member = members.find((entry) => entry.userId === item.responsibleUserId);
  if (!member) return t('items.responsible.unknown');
  if (member.userId === userId) return t('items.responsible.you');
  return t('items.responsible.read', { name: member.login });
}

export type RowFactKind = 'attention' | 'responsible' | 'details';

/**
 * One real fact of a Home row. There is a single wording at every width: the group heading already names the
 * state, so the row never says it a second time.
 */
export type RowFact = {
  kind: RowFactKind;
  text: string;
  /** The part of the text that carries the attention colour, if any. */
  emphasis?: string;
  tone: 'attention' | 'muted';
  icon: 'alert' | 'calendar' | null;
};

/** "Needs attention" is the heading, so its row only says since when; other groups name the date itself. */
function attentionFact(group: ItemGroup, date: string, t: Translate): RowFact {
  if (group === 'needs') {
    return { kind: 'attention', text: t('items.needsSinceCompact', { date }), emphasis: date, tone: 'attention', icon: 'alert' };
  }
  // The same wording All items uses: the product says "attention date", never "due".
  return { kind: 'attention', text: t('items.rowAttentionDate', { date }), tone: 'muted', icon: 'calendar' };
}

function detailsFact(item: Item, locale: string, t: Translate): RowFact | null {
  const details = [
    item.lastCompletedOn ? t('itemDetail.lastDone', { date: formatDate(item.lastCompletedOn, locale) }) : null,
    item.recurrence ? repeatSummary(t, item.recurrence, locale) : null,
  ].filter(Boolean).join(' · ');
  return details ? { kind: 'details', text: details, tone: 'muted', icon: null } : null;
}

type FactInput = {
  item: Item;
  group: ItemGroup;
  evaluatedOn: string;
  locale: string;
  t: Translate;
  responsibleLabel: string | null;
};

/**
 * The row's independent facts in reading order: the date that needs attention, who looks after it, then last
 * completion and repeat cadence. Nothing is substituted for anything else.
 */
export function rowFacts({ item, group, evaluatedOn, locale, t, responsibleLabel: label }: FactInput): RowFact[] {
  const attention = item.attentionOn ? attentionFact(group, formatAttentionDate(item.attentionOn, evaluatedOn, locale), t) : null;
  const responsible = label === null ? null : { kind: 'responsible' as const, text: label, tone: 'muted' as const, icon: null };
  return [attention, responsible, detailsFact(item, locale, t)].filter((fact): fact is RowFact => fact !== null);
}
