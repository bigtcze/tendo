import { useEffect, useRef, useState, type FormEvent } from 'react';
import { browserNavigation, startOidc, type StartResult } from './oidcApi';
import { login, type Session } from './sessionApi';

type LoginFailure = 'invalid' | 'rateLimited' | 'unavailable';
type OidcFailure = Exclude<StartResult['kind'], 'redirect'>;

export function useLoginActions({ onSignedIn, focusPassword, focusProvider, onProviderDisabled }: {
  onSignedIn: (session: Session) => void;
  focusPassword: () => void;
  focusProvider: () => void;
  onProviderDisabled: () => void;
}) {
  const [loginName, setLoginName] = useState('');
  const [password, setPassword] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<LoginFailure | null>(null);
  const [oidcPending, setOidcPending] = useState(false);
  const [oidcError, setOidcError] = useState<OidcFailure | null>(null);
  const pendingOidcRef = useRef(false);
  const mountedRef = useRef(false);
  const callbacks = useRef({ onSignedIn, focusPassword, focusProvider, onProviderDisabled });

  useEffect(() => { callbacks.current = { onSignedIn, focusPassword, focusProvider, onProviderDisabled }; });
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (pending || pendingOidcRef.current) return;
    setPending(true);
    setError(null);
    setOidcError(null);
    const result = await login(loginName, password);
    if (!mountedRef.current) return;
    if (result.kind === 'ok') {
      callbacks.current.onSignedIn(result.session);
      return;
    }
    setPassword('');
    setError(result.kind);
    setPending(false);
    callbacks.current.focusPassword();
  }

  async function signInWithProvider() {
    if (pendingOidcRef.current || pending) return;
    pendingOidcRef.current = true;
    setOidcPending(true);
    setOidcError(null);
    setError(null);
    const result = await startOidc({ purpose: 'login' });
    if (!mountedRef.current) return;
    if (result.kind === 'redirect') {
      browserNavigation.assign(result.url);
      return;
    }
    if (result.kind === 'disabled') callbacks.current.onProviderDisabled();
    pendingOidcRef.current = false;
    setOidcPending(false);
    setOidcError(result.kind);
    if (result.kind !== 'disabled') callbacks.current.focusProvider();
  }

  return {
    loginName, setLoginName, password, setPassword, pending, error,
    oidcPending, oidcError,
    submit,
    signInWithProvider,
  };
}
