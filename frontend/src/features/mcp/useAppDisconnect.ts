import { useEffect, useRef, useState } from 'react';
import type { MessageKey } from '../../i18n';
import type { McpGrant } from './mcpGrantsApi';
import type { ConnectedApps, DisconnectOutcome } from './useConnectedApps';

/** A refusal that keeps the dialog open so the person can try again or cancel; `rejected` changed nothing. */
export type DialogError = 'rateLimited' | 'unavailable' | 'rejected';

/** `client` names the app for a one-app outcome; an outcome about every app names none. */
export type AppNotice = { key: MessageKey; client?: string };

/**
 * How long the outcome sentence stays on screen. Long enough for a screen reader to finish announcing it and for a
 * sighted person to read it; short enough that an empty section does not linger on Account. It is a judgement call.
 */
export const NOTICE_VISIBLE_MS = 8000;

/** Where focus goes after a removal: the row that took the removed row's place, else the one above it. */
export type FocusPlan = { id: string; index: number };

/**
 * The disconnect dialog's state machine. The removed row leaves the list only after the server answers 204 (or 404,
 * which means it is already gone). The trigger is disabled only while its own disconnect is in flight, never by a
 * quiet reload (L020).
 */
export function useAppDisconnect(apps: Pick<ConnectedApps, 'grants' | 'disconnect'>) {
  const [pending, setPending] = useState<McpGrant | null>(null);
  const [busy, setBusy] = useState(false);
  const [dialogError, setDialogError] = useState<DialogError | null>(null);
  const [notice, setNotice] = useState<AppNotice | null>(null);
  const triggerRef = useRef<HTMLElement | null>(null);
  const busyRef = useRef(false);
  const focusPlanRef = useRef<FocusPlan | null>(null);
  const noticeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  function clearNoticeTimer() {
    if (noticeTimer.current !== null) clearTimeout(noticeTimer.current);
    noticeTimer.current = null;
  }

  // A pending notice timer must not fire after unmount, or it would set state on a gone section.
  useEffect(() => clearNoticeTimer, []);

  function request(grant: McpGrant, trigger: HTMLElement) {
    if (busyRef.current) return;
    triggerRef.current = trigger;
    // A new request makes the previous outcome stale, so it goes before the dialog opens.
    clearNoticeTimer();
    setNotice(null);
    setDialogError(null);
    setPending(grant);
  }

  function showNotice(next: AppNotice) {
    clearNoticeTimer();
    setNotice(next);
    noticeTimer.current = setTimeout(() => {
      noticeTimer.current = null;
      setNotice(null);
    }, NOTICE_VISIBLE_MS);
  }

  function cancel() {
    if (busyRef.current) return;
    setDialogError(null);
    setPending(null);
  }

  function settle(outcome: DisconnectOutcome, grant: McpGrant, index: number) {
    if (outcome === 'removed' || outcome === 'alreadyGone') {
      showNotice({ key: outcome === 'removed' ? 'connectedApps.notice.disconnected' : 'connectedApps.notice.alreadyGone', client: grant.clientName });
      focusPlanRef.current = { id: grant.id, index };
      setPending(null);
      return;
    }
    // Both end the list's own work: the screen signs out, or says the installation grant is gone.
    if (outcome === 'signedOut' || outcome === 'forbidden') { setPending(null); return; }
    setDialogError(outcome);
  }

  async function confirm() {
    if (!pending || busyRef.current) return;
    const grant = pending;
    const index = apps.grants.findIndex((item) => item.id === grant.id);
    busyRef.current = true;
    setBusy(true);
    setDialogError(null);
    const outcome = await apps.disconnect(grant);
    busyRef.current = false;
    setBusy(false);
    settle(outcome, grant, index);
  }

  /** Hands out the pending focus move once, after the dialog has closed and the row has left the list. */
  function takeFocusPlan(): FocusPlan | null {
    const plan = focusPlanRef.current;
    focusPlanRef.current = null;
    return plan;
  }

  return {
    pending, busy, dialogError, notice, triggerRef, takeFocusPlan,
    request, cancel, confirm: () => { void confirm(); },
    /** Says an outcome the list reached some other way, such as disconnecting every app at once; null clears it. */
    announce: (next: AppNotice | null) => {
      if (next) { showNotice(next); return; }
      clearNoticeTimer();
      setNotice(null);
    },
  };
}

export type AppDisconnect = ReturnType<typeof useAppDisconnect>;
