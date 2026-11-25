import type { components } from '../../../../api/generated/api';
import { api } from '../../lib/api';

export type McpGrant = components['schemas']['McpGrant'];

const PAGE_SIZE = 20;

/**
 * `forbidden` (the installation grant is gone) and `gone` (the account the list names is deleted) only ever come from
 * an administrator's list of another account; the signed-in account's own list cannot answer either.
 */
export type McpGrantPage =
  | { kind: 'ok'; items: McpGrant[]; nextCursor: string | null }
  | { kind: 'unauthenticated' | 'unavailable' | 'forbidden' | 'gone' };

/**
 * `notFound` is a grant that is no longer live, so the caller reloads quietly. `forbidden` and `rejected` (a refused
 * Origin) only come from an administrator's revocation; `rejected` changed nothing and a retry would only repeat it.
 */
export type McpGrantRevoke =
  | { kind: 'ok' }
  | { kind: 'notFound' | 'rateLimited' | 'unauthenticated' | 'unavailable' | 'forbidden' | 'rejected' };

/** Where a connected-apps list reads its grants and disconnects one: the viewer's own, or one account's for an administrator. */
export type GrantSource = {
  list: (cursor?: string) => Promise<McpGrantPage>;
  revoke: (grantId: string) => Promise<McpGrantRevoke>;
};

type Answer = { error?: unknown; response: Response };

const isAdminRefusal = (error: unknown) => (error as { code?: string } | undefined)?.code === 'instance_admin_required';
// Browsers set Origin themselves and ignore this value; it only satisfies the generated type.
const origin = () => ({ Origin: window.location.origin });

/** One page of the signed-in account's live connected apps, newest first. */
export async function listMcpGrants(cursor?: string): Promise<McpGrantPage> {
  try {
    const { data, response } = await api.GET('/api/v1/mcp/grants', {
      params: { query: { limit: PAGE_SIZE, ...(cursor ? { cursor } : {}) } },
    });
    if (data && response.status === 200) return { kind: 'ok', items: data.items, nextCursor: data.nextCursor };
    if (response.status === 401) return { kind: 'unauthenticated' };
  } catch { /* show the retryable unavailable state */ }
  return { kind: 'unavailable' };
}

/**
 * Disconnects one app. The server answers 404 for an app that is already gone, so the caller can reload quietly
 * instead of treating it as a failure. A 204 is the only success; nothing else is removed locally before it.
 */
export async function revokeMcpGrant(grantId: string): Promise<McpGrantRevoke> {
  try {
    const { response } = await api.DELETE('/api/v1/mcp/grants/{grantId}', {
      params: { path: { grantId }, header: origin() },
    });
    if (response.status === 204) return { kind: 'ok' };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'notFound' };
    if (response.status === 429) return { kind: 'rateLimited' };
  } catch { /* fall through */ }
  return { kind: 'unavailable' };
}

export const ownGrants: GrantSource = { list: listMcpGrants, revoke: revokeMcpGrant };

/** One page of one account's live connected apps, read by an installation administrator, newest first. */
export async function listAccountMcpGrants(userId: string, cursor?: string): Promise<McpGrantPage> {
  try {
    const { data, error, response } = await api.GET('/api/v1/admin/accounts/{userId}/mcp-grants', {
      params: { path: { userId }, query: { limit: PAGE_SIZE, ...(cursor ? { cursor } : {}) } },
    });
    if (data && response.status === 200) return { kind: 'ok', items: data.items, nextCursor: data.nextCursor };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 403 && isAdminRefusal(error)) return { kind: 'forbidden' };
    if (response.status === 404) return { kind: 'gone' };
  } catch { /* show the retryable unavailable state */ }
  return { kind: 'unavailable' };
}

type AdminRevokeAnswer = { kind: 'ok' | 'unauthenticated' | 'forbidden' | 'rejected' | 'rateLimited' };

/** The answers both administrator revocations share; null leaves the answer to the caller. */
function adminRevokeRefusal({ error, response }: Answer): AdminRevokeAnswer | null {
  if (response.status === 204) return { kind: 'ok' };
  if (response.status === 401) return { kind: 'unauthenticated' };
  if (response.status === 403) return isAdminRefusal(error) ? { kind: 'forbidden' } : { kind: 'rejected' };
  if (response.status === 429) return { kind: 'rateLimited' };
  return null;
}

/**
 * An administrator disconnects one app of one account. 404 means the grant is no longer live or the account is gone;
 * the caller reloads the list, whose own answer tells the two apart.
 */
export async function revokeAccountMcpGrant(userId: string, grantId: string): Promise<McpGrantRevoke> {
  try {
    const answer = await api.DELETE('/api/v1/admin/accounts/{userId}/mcp-grants/{grantId}', {
      params: { path: { userId, grantId }, header: origin() },
    });
    const refused = adminRevokeRefusal(answer);
    if (refused) return refused;
    if (answer.response.status === 404) return { kind: 'notFound' };
  } catch { /* fall through */ }
  return { kind: 'unavailable' };
}

export type McpGrantsRevokeAll = AdminRevokeAnswer | { kind: 'gone' | 'unavailable' };

/** An administrator disconnects every app of one account. 404 is only ever the account itself being gone. */
export async function revokeAllAccountMcpGrants(userId: string): Promise<McpGrantsRevokeAll> {
  try {
    const answer = await api.DELETE('/api/v1/admin/accounts/{userId}/mcp-grants', {
      params: { path: { userId }, header: origin() },
    });
    const refused = adminRevokeRefusal(answer);
    if (refused) return refused;
    if (answer.response.status === 404) return { kind: 'gone' };
  } catch { /* fall through */ }
  return { kind: 'unavailable' };
}

/** One account's grants as an administrator sees and disconnects them. */
export function accountGrants(userId: string): GrantSource {
  return {
    list: (cursor) => listAccountMcpGrants(userId, cursor),
    revoke: (grantId) => revokeAccountMcpGrant(userId, grantId),
  };
}
