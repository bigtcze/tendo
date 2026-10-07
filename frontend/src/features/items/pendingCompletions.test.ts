import { afterEach, describe, expect, it, vi } from 'vitest';

const prefix = 'tendo.pendingCompletion:';
const keyFor = (userId: string, householdId: string, itemId: string) => `${prefix}${userId}:${householdId}:${itemId}`;

afterEach(() => {
  sessionStorage.clear();
  vi.restoreAllMocks();
});

describe('pending completion storage', () => {
  it('restores a key from sessionStorage after the module memory is reset', async () => {
    const pending = await import('./pendingCompletions');
    pending.setPendingCompletion('user-1', 'home-1', 'item-1', 'attempt-1');

    vi.resetModules();
    const reloaded = await import('./pendingCompletions');

    expect(reloaded.getPendingCompletion('user-1', 'home-1', 'item-1')).toBe('attempt-1');
  });

  it('isolates entries by user and household and clears only the selected entry', async () => {
    const pending = await import('./pendingCompletions');
    pending.setPendingCompletion('user-1', 'home-1', 'item-1', 'one');
    pending.setPendingCompletion('user-2', 'home-1', 'item-1', 'other-user');
    pending.setPendingCompletion('user-1', 'home-2', 'item-1', 'other-home');

    expect(pending.getPendingCompletion('user-2', 'home-1', 'item-1')).toBe('other-user');
    expect(pending.getPendingCompletion('user-1', 'home-2', 'item-1')).toBe('other-home');
    pending.clearPendingCompletion('user-1', 'home-1', 'item-1');
    expect(pending.getPendingCompletion('user-1', 'home-1', 'item-1')).toBeUndefined();
    expect(pending.getPendingCompletion('user-2', 'home-1', 'item-1')).toBe('other-user');
    expect(pending.getPendingCompletion('user-1', 'home-2', 'item-1')).toBe('other-home');
  });

  it('clears only prefixed sessionStorage keys', async () => {
    const pending = await import('./pendingCompletions');
    sessionStorage.setItem(keyFor('user-1', 'home-1', 'item-1'), 'attempt-1');
    sessionStorage.setItem('unrelated', 'keep');

    pending.clearPendingCompletions();

    expect(sessionStorage.getItem(keyFor('user-1', 'home-1', 'item-1'))).toBeNull();
    expect(sessionStorage.getItem('unrelated')).toBe('keep');
  });

  it('falls back to memory when sessionStorage access throws', async () => {
    const pending = await import('./pendingCompletions');
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('storage unavailable'); });
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('storage unavailable'); });

    pending.setPendingCompletion('user-1', 'home-1', 'item-1', 'attempt-1');

    expect(pending.getPendingCompletion('user-1', 'home-1', 'item-1')).toBe('attempt-1');
  });
});
