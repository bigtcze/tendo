import { useEffect, useState } from 'react';
import { CONNECT_PATH } from './routes';
import {
  clearConnectEntry, parseConnectFragment, readConnectEntry, saveConnectHandle,
  type ConnectOutcome,
} from '../features/mcp/connectEntry';

export type { ConnectOutcome };

export type ConnectVisit = { visit: number; outcome: ConnectOutcome };

/** The meaning of the fragment on the consent page; no side effects, so it is safe during render. */
function fragmentOutcome(): ConnectOutcome | null {
  if (window.location.pathname !== CONNECT_PATH || !window.location.hash) return null;
  const fragment = parseConnectFragment(window.location.hash);
  if (fragment?.kind === 'request') return { kind: 'request', handle: fragment.handle };
  if (fragment?.kind === 'error') return { kind: 'error', code: fragment.code };
  return { kind: 'invalid' };
}

/** Without a fragment, a fresh stored handle survives a reload or the provider round trip. */
function storedOutcome(): ConnectOutcome {
  const entry = readConnectEntry();
  return entry ? { kind: 'request', handle: entry.handle } : { kind: 'none' };
}

/**
 * Stores the outcome and removes the fragment from the address bar, like the password-reset link. A valid request
 * is kept in the short-lived store; anything else clears it so a stale handle cannot be retried.
 */
function consumeFragment(outcome: ConnectOutcome): void {
  window.history.replaceState(null, '', CONNECT_PATH);
  if (outcome.kind === 'request') saveConnectHandle(outcome.handle);
  else clearConnectEntry();
}

export function useConnectVisit(): ConnectVisit {
  const [visit, setVisit] = useState<ConnectVisit>(() => ({ visit: 0, outcome: fragmentOutcome() ?? storedOutcome() }));

  useEffect(() => {
    // The initial fragment is consumed once here; the state above already reflects it.
    const initial = fragmentOutcome();
    if (initial) consumeFragment(initial);

    const capture = () => {
      const outcome = fragmentOutcome();
      if (!outcome) return;
      consumeFragment(outcome);
      setVisit((current) => ({ visit: current.visit + 1, outcome }));
    };
    window.addEventListener('hashchange', capture);
    window.addEventListener('popstate', capture);
    return () => {
      window.removeEventListener('hashchange', capture);
      window.removeEventListener('popstate', capture);
    };
  }, []);

  return visit;
}
