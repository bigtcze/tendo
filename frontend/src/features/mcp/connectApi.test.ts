import { afterEach, describe, expect, it, vi } from 'vitest';
import { describeRedirect, decideConnectRequest, fetchConnectRequest, isSafeRedirect } from './connectApi';
import { installFakeServer, json } from '../../test/fakeServer';

const handle = 'A'.repeat(43);
const path = `/api/v1/mcp/authorization-requests/${handle}`;
const request = {
  clientName: 'Claude Code', clientNameVerified: false as const, redirectUri: 'http://localhost:54123/callback',
  scopes: ['tendo:read'] as ('tendo:read' | 'tendo:write')[],
  allowedScopes: ['tendo:read'] as ('tendo:read' | 'tendo:write')[], expiresAt: '2026-11-20T10:05:00Z',
};

afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe('fetchConnectRequest', () => {
  it('returns the consent view on 200', async () => {
    installFakeServer({ [`GET ${path}`]: json(200, request) });
    expect(await fetchConnectRequest(handle)).toEqual({ kind: 'ok', request });
  });

  it.each([
    [404, 'notFound'],
    [401, 'unauthenticated'],
    [429, 'rateLimited'],
    [400, 'unavailable'],
    [403, 'unavailable'],
    [421, 'unavailable'],
    [503, 'unavailable'],
  ])('maps %i to %s', async (status, kind) => {
    installFakeServer({ [`GET ${path}`]: json(status, { title: 'x', status }) });
    expect(await fetchConnectRequest(handle)).toEqual({ kind });
  });

  it('maps a network failure to unavailable', async () => {
    installFakeServer({ [`GET ${path}`]: () => Promise.reject(new TypeError('offline')) });
    expect(await fetchConnectRequest(handle)).toEqual({ kind: 'unavailable' });
  });
});

describe('decideConnectRequest', () => {
  it('posts the decision with the origin and returns the redirect', async () => {
    const fake = installFakeServer({
      [`POST ${path}/decision`]: json(200, { redirectTo: 'http://localhost:54123/callback?code=x&state=y' }),
    });
    expect(await decideConnectRequest(handle, 'approve')).toEqual({
      kind: 'redirect', redirectTo: 'http://localhost:54123/callback?code=x&state=y',
    });
    expect(fake.requests[0]?.body).toBe(JSON.stringify({ decision: 'approve' }));
    expect(fake.requests[0]?.headers.get('Origin')).toBe(window.location.origin);
  });

  it('sends the scopes the screen showed with an approval, and none with a denial or an empty list', async () => {
    const fake = installFakeServer({ [`POST ${path}/decision`]: json(200, { redirectTo: 'http://localhost:54123/callback?code=x' }) });
    await decideConnectRequest(handle, 'approve', ['tendo:read', 'tendo:write']);
    await decideConnectRequest(handle, 'deny');
    await decideConnectRequest(handle, 'approve', []);
    expect(fake.requests.map((r) => JSON.parse(r.body))).toEqual([
      { decision: 'approve', scopes: ['tendo:read', 'tendo:write'] }, { decision: 'deny' }, { decision: 'approve' },
    ]);
  });

  it.each([
    [404, 'notFound'],
    [401, 'unauthenticated'],
    [429, 'rateLimited'],
    [500, 'unavailable'],
  ])('maps %i to %s', async (status, kind) => {
    installFakeServer({ [`POST ${path}/decision`]: json(status, { title: 'x', status }) });
    expect(await decideConnectRequest(handle, 'deny')).toEqual({ kind });
  });
});

describe('isSafeRedirect', () => {
  it.each([
    ['same https endpoint with the appended code', 'https://client.example/cb?x=1', 'https://client.example/cb?x=1&code=c&state=s'],
    ['https with no registered query', 'https://client.example/cb', 'https://client.example/cb?code=c'],
    ['loopback with another port', 'http://127.0.0.1:5000/cb', 'http://127.0.0.1:6000/cb?code=c'],
    ['loopback IPv6 with another port', 'http://[::1]/cb', 'http://[::1]:7000/cb?code=c'],
    ['localhost with another port', 'http://localhost:1/cb', 'http://localhost:2/cb?code=c&error=access_denied'],
    ['private-use scheme endpoint', 'com.example.app:/oauth2redirect', 'com.example.app:/oauth2redirect?code=c'],
  ])('accepts %s', (_name, registered, to) => {
    expect(isSafeRedirect(registered, to)).toBe(true);
  });

  it.each([
    ['a different host', 'https://client.example/cb', 'https://evil.example/cb?code=c'],
    ['a different path', 'https://client.example/cb', 'https://client.example/other?code=c'],
    ['a different scheme', 'https://client.example/cb', 'http://client.example/cb?code=c'],
    ['a different port on https', 'https://client.example/cb', 'https://client.example:8443/cb?code=c'],
    ['localhost is not 127.0.0.1', 'http://localhost:5000/cb', 'http://127.0.0.1:5000/cb?code=c'],
    ['a non-loopback http host', 'http://client.example/cb', 'http://client.example:9/cb?code=c'],
    ['a dropped registered query value', 'https://client.example/cb?tenant=a', 'https://client.example/cb?code=c'],
    ['a changed registered query value', 'https://client.example/cb?tenant=a', 'https://client.example/cb?tenant=b&code=c'],
    ['javascript scheme', 'https://client.example/cb', 'javascript:alert(1)'],
    ['data scheme', 'https://client.example/cb', 'data:text/html,hi'],
    ['vbscript scheme', 'https://client.example/cb', 'vbscript:msgbox(1)'],
    ['file scheme', 'https://client.example/cb', 'file:///etc/passwd'],
    ['blob scheme', 'https://client.example/cb', 'blob:https://client.example/uuid'],
    ['an unparsable target', 'https://client.example/cb', 'not a url'],
    ['an unparsable registered URI', 'not a url', 'https://client.example/cb?code=c'],
  ])('rejects %s', (_name, registered, to) => {
    expect(isSafeRedirect(registered, to)).toBe(false);
  });
});

describe('describeRedirect', () => {
  it('shows the punycode host of an https client without decoding it', () => {
    expect(describeRedirect('https://xn--80ak6aa92e.com/cb')).toEqual({ kind: 'web', host: 'xn--80ak6aa92e.com' });
  });

  it('shows a loopback host with its port', () => {
    expect(describeRedirect('http://localhost:54123/callback')).toEqual({ kind: 'loopback', host: 'localhost:54123' });
    expect(describeRedirect('http://127.0.0.1:8080/cb')).toEqual({ kind: 'loopback', host: '127.0.0.1:8080' });
  });

  it('names a private-use scheme without its colon', () => {
    expect(describeRedirect('com.example.app:/oauth2redirect')).toEqual({ kind: 'scheme', scheme: 'com.example.app' });
  });

  it.each([
    ['a plain http host that is not loopback', 'http://client.example/cb'],
    ['a dangerous scheme', 'javascript:alert(1)'],
    ['a scheme without a dot', 'myapp:/cb'],
    ['an unparsable value', 'nope'],
  ])('reports %s as unknown', (_name, value) => {
    expect(describeRedirect(value)).toEqual({ kind: 'unknown' });
  });
});
