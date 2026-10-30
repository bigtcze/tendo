import { useEffect, useRef, useState } from 'react';
import { isFieldError, isLoginError, isPasswordError, isValidInviteToken, type ErrorState } from './inviteAcceptModel';
import type { Session } from '../auth/sessionApi';

export function useInviteAcceptState(token: string | null, session: Session | undefined, focusLogin: boolean) {
  const [loginName, setLoginName] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<ErrorState | null>(null);
  const [pending, setPending] = useState(false);
  const [signedOutMode, setSignedOutMode] = useState(!session);
  const [invalidLink, setInvalidLink] = useState(false);
  const [accepted, setAccepted] = useState(false);
  const mounted = useRef(false);
  const loginRef = useRef<HTMLInputElement>(null);
  const passwordRef = useRef<HTMLInputElement>(null);
  const noticeRef = useRef<HTMLDivElement>(null);
  const valid = isValidInviteToken(token);
  const currentSession = session && !signedOutMode ? session : undefined;
  const fieldError = isFieldError(error);

  useEffect(() => {
    mounted.current = true;
    if (focusLogin) loginRef.current?.focus();
    return () => { mounted.current = false; };
  }, [focusLogin]);
  useEffect(() => {
    if ((error && !fieldError) || accepted) noticeRef.current?.focus();
  }, [error, fieldError, accepted]);

  function fail(next: ErrorState) {
    setError(next);
    setPending(false);
    if (isLoginError(next)) loginRef.current?.focus();
    if (isPasswordError(next)) passwordRef.current?.focus();
    if (next === 'invalid') setInvalidLink(true);
  }

  return {
    loginName, setLoginName, password, setPassword, error, setError, fieldError,
    pending, setPending, signedOutMode, setSignedOutMode, invalidLink, setInvalidLink,
    accepted, setAccepted, mounted, loginRef, passwordRef, noticeRef, valid, currentSession, fail,
  };
}
