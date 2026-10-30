import type { MessageKey } from '../../i18n';
import type { Invalid, SubjectType } from './subjectsApi';

export interface SubjectValues { name: string; type: SubjectType; linkedUserId: string | null }
export type SubmitOutcome = { kind: 'done' } | { kind: 'handled' } | { kind: 'failed' } | Invalid;
export type FormError = { kind: 'empty' } | Invalid | { kind: 'failed' };
export type ErrorTarget = 'name' | 'type' | 'linkedUserId' | null;

const typeLabels: Record<SubjectType, MessageKey> = {
  person: 'subjects.type.person', home: 'subjects.type.home', vehicle: 'subjects.type.vehicle',
  pet: 'subjects.type.pet', custom: 'subjects.type.custom',
};
export function typeLabelKey(type: SubjectType): MessageKey { return typeLabels[type]; }

export function errorKey(error: FormError): MessageKey {
  if (error.kind === 'empty') return 'subjects.error.nameEmpty';
  if (error.kind === 'failed') return 'subjects.error.save';
  if (error.field === 'type') return 'subjects.error.type';
  if (error.field === 'linkedUserId') {
    return error.code === 'requires_person' ? 'subjects.error.linkRequiresPerson' : 'subjects.error.linkInvalid';
  }
  if (error.code === 'invalid_characters') return 'subjects.error.nameCharacters';
  if (error.code === 'invalid_length') return 'subjects.error.nameLength';
  return 'subjects.error.nameInvalid';
}
export function errorTarget(error: FormError): ErrorTarget {
  if (error.kind === 'empty') return 'name';
  if (error.kind === 'invalid') return error.field;
  return null;
}
export function hasEmptyName(name: string): boolean { return !name.trim(); }
export function submittedValues(name: string, type: SubjectType, linkedUserId: string | null): SubjectValues {
  return { name, type, linkedUserId: type === 'person' ? linkedUserId : null };
}
export function shouldShowLinkedAccount(type: SubjectType, membersLoaded: boolean, memberCount: number, linkedUserId: string | null): boolean {
  return type === 'person' && membersLoaded && (memberCount > 0 || linkedUserId !== null);
}
export function hasUnknownMember(linkedUserId: string | null, members: { userId: string }[]): linkedUserId is string {
  return Boolean(linkedUserId) && !members.some((member) => member.userId === linkedUserId);
}
export function shouldShowClearedLinkNotice(type: SubjectType, initialLinkedUserId: string | null, linkedUserId: string | null): boolean {
  return type !== 'person' && Boolean(initialLinkedUserId) && Boolean(linkedUserId);
}
