import type { Translate } from '../../i18n';
import type { Member } from '../members/membersApi';
import type { Item, Subject } from './itemsApi';
import type { ItemFormValues } from './ItemForm';
import { formatDate } from './formatDate';
import { recurrenceEqual } from './itemEditModel';
import { repeatLabel } from './ItemDetailReadView';

type Props = { latest: Item; mine?: ItemFormValues; subjects: Subject[]; members: Member[]; locale: string; t: Translate };

function responsibleName({ latest, members, t }: Props) {
  if (!latest.responsibleUserId) return t('items.responsible.none');
  return members.find((member) => member.userId === latest.responsibleUserId)?.login ?? t('items.responsible.unknown');
}

function latestRepeat({ latest, locale, t }: Props) {
  if (!latest.recurrence) return t('itemDetail.edit.noRepeat');
  const { interval, mode } = repeatLabel(latest.recurrence, locale, t);
  return `${interval} · ${mode}`;
}

function subjectName({ latest, subjects, t }: Props) {
  return subjects.find((entry) => entry.id === latest.subjectId)?.name ?? t('items.subject.unknown');
}

function latestDate({ latest, locale, t }: Props) {
  return latest.attentionOn ? formatDate(latest.attentionOn, locale) : t('itemDetail.edit.noDate');
}

type FieldSpec = { key: string; wrap?: boolean; changed: (props: Props) => boolean; text: (props: Props) => string };

/** Form-ordered fields; each reports whether the latest saved value differs from the rebased draft. */
const FIELDS: FieldSpec[] = [
  { key: 'title', changed: ({ latest, mine }) => latest.title !== mine?.title,
    text: (p) => p.t('itemDetail.edit.latest.title', { value: p.latest.title }) },
  { key: 'subject', changed: ({ latest, mine }) => latest.subjectId !== mine?.subjectId,
    text: (p) => p.t('itemDetail.edit.latest.subject', { value: subjectName(p) }) },
  { key: 'responsible', changed: ({ latest, mine }) => latest.responsibleUserId !== mine?.responsibleUserId,
    text: (p) => p.t('itemDetail.edit.latest.responsible', { value: responsibleName(p) }) },
  { key: 'date', changed: ({ latest, mine }) => (latest.attentionOn ?? '') !== mine?.attentionOn,
    text: (p) => p.t('itemDetail.edit.latest.date', { value: latestDate(p) }) },
  { key: 'notes', wrap: true, changed: ({ latest, mine }) => (latest.notes ?? '') !== mine?.notes,
    text: (p) => p.t('itemDetail.edit.latest.notes', { value: p.latest.notes ?? p.t('itemDetail.edit.noNotes') }) },
  { key: 'repeat', changed: ({ latest, mine }) => !recurrenceEqual(latest.recurrence, mine?.recurrence ?? null),
    text: (p) => p.t('itemDetail.edit.latest.repeat', { value: latestRepeat(p) }) },
];

/** After a 412: one line per field where the latest saved value differs from the rebased draft. */
export function ItemEditConflictSummary(props: Props) {
  return <div className="space-y-1 rounded-xl border border-line bg-sand/70 px-4 py-3 text-sm">
    <p className="font-medium">{props.t('itemDetail.edit.conflict')}</p>
    {FIELDS.filter((field) => field.changed(props)).map((field) => (
      <p key={field.key} className={field.wrap ? 'whitespace-pre-wrap break-words' : undefined}>{field.text(props)}</p>
    ))}
  </div>;
}
