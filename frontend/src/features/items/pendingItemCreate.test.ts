import { afterEach, describe, expect, it, vi } from 'vitest';
import { clearPendingItemCreate, clearPendingItemCreates, getPendingItemCreate, setPendingItemCreate } from './pendingItemCreate';

const attempt = { version: 1 as const, key: 'ab'.repeat(16), body: { title: 'Name', subjectId: 'subject-a', recurrence: null } };
const storageDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'sessionStorage');

afterEach(() => {
  vi.restoreAllMocks();
  Object.defineProperty(globalThis, 'sessionStorage', storageDescriptor!);
  clearPendingItemCreates();
});

describe('pending item create storage', () => {
  it('scopes records by user and household and rejects corrupt or invalid records', () => {
    setPendingItemCreate('user-a', 'household-a', attempt);
    expect(getPendingItemCreate('user-a', 'other-household')).toBeUndefined();
    expect(getPendingItemCreate('other-user', 'household-a')).toBeUndefined();
    sessionStorage.setItem('tendo.pendingItemCreate.v1:corrupt:scope', '{bad');
    expect(getPendingItemCreate('corrupt', 'scope')).toBeUndefined();
    sessionStorage.setItem('tendo.pendingItemCreate.v1:invalid:scope', JSON.stringify({ ...attempt, body: { ...attempt.body, recurrence: { intervalValue: '1' } } }));
    expect(getPendingItemCreate('invalid', 'scope')).toBeUndefined();
  });

  it('restores an attempt from sessionStorage with a fresh module memory map', async () => {
    sessionStorage.setItem('tendo.pendingItemCreate.v1:user-a:household-a', JSON.stringify(attempt));
    vi.resetModules();
    const storageModule = await import('./pendingItemCreate');
    expect(storageModule.getPendingItemCreate('user-a', 'household-a')).toEqual(attempt);
  });

  it.each(['getter', 'setItem'] as const)('uses memory fallback when storage %s is unavailable', (failure) => {
    const next = { ...attempt, key: 'cd'.repeat(16) };
    if (failure === 'getter') Object.defineProperty(globalThis, 'sessionStorage', { configurable: true, get() { throw new Error('blocked'); } });
    else vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
    setPendingItemCreate('user-a', 'household-a', next);
    expect(getPendingItemCreate('user-a', 'household-a')).toEqual(next);
    clearPendingItemCreate('user-a', 'household-a', next.key);
    expect(getPendingItemCreate('user-a', 'household-a')).toBeUndefined();
  });

  it.each(['obsolete', 'current'] as const)('preserves authoritative memory after failed storage writes (%s settle)', (settlement) => {
    const next = { ...attempt, key: 'cd'.repeat(16), body: { ...attempt.body, title: 'Next' } };
    sessionStorage.setItem('tendo.pendingItemCreate.v1:user-a:household-a', JSON.stringify(attempt));
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => { throw new Error('blocked'); });
    clearPendingItemCreate('user-a', 'household-a', attempt.key);
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
    setPendingItemCreate('user-a', 'household-a', next);
    clearPendingItemCreate('user-a', 'household-a', settlement === 'obsolete' ? attempt.key : next.key);
    expect(getPendingItemCreate('user-a', 'household-a')).toEqual(settlement === 'obsolete' ? next : undefined);
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked'); });
    expect(getPendingItemCreate('user-a', 'household-a')).toEqual(settlement === 'obsolete' ? next : undefined);
  });
});
