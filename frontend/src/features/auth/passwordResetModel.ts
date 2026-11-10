import type { MessageKey } from '../../i18n';
import { codePointLength } from '../../lib/textLength';

export const PASSWORD_MIN = 15;
export const PASSWORD_MAX = 128;

export type ResetField = 'password' | 'confirm';
export type ResetFailure = 'length' | 'mismatch' | 'invalidLink' | 'rateLimited' | 'unavailable';

/** The issued token is 32 random bytes in base64url, so a malformed or missing fragment never reaches the API. */
export function isValidResetToken(token: string | null): token is string {
  return token !== null && /^[A-Za-z0-9_-]{43}$/.test(token);
}

/** Client checks mirror the server policy and count code points, like the API does. */
export function passwordProblem(password: string, confirm: string): { field: ResetField; failure: 'length' | 'mismatch' } | null {
  const length = codePointLength(password);
  if (length < PASSWORD_MIN || length > PASSWORD_MAX) return { field: 'password', failure: 'length' };
  if (password !== confirm) return { field: 'confirm', failure: 'mismatch' };
  return null;
}

const failureKeys: Record<Exclude<ResetFailure, 'invalidLink'>, MessageKey> = {
  length: 'reset.error.length',
  mismatch: 'reset.error.mismatch',
  rateLimited: 'reset.error.rateLimited',
  unavailable: 'reset.error.unavailable',
};

export function resetFailureKey(failure: ResetFailure): MessageKey {
  return failure === 'invalidLink' ? 'reset.invalid.body' : failureKeys[failure];
}

export function failureField(failure: ResetFailure | null): ResetField | null {
  if (failure === 'length') return 'password';
  if (failure === 'mismatch') return 'confirm';
  return null;
}
