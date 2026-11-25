import { useRef, useState, type RefObject } from 'react';
import type { McpGrantsRevokeAll } from './mcpGrantsApi';
import type { AppDisconnect, DialogError } from './useAppDisconnect';
import type { ConnectedApps, ConnectedAppsCallbacks } from './useConnectedApps';

/**
 * Disconnect all, for an administrator: one confirmation, one request, and the rows leave only after the server's
 * 204. A refusal that changed nothing stays in the dialog; a signed-out session, a lost installation grant or a gone
 * account close it and hand the answer to the screen. Nothing starts while a single disconnect is in flight.
 */
export function useDisconnectAll({ revokeAll, apps, disconnect, callbacks, clearedFocusRef }: {
  revokeAll: () => Promise<McpGrantsRevokeAll>;
  apps: Pick<ConnectedApps, 'cleared'>;
  disconnect: Pick<AppDisconnect, 'busy' | 'announce'>;
  callbacks: Required<ConnectedAppsCallbacks>;
  /** Where focus goes once every row is gone, since the control that opened the dialog leaves with them. */
  clearedFocusRef: RefObject<HTMLElement | null>;
}) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<DialogError | null>(null);
  const triggerRef = useRef<HTMLElement | null>(null);
  /** Empty except after a success, so a cancelled dialog still hands focus back to its trigger. */
  const returnFocusRef = useRef<HTMLElement | null>(null);
  const busyRef = useRef(false);

  function request(trigger: HTMLElement) {
    if (busyRef.current || disconnect.busy) return;
    triggerRef.current = trigger;
    returnFocusRef.current = null;
    // A new request makes the previous outcome stale, so it goes before the dialog opens.
    disconnect.announce(null);
    setError(null);
    setOpen(true);
  }

  function cancel() {
    if (busyRef.current) return;
    setError(null);
    setOpen(false);
  }

  function settle(result: McpGrantsRevokeAll) {
    if (result.kind === 'rateLimited' || result.kind === 'unavailable' || result.kind === 'rejected') {
      setError(result.kind);
      return;
    }
    setOpen(false);
    if (result.kind === 'unauthenticated') callbacks.onSignedOut();
    else if (result.kind === 'forbidden') callbacks.onForbidden();
    else if (result.kind === 'gone') callbacks.onGone();
    else {
      // Set in the same render that closes the dialog, so its close hands focus here instead of to a removed trigger.
      returnFocusRef.current = clearedFocusRef.current;
      apps.cleared();
      disconnect.announce({ key: 'accountApps.notice.allDisconnected', client: '' });
    }
  }

  async function confirm() {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    setError(null);
    const result = await revokeAll();
    busyRef.current = false;
    setBusy(false);
    settle(result);
  }

  return { open, busy, error, triggerRef, returnFocusRef, request, cancel, confirm: () => { void confirm(); } };
}

export type DisconnectAll = ReturnType<typeof useDisconnectAll>;
