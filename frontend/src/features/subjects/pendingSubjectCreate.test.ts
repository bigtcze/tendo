import { afterEach, describe, expect, it, vi } from 'vitest';
import { clearPendingSubjectCreate, clearPendingSubjectCreates, getPendingSubjectCreate, setPendingSubjectCreate } from './pendingSubjectCreate';

const user = 'user-a';
const household = 'household-a';
const attempt = { version: 1 as const, key: 'ab'.repeat(16), body: { name: 'Name', type: 'person' as const } };
const storageDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'sessionStorage');

afterEach(() => {
  vi.restoreAllMocks();
  Object.defineProperty(globalThis, 'sessionStorage', storageDescriptor!);
  clearPendingSubjectCreates();
});

describe('pending subject create storage', () => {
  it('scopes records by user and household and ignores corrupt JSON', () => {
    setPendingSubjectCreate(user, household, attempt);
    expect(getPendingSubjectCreate(user, 'other-household')).toBeUndefined();
    expect(getPendingSubjectCreate('other-user', household)).toBeUndefined();
    sessionStorage.setItem('tendo.pendingSubjectCreate.v1:corrupt:scope', '{bad');
    expect(getPendingSubjectCreate('corrupt', 'scope')).toBeUndefined();
  });

  it.each(['getter', 'setItem'] as const)('uses memory fallback when storage %s is unavailable', (failure) => {
    const next = { ...attempt, key: 'cd'.repeat(16) };
    if (failure === 'getter') Object.defineProperty(globalThis, 'sessionStorage', { configurable: true, get() { throw new Error('blocked'); } });
    else vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
    setPendingSubjectCreate(user, household, next);
    expect(getPendingSubjectCreate(user, household)).toEqual(next);
    clearPendingSubjectCreate(user, household, next.key);
    expect(getPendingSubjectCreate(user, household)).toBeUndefined();
  });

  it.each(['obsolete', 'current'] as const)('preserves authoritative memory after failed storage writes (%s settle)', (settlement) => {
    const next = { ...attempt, key: 'cd'.repeat(16), body: { ...attempt.body, name: 'Next' } };
    sessionStorage.setItem('tendo.pendingSubjectCreate.v1:user-a:household-a', JSON.stringify(attempt));
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => { throw new Error('blocked'); });
    clearPendingSubjectCreate(user, household, attempt.key);
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
    setPendingSubjectCreate(user, household, next);
    clearPendingSubjectCreate(user, household, settlement === 'obsolete' ? attempt.key : next.key);
    expect(getPendingSubjectCreate(user, household)).toEqual(settlement === 'obsolete' ? next : undefined);
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked'); });
    expect(getPendingSubjectCreate(user, household)).toEqual(settlement === 'obsolete' ? next : undefined);
  });
});
