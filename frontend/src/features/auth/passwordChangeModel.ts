import type { MessageKey } from '../../i18n';
import { passwordProblem } from './passwordResetModel';

export type ChangeField = 'current' | 'newPassword' | 'confirm';

/**
 * The refusals that point at one field. The policy and the confirmation match reuse the reset form's rules, so both
 * password forms count code points and check the policy before the match, exactly like the server.
 */
const FIELD_FAILURES = {
  currentRequired: { field: 'current', message: 'account.password.error.currentRequired' },
  incorrect: { field: 'current', message: 'account.password.error.current' },
  length: { field: 'newPassword', message: 'account.password.error.length' },
  mismatch: { field: 'confirm', message: 'account.password.error.mismatch' },
} as const satisfies Record<string, { field: ChangeField; message: MessageKey }>;

export type FieldFailure = keyof typeof FIELD_FAILURES;
/** The form-level refusals leave every field as it was, so the form stays usable and a retry is the whole recovery. */
export type FormFailure = 'rateLimited' | 'unavailable';
export type ChangeFailure = FieldFailure | FormFailure;

const FORM_MESSAGES: Record<FormFailure, MessageKey> = {
  rateLimited: 'account.password.error.rateLimited',
  unavailable: 'account.password.error.unavailable',
};

export function fieldFailure(failure: ChangeFailure | null): { field: ChangeField; message: MessageKey } | null {
  return failure !== null && failure in FIELD_FAILURES
    ? FIELD_FAILURES[failure as FieldFailure]
    : null;
}

export function formMessageKey(failure: ChangeFailure | null): MessageKey | null {
  return failure === 'rateLimited' || failure === 'unavailable' ? FORM_MESSAGES[failure] : null;
}

/** Client checks mirror the server order: an empty current password, then the new-password policy, then the match. */
export function changeProblem(currentPassword: string, newPassword: string, confirm: string): FieldFailure | null {
  if (currentPassword === '') return 'currentRequired';
  const problem = passwordProblem(newPassword, confirm);
  if (!problem) return null;
  return problem.failure === 'length' ? 'length' : 'mismatch';
}
