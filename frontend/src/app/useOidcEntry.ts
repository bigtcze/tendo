import { useCallback, useEffect, useState } from 'react';
import type { MessageKey } from '../i18n';
import { isOidcContinueFragment, oidcNoticeFromFragment } from './oidcEntry';

/** Entry state the OIDC callback leaves in the URL; it is consumed once and then removed from the address bar. */
export type OidcEntry = {
  /** The provider sign-in still needs a decision (existing account or a new one). */
  continueRequested: boolean;
  /** One-time calm notice for the signed-in Home screen. */
  notice: MessageKey | null;
};

export type OidcEntryActions = {
  clearContinue: () => void;
  requestContinue: () => void;
  clearNotice: () => void;
  showNotice: (notice: MessageKey | null) => void;
};

export function useOidcEntry(): OidcEntry & OidcEntryActions {
  const [initial] = useState(() => ({
    continueRequested: isOidcContinueFragment(window.location.pathname, window.location.hash),
    notice: oidcNoticeFromFragment(window.location.pathname, window.location.hash),
  }));
  const [continueRequested, setContinueRequested] = useState(initial.continueRequested);
  const [notice, setNotice] = useState<MessageKey | null>(initial.notice);

  // The notice fragment is read once at start; keep the address bar clean without leaving the Home path.
  useEffect(() => {
    if (initial.notice) window.history.replaceState(window.history.state, '', '/');
  }, [initial]);

  const clearContinue = useCallback(() => setContinueRequested(false), []);
  const requestContinue = useCallback(() => {
    setContinueRequested(true);
    setNotice(null);
  }, []);
  const clearNotice = useCallback(() => setNotice(null), []);
  const showNotice = useCallback((next: MessageKey | null) => setNotice(next), []);

  return { continueRequested, notice, clearContinue, requestContinue, clearNotice, showNotice };
}
