import { useCallback, useEffect, useRef, useState } from 'react';
import type { GrantSource, McpGrant, McpGrantPage } from './mcpGrantsApi';

/** `loading` only before the first attempt settles; a failed first load stays `error` so the section can offer Retry. */
export type ConnectedAppsState = 'loading' | 'ready' | 'error';

export type DisconnectOutcome = 'removed' | 'alreadyGone' | 'rateLimited' | 'unavailable' | 'rejected' | 'signedOut' | 'forbidden';

/**
 * What the list hands back to its screen. `onForbidden` and `onGone` are only reached by an administrator's list of
 * another account: the installation grant is gone, or the account itself is. `duringAction` is false only for the
 * first read on arrival; any later answer follows something the reader did, so the screen moves focus for it.
 */
export type ConnectedAppsCallbacks = {
  onSignedOut: () => void;
  onForbidden?: (duringAction: boolean) => void;
  onGone?: (duringAction: boolean) => void;
};

/**
 * Connected apps as an append-only list read from one grant source. The first page loads on mount and after every
 * `reload`; Show more appends by cursor; a disconnect removes its row only after the server answers. Each read takes a
 * generation number, so an answer overtaken by a newer read or by unmount never writes stale rows back. The source
 * and callbacks are read when used, so callers need not keep them stable; a list for another account mounts anew.
 */
export function useConnectedApps(source: GrantSource, callbacks: ConnectedAppsCallbacks) {
  const [state, setState] = useState<ConnectedAppsState>('loading');
  const [grants, setGrants] = useState<McpGrant[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  /** A refresh failed: shown rows stay, and the list offers Try again. */
  const [refreshFailed, setRefreshFailed] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreFailed, setMoreFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const generation = useRef(0);
  const sourceRef = useRef(source);
  const callbacksRef = useRef(callbacks);

  useEffect(() => {
    sourceRef.current = source;
    callbacksRef.current = callbacks;
  });

  /** The answers every read shares that end the list's own work; true when the page was one of them. */
  const handedBack = useCallback((page: McpGrantPage, duringAction: boolean) => {
    const { onSignedOut, onForbidden, onGone } = callbacksRef.current;
    if (page.kind === 'unauthenticated') { onSignedOut(); return true; }
    if (page.kind === 'forbidden' && onForbidden) { onForbidden(duringAction); return true; }
    if (page.kind === 'gone' && onGone) { onGone(duringAction); return true; }
    return false;
  }, []);

  // The first page is read in the effect, not synchronously in a callback: `reload` only bumps `attempt`.
  useEffect(() => {
    const current = ++generation.current;
    (async () => {
      const page = await sourceRef.current.list();
      // Only the first read (attempt 0) is on arrival; a retry or a re-read after a disconnect follows an action.
      if (current !== generation.current || handedBack(page, attempt > 0)) return;
      if (page.kind !== 'ok') {
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
  }, [attempt, handedBack]);

  const reload = useCallback(() => setAttempt((n) => n + 1), []);

  async function loadMore() {
    if (!nextCursor || loadingMore) return;
    const current = generation.current;
    setLoadingMore(true);
    const page = await sourceRef.current.list(nextCursor);
    // Cleared even when a newer read overtook this page, so Show more can never stay stuck while loading.
    setLoadingMore(false);
    if (current !== generation.current || handedBack(page, true)) return;
    if (page.kind !== 'ok') { setMoreFailed(true); return; }
    setGrants((existing) => [...existing, ...page.items]);
    setNextCursor(page.nextCursor);
    setMoreFailed(false);
  }

  async function disconnect(grant: McpGrant): Promise<DisconnectOutcome> {
    const result = await sourceRef.current.revoke(grant.id);
    if (result.kind === 'unauthenticated') { callbacksRef.current.onSignedOut(); return 'signedOut'; }
    if (result.kind === 'forbidden') { callbacksRef.current.onForbidden?.(true); return 'forbidden'; }
    if (result.kind === 'rateLimited' || result.kind === 'unavailable' || result.kind === 'rejected') return result.kind;
    setGrants((existing) => existing.filter((item) => item.id !== grant.id));
    if (result.kind === 'ok') return 'removed';
    // 404: already disconnected elsewhere. The row is dropped and the list is read again quietly to match the server;
    // for an administrator that read also finds out whether the account itself is gone.
    reload();
    return 'alreadyGone';
  }

  /** Every app is gone on the server: the rows leave at once and the list is read again to match it. */
  function cleared() {
    setGrants([]);
    setNextCursor(null);
    setMoreFailed(false);
    reload();
  }

  return {
    state, grants, nextCursor, refreshFailed, loadingMore, moreFailed,
    retry: reload, loadMore, disconnect, cleared,
  };
}

export type ConnectedApps = ReturnType<typeof useConnectedApps>;
