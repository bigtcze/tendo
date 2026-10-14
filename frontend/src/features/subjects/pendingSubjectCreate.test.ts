import { afterEach, describe, expect, it, vi } from 'vitest';
import { clearPendingSubjectCreates, getPendingSubjectCreate, setPendingSubjectCreate } from './pendingSubjectCreate';

const user = 'user-a';
const household = 'household-a';
const attempt = { version: 1 as const, key: 'ab'.repeat(16), body: { name: 'Name', type: 'person' as const } };

afterEach(() => {
  clearPendingSubjectCreates();
  vi.restoreAllMocks();
});

describe('pending subject create storage', () => {
  it('scopes records by user and household and ignores corrupt JSON', () => {
    setPendingSubjectCreate(user, household, attempt);
    expect(getPendingSubjectCreate(user, 'other-household')).toBeUndefined();
    expect(getPendingSubjectCreate('other-user', household)).toBeUndefined();
    sessionStorage.setItem('tendo.pendingSubjectCreate.v1:corrupt:scope', '{bad');
    expect(getPendingSubjectCreate('corrupt', 'scope')).toBeUndefined();
  });

  it('keeps a memory fallback when sessionStorage writes throw', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
    setPendingSubjectCreate(user, household, attempt);
    expect(getPendingSubjectCreate(user, household)).toEqual(attempt);
  });
});
