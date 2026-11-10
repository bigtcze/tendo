import type { SubjectValues } from './SubjectForm';
import type { PendingSubjectCreate } from './pendingSubjectCreate';
import type { Subject } from './subjectsApi';
import { typeLabelKey } from './SubjectForm';
import type { MessageKey } from '../../i18n';

export type ListState =
  | { kind: 'loading' }
  | { kind: 'error' }
  | { kind: 'noHousehold' }
  | { kind: 'ready'; items: Subject[]; nextCursor: string | null };

/** A reassociation the server refused because the client's view is stale. */
export type TransferProblem = 'changed' | 'stale' | 'notEligible' | 'notFound';

export type Notice =
  | { kind: 'added'; subject: Subject }
  | { kind: 'archived'; subject: Subject }
  | { kind: 'restored'; subject: Subject }
  | { kind: 'transferred'; subject: Subject; previous: Subject; login: string }
  | { kind: 'transferError'; code: TransferProblem }
  | { kind: 'gone' }
  | { kind: 'changed' }
  | { kind: 'uncertain' }
  | { kind: 'reused' };

export type Editing = { subject: Subject; etag: string; conflict: boolean; mine?: SubjectValues };

/** Every member has exactly one linked person; that record is locked except for its name. */
export function isMemberPerson(subject: Subject): boolean {
  return subject.type === 'person' && subject.linkedUserId !== null;
}

export function mergeById(current: Subject[], more: Subject[]): Subject[] {
  const seen = new Set(current.map((s) => s.id));
  return [...current, ...more.filter((s) => !seen.has(s.id))];
}

/** Ids are UUIDv7, so id order is creation order, the same order the server lists in. */
export function insertById(current: Subject[], subject: Subject): Subject[] {
  return mergeById(current, [subject]).sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
}

export function replaceById(items: Subject[], subject: Subject): Subject[] {
  return items.map((s) => (s.id === subject.id ? subject : s));
}

/** The member link is never sent: it only moves through the explicit member-person transfer. */
export function subjectChanges(values: SubjectValues, subject: Subject) {
  return {
    ...(values.name !== subject.name ? { name: values.name } : {}),
    ...(values.type !== subject.type ? { type: values.type } : {}),
  };
}

export function yourEntryText(
  editing: Editing | null,
  t: (key: MessageKey, values?: Record<string, string>) => string,
): string | undefined {
  const mine = editing?.mine;
  return mine && editing && (mine.name !== editing.subject.name || mine.type !== editing.subject.type)
    ? t('subjects.notice.yourEntry', {
        name: `${mine.name.trim()} (${t(typeLabelKey(mine.type))})`,
      })
    : undefined;
}

export function pendingFormInitial(createUncertain: boolean, pendingCreate: PendingSubjectCreate | undefined): SubjectValues {
  return createUncertain
    ? { name: pendingCreate?.body.name ?? '', type: pendingCreate?.body.type ?? 'person' }
    : { name: '', type: 'person' };
}
