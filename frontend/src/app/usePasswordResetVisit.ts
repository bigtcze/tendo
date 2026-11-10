import { useEffect, useState } from 'react';
import { RESET_PASSWORD_PATH } from './routes';

function readResetToken(): string | null {
  if (window.location.pathname !== RESET_PASSWORD_PATH) return null;
  const hash = window.location.hash;
  return hash.startsWith('#') ? hash.slice(1) || null : null;
}

/**
 * The reset token lives in the fragment so it never reaches the server, a proxy or another site's address.
 * It is read once at start, kept in memory only and immediately removed from the address bar; a reload
 * therefore shows the missing-link state instead of retrying a secret that has already been used.
 */
export function usePasswordResetVisit(): string | null {
  const [token, setToken] = useState(readResetToken);

  useEffect(() => {
    if (window.location.pathname === RESET_PASSWORD_PATH && window.location.hash) {
      window.history.replaceState(null, '', RESET_PASSWORD_PATH);
    }
    const capture = () => {
      if (window.location.pathname !== RESET_PASSWORD_PATH || !window.location.hash) return;
      const next = window.location.hash.slice(1) || null;
      window.history.replaceState(null, '', RESET_PASSWORD_PATH);
      setToken(next);
    };
    window.addEventListener('hashchange', capture);
    window.addEventListener('popstate', capture);
    return () => {
      window.removeEventListener('hashchange', capture);
      window.removeEventListener('popstate', capture);
    };
  }, []);

  return token;
}
