import { afterEach, describe, expect, it, vi } from 'vitest';
import { installFakeServer, json, session } from '../../test/fakeServer';
import { changePassword } from './passwordChangeApi';

const changePath = 'POST /api/v1/auth/password';
const OLD = 'an-old-long-unique-passphrase';
const NEW = 'a-new-long-unique-passphrase';

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('changePassword', () => {
  it('sends both passwords with the browser Origin and returns the rotated session', async () => {
    const rotated = { ...session(), expiresAt: '2030-06-01T00:00:00Z' };
    const fake = installFakeServer({
      [changePath]: json(200, rotated, { 'Set-Cookie': 'tendo_session=rotated-1; Path=/; HttpOnly; SameSite=Lax' }),
    });

    await expect(changePassword(OLD, NEW)).resolves.toEqual({ kind: 'ok', session: rotated });

    expect(fake.requests).toHaveLength(1);
    const [sent] = fake.requests;
    expect(sent!.method).toBe('POST');
    expect(sent!.path).toBe('/api/v1/auth/password');
    expect(JSON.parse(sent!.body)).toEqual({ currentPassword: OLD, newPassword: NEW });
    // Browsers set this themselves; the generated types require it, so the call site names the current origin.
    expect(sent!.headers.get('Origin')).toBe(window.location.origin);
  });

  it.each([
    { status: 422, body: { code: 'incorrect', field: 'currentPassword' }, kind: 'incorrect' },
    { status: 422, body: { code: 'invalid_length', field: 'newPassword' }, kind: 'invalidLength' },
    { status: 403, body: { code: 'local_session_required' }, kind: 'localSessionRequired' },
    { status: 403, body: { code: 'forbidden_origin' }, kind: 'unavailable' },
    { status: 401, body: { code: 'unauthenticated' }, kind: 'unauthenticated' },
    { status: 429, body: { code: 'rate_limited' }, kind: 'rateLimited' },
    { status: 503, body: { code: 'unavailable' }, kind: 'unavailable' },
  ])('maps a $status $body.code refusal to $kind', async ({ status, body, kind }) => {
    installFakeServer({ [changePath]: json(status, { type: 'about:blank', title: 'x', status, ...body }) });

    await expect(changePassword(OLD, NEW)).resolves.toEqual({ kind });
  });

  it('treats a network failure as a retryable refusal', async () => {
    installFakeServer({ [changePath]: () => { throw new TypeError('load failed'); } });

    await expect(changePassword(OLD, NEW)).resolves.toEqual({ kind: 'unavailable' });
  });
});
