import { useEffect, useState } from 'react';
import { parseOidcFragment, type OidcOutcome } from '../features/auth/oidcApi';
import { ACCOUNT_PATH, LOGIN_PATH } from './routes';

export function useOidcOutcome() {
  const [oidcOutcome, setOidcOutcome] = useState<OidcOutcome | null>(() => {
    const path = window.location.pathname;
    return path === LOGIN_PATH || path === ACCOUNT_PATH
      ? parseOidcFragment(window.location.hash)
      : null;
  });

  useEffect(() => {
    const path = window.location.pathname;
    if ((path === LOGIN_PATH || path === ACCOUNT_PATH) && window.location.hash) {
      window.history.replaceState(window.history.state, '', path);
    }
  }, []);

  return [oidcOutcome, setOidcOutcome] as const;
}
