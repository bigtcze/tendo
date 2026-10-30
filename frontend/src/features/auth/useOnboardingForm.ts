import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react';
import { createOwner, login, type Session } from './sessionApi';
import {
  detectTimezone,
  errorTarget,
  fieldIds,
  firstEmptyField,
  timezoneOptions,
  type FormError,
} from './onboardingModel';

export function useOnboardingForm({
  onSignedIn,
  onSetupComplete,
}: {
  onSignedIn: (session: Session) => void;
  onSetupComplete: () => void;
}) {
  const [detected] = useState(detectTimezone);
  const zones = useMemo(() => timezoneOptions(detected), [detected]);
  const [setupCode, setSetupCode] = useState('');
  const [householdName, setHouseholdName] = useState('');
  const [loginName, setLoginName] = useState('');
  const [password, setPassword] = useState('');
  const [timezone, setTimezone] = useState(detected);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<FormError | null>(null);
  const submitting = useRef(false);
  const mountedRef = useRef(false);

  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  function fail(next: FormError) {
    setError(next);
    setPending(false);
    submitting.current = false;
    const field = errorTarget(next);
    if (!field) return;
    const element = document.getElementById(fieldIds[field]);
    if (element instanceof HTMLElement) element.focus();
    if (element instanceof HTMLInputElement && field === 'setupCode') element.select();
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (submitting.current) return;
    const emptyField = firstEmptyField(setupCode, householdName, loginName, password);
    if (emptyField) {
      fail({ kind: 'empty', field: emptyField });
      return;
    }

    submitting.current = true;
    setPending(true);
    setError(null);
    const result = await createOwner({
      token: setupCode.trim(),
      login: loginName,
      password,
      householdName,
      timezone,
    });
    if (!mountedRef.current) return;
    if (result.kind === 'ok') {
      const signedIn = await login(loginName, password);
      if (!mountedRef.current) return;
      if (signedIn.kind === 'ok') onSignedIn(signedIn.session);
      else onSetupComplete();
      return;
    }
    if (result.kind === 'alreadySetUp') {
      onSetupComplete();
      return;
    }
    if (result.kind === 'badToken') {
      fail({ kind: 'setupCode' });
      return;
    }
    if (result.kind === 'invalidField') {
      fail({ kind: 'field', field: result.field });
      return;
    }
    fail({ kind: result.kind });
  }

  return {
    setupCode, setSetupCode, householdName, setHouseholdName, loginName, setLoginName,
    password, setPassword, timezone, setTimezone, zones, pending, error, submit,
  };
}
