import { describe, expect, it } from 'vitest';
import { errorKey, errorTarget, firstEmptyField, timezoneOptions } from './onboardingModel';

describe('onboarding model', () => {
  it.each([
    [{ kind: 'setupCode' }, 'onboarding.error.setupCode'],
    [{ kind: 'field', field: 'timezone' }, 'onboarding.error.timezone'],
    [{ kind: 'empty', field: 'setupCode' }, 'onboarding.error.setupCodeEmpty'],
    [{ kind: 'empty', field: 'householdName' }, 'onboarding.error.householdNameEmpty'],
    [{ kind: 'empty', field: 'login' }, 'onboarding.error.loginEmpty'],
    [{ kind: 'empty', field: 'password' }, 'onboarding.error.passwordEmpty'],
    [{ kind: 'empty', field: 'timezone' }, 'onboarding.error.timezoneEmpty'],
    [{ kind: 'setupDisabled' }, 'onboarding.error.setupDisabled'],
    [{ kind: 'rateLimited' }, 'onboarding.error.rateLimited'],
    [{ kind: 'unavailable' }, 'onboarding.error.unavailable'],
  ] as const)('maps %o to its message key', (error, expected) => {
    expect(errorKey(error)).toBe(expected);
  });

  it.each([
    [{ kind: 'setupCode' }, 'setupCode'],
    [{ kind: 'field', field: 'password' }, 'password'],
    [{ kind: 'empty', field: 'householdName' }, 'householdName'],
    [{ kind: 'setupDisabled' }, null],
    [{ kind: 'rateLimited' }, null],
    [{ kind: 'unavailable' }, null],
  ] as const)('maps %o to focus target %s', (error, target) => {
    expect(errorTarget(error)).toBe(target);
  });

  it('validates trimmed setup code and household name before untrimmed login and password', () => {
    expect(firstEmptyField('', '', '', '')).toBe('setupCode');
    expect(firstEmptyField('', 'Home', 'login', 'password')).toBe('setupCode');
    expect(firstEmptyField(' \t ', 'Home', 'login', 'password')).toBe('setupCode');
    expect(firstEmptyField('token', '', '', '')).toBe('householdName');
    expect(firstEmptyField('token', '', 'login', 'password')).toBe('householdName');
    expect(firstEmptyField('token', ' \t ', 'login', 'password')).toBe('householdName');
    expect(firstEmptyField('token', 'Home', '', '')).toBe('login');
    expect(firstEmptyField('token', 'Home', ' ', 'password')).toBeNull();
    expect(firstEmptyField('token', 'Home', 'login', '')).toBe('password');
    expect(firstEmptyField('token', 'Home', 'login', 'password')).toBeNull();
  });

  it('deduplicates and sorts supported zones while including UTC and the detected zone', () => {
    expect(timezoneOptions('Europe/Prague', ['UTC', 'Europe/Prague', 'America/New_York', 'UTC']))
      .toEqual(['America/New_York', 'Europe/Prague', 'UTC']);
    expect(timezoneOptions('Pacific/Nowhere', ['Europe/Prague']))
      .toEqual(['Europe/Prague', 'Pacific/Nowhere', 'UTC']);
  });
});
