import { useCallback, useEffect, useRef, useState } from 'react';
import { browserNavigation } from '../auth/oidcApi';
import { clearConnectEntry, readConnectEntry } from './connectEntry';
import {
  decideConnectRequest, fetchConnectRequest, grantedScopes, isBlocked, isSafeRedirect,
  type ConnectDecision, type McpAuthorizationRequest,
} from './connectApi';

/** A visible, focused page must stay that way this long before Allow is enabled (click-jacking and double-click guard). */
export const ALLOW_DELAY_MS = 1000;

/**
 * Decision failures keep the request on screen: a `ready` state with `decisionError` set, so the buttons come back.
 * Only a failed load gets `unavailable`/`rateLimited` with `retry()`. A redirect that fails the safety check is
 * `invalidResponse`: the request was already consumed server-side, so retrying would only answer 404.
 */
export type ConsentState =
  | { kind: 'loading' }
  | { kind: 'ready'; request: McpAuthorizationRequest; allowEnabled: boolean; decisionError: 'rateLimited' | 'unavailable' | null }
  | { kind: 'deciding'; request: McpAuthorizationRequest; allowEnabled: boolean }
  | { kind: 'done' }
  | { kind: 'notFound' }
  | { kind: 'rateLimited' }
  | { kind: 'unavailable' }
  | { kind: 'invalidResponse' };

/**
 * Keeps Allow in step with focus and visibility for as long as the request is ready. Blur or hiding disables Allow
 * at once; focus or showing starts a fresh ALLOW_DELAY_MS timer that enables it. Nothing runs while `active` is false.
 */
function useAllowDelay(active: boolean, setAllow: (enabled: boolean) => void): void {
  const setRef = useRef(setAllow);
  useEffect(() => { setRef.current = setAllow; });
  useEffect(() => {
    if (!active) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const cancel = () => { clearTimeout(timer); timer = undefined; };
    const disable = () => { cancel(); setRef.current(false); };
    const arm = () => {
      cancel();
      setRef.current(false);
      if (document.visibilityState !== 'visible' || !document.hasFocus()) return;
      timer = setTimeout(() => { timer = undefined; setRef.current(true); }, ALLOW_DELAY_MS);
    };
    const onVisibility = () => (document.visibilityState === 'visible' ? arm() : disable());
    window.addEventListener('focus', arm);
    window.addEventListener('blur', disable);
    document.addEventListener('visibilitychange', onVisibility);
    arm();
    return () => {
      cancel();
      window.removeEventListener('focus', arm);
      window.removeEventListener('blur', disable);
      document.removeEventListener('visibilitychange', onVisibility);
    };
  }, [active]);
}

/**
 * Removes the stored entry only while it still belongs to this handle. A newer visit saves its own handle, and that
 * entry must survive an older visit's late answer.
 */
function clearIfCurrent(handle: string): void {
  if (readConnectEntry()?.handle === handle) clearConnectEntry();
}

export function useConnectConsent(handle: string, { onSessionEnded }: { onSessionEnded: () => void }) {
  const [state, setState] = useState<ConsentState>({ kind: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const inFlight = useRef(false);
  const finished = useRef(false);
  const alive = useRef(true);
  const sessionEnded = useRef(onSessionEnded);
  useEffect(() => { sessionEnded.current = onSessionEnded; });
  useEffect(() => {
    alive.current = true;
    return () => { alive.current = false; };
  }, []);

  useEffect(() => {
    let cancelled = false;
    fetchConnectRequest(handle).then((result) => {
      if (cancelled) return;
      if (result.kind === 'ok') {
        setState({ kind: 'ready', request: result.request, allowEnabled: false, decisionError: null });
      } else if (result.kind === 'unauthenticated') {
        sessionEnded.current();
      } else if (result.kind === 'notFound') {
        clearIfCurrent(handle);
        setState({ kind: 'notFound' });
      } else if (result.kind === 'rateLimited') {
        setState({ kind: 'rateLimited' });
      } else {
        setState({ kind: 'unavailable' });
      }
    });
    return () => { cancelled = true; };
  }, [handle, attempt]);

  // A blocked request never arms Allow: there is nothing it could grant.
  const isReady = state.kind === 'ready' && !isBlocked(state.request);
  useAllowDelay(isReady, (enabled) => setState((current) => (current.kind === 'ready' ? { ...current, allowEnabled: enabled } : current)));

  // A page restored from the back-forward cache must keep the finished state and its disabled buttons.
  useEffect(() => {
    const onPageShow = (event: PageTransitionEvent) => {
      if (event.persisted && finished.current) setState({ kind: 'done' });
    };
    window.addEventListener('pageshow', onPageShow);
    return () => window.removeEventListener('pageshow', onPageShow);
  }, []);

  const decide = useCallback((decision: ConnectDecision) => {
    if (state.kind !== 'ready' || finished.current || inFlight.current) return;
    if (decision === 'approve' && (!state.allowEnabled || isBlocked(state.request))) return;
    const { request, allowEnabled } = state;
    // The ref blocks a second POST from a double click before React re-renders the disabled buttons.
    inFlight.current = true;
    setState({ kind: 'deciding', request, allowEnabled });
    // An approval names what the screen showed, so the server never grants more even if the policy widened since.
    const shown = decision === 'approve' ? grantedScopes(request).allowedScopes : undefined;
    decideConnectRequest(handle, decision, shown).then((result) => {
      if (result.kind === 'redirect') {
        // The server consumed the request, so a safe redirect is followed even after unmount.
        clearIfCurrent(handle);
        finished.current = true;
        if (!isSafeRedirect(request.redirectUri, result.redirectTo)) {
          if (alive.current) setState({ kind: 'invalidResponse' });
          return;
        }
        if (alive.current) setState({ kind: 'done' });
        browserNavigation.assign(result.redirectTo);
        return;
      }
      inFlight.current = false;
      if (!alive.current) return;
      if (result.kind === 'notFound') {
        clearIfCurrent(handle);
        setState({ kind: 'notFound' });
      } else if (result.kind === 'unauthenticated') {
        sessionEnded.current();
      } else {
        // Allow re-arms through the delay from scratch; it never comes back enabled from a failed decision.
        setState({ kind: 'ready', request, allowEnabled: false, decisionError: result.kind === 'rateLimited' ? 'rateLimited' : 'unavailable' });
      }
    });
  }, [handle, state]);

  const retry = useCallback(() => {
    if (state.kind !== 'unavailable' && state.kind !== 'rateLimited') return;
    setState({ kind: 'loading' });
    setAttempt((count) => count + 1);
  }, [state.kind]);

  return { state, decide, retry };
}
