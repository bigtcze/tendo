import { describe, expect, it } from 'vitest';
import { isValidResetToken, passwordProblem } from './passwordResetModel';

describe('password reset model', () => {
  it('accepts only a complete 43-character base64url token', () => {
    expect(isValidResetToken('A'.repeat(43))).toBe(true);
    expect(isValidResetToken(`${'A'.repeat(42)}-`)).toBe(true);
    expect(isValidResetToken('A'.repeat(42))).toBe(false);
    expect(isValidResetToken('A'.repeat(44))).toBe(false);
    expect(isValidResetToken(`${'A'.repeat(42)}+`)).toBe(false);
    expect(isValidResetToken(null)).toBe(false);
  });

  it('counts code points at both policy bounds, like the server', () => {
    expect(passwordProblem('a'.repeat(14), 'a'.repeat(14))).toEqual({ field: 'password', failure: 'length' });
    expect(passwordProblem('a'.repeat(15), 'a'.repeat(15))).toBeNull();
    expect(passwordProblem('a'.repeat(128), 'a'.repeat(128))).toBeNull();
    expect(passwordProblem('a'.repeat(129), 'a'.repeat(129))).toEqual({ field: 'password', failure: 'length' });
    // 15 astral characters are 30 UTF-16 units but exactly 15 code points.
    expect(passwordProblem('😀'.repeat(15), '😀'.repeat(15))).toBeNull();
    expect(passwordProblem('😀'.repeat(14), '😀'.repeat(14))).toEqual({ field: 'password', failure: 'length' });
  });

  it('asks for the confirmation again when the two entries differ', () => {
    expect(passwordProblem('a long enough password', 'a long enough passwore')).toEqual({ field: 'confirm', failure: 'mismatch' });
  });
});
