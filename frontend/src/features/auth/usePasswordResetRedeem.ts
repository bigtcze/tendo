import { useEffect, useRef, useState, type FormEvent } from 'react';
import { redeemPasswordReset } from './passwordResetApi';
import {
  failureField, isValidResetToken, passwordProblem, type ResetFailure,
} from './passwordResetModel';

/**
 * One password reset: client checks run first, and every server refusal keeps the form usable except an
 * unusable link, which replaces the form with the calm invalid state. Success hands the login back so the
 * caller can move to sign-in.
 */
export function usePasswordResetRedeem(token: string | null, onRedeemed: (login: string) => void) {
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [failure, setFailure] = useState<ResetFailure | null>(null);
  const [pending, setPending] = useState(false);
  const passwordRef = useRef<HTMLInputElement>(null);
  const confirmRef = useRef<HTMLInputElement>(null);
  const pendingRef = useRef(false);
  const mountedRef = useRef(false);
  const onRedeemedRef = useRef(onRedeemed);

  useEffect(() => { onRedeemedRef.current = onRedeemed; });
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  const validToken = isValidResetToken(token);
  const invalidLink = !validToken || failure === 'invalidLink';

  function reject(next: ResetFailure) {
    setFailure(next);
    setPending(false);
    pendingRef.current = false;
    const field = failureField(next);
    if (field === 'password') passwordRef.current?.focus();
    if (field === 'confirm') confirmRef.current?.focus();
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (pendingRef.current) return;
    if (!validToken) { reject('invalidLink'); return; }
    const problem = passwordProblem(password, confirm);
    if (problem) { reject(problem.failure); return; }
    pendingRef.current = true;
    setFailure(null);
    setPending(true);
    const result = await redeemPasswordReset(token, password);
    if (!mountedRef.current) return;
    if (result.kind === 'ok') {
      pendingRef.current = false;
      setPending(false);
      onRedeemedRef.current(result.login);
      return;
    }
    reject(result.kind === 'invalidLength' ? 'length' : result.kind);
  }

  return { password, setPassword, confirm, setConfirm, failure, pending, invalidLink, passwordRef, confirmRef, submit };
}

export type PasswordResetRedeem = ReturnType<typeof usePasswordResetRedeem>;
