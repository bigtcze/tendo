import { act, renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { useShowMoreList, type ShowMorePage } from './useShowMoreList';

type Failed = { kind: 'unavailable' };
type Answer = ShowMorePage<string, Failed>;

/** A reader whose every call waits until the test answers it, in any order. */
function heldReader() {
  const calls: { cursor?: string; answer: (page: Answer) => void }[] = [];
  const read = (cursor?: string) => new Promise<Answer>((resolve) => { calls.push({ cursor, answer: resolve }); });
  return { calls, read };
}
const ok = (items: string[], nextCursor: string | null = null): Answer => ({ kind: 'ok', items, nextCursor });

describe('useShowMoreList', () => {
  it('never appends an older Show more page under a first page read again meanwhile', async () => {
    const reader = heldReader();
    const { result } = renderHook(() => useShowMoreList<string, Failed>({ read: reader.read, handOff: () => false }));
    await act(async () => { reader.calls[0].answer(ok(['b1', 'b2'], 'after-b2')); });
    expect(result.current.items).toEqual(['b1', 'b2']);

    // A reload starts, and Show more is pressed before its first page arrives: it still names the old cursor.
    act(() => result.current.reload());
    await waitFor(() => expect(reader.calls).toHaveLength(2));
    let more!: Promise<void>;
    act(() => { more = result.current.loadMore(); });
    expect(reader.calls[2].cursor).toBe('after-b2');

    // The new first page has one new row and only the first old one, so the old page 2 would leave a gap.
    await act(async () => { reader.calls[1].answer(ok(['a1', 'b1'], 'after-b1')); });
    await act(async () => { reader.calls[2].answer(ok(['b3'])); await more; });
    expect(result.current.items).toEqual(['a1', 'b1']);
    expect(result.current.nextCursor).toBe('after-b1');
  });

  it('appends Show more when nothing was read again meanwhile', async () => {
    const reader = heldReader();
    const { result } = renderHook(() => useShowMoreList<string, Failed>({ read: reader.read, handOff: () => false }));
    await act(async () => { reader.calls[0].answer(ok(['b1'], 'after-b1')); });
    let more!: Promise<void>;
    act(() => { more = result.current.loadMore(); });
    await act(async () => { reader.calls[1].answer(ok(['b2'])); await more; });
    expect(result.current.items).toEqual(['b1', 'b2']);
    expect(result.current.nextCursor).toBeNull();
  });
});
