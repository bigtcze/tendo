import { useEffect, useState } from 'react';
import { parseOidcFragment, type OidcErrorCode } from '../features/auth/oidcApi';
import { ACCOUNT_PATH, LOGIN_PATH } from './routes';

export function useOidcOutcome() {
  const [oidcOutcome, setOidcOutcome] = useState<OidcErrorCode | null>(() =>
    window.location.pathname === LOGIN_PATH
      ? parseOidcFragment(window.location.hash)
      : null,
  );

  useEffect(() => {
    const path = window.location.pathname;
    // /account also scrubs the retired linking fragment so a legacy API client never leaves a stale hash.
    if ((path === LOGIN_PATH || path === ACCOUNT_PATH) && window.location.hash) {
      window.history.replaceState(window.history.state, '', path);
    }
  }, []);

  return [oidcOutcome, setOidcOutcome] as const;
}
