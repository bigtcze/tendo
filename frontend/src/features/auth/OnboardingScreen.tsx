import { useMemo, useRef, useState, type FormEvent } from 'react';
import { Heading } from '../../app/Heading';
import { Button } from '../../components/ui/button';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { cn } from '../../lib/cn';
import { useI18n, type MessageKey } from '../../i18n';
import { createOwner, login, type Session, type SetupField } from './sessionApi';

type FormError =
  | { kind: 'setupCode' }
  | { kind: 'field'; field: SetupField }
  | { kind: 'empty'; field: 'setupCode' | SetupField }
  | { kind: 'setupDisabled' | 'rateLimited' | 'unavailable' };

type FocusTarget = 'setupCode' | SetupField;

function errorKey(error: FormError): MessageKey {
  switch (error.kind) {
    case 'setupCode':
      return 'onboarding.error.setupCode';
    case 'field':
      return `onboarding.error.${error.field}` as MessageKey;
    case 'empty':
      return error.field === 'setupCode' ? 'onboarding.error.setupCodeEmpty' : (`onboarding.error.${error.field}Empty` as MessageKey);
    case 'setupDisabled':
      return 'onboarding.error.setupDisabled';
    case 'rateLimited':
      return 'onboarding.error.rateLimited';
    case 'unavailable':
      return 'onboarding.error.unavailable';
  }
}

function errorTarget(error: FormError): FocusTarget | null {
  if (error.kind === 'setupCode') return 'setupCode';
  if (error.kind === 'field' || error.kind === 'empty') return error.field;
  return null;
}

function detectTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
}

function supportedZones(): string[] {
  try {
    const intl = Intl as typeof Intl & { supportedValuesOf?: (key: string) => string[] };
    return intl.supportedValuesOf?.('timeZone') ?? [];
  } catch {
    return [];
  }
}

function timezoneOptions(detected: string): string[] {
  const supported = supportedZones();
  return Array.from(new Set([...supported, 'UTC', detected])).sort((a, b) => a.localeCompare(b));
}

export function OnboardingScreen({
  onSignedIn,
  onSetupComplete,
}: {
  onSignedIn: (session: Session) => void;
  onSetupComplete: () => void;
}) {
  const { t } = useI18n();
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
  const ids: Record<FocusTarget, string> = {
    setupCode: 'setup-code',
    householdName: 'household-name',
    login: 'onboarding-login',
    password: 'onboarding-password',
    timezone: 'onboarding-timezone',
  };

  const target = error ? errorTarget(error) : null;

  function fail(next: FormError) {
    setError(next);
    setPending(false);
    submitting.current = false;
    const field = errorTarget(next);
    if (!field) return;
    const el = document.getElementById(ids[field]);
    if (el instanceof HTMLElement) el.focus();
    if (el instanceof HTMLInputElement && field === 'setupCode') el.select();
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (submitting.current) return;
    const token = setupCode.trim();
    const emptyField: FocusTarget | null = !token
      ? 'setupCode'
      : !householdName.trim()
        ? 'householdName'
        : !loginName
          ? 'login'
          : !password
            ? 'password'
            : null;
    if (emptyField) {
      fail({ kind: 'empty', field: emptyField });
      return;
    }
    submitting.current = true;
    setPending(true);
    setError(null);
    const result = await createOwner({ token, login: loginName, password, householdName, timezone });
    switch (result.kind) {
      case 'ok': {
        const signedIn = await login(loginName, password);
        if (signedIn.kind === 'ok') onSignedIn(signedIn.session);
        else onSetupComplete();
        return;
      }
      case 'alreadySetUp':
        onSetupComplete();
        return;
      case 'badToken':
        fail({ kind: 'setupCode' });
        return;
      case 'invalidField':
        fail({ kind: 'field', field: result.field });
        return;
      default:
        fail({ kind: result.kind });
    }
  }

  const describedBy = (field: FocusTarget, hint?: string) => {
    const ids = [hint, target === field ? 'onboarding-error' : undefined].filter(Boolean);
    return ids.length ? ids.join(' ') : undefined;
  };

  return (
    <section className="settle">
      <Heading className="font-display text-3xl leading-tight sm:text-4xl">{t('onboarding.title')}</Heading>
      <p className="mt-3 text-lg text-muted">{t('onboarding.subtitle')}</p>
      <form onSubmit={submit} className="mt-8 space-y-6" noValidate>
        <div className="space-y-2">
          <Label htmlFor="setup-code">{t('onboarding.setupCode')}</Label>
          <Input
            id="setup-code"
            name="setupCode"
            type="password"
            autoComplete="off"
            autoCapitalize="none"
            spellCheck={false}
            required
            value={setupCode}
            onChange={(e) => setSetupCode(e.target.value)}
            aria-invalid={target === 'setupCode'}
            aria-describedby={describedBy('setupCode', 'setup-code-hint')}
          />
          <p id="setup-code-hint" className="text-sm text-muted">
            {t('onboarding.setupCode.hint')}
          </p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="household-name" className="font-display text-xl">
            {t('onboarding.householdName')}
          </Label>
          <Input
            id="household-name"
            name="householdName"
            type="text"
            autoComplete="off"
            required
            placeholder={t('onboarding.householdName.placeholder')}
            value={householdName}
            onChange={(e) => setHouseholdName(e.target.value)}
            aria-invalid={target === 'householdName'}
            aria-describedby={describedBy('householdName')}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="onboarding-login">{t('onboarding.login')}</Label>
          <Input
            id="onboarding-login"
            name="login"
            type="text"
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
            required
            value={loginName}
            onChange={(e) => setLoginName(e.target.value)}
            aria-invalid={target === 'login'}
            aria-describedby={describedBy('login', 'onboarding-login-hint')}
          />
          <p id="onboarding-login-hint" className="text-sm text-muted">
            {t('onboarding.login.hint')}
          </p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="onboarding-password">{t('onboarding.password')}</Label>
          <Input
            id="onboarding-password"
            name="password"
            type="password"
            autoComplete="new-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            aria-invalid={target === 'password'}
            aria-describedby={describedBy('password', 'onboarding-password-hint')}
          />
          <p id="onboarding-password-hint" className="text-sm text-muted">
            {t('onboarding.password.hint')}
          </p>
        </div>
        <div className="space-y-2">
          <Label htmlFor="onboarding-timezone">{t('onboarding.timezone')}</Label>
          <select
            id="onboarding-timezone"
            name="timezone"
            value={timezone}
            onChange={(e) => setTimezone(e.target.value)}
            aria-invalid={target === 'timezone'}
            aria-describedby={describedBy('timezone', 'onboarding-timezone-hint')}
            className={cn(
              'min-h-11 w-full rounded-xl border border-line bg-white px-4 text-base text-ink focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent aria-[invalid=true]:border-danger',
            )}
          >
            {zones.map((zone) => (
              <option key={zone} value={zone}>
                {zone}
              </option>
            ))}
          </select>
          <p id="onboarding-timezone-hint" className="text-sm text-muted">
            {t('onboarding.timezone.hint', { timezone })}
          </p>
        </div>
        <div id="onboarding-error" role="alert" className="text-base text-danger">
          {error ? t(errorKey(error)) : null}
        </div>
        <Button type="submit" disabled={pending} className="w-full sm:w-auto">
          {pending ? t('onboarding.submitting') : t('onboarding.submit')}
        </Button>
      </form>
    </section>
  );
}
