import type { MessageKey } from '../../i18n';
import { codePointLength } from '../../lib/textLength';
import type { Invalid, Recurrence } from './itemsApi';

export type ItemFormValues = {
  title: string;
  subjectId: string;
  attentionOn: string;
  notes: string;
  recurrence: Recurrence | null;
  responsibleUserId: string | null;
  historicalCompletedOn?: string;
};
export type ItemFormOutcome = { kind: 'done' | 'handled' | 'failed' } | Invalid;
export type FormError = Invalid | { kind: 'empty' } | { kind: 'failed' };
export type FormField = Invalid['field'];
export const units = ['day', 'week', 'month', 'year'] as const;
export type IntervalUnit = (typeof units)[number];

/** Editable form state: raw control values, before conversion to the submitted shape. */
export type ItemDraft = {
  title: string;
  subjectId: string;
  attentionOn: string;
  notes: string;
  responsibleUserId: string | null;
  historicalCompletedOn: string;
  repeat: boolean;
  fluid: boolean;
  interval: string;
  unit: IntervalUnit;
};

export function initialDraft(initial: ItemFormValues | undefined, subjects: { id: string }[]): ItemDraft {
  if (!initial) {
    return {
      title: '', subjectId: subjects[0]?.id ?? '', attentionOn: '', notes: '', responsibleUserId: null,
      historicalCompletedOn: '', repeat: false, fluid: false, interval: '1', unit: 'year',
    };
  }
  return {
    ...recurrenceDraft(initial.recurrence),
    title: initial.title, subjectId: initial.subjectId, attentionOn: initial.attentionOn, notes: initial.notes,
    responsibleUserId: initial.responsibleUserId, historicalCompletedOn: initial.historicalCompletedOn ?? '',
  };
}

function recurrenceDraft(recurrence: Recurrence | null): Pick<ItemDraft, 'repeat' | 'fluid' | 'interval' | 'unit'> {
  if (!recurrence) return { repeat: false, fluid: false, interval: '1', unit: 'year' };
  return { repeat: true, fluid: recurrence.mode === 'after_completion', interval: String(recurrence.intervalValue), unit: recurrence.intervalUnit };
}

/** The values a form shows before anything was submitted; restored if a lock is lifted without a submission. */
export function baselineValues(draft: ItemDraft, initial: ItemFormValues | undefined): ItemFormValues {
  return {
    title: draft.title, subjectId: draft.subjectId, attentionOn: draft.attentionOn, notes: draft.notes,
    recurrence: initial?.recurrence ?? null, responsibleUserId: draft.responsibleUserId, historicalCompletedOn: initial?.historicalCompletedOn,
  };
}

/** Puts the last submitted values back into the controls. Repeat settings are kept when nothing repeating was submitted. */
export function restoredDraft(current: ItemDraft, saved: ItemFormValues): ItemDraft {
  return {
    ...current,
    ...(saved.recurrence ? recurrenceDraft(saved.recurrence) : {}),
    title: saved.title, subjectId: saved.subjectId, attentionOn: saved.attentionOn, notes: saved.notes,
    responsibleUserId: saved.responsibleUserId, historicalCompletedOn: saved.historicalCompletedOn ?? '',
  };
}

export function submittedValues(draft: ItemDraft, mode: 'add' | 'edit'): ItemFormValues {
  const recurrence = draft.repeat
    ? { intervalValue: Number(draft.interval), intervalUnit: draft.unit, mode: draft.fluid ? 'after_completion' as const : 'fixed' as const }
    : null;
  const withHistory = mode === 'add' && draft.repeat && draft.historicalCompletedOn;
  return {
    title: draft.title, subjectId: draft.subjectId, attentionOn: draft.attentionOn, notes: draft.notes, recurrence,
    responsibleUserId: draft.responsibleUserId, ...(withHistory ? { historicalCompletedOn: draft.historicalCompletedOn } : {}),
  };
}

/** Client checks that need no server round trip, in field order. */
export function localError({ title, subjectId, repeat, interval, notes }: ItemDraft): FormError | null {
  if (!title.trim()) return { kind: 'empty' };
  if (!subjectId) return { kind: 'invalid', field: 'subjectId', code: 'invalid_reference' };
  const count = Number(interval);
  if (repeat && (!Number.isInteger(count) || count < 1 || count > 999)) return { kind: 'invalid', field: 'recurrence', code: 'invalid_interval' };
  // The server counts code points; the HTML maxlength attribute counts UTF-16 units and would stop emoji early.
  if (codePointLength(notes) > 4000) return { kind: 'invalid', field: 'notes', code: 'invalid_length' };
  return null;
}

export function errorField(error: FormError | null): FormField | null {
  if (!error || error.kind === 'failed') return null;
  return error.kind === 'empty' ? 'title' : error.field;
}

const fieldMessages: Partial<Record<FormField, MessageKey>> = {
  subjectId: 'items.error.subject',
  responsibleUserId: 'items.error.responsible',
  attentionOn: 'items.error.date',
  notes: 'items.error.notes',
};
const historicalMessages = new Map<string, MessageKey>([
  ['future_date', 'items.error.historicalFuture'],
  ['conflicting_fields', 'items.error.historicalConflict'],
  ['requires_recurrence', 'items.error.historicalRecurrence'],
]);

function invalidMessageKey(error: Invalid): MessageKey {
  if (error.field === 'title') return error.code === 'invalid_length' ? 'items.error.titleLength' : 'items.error.titleInvalid';
  if (error.field === 'historicalCompletedOn') return historicalMessages.get(error.code) ?? 'items.error.historicalDate';
  if (error.field === 'recurrence' && error.code === 'date_overflow') return 'items.error.dateOverflow';
  return fieldMessages[error.field] ?? 'items.error.interval';
}

export function errorMessage(error: FormError | null, t: (key: MessageKey) => string): string {
  if (!error) return '';
  if (error.kind === 'empty') return t('items.error.titleEmpty');
  if (error.kind === 'failed') return t('items.error.save');
  return t(invalidMessageKey(error));
}
