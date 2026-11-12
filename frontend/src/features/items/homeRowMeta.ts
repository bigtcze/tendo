import type { Translate } from '../../i18n';
import type { Member } from '../members/membersApi';
import { formatAttentionDate, formatDate } from './formatDate';
import type { Item, ItemGroup } from './itemsApi';
import { repeatSummary } from './repeatSummary';

export function responsibleLabels(
  item: Item, members: Member[], membersLoaded: boolean, userId: string, t: Translate,
): { responsibleLabel: string | null } {
  if (!item.responsibleUserId || !membersLoaded) return { responsibleLabel: null };
  const member = members.find((entry) => entry.userId === item.responsibleUserId);
  if (!member) return { responsibleLabel: t('items.responsible.unknown') };
  if (member.userId === userId) return { responsibleLabel: t('items.responsible.you') };
  return { responsibleLabel: t('items.responsible.read', { name: member.login }) };
}

export type RowFact = {
  kind: 'attention' | 'responsible' | 'details';
  /** The one wording used at every width; a fact is never hidden on small screens. */
  text: string;
  /** The part of the text that carries the attention colour, if any. */
  emphasis?: string;
  tone: 'attention' | 'muted';
  icon: 'alert' | 'calendar' | null;
};

/** The group heading already says "needs attention", so the row names the date and marks it quietly. */
function attentionFact(group: ItemGroup, date: string, t: Translate): RowFact {
  if (group === 'needs') {
    return {
      kind: 'attention',
      text: t('items.needsSinceCompact', { date }),
      emphasis: date,
      tone: 'attention',
      icon: 'alert',
    };
  }
  return {
    kind: 'attention',
    text: group === 'upcoming' ? date : t('items.rowAttentionDate', { date }),
    tone: 'muted',
    icon: 'calendar',
  };
}

function responsibleFact(label: string): RowFact {
  return { kind: 'responsible', text: label, tone: 'muted', icon: null };
}

function detailsFact(item: Item, locale: string, t: Translate): RowFact | null {
  const details = [
    item.lastCompletedOn ? t('itemDetail.lastDone', { date: formatDate(item.lastCompletedOn, locale) }) : null,
    item.recurrence ? repeatSummary(t, item.recurrence, locale) : null,
  ].filter(Boolean).join(' · ');
  return details ? { kind: 'details', text: details, tone: 'muted', icon: null } : null;
}

/**
 * The row's independent facts in reading order: the date that needs attention, who looks after it,
 * then last completion and repeat cadence. Nothing is substituted for anything else.
 */
export function rowFacts({ item, group, evaluatedOn, locale, t, responsibleLabel }: {
  item: Item;
  group: ItemGroup;
  evaluatedOn: string;
  locale: string;
  t: Translate;
  responsibleLabel: string | null;
}): RowFact[] {
  const attention = item.attentionOn ? attentionFact(group, formatAttentionDate(item.attentionOn, evaluatedOn, locale), t) : null;
  const responsible = responsibleLabel === null ? null : responsibleFact(responsibleLabel);
  return [attention, responsible, detailsFact(item, locale, t)].filter((fact): fact is RowFact => fact !== null);
}
