import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { installFakeServer, json } from '../../test/fakeServer';
import type { components } from '../../../../api/generated/api';
import { useHouseholdCreate } from './useHouseholdCreate';

type AdminHousehold = components['schemas']['AdminHousehold'];
type DirectoryAccount = components['schemas']['DirectoryAccount'];

const petr: DirectoryAccount = { userId: '0198a2f0-0000-7000-8000-000000000002', login: 'petr' };
const first: AdminHousehold = {
  id: '0198a2f0-0000-7000-8000-0000000000aa', name: 'Chata', timezone: 'UTC', createdAt: '2026-11-15T10:00:00Z',
};

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('useHouseholdCreate', () => {
  // Household names are not unique, so a second household with the same name, zone and first admin is a real, new
  // intent. The screen happens to unmount after a create today; the hook must not depend on that to mint a new key.
  it('sends a new key for an identical second household after the first was created', async () => {
    const fake = installFakeServer({ 'POST /api/v1/admin/households': json(201, first) });
    const onCreated = vi.fn();
    const { result } = renderHook(() => useHouseholdCreate({
      viewerId: '0198a2f0-0000-7000-8000-000000000001',
      onSignedOut: vi.fn(),
      onForbidden: vi.fn(),
      onCreated,
    }));
    const trigger = document.createElement('button');

    for (let round = 1; round <= 2; round += 1) {
      act(() => { result.current.start(trigger); });
      act(() => {
        result.current.editName('Chata');
        result.current.editTimezone('UTC');
        result.current.choose(petr);
      });
      await act(async () => { await result.current.submit(); });
      await waitFor(() => expect(onCreated).toHaveBeenCalledTimes(round));
    }

    const keys = fake.requests
      .filter((request) => request.method === 'POST' && request.path === '/api/v1/admin/households')
      .map((request) => request.headers.get('Idempotency-Key'));
    expect(keys).toHaveLength(2);
    expect(keys[0]).toMatch(/^[0-9a-f]{32}$/);
    expect(keys[1]).toMatch(/^[0-9a-f]{32}$/);
    expect(keys[1]).not.toBe(keys[0]);
  });
});
