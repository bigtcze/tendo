import { afterEach, describe, expect, it, vi } from 'vitest';
import { installFakeServer, json, session } from '../../test/fakeServer';
import type { Session } from '../auth/sessionApi';
import { rereadInstanceAdminSession } from './installationSession';

const other = { ...session(), userId: '0198a2f0-0000-7000-8000-0000000000ff', login: 'bea' };

function handlers(viewerId = session().userId) {
  return {
    viewerId,
    onSession: vi.fn(),
    onSignedOut: vi.fn(),
    onReboot: vi.fn(),
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('rereadInstanceAdminSession', () => {
  it('adopts the grant the server confirms for the same account', async () => {
    installFakeServer({ 'GET /api/v1/session': json(200, { ...session(), instanceAdmin: true }) });
    const calls = handlers();
    await rereadInstanceAdminSession(calls);
    expect(calls.onSession).toHaveBeenCalledWith(expect.objectContaining({ instanceAdmin: true }));
    expect(calls.onReboot).not.toHaveBeenCalled();
    expect(calls.onSignedOut).not.toHaveBeenCalled();
  });

  it('re-boots instead of folding another account into this shell', async () => {
    installFakeServer({ 'GET /api/v1/session': json(200, other) });
    const calls = handlers();
    await rereadInstanceAdminSession(calls);
    expect(calls.onSession).not.toHaveBeenCalled();
    expect(calls.onReboot).toHaveBeenCalledTimes(1);
  });

  it('goes back to sign-in when the session has ended', async () => {
    installFakeServer({ 'GET /api/v1/session': json(401, { type: 'about:blank', title: 'Unauthorized', status: 401 }) });
    const calls = handlers();
    await rereadInstanceAdminSession(calls);
    expect(calls.onSignedOut).toHaveBeenCalledTimes(1);
    expect(calls.onSession).not.toHaveBeenCalled();
    expect(calls.onReboot).not.toHaveBeenCalled();
  });

  it('leaves the refusal standing when the read itself failed', async () => {
    installFakeServer({ 'GET /api/v1/session': json(503, { type: 'about:blank', title: 'Unavailable', status: 503 }) });
    const calls = handlers();
    await rereadInstanceAdminSession(calls);
    for (const callback of [calls.onSession, calls.onSignedOut, calls.onReboot]) {
      expect(callback).not.toHaveBeenCalled();
    }
  });

  it('reports a failed read to a caller that has something riding on it', async () => {
    installFakeServer({ 'GET /api/v1/session': json(503, { type: 'about:blank', title: 'Unavailable', status: 503 }) });
    const calls = { ...handlers(), onError: vi.fn() };
    await rereadInstanceAdminSession(calls);
    expect(calls.onError).toHaveBeenCalledTimes(1);
    for (const callback of [calls.onSession, calls.onSignedOut, calls.onReboot]) {
      expect(callback).not.toHaveBeenCalled();
    }
  });

  it('hands the fresh session to the caller, which decides what it means', async () => {
    installFakeServer({ 'GET /api/v1/session': json(200, session()) });
    const calls = handlers();
    const seen: Session[] = [];
    await rereadInstanceAdminSession({ ...calls, onSession: (next) => seen.push(next) });
    expect(seen).toEqual([session()]);
    expect(calls.onReboot).not.toHaveBeenCalled();
  });
});
