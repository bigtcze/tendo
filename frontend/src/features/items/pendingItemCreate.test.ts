import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { clearPendingItemCreate, clearPendingItemCreates, getPendingItemCreate, setPendingItemCreate } from './pendingItemCreate';

const attempt = { version: 1 as const, key: 'ab'.repeat(16), body: { title: 'Name', subjectId: 'subject-a', recurrence: null } };
const storageDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'sessionStorage');

afterEach(() => {
  vi.restoreAllMocks();
  Object.defineProperty(globalThis, 'sessionStorage', storageDescriptor!);
  clearPendingItemCreates();
});

describe('pending item create bulk sign-out cleanup', () => {
  beforeEach(() => {
    sessionStorage.clear();
    vi.resetModules();
  });

  it('keeps a settled create absent after bulk cleanup fails', async () => {
    const pending = await import('./pendingItemCreate');
    pending.setPendingItemCreate('user-a', 'household-a', attempt);
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => { throw new Error('blocked'); });
    pending.clearPendingItemCreate('user-a', 'household-a', attempt.key);
    expect(pending.getPendingItemCreate('user-a', 'household-a')).toBeUndefined();

    pending.clearPendingItemCreates();

    expect(pending.getPendingItemCreate('user-a', 'household-a')).toBeUndefined();
  });

  it('does not recover entries unknown to memory after bulk cleanup fails', async () => {
    sessionStorage.setItem('tendo.pendingItemCreate.v1:user-a:household-a', JSON.stringify(attempt));
    const pending = await import('./pendingItemCreate');
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => { throw new Error('blocked'); });

    pending.clearPendingItemCreates();

    expect(pending.getPendingItemCreate('user-a', 'household-a')).toBeUndefined();
  });

  it('resets fully after successful bulk cleanup and persists later creates', async () => {
    const pending = await import('./pendingItemCreate');
    pending.setPendingItemCreate('user-a', 'household-a', attempt);
    pending.clearPendingItemCreates();
    expect(pending.getPendingItemCreate('user-a', 'household-a')).toBeUndefined();

    const next = { ...attempt, key: 'cd'.repeat(16) };
    pending.setPendingItemCreate('user-a', 'household-a', next);
    vi.resetModules();
    const recovered = await import('./pendingItemCreate');

    expect(recovered.getPendingItemCreate('user-a', 'household-a')).toEqual(next);
  });

  it('does not recover stored creates when storage is unavailable during cleanup', async () => {
    sessionStorage.setItem('tendo.pendingItemCreate.v1:user-a:household-a', JSON.stringify(attempt));
    const pending = await import('./pendingItemCreate');
    Object.defineProperty(globalThis, 'sessionStorage', { configurable: true, get() { throw new Error('blocked'); } });

    pending.clearPendingItemCreates();
    Object.defineProperty(globalThis, 'sessionStorage', storageDescriptor!);

    expect(pending.getPendingItemCreate('user-a', 'household-a')).toBeUndefined();
  });
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

  it.each([
    ['valid optional strings', { body: { ...attempt.body, attentionOn: '2026-01-01', notes: '', responsibleUserId: 'u', historicalCompletedOn: '' } }, true],
    ['minimum key length', { key: '!' }, true],
    ['maximum key length', { key: '!'.repeat(128) }, true],
    ['key too long', { key: '!'.repeat(129) }, false],
    ['non-object body', { body: null }, false],
    ['numeric title', { body: { ...attempt.body, title: 1 } }, false],
    ['numeric subjectId', { body: { ...attempt.body, subjectId: 1 } }, false],
    ['null notes', { body: { ...attempt.body, notes: null } }, false],
    ['null responsible user', { body: { ...attempt.body, responsibleUserId: null } }, false],
    ['null historical completion date', { body: { ...attempt.body, historicalCompletedOn: null } }, false],
    ['undefined recurrence', { body: { title: 'Name', subjectId: 'subject-a', recurrence: undefined } }, true],
    ['non-object recurrence', { body: { ...attempt.body, recurrence: 'daily' } }, false],
    ['bad recurrence mode', { body: { ...attempt.body, recurrence: { intervalValue: 1, intervalUnit: 'day', mode: 'bad' } } }, false],
    ['valid recurrence', { body: { ...attempt.body, recurrence: { intervalValue: 2, intervalUnit: 'week', mode: 'after_completion' } } }, true],
    ['null recurrence', { body: { title: 'Name', subjectId: 'subject-a', recurrence: null } }, true],
    ['null recurrence mode', { body: { ...attempt.body, recurrence: { intervalValue: 1, intervalUnit: 'day', mode: null } } }, false],
    ['numeric recurrence mode', { body: { ...attempt.body, recurrence: { intervalValue: 1, intervalUnit: 'day', mode: 1 } } }, false],
    ['array recurrence mode', { body: { ...attempt.body, recurrence: { intervalValue: 1, intervalUnit: 'day', mode: ['fixed'] } } }, false],
    ['invalid version', { version: 2 }, false],
    ['empty key', { key: '' }, false],
    ['non-ascii key', { key: 'aé' }, false],
    ['unknown body key', { body: { ...attempt.body, extra: true } }, false],
    ['null attentionOn', { body: { ...attempt.body, attentionOn: null } }, false],
    ['numeric notes', { body: { ...attempt.body, notes: 1 } }, false],
    ['non-integer recurrence', { body: { ...attempt.body, recurrence: { intervalValue: 1.5, intervalUnit: 'day', mode: 'fixed' } } }, false],
    ['bad recurrence unit', { body: { ...attempt.body, recurrence: { intervalValue: 1, intervalUnit: 'hour', mode: 'fixed' } } }, false],
  ] as const)('validates %s records through sessionStorage', (name, patch, valid) => {
    const value = { ...attempt, ...patch };
    sessionStorage.setItem(`tendo.pendingItemCreate.v1:table:${name}`, JSON.stringify(value));
    expect(getPendingItemCreate('table', name)).toEqual(valid ? value : undefined);
    if (!valid) expect(sessionStorage.getItem(`tendo.pendingItemCreate.v1:table:${name}`)).toBeNull();
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
