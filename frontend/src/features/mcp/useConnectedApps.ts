import { useCallback, useEffect, useRef, useState } from 'react';
import { listMcpGrants, revokeMcpGrant, type McpGrant } from './mcpGrantsApi';

/** `loading` only before the first attempt settles; a failed first load stays `error` so the section can offer Retry. */
export type ConnectedAppsState = 'loading' | 'ready' | 'error';

export type DisconnectOutcome = 'removed' | 'alreadyGone' | 'rateLimited' | 'unavailable' | 'signedOut';

/**
 * The account's connected apps as an append-only list. The first page loads on mount and after every `reload`; Show
 * more appends by cursor; a disconnect removes its row only after the server answers. Each read takes a generation
 * number, so an answer overtaken by a newer read or by unmount never writes stale rows back.
 */
export function useConnectedApps(onSignedOut: () => void) {
  const [state, setState] = useState<ConnectedAppsState>('loading');
  const [grants, setGrants] = useState<McpGrant[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  /** A refresh failed: shown rows stay, and the list offers Try again. */
  const [refreshFailed, setRefreshFailed] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreFailed, setMoreFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const generation = useRef(0);
  const signedOutRef = useRef(onSignedOut);

  useEffect(() => {
    signedOutRef.current = onSignedOut;
  }, [onSignedOut]);

  // The first page is read in the effect, not synchronously in a callback: `reload` only bumps `attempt`.
  useEffect(() => {
    const current = ++generation.current;
    (async () => {
      const page = await listMcpGrants();
      if (current !== generation.current) return;
      if (page.kind === 'unauthenticated') { signedOutRef.current(); return; }
      if (page.kind === 'unavailable') {
        setState((previous) => (previous === 'loading' ? 'error' : previous));
        setRefreshFailed(true);
        return;
      }
      setGrants(page.items);
      setNextCursor(page.nextCursor);
      setMoreFailed(false);
      setRefreshFailed(false);
      setState('ready');
    })();
    return () => { generation.current += 1; };
  }, [attempt]);

  const reload = useCallback(() => setAttempt((n) => n + 1), []);

  async function loadMore() {
    if (!nextCursor || loadingMore) return;
    const current = generation.current;
    setLoadingMore(true);
    const page = await listMcpGrants(nextCursor);
    // Cleared even when a newer read overtook this page, so Show more can never stay stuck while loading.
    setLoadingMore(false);
    if (current !== generation.current) return;
    if (page.kind === 'unauthenticated') { signedOutRef.current(); return; }
    if (page.kind === 'unavailable') { setMoreFailed(true); return; }
    setGrants((existing) => [...existing, ...page.items]);
    setNextCursor(page.nextCursor);
    setMoreFailed(false);
  }

  async function disconnect(grant: McpGrant): Promise<DisconnectOutcome> {
    const result = await revokeMcpGrant(grant.id);
    if (result.kind === 'unauthenticated') { signedOutRef.current(); return 'signedOut'; }
    if (result.kind === 'rateLimited') return 'rateLimited';
    if (result.kind === 'unavailable') return 'unavailable';
    setGrants((existing) => existing.filter((item) => item.id !== grant.id));
    if (result.kind === 'ok') return 'removed';
    // 404: already disconnected elsewhere. The row is dropped and the list is read again quietly to match the server.
    reload();
    return 'alreadyGone';
  }

  return {
    state, grants, nextCursor, refreshFailed, loadingMore, moreFailed,
    retry: reload, loadMore, disconnect,
  };
}

export type ConnectedApps = ReturnType<typeof useConnectedApps>;
