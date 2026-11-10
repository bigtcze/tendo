import { useEffect, useRef, useState } from 'react';
import { discardOidcAccount } from './oidcPendingApi';

export type DiscardFailure = 'notDiscardable' | 'disabled' | 'rateLimited' | 'unavailable' | null;

/**
 * Removes the just-created OIDC account so the same provider sign-in can be connected to an
 * existing Tendo account instead. Only the server decides whether that is allowed.
 */
export function useDiscardAccount({ onSignedOut, onDiscarded }: {
  onSignedOut: () => void;
  onDiscarded: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState<DiscardFailure>(null);
  const pendingRef = useRef(false);
  const mountedRef = useRef(false);
  const callbacksRef = useRef({ onSignedOut, onDiscarded });

  useEffect(() => { callbacksRef.current = { onSignedOut, onDiscarded }; });
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  function openDialog() {
    setFailure(null);
    setOpen(true);
  }

  function closeDialog() {
    if (pendingRef.current) return;
    setOpen(false);
  }

  async function confirm() {
    if (pendingRef.current) return;
    pendingRef.current = true;
    setPending(true);
    setFailure(null);
    const result = await discardOidcAccount();
    if (!mountedRef.current) return;
    if (result.kind === 'ok') { callbacksRef.current.onDiscarded(); return; }
    pendingRef.current = false;
    setPending(false);
    setOpen(false);
    if (result.kind === 'unauthenticated') { callbacksRef.current.onSignedOut(); return; }
    setFailure(result.kind);
  }

  return { open, pending, failure, openDialog, closeDialog, confirm: () => void confirm(), clearFailure: () => setFailure(null) };
}
