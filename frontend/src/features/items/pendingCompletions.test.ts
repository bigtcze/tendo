import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const prefix = 'tendo.pendingCompletion:';
const keyFor = (userId: string, householdId: string, itemId: string) => `${prefix}${userId}:${householdId}:${itemId}`;

beforeEach(() => {
  sessionStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.resetModules();
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
    pending.clearPendingCompletion('user-1', 'home-1', 'item-1', 'one');
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

  it('does not resurrect a settled key after sign-out cleanup fails', async () => {
    const pending = await import('./pendingCompletions');
    pending.setPendingCompletion('user-1', 'home-1', 'item-1', 'attempt-1');
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => { throw new Error('storage unavailable'); });

    pending.clearPendingCompletion('user-1', 'home-1', 'item-1', 'attempt-1');
    expect(pending.getPendingCompletion('user-1', 'home-1', 'item-1')).toBeUndefined();

    pending.clearPendingCompletions();

    expect(pending.getPendingCompletion('user-1', 'home-1', 'item-1')).toBeUndefined();
  });

  it('does not recover stored entries after failed sign-out cleanup when they were unknown to memory', async () => {
    const pending = await import('./pendingCompletions');
    sessionStorage.setItem(keyFor('user-1', 'home-1', 'item-1'), 'stored-attempt');
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => { throw new Error('storage unavailable'); });

    pending.clearPendingCompletions();

    expect(pending.getPendingCompletion('user-1', 'home-1', 'item-1')).toBeUndefined();
  });

  it('clears memory and storage after successful sign-out cleanup so new attempts recover', async () => {
    const pending = await import('./pendingCompletions');
    pending.setPendingCompletion('user-1', 'home-1', 'item-1', 'old-attempt');

    pending.clearPendingCompletions();

    expect(pending.getPendingCompletion('user-1', 'home-1', 'item-1')).toBeUndefined();
    pending.setPendingCompletion('user-1', 'home-1', 'item-1', 'new-attempt');
    vi.resetModules();
    const recovered = await import('./pendingCompletions');

    expect(recovered.getPendingCompletion('user-1', 'home-1', 'item-1')).toBe('new-attempt');
  });

  it('does not resurrect a settled key when removing it from storage fails', async () => {
    const pending = await import('./pendingCompletions');
    pending.setPendingCompletion('user-1', 'home-1', 'item-1', 'attempt-1');
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => { throw new Error('storage unavailable'); });

    pending.clearPendingCompletion('user-1', 'home-1', 'item-1', 'attempt-1');

    expect(pending.getPendingCompletion('user-1', 'home-1', 'item-1')).toBeUndefined();
  });

  it('keeps a newer attempt authoritative when its storage write fails', async () => {
    const pending = await import('./pendingCompletions');
    sessionStorage.setItem(keyFor('user-1', 'home-1', 'item-1'), 'attempt-1');
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('storage unavailable'); });

    pending.setPendingCompletion('user-1', 'home-1', 'item-1', 'attempt-2');

    expect(pending.getPendingCompletion('user-1', 'home-1', 'item-1')).toBe('attempt-2');
  });

  it('does not clear a newer key when an obsolete completion response settles', async () => {
    const pending = await import('./pendingCompletions');
    const outcomes = await import('./completionOutcomes');
    pending.setPendingCompletion('user-1', 'home-1', 'item-1', 'attempt-a');
    pending.setPendingCompletion('user-1', 'home-1', 'item-1', 'attempt-b');
    const tools = {
      userId: 'user-1', householdId: 'home-1', t: (key: string) => key, locale: 'en',
      refresh: vi.fn(async () => {}), setNotice: vi.fn(), onSignedOut: vi.fn(),
      focusedRowRef: { current: false }, undoFocusRef: { current: null }, noticeRef: { current: null },
    } as unknown as import('./completionOutcomes').CompletionTools;
    const current = { kind: 'ok', item: { recurrence: null, title: 'Thing' }, etag: 'etag' } as unknown as import('./itemsApi').ItemRead;
    const completion = { nextAttentionOn: null } as unknown as import('./itemsApi').Completion;

    await outcomes.handleCompletionResult(tools, 'item-1', 'attempt-a', false, null, {
      current,
      result: { kind: 'ok', completion },
    });

    expect(pending.getPendingCompletion('user-1', 'home-1', 'item-1')).toBe('attempt-b');
  });

  it('replays the stored receipt key after module memory is reset', async () => {
    const pending = await import('./pendingCompletions');
    pending.setPendingCompletion('user-1', 'home-1', 'item-1', 'receipt-key');
    vi.resetModules();
    const recovered = await import('./pendingCompletions');

    expect(recovered.getPendingCompletion('user-1', 'home-1', 'item-1')).toBe('receipt-key');
  });

  it('falls back to memory when sessionStorage access throws', async () => {
    const pending = await import('./pendingCompletions');
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('storage unavailable'); });
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('storage unavailable'); });

    pending.setPendingCompletion('user-1', 'home-1', 'item-1', 'attempt-1');

    expect(pending.getPendingCompletion('user-1', 'home-1', 'item-1')).toBe('attempt-1');
  });
});
