import type { MessageKey } from '../../i18n';
import { getSubject } from '../subjects/subjectsApi';
import type { Member } from '../members/membersApi';
import { listActiveSubjects, type Item, type Subject } from './itemsApi';
import { collectMembers } from './collectMembers';

const MAX_SUBJECT_PAGES = 20;

export type EditChoices =
  | { kind: 'ok'; subjects: Subject[]; members: Member[] }
  | { kind: 'stale' } | { kind: 'unauthenticated' } | { kind: 'gone' }
  | { kind: 'failed'; notice: MessageKey };

/**
 * Loads what the edit form can choose from: up to 20 pages of active subjects plus the item's own
 * subject when it is archived or beyond the cap, then members. A failed member list only hides the
 * assignee chooser; a failed subject list stops the edit. `isCurrent` is checked after every request.
 */
export async function loadEditChoices(householdId: string, item: Item, isCurrent: () => boolean): Promise<EditChoices> {
  const subjects: Subject[] = [];
  let cursor: string | undefined;
  for (let page = 0; page < MAX_SUBJECT_PAGES; page++) {
    const listed = await listActiveSubjects(householdId, false, cursor);
    if (!isCurrent()) return { kind: 'stale' };
    if (listed.kind === 'unauthenticated') return { kind: 'unauthenticated' };
    if (listed.kind !== 'ok') return { kind: 'failed', notice: 'items.subjects.error' };
    subjects.push(...listed.subjects);
    if (!listed.nextCursor) break;
    cursor = listed.nextCursor;
  }
  if (!subjects.some((subject) => subject.id === item.subjectId)) {
    const selected = await getSubject(householdId, item.subjectId);
    if (!isCurrent()) return { kind: 'stale' };
    if (selected.kind === 'unauthenticated') return { kind: 'unauthenticated' };
    if (selected.kind === 'notFound') return { kind: 'gone' };
    if (selected.kind !== 'ok') return { kind: 'failed', notice: 'itemDetail.actionFailed' };
    subjects.push(selected.subject);
  }
  const members = await collectMembers(householdId, isCurrent);
  if (members.kind === 'stale') return { kind: 'stale' };
  if (members.kind === 'unauthenticated') return { kind: 'unauthenticated' };
  return { kind: 'ok', subjects, members: members.kind === 'ok' ? members.members : [] };
}
