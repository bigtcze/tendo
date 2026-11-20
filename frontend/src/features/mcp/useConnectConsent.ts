import { useCallback, useEffect, useRef, useState } from 'react';
import { browserNavigation } from '../auth/oidcApi';
import { clearConnectEntry } from './connectEntry';
import {
  decideConnectRequest, fetchConnectRequest, isSafeRedirect,
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
 * Calls `onArmed` once the page has stayed visible and focused for ALLOW_DELAY_MS in a row. Blur or hiding cancels the
 * timer; focus or showing starts it again. Nothing runs while `active` is false.
 */
function useAllowDelay(active: boolean, onArmed: () => void): void {
  const armedRef = useRef(onArmed);
  useEffect(() => { armedRef.current = onArmed; });
  useEffect(() => {
    if (!active) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const cancel = () => { clearTimeout(timer); timer = undefined; };
    const arm = () => {
      cancel();
      if (document.visibilityState !== 'visible' || !document.hasFocus()) return;
      timer = setTimeout(() => { timer = undefined; armedRef.current(); }, ALLOW_DELAY_MS);
    };
    const onVisibility = () => (document.visibilityState === 'visible' ? arm() : cancel());
    window.addEventListener('focus', arm);
    window.addEventListener('blur', cancel);
    document.addEventListener('visibilitychange', onVisibility);
    arm();
    return () => {
      cancel();
      window.removeEventListener('focus', arm);
      window.removeEventListener('blur', cancel);
      document.removeEventListener('visibilitychange', onVisibility);
    };
  }, [active]);
}

export function useConnectConsent(handle: string, { onSessionEnded }: { onSessionEnded: () => void }) {
  const [state, setState] = useState<ConsentState>({ kind: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const inFlight = useRef(false);
  const finished = useRef(false);
  const sessionEnded = useRef(onSessionEnded);
  useEffect(() => { sessionEnded.current = onSessionEnded; });

  useEffect(() => {
    let cancelled = false;
    fetchConnectRequest(handle).then((result) => {
      if (cancelled) return;
      if (result.kind === 'ok') {
        setState({ kind: 'ready', request: result.request, allowEnabled: false, decisionError: null });
      } else if (result.kind === 'unauthenticated') {
        sessionEnded.current();
      } else if (result.kind === 'notFound') {
        clearConnectEntry();
        setState({ kind: 'notFound' });
      } else if (result.kind === 'rateLimited') {
        setState({ kind: 'rateLimited' });
      } else {
        setState({ kind: 'unavailable' });
      }
    });
    return () => { cancelled = true; };
  }, [handle, attempt]);

  const awaitingAllow = state.kind === 'ready' && !state.allowEnabled;
  useAllowDelay(awaitingAllow, () => setState((current) => (current.kind === 'ready' ? { ...current, allowEnabled: true } : current)));

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
    if (decision === 'approve' && !state.allowEnabled) return;
    const { request, allowEnabled } = state;
    // The ref blocks a second POST from a double click before React re-renders the disabled buttons.
    inFlight.current = true;
    setState({ kind: 'deciding', request, allowEnabled });
    decideConnectRequest(handle, decision).then((result) => {
      if (result.kind === 'redirect') {
        clearConnectEntry();
        finished.current = true;
        if (!isSafeRedirect(request.redirectUri, result.redirectTo)) {
          setState({ kind: 'invalidResponse' });
          return;
        }
        setState({ kind: 'done' });
        browserNavigation.assign(result.redirectTo);
        return;
      }
      inFlight.current = false;
      if (result.kind === 'notFound') {
        clearConnectEntry();
        setState({ kind: 'notFound' });
      } else if (result.kind === 'unauthenticated') {
        sessionEnded.current();
      } else {
        setState({ kind: 'ready', request, allowEnabled, decisionError: result.kind === 'rateLimited' ? 'rateLimited' : 'unavailable' });
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
