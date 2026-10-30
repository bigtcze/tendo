import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { fetchOidcIdentity, fetchOidcStatus } from './oidcApi';

export type Section =
  | { kind: 'loading' }
  | { kind: 'hidden' }
  | { kind: 'error'; provider: string | null }
  | { kind: 'linked'; provider: string }
  | { kind: 'notLinked'; provider: string };

export function useAccountOidcSection(onSignedOut: () => void) {
  const [section, setSection] = useState<Section>({ kind: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const [showRetry, setShowRetry] = useState(false);
  const retryRef = useRef<HTMLButtonElement>(null);
  const sectionHeadingRef = useRef<HTMLHeadingElement>(null);
  const signedInRef = useRef<HTMLParagraphElement>(null);
  const moveFocusAfterRetry = useRef(false);
  const onSignedOutRef = useRef(onSignedOut);
  useEffect(() => { onSignedOutRef.current = onSignedOut; }, [onSignedOut]);

  useEffect(() => {
    let cancelled = false;
    const settle = (next: Section) => {
      moveFocusAfterRetry.current = next.kind !== 'error'
        && document.activeElement !== null
        && document.activeElement === retryRef.current;
      setSection(next);
    };
    (async () => {
      try {
        const status = await fetchOidcStatus();
        if (cancelled) return;
        if (status.kind === 'disabled') return settle({ kind: 'hidden' });
        if (status.kind === 'error') return settle({ kind: 'error', provider: null });
        const identity = await fetchOidcIdentity();
        if (cancelled) return;
        if (identity.kind === 'unauthenticated') return onSignedOutRef.current();
        if (identity.kind === 'disabled') return settle({ kind: 'hidden' });
        if (identity.kind === 'linked') return settle({ kind: 'linked', provider: status.displayName });
        if (identity.kind === 'notLinked') return settle({ kind: 'notLinked', provider: status.displayName });
        settle({ kind: 'error', provider: status.displayName });
      } catch {
        if (!cancelled) setSection({ kind: 'error', provider: null });
      }
    })();
    return () => { cancelled = true; };
  }, [attempt]);

  useLayoutEffect(() => {
    if (!moveFocusAfterRetry.current) return;
    moveFocusAfterRetry.current = false;
    if (section.kind === 'linked' || section.kind === 'notLinked') sectionHeadingRef.current?.focus();
    else if (section.kind === 'hidden') signedInRef.current?.focus();
  }, [section]);

  function hide() {
    setSection({ kind: 'hidden' });
  }

  function retry() {
    setShowRetry(true);
    setSection({ kind: 'loading' });
    setAttempt((current) => current + 1);
  }

  return { section, showRetry, retry, hide, retryRef, sectionHeadingRef, signedInRef };
}

export type AccountSectionState = ReturnType<typeof useAccountOidcSection>;
