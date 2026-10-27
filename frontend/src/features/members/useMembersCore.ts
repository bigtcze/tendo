import { useEffect, useRef, useState } from 'react';
import type { Translate } from '../../i18n';
import type { State } from './membersModel';

/**
 * Shared screen state and request guards. Effect order is observable and
 * matches the original screen: callback refs sync, then mount tracking whose
 * cleanup invalidates every in-flight member, member-page and invitation
 * request.
 */
export function useMembersCore(onSignedOut: () => void, t: Translate) {
  const [state, setState] = useState<State>('loading');
  const [owner, setOwner] = useState<boolean | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [notice, setNotice] = useState<string | null>(null);
  const onSignedOutRef = useRef(onSignedOut);
  useEffect(() => { onSignedOutRef.current = onSignedOut; }, [onSignedOut]);
  const mountedRef = useRef(false);
  const memberGenerationRef = useRef(0);
  const memberPageGenerationRef = useRef(0);
  const invitationGenerationRef = useRef(0);
  const tRef = useRef(t);
  useEffect(() => { tRef.current = t; }, [t]);
  const noticeRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; memberGenerationRef.current += 1; memberPageGenerationRef.current += 1; invitationGenerationRef.current += 1; };
  }, []);

  return {
    state, setState, owner, setOwner, attempt, setAttempt, notice, setNotice, noticeRef,
    onSignedOutRef, tRef, mountedRef, memberGenerationRef, memberPageGenerationRef, invitationGenerationRef,
  };
}

export type MembersCore = ReturnType<typeof useMembersCore>;
