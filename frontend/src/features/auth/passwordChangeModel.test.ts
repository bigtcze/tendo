import { describe, expect, it } from 'vitest';
import { changeProblem, fieldFailure, formMessageKey } from './passwordChangeModel';

const OK = 'a long enough password';

describe('password change model', () => {
  it('asks for the current password before anything else', () => {
    expect(changeProblem('', OK, OK)).toBe('currentRequired');
  });

  it('checks the new-password policy before the match, like the server', () => {
    expect(changeProblem('old', 'too short', 'different')).toBe('length');
    expect(changeProblem('old', OK, 'a long enough passwore')).toBe('mismatch');
    expect(changeProblem('old', OK, OK)).toBeNull();
  });

  it('counts code points at the policy bounds', () => {
    expect(changeProblem('old', '😀'.repeat(14), '😀'.repeat(14))).toBe('length');
    expect(changeProblem('old', '😀'.repeat(15), '😀'.repeat(15))).toBeNull();
  });

  it('points each field refusal at its field and keeps form refusals on the form', () => {
    expect(fieldFailure('currentRequired')).toEqual({ field: 'current', message: 'account.password.error.currentRequired' });
    expect(fieldFailure('incorrect')).toEqual({ field: 'current', message: 'account.password.error.current' });
    expect(fieldFailure('length')?.field).toBe('newPassword');
    expect(fieldFailure('mismatch')?.field).toBe('confirm');
    expect(fieldFailure('rateLimited')).toBeNull();
    expect(formMessageKey('rateLimited')).toBe('account.password.error.rateLimited');
    expect(formMessageKey('unavailable')).toBe('account.password.error.unavailable');
    expect(formMessageKey('incorrect')).toBeNull();
  });
});
