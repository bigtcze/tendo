import type { AcceptResult, acceptNewAccount } from './membersApi';
import type { Session } from '../auth/sessionApi';
import type { MessageKey } from '../../i18n';

export type ErrorState =
  | 'invalid' | 'loginTaken' | 'login' | 'loginInvalid' | 'password'
  | 'passwordInvalid' | 'rateLimited' | 'unavailable' | 'alreadyMember'
  | 'householdConflict';
export type NewAccountResult = Awaited<ReturnType<typeof acceptNewAccount>>;
export type NewAccountOutcome = { kind: 'accepted' } | { kind: 'error'; error: ErrorState };
export type SignedInOutcome = { kind: 'accepted' } | { kind: 'unauthenticated' } | { kind: 'error'; error: ErrorState };

export function isValidInviteToken(token: string | null): token is string {
  return token !== null && /^[A-Za-z0-9_-]{43}$/.test(token);
}

export function mapNewAccountResult(result: NewAccountResult): NewAccountOutcome {
  if (result.kind === 'ok') return { kind: 'accepted' };
  if (result.kind === 'invalid') return { kind: 'error', error: 'invalid' };
  if (result.kind === 'loginTaken') return { kind: 'error', error: 'loginTaken' };
  if (result.kind === 'invalidField') return { kind: 'error', error: result.field === 'password' ? 'passwordInvalid' : 'loginInvalid' };
  if (result.kind === 'rateLimited') return { kind: 'error', error: 'rateLimited' };
  return { kind: 'error', error: 'unavailable' };
}

export function mapSignedInResult(result: AcceptResult): SignedInOutcome {
  if (result === 'ok') return { kind: 'accepted' };
  if (result === 'unauthenticated') return { kind: 'unauthenticated' };
  if (result === 'invalid') return { kind: 'error', error: 'invalid' };
  if (result === 'alreadyMember') return { kind: 'error', error: 'alreadyMember' };
  if (result === 'householdConflict') return { kind: 'error', error: 'householdConflict' };
  if (result === 'rateLimited') return { kind: 'error', error: 'rateLimited' };
  return { kind: 'error', error: 'unavailable' };
}

export function isFieldError(error: ErrorState | null): boolean {
  return isLoginError(error) || isPasswordError(error);
}

export function isLoginError(error: ErrorState | null): boolean {
  return error === 'login' || error === 'loginTaken' || error === 'loginInvalid';
}

export function isPasswordError(error: ErrorState | null): boolean {
  return error === 'password' || error === 'passwordInvalid';
}

const inviteErrorMessageKeys: Record<ErrorState, MessageKey> = {
  invalid: 'invite.error.invalid',
  loginTaken: 'invite.error.loginTaken',
  login: 'invite.error.login',
  loginInvalid: 'invite.error.loginValidation',
  password: 'invite.error.password',
  passwordInvalid: 'invite.error.passwordValidation',
  rateLimited: 'invite.error.rateLimited',
  unavailable: 'invite.error.unavailable',
  alreadyMember: 'invite.error.alreadyMember',
  householdConflict: 'invite.error.householdConflict',
};

export function inviteErrorMessageKey(error: ErrorState): MessageKey {
  return inviteErrorMessageKeys[error];
}

export type InviteCallbacks = {
  onSignedIn: (session: Session) => void;
  onSignedOut: () => void;
  onAccountReady: () => void;
  onJoinedSignedOut: () => void;
};
