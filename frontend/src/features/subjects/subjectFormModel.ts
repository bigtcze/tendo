import type { MessageKey } from '../../i18n';
import type { Invalid, SubjectType } from './subjectsApi';

export interface SubjectValues { name: string; type: SubjectType }
/** `locked`/`exists` are member-person conflicts: the client's view is stale, so the save cannot proceed. */
export type SubmitOutcome =
  | { kind: 'done' }
  | { kind: 'handled' }
  | { kind: 'failed' }
  | { kind: 'locked' }
  | { kind: 'exists' }
  | Invalid;
export type FormError = { kind: 'empty' } | { kind: 'locked' } | { kind: 'exists' } | Invalid | { kind: 'failed' };
export type ErrorTarget = 'name' | 'type' | null;

const typeLabels: Record<SubjectType, MessageKey> = {
  person: 'subjects.type.person', home: 'subjects.type.home', vehicle: 'subjects.type.vehicle',
  pet: 'subjects.type.pet', custom: 'subjects.type.custom',
};
export function typeLabelKey(type: SubjectType): MessageKey { return typeLabels[type]; }

export function errorKey(error: FormError): MessageKey {
  if (error.kind === 'empty') return 'subjects.error.nameEmpty';
  if (error.kind === 'failed') return 'subjects.error.save';
  if (error.kind === 'locked') return 'subjects.error.memberPersonLocked';
  if (error.kind === 'exists') return 'subjects.error.memberPersonExists';
  if (error.field === 'type') return 'subjects.error.type';
  if (error.code === 'invalid_characters') return 'subjects.error.nameCharacters';
  if (error.code === 'invalid_length') return 'subjects.error.nameLength';
  return 'subjects.error.nameInvalid';
}
export function errorTarget(error: FormError): ErrorTarget {
  if (error.kind === 'empty') return 'name';
  // Only the fields this form renders can receive focus; any other server field stays a generic message.
  if (error.kind === 'invalid') return error.field === 'name' || error.field === 'type' ? error.field : null;
  return null;
}
export function hasEmptyName(name: string): boolean { return !name.trim(); }
export function submittedValues(name: string, type: SubjectType): SubjectValues {
  return { name, type };
}
