import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { installFakeServer, json, session } from '../test/fakeServer';
import { useAppSession } from './useAppSession';

afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); window.history.replaceState(null, '', '/'); });

describe('useAppSession', () => {
  it('a stale sessionEnded callback is inert once the session it belongs to has ended', async () => {
    installFakeServer({ 'GET /api/v1/session': json(200, session()) });
    const setRoute = vi.fn();
    const setOidcOutcome = vi.fn();
    const transitionedRef = { current: false };
    const { result } = renderHook(() => useAppSession({ setRoute, transitionedRef, setOidcOutcome }));
    await waitFor(() => expect(result.current.state.kind).toBe('signedIn'));
    const staleSessionEnded = result.current.sessionEnded;

    act(() => staleSessionEnded());
    expect(result.current.state.kind).toBe('signedOut');
    expect(result.current.sessionEndedNotice).toBe(true);

    window.history.replaceState(null, '', '/invite');
    setRoute.mockClear();
    act(() => staleSessionEnded());
    expect(setRoute).not.toHaveBeenCalled();
    expect(window.location.pathname).toBe('/invite');
  });
});
