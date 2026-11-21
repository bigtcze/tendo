import type { components } from '../../../../api/generated/api';
import { api } from '../../lib/api';

export type McpGrant = components['schemas']['McpGrant'];

const PAGE_SIZE = 20;

export type McpGrantPage =
  | { kind: 'ok'; items: McpGrant[]; nextCursor: string | null }
  | { kind: 'unauthenticated' }
  | { kind: 'unavailable' };

export type McpGrantRevoke =
  | { kind: 'ok' }
  | { kind: 'notFound' | 'rateLimited' | 'unauthenticated' | 'unavailable' };

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
      params: {
        path: { grantId },
        // Browsers set Origin themselves and ignore this value; it only satisfies the generated type.
        header: { Origin: window.location.origin },
      },
    });
    if (response.status === 204) return { kind: 'ok' };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'notFound' };
    if (response.status === 429) return { kind: 'rateLimited' };
  } catch { /* fall through */ }
  return { kind: 'unavailable' };
}
