import { act, cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Member, PageResult } from '../members/membersApi';
import { useHomeMembers } from './useHomeMembers';

const listMembers = vi.hoisted(() => vi.fn<(householdId: string, cursor?: string) => Promise<PageResult<Member>>>());
vi.mock('../members/membersApi', () => ({ listMembers }));

const alice: Member = { userId: 'user-a', login: 'alice', role: 'owner' };
const bob: Member = { userId: 'user-b', login: 'bob', role: 'owner' };

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

afterEach(() => { cleanup(); listMembers.mockReset(); });

describe('useHomeMembers', () => {
  // #321 follow-up 1: a household switch in place must never surface the previous household's members.
  it('drops members of the previous household when the household changes in place', async () => {
    const slowA = deferred<PageResult<Member>>();
    const fastB = deferred<PageResult<Member>>();
    listMembers.mockImplementation((householdId) => householdId === 'house-a' ? slowA.promise : fastB.promise);
    const onSignedOut = vi.fn();
    const { result, rerender } = renderHook(({ householdId }) => useHomeMembers(householdId, onSignedOut),
      { initialProps: { householdId: 'house-a' } });
    expect(result.current).toEqual({ members: [], loaded: false });

    rerender({ householdId: 'house-b' });
    await act(async () => { fastB.resolve({ kind: 'ok', items: [bob], nextCursor: null }); });
    await waitFor(() => expect(result.current).toEqual({ members: [bob], loaded: true }));

    await act(async () => { slowA.resolve({ kind: 'ok', items: [alice], nextCursor: null }); });
    expect(result.current).toEqual({ members: [bob], loaded: true });
    expect(listMembers.mock.calls.map(([householdId]) => householdId)).toEqual(['house-a', 'house-b']);
    expect(onSignedOut).not.toHaveBeenCalled();
  });

  it('hides loaded members immediately after switching until the new household loads', async () => {
    const b = deferred<PageResult<Member>>();
    listMembers.mockImplementation((householdId) => householdId === 'house-a'
      ? Promise.resolve({ kind: 'ok', items: [alice], nextCursor: null })
      : b.promise);
    const onSignedOut = vi.fn();
    const { result, rerender } = renderHook(({ householdId }) => useHomeMembers(householdId, onSignedOut),
      { initialProps: { householdId: 'house-a' } });
    await waitFor(() => expect(result.current).toEqual({ members: [alice], loaded: true }));

    rerender({ householdId: 'house-b' });
    expect(result.current).toEqual({ members: [], loaded: false });
    await act(async () => { b.resolve({ kind: 'ok', items: [bob], nextCursor: null }); });
    await waitFor(() => expect(result.current).toEqual({ members: [bob], loaded: true }));
  });
});
