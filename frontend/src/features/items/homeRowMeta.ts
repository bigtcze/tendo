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
};

export type RowFacts = {
  /** Last completion and recurrence summary, both real fields when present. */
  details: string | null;
  /** Attention status; needs uses "since", every other group uses the planned date. */
  attention: string | null;
  responsible: string | null;
};

/** Independent facts, never substituted for one another. */
export function rowFacts({ item, group, evaluatedOn, locale, t, responsibleLabel }: FactInput): RowFacts {
  return {
    details: [
      item.lastCompletedOn ? t('itemDetail.lastDone', { date: formatDate(item.lastCompletedOn, locale) }) : null,
      item.recurrence ? repeatSummary(t, item.recurrence, locale) : null,
    ].filter(Boolean).join(' · ') || null,
    attention: item.attentionOn
      ? group === 'needs'
        ? t('items.needsSince', { date: formatAttentionDate(item.attentionOn, evaluatedOn, locale) })
        : t('items.rowAttentionDate', { date: formatAttentionDate(item.attentionOn, evaluatedOn, locale) })
      : null,
    responsible: responsibleLabel,
  };
}

export type RowStatus = { kind: 'attention' | 'date' | 'person'; text: string } | null;

/** Desktop date/assignee column; mobile keeps the same facts in the row's metadata lines. */
export function rowStatus({ item, group, evaluatedOn, locale, t, responsibleLabel, assigneeLabel }: FactInput & { assigneeLabel?: string | null }): RowStatus {
  if (group === 'needs') {
    return item.attentionOn
      ? { kind: 'attention', text: t('items.needsSince', { date: formatAttentionDate(item.attentionOn, evaluatedOn, locale) }) }
      : null;
  }
  if (group === 'upcoming' && item.attentionOn) {
    const date = formatAttentionDate(item.attentionOn, evaluatedOn, locale);
    const assignee = assigneeLabel ?? responsibleLabel;
    return { kind: 'date', text: assignee ? `${date} · ${assignee}` : date };
  }
  return responsibleLabel ? { kind: 'person', text: responsibleLabel } : null;
}
