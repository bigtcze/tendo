import type { SubjectValues } from './SubjectForm';
import type { PendingSubjectCreate } from './pendingSubjectCreate';
import type { Subject } from './subjectsApi';
import type { Member } from '../members/membersApi';
import { typeLabelKey } from './SubjectForm';
import type { MessageKey } from '../../i18n';

export type ListState =
  | { kind: 'loading' }
  | { kind: 'error' }
  | { kind: 'noHousehold' }
  | { kind: 'ready'; items: Subject[]; nextCursor: string | null };

export type Notice =
  | { kind: 'added'; subject: Subject }
  | { kind: 'archived'; subject: Subject }
  | { kind: 'restored'; subject: Subject }
  | { kind: 'gone' }
  | { kind: 'changed' }
  | { kind: 'uncertain' }
  | { kind: 'reused' };

export type Editing = { subject: Subject; etag: string; conflict: boolean; mine?: SubjectValues };

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

export function subjectChanges(values: SubjectValues, subject: Subject) {
  return {
    ...(values.name !== subject.name ? { name: values.name } : {}),
    ...(values.type !== subject.type ? { type: values.type } : {}),
    ...(values.linkedUserId !== subject.linkedUserId ? { linkedUserId: values.linkedUserId } : {}),
  };
}

export function yourEntryText(
  editing: Editing | null,
  members: Member[],
  t: (key: MessageKey, values?: Record<string, string>) => string,
): string | undefined {
  const mine = editing?.mine;
  return mine && editing && (mine.name !== editing.subject.name || mine.type !== editing.subject.type || mine.linkedUserId !== editing.subject.linkedUserId)
    ? t('subjects.notice.yourEntry', {
        name: `${mine.name.trim()} (${t(typeLabelKey(mine.type))}${mine.linkedUserId !== editing.subject.linkedUserId
          ? `, ${mine.linkedUserId
            ? t('subjects.link.row', {
                login: members.find((member) => member.userId === mine.linkedUserId)?.login ?? t('subjects.link.unknown'),
              })
            : t('subjects.link.none')}`
          : ''})`,
      })
    : undefined;
}

export function pendingFormInitial(createUncertain: boolean, pendingCreate: PendingSubjectCreate | undefined): SubjectValues {
  return createUncertain
    ? {
        name: pendingCreate?.body.name ?? '',
        type: pendingCreate?.body.type ?? 'person',
        linkedUserId: pendingCreate?.body.linkedUserId ?? null,
      }
    : { name: '', type: 'person', linkedUserId: null };
}
