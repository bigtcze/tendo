import { cleanup, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { HOUSEHOLD_ID, installFakeServer, json, session } from '../../test/fakeServer';
import type { Subject } from './itemsApi';
import { useAllItemsFacets } from './useAllItemsFacets';

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const subject: Subject = { id: 's-1', name: 'Café', type: 'person', linkedUserId: null, archived: false, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' };
const member = { userId: session().userId, login: 'anna', role: 'owner' as const };

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe('useAllItemsFacets', () => {
  it('keeps previously loaded subject and member values when a refetch fails', async () => {
    let fail = false;
    installFakeServer({
      [`GET ${base}/subjects`]: () => fail ? json(500, {}) : json(200, { items: [subject], nextCursor: null }),
      [`GET ${base}/members`]: () => fail ? json(500, {}) : json(200, { items: [member], nextCursor: null }),
    });
    const onNoHousehold = () => {};
    const { result, rerender } = renderHook(({ onSignedOut }) => useAllItemsFacets(HOUSEHOLD_ID, onSignedOut, onNoHousehold),
      { initialProps: { onSignedOut: () => {} } });
    await waitFor(() => expect(result.current.subjects.state).toBe('ready'));
    await waitFor(() => expect(result.current.members.state).toBe('ready'));

    fail = true;
    rerender({ onSignedOut: () => {} });

    await waitFor(() => expect(result.current.subjects.state).toBe('failed'));
    await waitFor(() => expect(result.current.members.state).toBe('failed'));
    expect(result.current.subjects.values).toEqual([subject]);
    expect(result.current.members.values).toEqual([member]);
  });
});
