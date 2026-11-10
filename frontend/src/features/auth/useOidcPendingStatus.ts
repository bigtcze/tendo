import { useEffect, useRef, useState } from 'react';
import { fetchOidcPending, type OidcPendingStatus } from './oidcPendingApi';

export type ContinueLoad = 'loading' | 'ready' | 'unavailable' | 'rateLimited' | 'alreadySignedIn';

/**
 * Reads the continuation behind the pending cookie. Read-only, so retrying is safe; an absent or
 * expired continuation leaves this screen immediately through `onExpired`.
 */
export function useOidcPendingStatus(onExpired: () => void) {
  const [load, setLoad] = useState<ContinueLoad>('loading');
  const [status, setStatus] = useState<OidcPendingStatus | null>(null);
  const mountedRef = useRef(false);
  const startedRef = useRef(false);
  const expiredRef = useRef(onExpired);

  useEffect(() => { expiredRef.current = onExpired; });
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  async function loadPending() {
    setLoad('loading');
    const result = await fetchOidcPending();
    if (!mountedRef.current) return;
    if (result.kind === 'ok') {
      setStatus(result.status);
      setLoad('ready');
      return;
    }
    if (result.kind === 'expired') { expiredRef.current(); return; }
    setLoad(result.kind);
  }

  // One read per mount; StrictMode's second setup reuses the first result.
  useEffect(() => {
    if (startedRef.current) return;
    startedRef.current = true;
    void loadPending();
  }, []);

  return { load, setLoad, status, retry: () => void loadPending() };
}
