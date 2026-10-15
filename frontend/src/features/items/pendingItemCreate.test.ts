import { afterEach, describe, expect, it, vi } from 'vitest';
import { clearPendingItemCreates, getPendingItemCreate, setPendingItemCreate } from './pendingItemCreate';

const attempt = { version: 1 as const, key: 'ab'.repeat(16), body: { title: 'Name', subjectId: 'subject-a', recurrence: null } };

afterEach(() => { clearPendingItemCreates(); vi.restoreAllMocks(); });

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

  it('keeps a memory fallback when sessionStorage writes throw', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
    setPendingItemCreate('user-a', 'household-a', attempt);
    expect(getPendingItemCreate('user-a', 'household-a')).toEqual(attempt);
  });
});
