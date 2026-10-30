import { useEffect, useRef, useState, type FormEvent } from 'react';
import { browserNavigation, startOidc, type StartResult } from './oidcApi';

export type LinkStartFailure = Exclude<StartResult['kind'], 'redirect' | 'unauthenticated'>;

export function useOidcLinkStart(onSignedOut: () => void, onDisabled: () => void, focusPassword: () => void) {
  const [password, setPassword] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<LinkStartFailure | null>(null);
  const pendingRef = useRef(false);
  const mountedRef = useRef(false);
  const callbacks = useRef({ onSignedOut, onDisabled, focusPassword });

  useEffect(() => { callbacks.current = { onSignedOut, onDisabled, focusPassword }; });
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  async function connect(event: FormEvent) {
    event.preventDefault();
    if (pendingRef.current) return;
    pendingRef.current = true;
    setPending(true);
    setError(null);
    const result = await startOidc({ purpose: 'link', currentPassword: password });
    if (!mountedRef.current) return;
    if (result.kind === 'redirect') {
      browserNavigation.assign(result.url);
      return;
    }
    if (result.kind === 'unauthenticated') {
      callbacks.current.onSignedOut();
      return;
    }
    pendingRef.current = false;
    setPending(false);
    setPassword('');
    if (result.kind === 'disabled') callbacks.current.onDisabled();
    setError(result.kind);
    callbacks.current.focusPassword();
  }

  return { password, setPassword, pending, error, connect };
}
