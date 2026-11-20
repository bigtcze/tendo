import { afterEach, describe, expect, it, vi } from 'vitest';
import { installFakeServer, json } from '../../test/fakeServer';
import { listMcpGrants, revokeMcpGrant, type McpGrant } from './mcpGrantsApi';

const id = '0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b90';
const grant: McpGrant = {
  id,
  clientName: 'Claude Code',
  clientNameVerified: false,
  scopes: ['tendo:read'],
  createdAt: '2026-11-20T10:00:00Z',
  expiresAt: '2027-02-18T10:00:00Z',
};
const problem = (status: number, code?: string) => json(status, { type: 'about:blank', title: 'x', status, ...(code ? { code } : {}) });

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('mcpGrantsApi list', () => {
  it('reads one page with the fixed page size and passes the opaque cursor through', async () => {
    const fake = installFakeServer({
      'GET /api/v1/mcp/grants': json(200, { items: [grant], nextCursor: 'c1' }),
    });
    await expect(listMcpGrants()).resolves.toEqual({ kind: 'ok', items: [grant], nextCursor: 'c1' });
    expect(fake.requests[0].query.get('limit')).toBe('20');
    expect(fake.requests[0].query.has('cursor')).toBe(false);

    await listMcpGrants('c1');
    expect(fake.requests[1].query.get('cursor')).toBe('c1');
  });

  it('maps a missing session to unauthenticated and everything else to unavailable', async () => {
    installFakeServer({ 'GET /api/v1/mcp/grants': problem(401, 'unauthenticated') });
    await expect(listMcpGrants()).resolves.toEqual({ kind: 'unauthenticated' });

    installFakeServer({ 'GET /api/v1/mcp/grants': problem(503, 'unavailable') });
    await expect(listMcpGrants()).resolves.toEqual({ kind: 'unavailable' });

    installFakeServer({ 'GET /api/v1/mcp/grants': () => Promise.reject(new Error('offline')) });
    await expect(listMcpGrants()).resolves.toEqual({ kind: 'unavailable' });
  });
});

describe('mcpGrantsApi revoke', () => {
  it('disconnects with the canonical Origin and treats only 204 as success', async () => {
    const fake = installFakeServer({ [`DELETE /api/v1/mcp/grants/${id}`]: json(204) });
    await expect(revokeMcpGrant(id)).resolves.toEqual({ kind: 'ok' });
    expect(fake.requests[0].headers.get('Origin')).toBe(window.location.origin);
  });

  it('separates already-gone, rate limited, signed out and unavailable', async () => {
    const refuse = (response: Response | (() => Promise<Response>)) => installFakeServer({ [`DELETE /api/v1/mcp/grants/${id}`]: response });
    refuse(problem(404, 'mcp_grant_not_found'));
    await expect(revokeMcpGrant(id)).resolves.toEqual({ kind: 'notFound' });
    refuse(problem(429, 'rate_limited'));
    await expect(revokeMcpGrant(id)).resolves.toEqual({ kind: 'rateLimited' });
    refuse(problem(401, 'unauthenticated'));
    await expect(revokeMcpGrant(id)).resolves.toEqual({ kind: 'unauthenticated' });
    refuse(problem(503, 'unavailable'));
    await expect(revokeMcpGrant(id)).resolves.toEqual({ kind: 'unavailable' });
    refuse(() => Promise.reject(new Error('offline')));
    await expect(revokeMcpGrant(id)).resolves.toEqual({ kind: 'unavailable' });
  });
});
