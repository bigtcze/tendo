import { useEffect, useRef, useState, type FormEvent } from 'react';
import { changePassword } from './passwordChangeApi';
import { changeProblem, fieldFailure, type ChangeFailure } from './passwordChangeModel';
import type { Session } from './sessionApi';

type Options = {
  signInMethod: Session['signInMethod'];
  /** The fresh session the server issued with the rotated cookie. */
  onChanged: (session: Session) => void;
  onSignedOut: () => void;
  /** The session is no longer a local one, so the form cannot stay: the screen explains it instead. */
  onRefused: () => void;
};

/**
 * One password change. Client checks run first, a field refusal keeps the entries so the person can correct them, and
 * success clears all three fields so the typed passwords never stay in the DOM. A session that is no longer local
 * replaces the form with the calm explanation; a session that is gone follows the app's signed-out path.
 */
export function usePasswordChange({ signInMethod, onChanged, onSignedOut, onRefused }: Options) {
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [failure, setFailure] = useState<ChangeFailure | null>(null);
  const [success, setSuccess] = useState(false);
  const [pending, setPending] = useState(false);
  const currentRef = useRef<HTMLInputElement>(null);
  const newRef = useRef<HTMLInputElement>(null);
  const confirmRef = useRef<HTMLInputElement>(null);
  const pendingRef = useRef(false);
  const mountedRef = useRef(false);
  const onChangedRef = useRef(onChanged);
  const onSignedOutRef = useRef(onSignedOut);
  const onRefusedRef = useRef(onRefused);

  useEffect(() => {
    onChangedRef.current = onChanged;
    onSignedOutRef.current = onSignedOut;
    onRefusedRef.current = onRefused;
  });
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  /** A refusal replaces the outcome of the previous submit and takes focus to the field it names. */
  function reject(next: ChangeFailure) {
    pendingRef.current = false;
    setPending(false);
    setSuccess(false);
    setFailure(next);
    const field = fieldFailure(next)?.field;
    if (field === 'current') currentRef.current?.focus();
    if (field === 'newPassword') newRef.current?.focus();
    if (field === 'confirm') confirmRef.current?.focus();
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (pendingRef.current || signInMethod !== 'password') return;
    const problem = changeProblem(currentPassword, newPassword, confirm);
    if (problem) { reject(problem); return; }
    pendingRef.current = true;
    setFailure(null);
    setSuccess(false);
    setPending(true);
    const result = await changePassword(currentPassword, newPassword);
    if (!mountedRef.current) return;
    if (result.kind === 'ok') {
      pendingRef.current = false;
      setPending(false);
      setCurrentPassword('');
      setNewPassword('');
      setConfirm('');
      setSuccess(true);
      onChangedRef.current(result.session);
      return;
    }
    if (result.kind === 'unauthenticated') { onSignedOutRef.current(); return; }
    if (result.kind === 'localSessionRequired') {
      pendingRef.current = false;
      setPending(false);
      onRefusedRef.current();
      return;
    }
    reject(result.kind === 'invalidLength' ? 'length' : result.kind);
  }

  return {
    currentPassword, setCurrentPassword, newPassword, setNewPassword, confirm, setConfirm,
    failure, success, pending, currentRef, newRef, confirmRef, submit,
  };
}
