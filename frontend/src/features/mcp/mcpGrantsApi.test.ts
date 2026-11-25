import { afterEach, describe, expect, it, vi } from 'vitest';
import { installFakeServer, json } from '../../test/fakeServer';
import {
  listAccountMcpGrants, listMcpGrants, revokeAccountMcpGrant, revokeAllAccountMcpGrants, revokeMcpGrant, type McpGrant,
} from './mcpGrantsApi';

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

describe('mcpGrantsApi for an administrator', () => {
  const user = '0198a2f0-0000-7000-8000-0000000000b0';
  const list = `GET /api/v1/admin/accounts/${user}/mcp-grants`;
  const one = `DELETE /api/v1/admin/accounts/${user}/mcp-grants/${id}`;
  const all = `DELETE /api/v1/admin/accounts/${user}/mcp-grants`;

  it('reads one account’s page with the fixed page size and the opaque cursor', async () => {
    const fake = installFakeServer({ [list]: json(200, { items: [grant], nextCursor: null }) });
    await expect(listAccountMcpGrants(user, 'g1x')).resolves.toEqual({ kind: 'ok', items: [grant], nextCursor: null });
    expect(fake.requests[0].query.get('limit')).toBe('20');
    expect(fake.requests[0].query.get('cursor')).toBe('g1x');
  });

  it('tells a lost grant, a gone account, a missing session and anything else apart on the list', async () => {
    installFakeServer({ [list]: problem(403, 'instance_admin_required') });
    await expect(listAccountMcpGrants(user)).resolves.toEqual({ kind: 'forbidden' });
    installFakeServer({ [list]: problem(404, 'not_found') });
    await expect(listAccountMcpGrants(user)).resolves.toEqual({ kind: 'gone' });
    installFakeServer({ [list]: problem(401, 'unauthenticated') });
    await expect(listAccountMcpGrants(user)).resolves.toEqual({ kind: 'unauthenticated' });
    installFakeServer({ [list]: problem(403) });
    await expect(listAccountMcpGrants(user)).resolves.toEqual({ kind: 'unavailable' });
    installFakeServer({ [list]: () => Promise.reject(new Error('offline')) });
    await expect(listAccountMcpGrants(user)).resolves.toEqual({ kind: 'unavailable' });
  });

  it('revokes one grant with the canonical Origin and maps every answer', async () => {
    const fake = installFakeServer({ [one]: json(204) });
    await expect(revokeAccountMcpGrant(user, id)).resolves.toEqual({ kind: 'ok' });
    expect(fake.requests[0].headers.get('Origin')).toBe(window.location.origin);
    const cases: [Response, string][] = [
      [problem(404, 'not_found'), 'notFound'], [problem(403, 'instance_admin_required'), 'forbidden'],
      [problem(403, 'origin_mismatch'), 'rejected'], [problem(429, 'rate_limited'), 'rateLimited'],
      [problem(401, 'unauthenticated'), 'unauthenticated'], [problem(503, 'unavailable'), 'unavailable'],
    ];
    for (const [response, kind] of cases) {
      installFakeServer({ [one]: response });
      await expect(revokeAccountMcpGrant(user, id)).resolves.toEqual({ kind });
    }
  });

  it('revokes every grant of the account with the canonical Origin, and 404 is the account gone', async () => {
    const fake = installFakeServer({ [all]: json(204) });
    await expect(revokeAllAccountMcpGrants(user)).resolves.toEqual({ kind: 'ok' });
    expect(fake.requests[0].headers.get('Origin')).toBe(window.location.origin);
    const cases: [Response | (() => Promise<Response>), string][] = [
      [problem(404, 'not_found'), 'gone'], [problem(403, 'instance_admin_required'), 'forbidden'],
      [problem(403, 'origin_mismatch'), 'rejected'], [problem(429, 'rate_limited'), 'rateLimited'],
      [problem(401, 'unauthenticated'), 'unauthenticated'], [problem(503, 'unavailable'), 'unavailable'],
      [() => Promise.reject(new Error('offline')), 'unavailable'],
    ];
    for (const [response, kind] of cases) {
      installFakeServer({ [all]: response });
      await expect(revokeAllAccountMcpGrants(user)).resolves.toEqual({ kind });
    }
  });
});
