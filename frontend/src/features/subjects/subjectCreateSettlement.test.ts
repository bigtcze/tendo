import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { clearPendingSubjectCreates, getPendingSubjectCreate, setPendingSubjectCreate } from './pendingSubjectCreate';
import { useSubjectCreate } from './useSubjectCreate';
import { createSubject } from './subjectsApi';

vi.mock('./subjectsApi', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./subjectsApi')>();
  return { ...actual, createSubject: vi.fn() };
});

const original = { version: 1 as const, key: 'aa'.repeat(16), body: { name: 'Original', type: 'person' as const } };
const next = { version: 1 as const, key: 'bb'.repeat(16), body: { name: 'Next', type: 'person' as const } };
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}
function context(pending: Awaited<ReturnType<typeof getPendingSubjectCreate>> | undefined) {
  return {
    userId: 'user', householdId: 'home', generation: { current: 0 }, addForm: { current: 0 }, pendingCreate: pending,
    setPendingCreate: vi.fn(), setCreateUncertain: vi.fn(), setNotice: vi.fn(), setAdding: vi.fn(), setMembers: vi.fn(),
    loadingMore: false, mutationPending: false, busy: { current: false }, setEditing: vi.fn(), setActionFailed: vi.fn(),
    setLoadingMore: vi.fn(), pagingFocus: { current: null }, pageLoad: { current: 0 }, setCursorStack: vi.fn(),
    setCurrentCursor: vi.fn(), setList: vi.fn(), onSignedOut: vi.fn(),
  };
}

afterEach(() => { clearPendingSubjectCreates(); vi.restoreAllMocks(); });

describe('subject create settlement', () => {
  it.each([
    { kind: 'ok', subject: { id: 'subject', name: 'Original', type: 'person', linkedUserId: null, archived: false, createdAt: '', updatedAt: '' } },
    { kind: 'definitive' },
  ])('preserves newer attempt after an obsolete response: $kind', async (result) => {
    setPendingSubjectCreate('user', 'home', original);
    const response = deferred<Awaited<ReturnType<typeof createSubject>>>();
    vi.mocked(createSubject).mockReturnValueOnce(response.promise);
    const ctx = context(original);
    const obsolete = renderHook(() => useSubjectCreate(ctx));
    const submission = obsolete.result.current.add({ name: 'Original', type: 'person', linkedUserId: '' });
    await waitFor(() => expect(createSubject).toHaveBeenCalled());
    obsolete.unmount();

    renderHook(() => useSubjectCreate(context(getPendingSubjectCreate('user', 'home'))));
    setPendingSubjectCreate('user', 'home', next);
    await act(async () => { response.resolve(result as Awaited<ReturnType<typeof createSubject>>); await submission; });
    expect(getPendingSubjectCreate('user', 'home')).toEqual(next);
  });
});
