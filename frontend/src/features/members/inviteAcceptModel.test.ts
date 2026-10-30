import { describe, expect, it } from 'vitest';
import { en } from '../../i18n/en';
import { inviteErrorMessageKey, isFieldError, isLoginError, isPasswordError, isValidInviteToken, mapNewAccountResult, mapSignedInResult } from './inviteAcceptModel';

describe('invite acceptance model', () => {
  it.each([
    [{ kind: 'ok' }, { kind: 'accepted' }],
    [{ kind: 'invalid' }, { kind: 'error', error: 'invalid' }],
    [{ kind: 'loginTaken' }, { kind: 'error', error: 'loginTaken' }],
    [{ kind: 'invalidField', field: 'login' }, { kind: 'error', error: 'loginInvalid' }],
    [{ kind: 'invalidField', field: 'password' }, { kind: 'error', error: 'passwordInvalid' }],
    [{ kind: 'rateLimited' }, { kind: 'error', error: 'rateLimited' }],
    [{ kind: 'unavailable' }, { kind: 'error', error: 'unavailable' }],
    [{ kind: 'error' }, { kind: 'error', error: 'unavailable' }],
  ] as const)('maps new account result %o', (result, expected) => {
    expect(mapNewAccountResult(result)).toEqual(expected);
  });

  it.each([
    ['ok', { kind: 'accepted' }],
    ['invalid', { kind: 'error', error: 'invalid' }],
    ['alreadyMember', { kind: 'error', error: 'alreadyMember' }],
    ['householdConflict', { kind: 'error', error: 'householdConflict' }],
    ['unauthenticated', { kind: 'unauthenticated' }],
    ['rateLimited', { kind: 'error', error: 'rateLimited' }],
    ['error', { kind: 'error', error: 'unavailable' }],
  ] as const)('maps signed-in result %s', (result, expected) => {
    expect(mapSignedInResult(result)).toEqual(expected);
  });

  it.each([42, 44])('rejects token length %i', (length) => {
    expect(isValidInviteToken('a'.repeat(length))).toBe(false);
  });
  it('accepts exactly 43 URL-safe characters', () => {
    expect(isValidInviteToken('aZ09_-'.repeat(7).slice(0, 42) + 'a')).toBe(true);
  });
  it.each(['a'.repeat(42) + '!', 'a'.repeat(42) + '/', 'a'.repeat(42) + ' '])('rejects invalid token character', (token) => {
    expect(isValidInviteToken(token)).toBe(false);
  });
  it('classifies login and password field errors distinctly', () => {
    for (const error of ['login', 'loginTaken', 'loginInvalid'] as const) {
      expect(isFieldError(error)).toBe(true);
      expect(isLoginError(error)).toBe(true);
      expect(isPasswordError(error)).toBe(false);
    }
    for (const error of ['password', 'passwordInvalid'] as const) {
      expect(isFieldError(error)).toBe(true);
      expect(isLoginError(error)).toBe(false);
      expect(isPasswordError(error)).toBe(true);
    }
    for (const error of ['invalid', 'rateLimited', 'unavailable', 'alreadyMember', 'householdConflict'] as const) {
      expect(isFieldError(error)).toBe(false);
      expect(isLoginError(error)).toBe(false);
      expect(isPasswordError(error)).toBe(false);
    }
    expect(isFieldError(null)).toBe(false);
  });

  it.each([
    ['loginInvalid', 'invite.error.loginValidation'],
    ['passwordInvalid', 'invite.error.passwordValidation'],
    ['loginTaken', 'invite.error.loginTaken'],
    ['householdConflict', 'invite.error.householdConflict'],
  ] as const)('maps %s to an existing message key %s', (error, key) => {
    expect(inviteErrorMessageKey(error)).toBe(key);
    expect(en[key]).toBeTruthy();
  });
});
