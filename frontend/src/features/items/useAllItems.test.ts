import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { LoadOutcome } from './allItemsLoad';
import type { Context } from './allItemsUrl';

const pending: Array<{ context: Context; resolve: (outcome: LoadOutcome) => void }> = [];
vi.mock('./allItemsLoad', () => ({
  fetchAllItemsPage: vi.fn((_householdId: string, context: Context) =>
    new Promise<LoadOutcome>((resolve) => { pending.push({ context, resolve }); })),
}));

const { useAllItems } = await import('./useAllItems');

beforeEach(() => { pending.length = 0; window.history.replaceState(null, '', '/items?status=archived&cursor=old'); });
afterEach(() => { vi.clearAllMocks(); });

describe('useAllItems loader', () => {
  // A loader outcome that passed its own currency check must still be dropped when navigation lands before the
  // hook's continuation runs; otherwise a stale restart rewrites the newer URL and issues an obsolete request.
  it('drops an outcome when navigation happens after the loader settles but before it is applied', async () => {
    const onSignedOut = () => {};
    const { result } = renderHook(() => useAllItems('household', onSignedOut));
    await waitFor(() => expect(pending).toHaveLength(1));
    expect(pending[0]!.context).toMatchObject({ status: 'archived', cursor: 'old' });

    act(() => {
      pending[0]!.resolve({ kind: 'stale' });
      result.current.actions.update({ q: 'newer' });
    });

    await waitFor(() => expect(pending).toHaveLength(2));
    expect(pending[1]!.context).toMatchObject({ status: 'archived', q: 'newer', cursor: undefined });
    expect(window.location.search).toBe('?q=newer&status=archived');
    expect(result.current.context).toMatchObject({ q: 'newer', cursor: undefined });
    expect(result.current.page.notice).toBe(false);
    // Let any extra continuation run; no restart request for the superseded context may follow.
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(pending).toHaveLength(2);
  });
});
