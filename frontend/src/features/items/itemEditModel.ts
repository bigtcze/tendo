import type { MessageKey } from '../../i18n';
import type { Item, ItemField, Recurrence } from './itemsApi';
import type { ItemFormValues } from './ItemForm';

/** Fields the detail edit form owns; a PATCH body only ever carries the ones the user changed. */
export type EditPatch = Partial<Pick<Item, 'title' | 'subjectId' | 'attentionOn' | 'notes' | 'recurrence' | 'responsibleUserId'>>;
export type Editing = { etag: string; baseline: Item; conflict: boolean; latest?: Item; mine?: ItemFormValues };

const EDITABLE_FIELDS: readonly ItemField[] = ['title', 'subjectId', 'attentionOn', 'notes', 'recurrence', 'responsibleUserId'];

export function recurrenceEqual(a: Recurrence | null, b: Recurrence | null) {
  if (a === null || b === null) return a === b;
  return a.intervalValue === b.intervalValue && a.intervalUnit === b.intervalUnit && a.mode === b.mode;
}

/** Blank notes are stored as null; non-blank notes are kept exactly, including surrounding whitespace. */
export function normalizeNotes(value: string | null | undefined): string | null {
  return value?.trim() ? value : null;
}

function sameValue(field: keyof ItemFormValues, a: ItemFormValues, b: ItemFormValues) {
  if (field === 'recurrence') return recurrenceEqual(a.recurrence, b.recurrence);
  if (field === 'notes') return normalizeNotes(a.notes) === normalizeNotes(b.notes);
  return a[field] === b[field];
}

export function formValues(item: Item): ItemFormValues {
  return {
    title: item.title, subjectId: item.subjectId, attentionOn: item.attentionOn ?? '', notes: item.notes ?? '',
    recurrence: item.recurrence, responsibleUserId: item.responsibleUserId,
  };
}

/** Builds the PATCH body from the fields that differ from the edit-open baseline. */
export function changedEditFields(values: ItemFormValues, original: Item): EditPatch {
  const recurrence: Recurrence | null = values.recurrence;
  return {
    ...(values.title !== original.title ? { title: values.title } : {}),
    ...(values.subjectId !== original.subjectId ? { subjectId: values.subjectId } : {}),
    ...(values.attentionOn !== (original.attentionOn ?? '') ? { attentionOn: values.attentionOn || null } : {}),
    ...(normalizeNotes(values.notes) !== normalizeNotes(original.notes) ? { notes: normalizeNotes(values.notes) } : {}),
    ...(!recurrenceEqual(recurrence, original.recurrence) ? { recurrence } : {}),
    ...(values.responsibleUserId !== original.responsibleUserId ? { responsibleUserId: values.responsibleUserId } : {}),
  };
}

/** Three-way rebase after a 412: fields the user left untouched adopt the latest value; changed fields keep the draft. */
export function rebaseEdit(values: ItemFormValues, original: Item, latest: Item): ItemFormValues {
  const before = formValues(original);
  const theirs = formValues(latest);
  const fields = Object.keys(before) as (keyof ItemFormValues)[];
  return Object.fromEntries(fields.map((field) => [field, sameValue(field, values, before) ? theirs[field] : values[field]])) as ItemFormValues;
}

export function invalidEditField(field: string): ItemField | null {
  return EDITABLE_FIELDS.find((editable) => editable === field) ?? null;
}

export function pluralKey(unit: Recurrence['intervalUnit'], count: number, locale: string): MessageKey {
  const category = new Intl.PluralRules(locale).select(count);
  const supported: Record<string, readonly string[]> = { en: ['one', 'other'], cs: ['one', 'few', 'other'] };
  return `items.every.${unit}.${supported[locale]?.includes(category) ? category : 'other'}` as MessageKey;
}

export function detailStatusKey(item: Item): MessageKey {
  if (item.done) return 'itemDetail.state.done';
  if (item.workflowState === 'in_progress') return 'itemDetail.state.progress';
  if (item.workflowState === 'waiting') return 'itemDetail.state.waiting';
  if (item.workflowState === 'paused') return 'itemDetail.state.paused';
  if (item.attention === 'upcoming') return 'items.group.upcoming';
  return 'items.group.needs';
}
