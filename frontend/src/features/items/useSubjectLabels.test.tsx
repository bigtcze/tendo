import { afterEach, describe, expect, it, vi } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { json, installFakeServer, HOUSEHOLD_ID } from '../../test/fakeServer';
import { useSubjectLabels } from './useSubjectLabels';

const path = `/api/v1/households/${HOUSEHOLD_ID}/subjects`;
const subject = { id: 'found', name: 'Found', type: 'person', archived: false, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' };

afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe('useSubjectLabels classification', () => {
  it('reports found and missing after two successful batches', async () => {
    installFakeServer({ [`GET ${path}`]: (request) => request.query.get('archived') === 'false'
      ? json(200, { items: [subject], nextCursor: null })
      : json(200, { items: [], nextCursor: null }) });
    const { result } = renderHook(() => useSubjectLabels('user', HOUSEHOLD_ID, ['found', 'missing'], vi.fn()));
    await waitFor(() => expect(result.current.stateFor('missing')).toBe('missing'));
    expect(result.current.names.get('found')).toBe('Found');
  });

  it('keeps returned IDs found and marks unreturned IDs failed when one request fails', async () => {
    installFakeServer({ [`GET ${path}`]: (request) => request.query.get('archived') === 'false'
      ? json(200, { items: [subject], nextCursor: null })
      : json(500, {}) });
    const { result } = renderHook(() => useSubjectLabels('user', HOUSEHOLD_ID, ['found', 'unreturned'], vi.fn()));
    await waitFor(() => expect(result.current.stateFor('unreturned')).toBe('failed'));
    expect(result.current.stateFor('found')).toBe('found');
  });

  it.each([false, true])('calls onSignedOut once when either stream returns 401 (archived=%s)', async (archived) => {
    const onSignedOut = vi.fn();
    installFakeServer({ [`GET ${path}`]: (request) => request.query.get('archived') === String(archived)
      ? json(401, {})
      : json(200, { items: [], nextCursor: null }) });
    renderHook(() => useSubjectLabels('user', HOUSEHOLD_ID, ['missing'], onSignedOut));
    await waitFor(() => expect(onSignedOut).toHaveBeenCalledTimes(1));
  });
});
