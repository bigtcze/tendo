import { useEffect, useRef, useState } from 'react';
import { fetchOidcStatus } from './oidcApi';

export function useOidcProviderStatus(focusPassword: () => void, focusProvider: () => void) {
  const [provider, setProvider] = useState<string | null>(null);
  const [status, setStatus] = useState<'loading' | 'enabled' | 'disabled' | 'error'>('loading');
  const [pending, setPending] = useState(false);
  const generation = useRef(0);
  const mounted = useRef(false);
  const retryRef = useRef<HTMLButtonElement>(null);
  const moveFocusAfterStatus = useRef(false);
  const focusCallbacks = useRef({ focusPassword, focusProvider });
  useEffect(() => { focusCallbacks.current = { focusPassword, focusProvider }; });

  async function load() {
    const current = ++generation.current;
    setPending(true);
    const result = await fetchOidcStatus();
    if (!mounted.current || current !== generation.current) return;
    moveFocusAfterStatus.current = document.activeElement === retryRef.current;
    setProvider(result.kind === 'enabled' ? result.displayName : null);
    setStatus(result.kind);
    setPending(false);
  }

  useEffect(() => {
    mounted.current = true;
    void Promise.resolve().then(load);
    return () => {
      mounted.current = false;
      generation.current += 1;
    };
  }, []);

  useEffect(() => {
    if (!moveFocusAfterStatus.current || pending) return;
    moveFocusAfterStatus.current = false;
    if (status === 'enabled') focusCallbacks.current.focusProvider();
    else if (status === 'disabled') focusCallbacks.current.focusPassword();
  }, [status, pending]);

  function disableProvider() {
    setProvider(null);
  }

  return { provider, status, pending, retryRef, retry: () => { if (!pending) void load(); }, disableProvider };
}
