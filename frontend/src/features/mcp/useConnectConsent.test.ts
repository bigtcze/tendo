import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { browserNavigation } from '../auth/oidcApi';
import { clearConnectEntry, readConnectEntry, saveConnectHandle } from './connectEntry';
import { grantedScopes } from './connectApi';
import { useConnectConsent, type ConsentState } from './useConnectConsent';
import { installFakeServer, json } from '../../test/fakeServer';

const handle = 'A'.repeat(43);
const path = `/api/v1/mcp/authorization-requests/${handle}`;
const request = {
  clientName: 'Claude Code', clientNameVerified: false as const, redirectUri: 'https://client.example/cb?tenant=a',
  scopes: ['tendo:read'] as ('tendo:read' | 'tendo:write')[],
  allowedScopes: ['tendo:read'] as ('tendo:read' | 'tendo:write')[], expiresAt: '2026-11-20T10:05:00Z',
};
const redirectTo = 'https://client.example/cb?tenant=a&code=c&state=s';

let focused = true;
let visibility: DocumentVisibilityState = 'visible';

beforeEach(() => {
  // Real timers would make the Allow gate flaky; the fake clock advances with real time so fetches still settle.
  vi.useFakeTimers({ shouldAdvanceTime: true });
  focused = true;
  visibility = 'visible';
  vi.spyOn(document, 'hasFocus').mockImplementation(() => focused);
  vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => visibility);
  vi.spyOn(browserNavigation, 'assign').mockImplementation(() => undefined);
  sessionStorage.clear();
  clearConnectEntry();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function renderConsent(onSessionEnded = vi.fn()) {
  return renderHook(() => useConnectConsent(handle, { onSessionEnded }));
}

async function loadReady(routes: Record<string, Parameters<typeof installFakeServer>[0][string]>) {
  const fake = installFakeServer({ [`GET ${path}`]: json(200, request), ...routes });
  const hook = renderConsent();
  await waitFor(() => expect(hook.result.current.state.kind).toBe('ready'));
  return { hook, fake };
}

function allowEnabled(state: ConsentState): boolean {
  return state.kind === 'ready' && state.allowEnabled;
}

describe('allow delay', () => {
  it('enables approve only after 1000 ms of visible, focused time', async () => {
    // The fake clock also follows real time (shouldAdvanceTime), so the checks keep a margin on both sides of the gate.
    const { hook } = await loadReady({});
    expect(allowEnabled(hook.result.current.state)).toBe(false);
    await act(() => vi.advanceTimersByTime(500));
    expect(allowEnabled(hook.result.current.state)).toBe(false);
    await act(() => vi.advanceTimersByTime(600));
    expect(allowEnabled(hook.result.current.state)).toBe(true);
  });

  it('a window blur resets the timer, and focus starts it again', async () => {
    const { hook } = await loadReady({});
    await act(() => vi.advanceTimersByTime(500));
    focused = false;
    await act(async () => { window.dispatchEvent(new Event('blur')); });
    await act(() => vi.advanceTimersByTime(2000));
    expect(allowEnabled(hook.result.current.state)).toBe(false);
    focused = true;
    await act(async () => { window.dispatchEvent(new Event('focus')); });
    await act(() => vi.advanceTimersByTime(1000));
    expect(allowEnabled(hook.result.current.state)).toBe(true);
  });

  it('hiding the page resets the timer', async () => {
    const { hook } = await loadReady({});
    await act(() => vi.advanceTimersByTime(500));
    visibility = 'hidden';
    await act(async () => { document.dispatchEvent(new Event('visibilitychange')); });
    await act(() => vi.advanceTimersByTime(2000));
    expect(allowEnabled(hook.result.current.state)).toBe(false);
  });
});

describe('blocked request', () => {
  it('never arms Allow and never sends approve when nothing asked for is allowed', async () => {
    const fake = installFakeServer({
      [`GET ${path}`]: json(200, { ...request, allowedScopes: [] }),
      [`POST ${path}/decision`]: json(200, { redirectTo }),
    });
    const hook = renderConsent();
    await waitFor(() => expect(hook.result.current.state.kind).toBe('ready'));
    await act(() => vi.advanceTimersByTime(3000));
    expect(allowEnabled(hook.result.current.state)).toBe(false);
    act(() => hook.result.current.decide('approve'));
    expect(fake.requests.some((r) => r.method === 'POST')).toBe(false);
    act(() => hook.result.current.decide('deny'));
    await waitFor(() => expect(browserNavigation.assign).toHaveBeenCalledWith(redirectTo));
    expect(fake.requests.filter((r) => r.method === 'POST').map((r) => JSON.parse(r.body))).toEqual([{ decision: 'deny' }]);
  });

  it('ignores an allowed scope the app did not ask for', async () => {
    const { allowedScopes } = grantedScopes({ ...request, scopes: ['tendo:read'], allowedScopes: ['tendo:write'] });
    expect(allowedScopes).toEqual([]);
    expect(grantedScopes({ ...request, scopes: ['tendo:write', 'tendo:read'], allowedScopes: ['tendo:read'] }))
      .toEqual({ allowedScopes: ['tendo:read'], withheldScopes: ['tendo:write'] });
  });
});

describe('decisions', () => {
  it('sends one POST for a double click and navigates once to the safe redirect', async () => {
    const { hook, fake } = await loadReady({
      [`POST ${path}/decision`]: json(200, { redirectTo }),
    });
    await act(() => vi.advanceTimersByTime(1000));
    act(() => {
      hook.result.current.decide('approve');
      hook.result.current.decide('approve');
    });
    await waitFor(() => expect(browserNavigation.assign).toHaveBeenCalledTimes(1));
    expect(browserNavigation.assign).toHaveBeenCalledWith(redirectTo);
    expect(fake.requests.filter((r) => r.method === 'POST')).toHaveLength(1);
    expect(hook.result.current.state.kind).toBe('done');
  });

  it('ignores approve until the allow delay has passed', async () => {
    const { hook, fake } = await loadReady({ [`POST ${path}/decision`]: json(200, { redirectTo }) });
    act(() => hook.result.current.decide('approve'));
    expect(fake.requests.some((r) => r.method === 'POST')).toBe(false);
  });

  it('never navigates to an unsafe redirect', async () => {
    const { hook } = await loadReady({
      [`POST ${path}/decision`]: json(200, { redirectTo: 'https://evil.example/cb?code=c' }),
    });
    await act(() => vi.advanceTimersByTime(1000));
    act(() => hook.result.current.decide('approve'));
    await waitFor(() => expect(hook.result.current.state.kind).toBe('invalidResponse'));
    expect(browserNavigation.assign).not.toHaveBeenCalled();
  });

  it('deny needs no allow delay', async () => {
    const { hook } = await loadReady({ [`POST ${path}/decision`]: json(200, { redirectTo }) });
    act(() => hook.result.current.decide('deny'));
    await waitFor(() => expect(browserNavigation.assign).toHaveBeenCalledWith(redirectTo));
  });

  it('a decision 404 clears the stored entry and shows not found', async () => {
    saveConnectHandle(handle);
    const { hook } = await loadReady({ [`POST ${path}/decision`]: json(404, { title: 'x', status: 404 }) });
    await act(() => vi.advanceTimersByTime(1000));
    act(() => hook.result.current.decide('approve'));
    await waitFor(() => expect(hook.result.current.state.kind).toBe('notFound'));
    expect(readConnectEntry()).toBeNull();
  });

  it('a decision 429 returns to ready with an error the buttons can recover from', async () => {
    const { hook } = await loadReady({ [`POST ${path}/decision`]: json(429, { title: 'x', status: 429 }) });
    act(() => hook.result.current.decide('deny'));
    await waitFor(() => expect(hook.result.current.state).toMatchObject({ kind: 'ready', decisionError: 'rateLimited' }));
  });

  it('a failed approve comes back with Allow disabled until another 1000 ms of focus', async () => {
    const { hook, fake } = await loadReady({ [`POST ${path}/decision`]: json(503, { title: 'x', status: 503 }) });
    await act(() => vi.advanceTimersByTime(1100));
    expect(allowEnabled(hook.result.current.state)).toBe(true);
    act(() => hook.result.current.decide('approve'));
    await waitFor(() => expect(hook.result.current.state).toMatchObject({ kind: 'ready', decisionError: 'unavailable' }));
    expect(allowEnabled(hook.result.current.state)).toBe(false);
    await act(() => vi.advanceTimersByTime(1100));
    expect(allowEnabled(hook.result.current.state)).toBe(true);
    expect(fake.requests.filter((request) => request.path === `${path}/decision`)).toHaveLength(1);
  });
});

describe('load failures', () => {
  it('a 404 load clears the entry and shows not found', async () => {
    saveConnectHandle(handle);
    installFakeServer({ [`GET ${path}`]: json(404, { title: 'x', status: 404 }) });
    const hook = renderConsent();
    await waitFor(() => expect(hook.result.current.state.kind).toBe('notFound'));
    expect(readConnectEntry()).toBeNull();
  });

  it('a 401 load calls onSessionEnded and keeps the entry', async () => {
    saveConnectHandle(handle);
    installFakeServer({ [`GET ${path}`]: json(401, { title: 'x', status: 401 }) });
    const onSessionEnded = vi.fn();
    renderHook(() => useConnectConsent(handle, { onSessionEnded }));
    await waitFor(() => expect(onSessionEnded).toHaveBeenCalledTimes(1));
    expect(readConnectEntry()?.handle).toBe(handle);
  });

  it('retry reloads an unavailable request', async () => {
    let calls = 0;
    installFakeServer({
      [`GET ${path}`]: () => { calls += 1; return calls === 1 ? json(503, { title: 'x', status: 503 }) : json(200, request); },
    });
    const hook = renderConsent();
    await waitFor(() => expect(hook.result.current.state.kind).toBe('unavailable'));
    act(() => hook.result.current.retry());
    await waitFor(() => expect(hook.result.current.state.kind).toBe('ready'));
  });
});

describe('allow re-disables after the gate was passed', () => {
  it('a blur after Allow is armed disables it again, and focus re-arms after 1000 ms', async () => {
    const { hook } = await loadReady({});
    await act(() => vi.advanceTimersByTime(1100));
    expect(allowEnabled(hook.result.current.state)).toBe(true);
    focused = false;
    await act(async () => { window.dispatchEvent(new Event('blur')); });
    expect(allowEnabled(hook.result.current.state)).toBe(false);
    focused = true;
    await act(async () => { window.dispatchEvent(new Event('focus')); });
    await act(() => vi.advanceTimersByTime(500));
    expect(allowEnabled(hook.result.current.state)).toBe(false);
    await act(() => vi.advanceTimersByTime(600));
    expect(allowEnabled(hook.result.current.state)).toBe(true);
  });

  it('hiding the page after Allow is armed disables it again', async () => {
    const { hook } = await loadReady({});
    await act(() => vi.advanceTimersByTime(1100));
    expect(allowEnabled(hook.result.current.state)).toBe(true);
    visibility = 'hidden';
    await act(async () => { document.dispatchEvent(new Event('visibilitychange')); });
    expect(allowEnabled(hook.result.current.state)).toBe(false);
    visibility = 'visible';
    await act(async () => { document.dispatchEvent(new Event('visibilitychange')); });
    await act(() => vi.advanceTimersByTime(1100));
    expect(allowEnabled(hook.result.current.state)).toBe(true);
  });
});

describe('stale decisions', () => {
  it('an old decision that fails after unmount leaves a newer visit entry alone', async () => {
    saveConnectHandle(handle);
    let release!: (value: Response) => void;
    const held = new Promise<Response>((resolve) => { release = resolve; });
    const { hook } = await loadReady({ [`POST ${path}/decision`]: () => held });
    act(() => hook.result.current.decide('deny'));
    hook.unmount();
    const newer = 'B'.repeat(43);
    saveConnectHandle(newer);
    await act(async () => { release(json(404, { title: 'x', status: 404 })); });
    await act(() => vi.advanceTimersByTime(50));
    expect(readConnectEntry()?.handle).toBe(newer);
  });

  it('a safe redirect still navigates after unmount, because the request is already consumed', async () => {
    const held = Promise.resolve(json(200, { redirectTo }));
    const { hook } = await loadReady({ [`POST ${path}/decision`]: () => held });
    act(() => hook.result.current.decide('deny'));
    hook.unmount();
    await waitFor(() => expect(browserNavigation.assign).toHaveBeenCalledWith(redirectTo));
  });

  it('a decision 401 calls onSessionEnded and keeps the stored entry', async () => {
    saveConnectHandle(handle);
    const onSessionEnded = vi.fn();
    installFakeServer({
      [`GET ${path}`]: json(200, request),
      [`POST ${path}/decision`]: json(401, { title: 'x', status: 401 }),
    });
    const hook = renderHook(() => useConnectConsent(handle, { onSessionEnded }));
    await waitFor(() => expect(hook.result.current.state.kind).toBe('ready'));
    act(() => hook.result.current.decide('deny'));
    await waitFor(() => expect(onSessionEnded).toHaveBeenCalledTimes(1));
    expect(readConnectEntry()?.handle).toBe(handle);
  });
});

describe('bfcache return', () => {
  it('stays done when the page is restored after navigating away', async () => {
    const { hook } = await loadReady({ [`POST ${path}/decision`]: json(200, { redirectTo }) });
    await act(() => vi.advanceTimersByTime(1000));
    act(() => hook.result.current.decide('approve'));
    await waitFor(() => expect(hook.result.current.state.kind).toBe('done'));
    act(() => { window.dispatchEvent(Object.assign(new Event('pageshow'), { persisted: true })); });
    expect(hook.result.current.state.kind).toBe('done');
  });
});
