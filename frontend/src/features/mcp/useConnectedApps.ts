import { useCallback, useEffect, useRef } from 'react';
import { useShowMoreList } from '../../app/useShowMoreList';
import type { GrantSource, McpGrant, McpGrantPage } from './mcpGrantsApi';

type Failed = Exclude<McpGrantPage, { kind: 'ok' }>;

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
 * Connected apps as an append-only list read from one grant source (see `useShowMoreList`); a disconnect removes its
 * row only after the server answers. The source and callbacks are read when used, so callers need not keep them
 * stable; a list for another account mounts anew.
 */
export function useConnectedApps(source: GrantSource, callbacks: ConnectedAppsCallbacks) {
  const sourceRef = useRef(source);
  const callbacksRef = useRef(callbacks);

  useEffect(() => {
    sourceRef.current = source;
    callbacksRef.current = callbacks;
  });

  /** The answers every read shares that end the list's own work; true when the page was one of them. */
  const handOff = useCallback((page: Failed, duringAction: boolean) => {
    const { onSignedOut, onForbidden, onGone } = callbacksRef.current;
    if (page.kind === 'unauthenticated') { onSignedOut(); return true; }
    if (page.kind === 'forbidden' && onForbidden) { onForbidden(duringAction); return true; }
    if (page.kind === 'gone' && onGone) { onGone(duringAction); return true; }
    return false;
  }, []);

  const list = useShowMoreList<McpGrant, Failed>({ read: (cursor) => sourceRef.current.list(cursor), handOff });
  const { reload, remove } = list;

  async function disconnect(grant: McpGrant): Promise<DisconnectOutcome> {
    const result = await sourceRef.current.revoke(grant.id);
    if (result.kind === 'unauthenticated') { callbacksRef.current.onSignedOut(); return 'signedOut'; }
    if (result.kind === 'forbidden') { callbacksRef.current.onForbidden?.(true); return 'forbidden'; }
    if (result.kind === 'rateLimited' || result.kind === 'unavailable' || result.kind === 'rejected') return result.kind;
    remove((item) => item.id !== grant.id);
    if (result.kind === 'ok') return 'removed';
    // 404: already disconnected elsewhere. The row is dropped and the list is read again quietly to match the server;
    // for an administrator that read also finds out whether the account itself is gone.
    reload();
    return 'alreadyGone';
  }

  return {
    state: list.state, grants: list.items, nextCursor: list.nextCursor, refreshFailed: list.refreshFailed,
    loadingMore: list.loadingMore, moreFailed: list.moreFailed,
    retry: reload, loadMore: list.loadMore, disconnect, cleared: list.cleared,
  };
}

export type ConnectedApps = ReturnType<typeof useConnectedApps>;
