import { Heading } from '../../app/Heading';
import { Button } from '../../components/ui/button';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { useI18n } from '../../i18n';
import type { Session } from './sessionApi';
import { errorKey, errorTarget, type FormError } from './onboardingModel';
import { useOnboardingForm } from './useOnboardingForm';

const timezoneClassName = [
  'min-h-11 w-full rounded-xl border border-field bg-white px-4 text-base text-ink',
  'focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent',
  'aria-[invalid=true]:border-danger',
].join(' ');

/** Hint first, then the shared error region when this field owns the current error. */
function describedBy(hintId: string | undefined, invalid: boolean): string | undefined {
  const ids = [hintId, invalid ? 'onboarding-error' : undefined].filter(Boolean);
  return ids.length ? ids.join(' ') : undefined;
}

type TextFieldProps = {
  id: string;
  name: string;
  label: string;
  type: 'text' | 'password';
  autoComplete: string;
  value: string;
  invalid: boolean;
  onChange: (value: string) => void;
  autoCapitalize?: 'none';
  spellCheck?: false;
  placeholder?: string;
  labelClassName?: string;
  hint?: string;
  hintId?: string;
};

function TimezoneField({ value, onChange, invalid, zones }: {
  value: string;
  onChange: (value: string) => void;
  invalid: boolean;
  zones: string[];
}) {
  const { t } = useI18n();
  return (
    <div className="space-y-2">
      <Label htmlFor="onboarding-timezone">{t('onboarding.timezone')}</Label>
      <select
        id="onboarding-timezone"
        name="timezone"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        aria-invalid={invalid}
        aria-describedby={describedBy('onboarding-timezone-hint', invalid)}
        className={timezoneClassName}
      >
        {zones.map((zone) => <option key={zone} value={zone}>{zone}</option>)}
      </select>
      <p id="onboarding-timezone-hint" className="text-sm text-muted">
        {t('onboarding.timezone.hint', { timezone: value })}
      </p>
    </div>
  );
}

function TextField({
  id, name, label, type, autoComplete, value, invalid, onChange,
  autoCapitalize, spellCheck, placeholder, labelClassName, hint, hintId,
}: TextFieldProps) {
  return (
    <div className="space-y-2">
      <Label htmlFor={id} className={labelClassName}>{label}</Label>
      <Input
        id={id}
        name={name}
        type={type}
        autoComplete={autoComplete}
        autoCapitalize={autoCapitalize}
        spellCheck={spellCheck}
        required
        placeholder={placeholder}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        aria-invalid={invalid}
        aria-describedby={describedBy(hintId, invalid)}
      />
      {hint ? <p id={hintId} className="text-sm text-muted">{hint}</p> : null}
    </div>
  );
}

function OnboardingError({ error }: { error: FormError | null }) {
  const { t } = useI18n();
  return (
    <div id="onboarding-error" role="alert" className="text-base text-danger">
      {error ? t(errorKey(error)) : null}
    </div>
  );
}

function OnboardingSubmit({ pending }: { pending: boolean }) {
  const { t } = useI18n();
  return (
    <Button type="submit" disabled={pending} className="w-full sm:w-auto">
      {pending ? t('onboarding.submitting') : t('onboarding.submit')}
    </Button>
  );
}

function OnboardingIntroduction() {
  const { t } = useI18n();
  return (
    <>
      <Heading className="font-display text-3xl leading-tight sm:text-4xl">{t('onboarding.title')}</Heading>
      <p className="mt-3 text-lg text-muted">{t('onboarding.subtitle')}</p>
    </>
  );
}

export function OnboardingScreen({
  onSignedIn,
  onSetupComplete,
}: {
  onSignedIn: (session: Session) => void;
  onSetupComplete: () => void;
}) {
  const { t } = useI18n();
  const form = useOnboardingForm({ onSignedIn, onSetupComplete });
  const target = form.error ? errorTarget(form.error) : null;

  return (
    <section className="settle">
      <OnboardingIntroduction />
      <form onSubmit={form.submit} className="mt-8 space-y-6" noValidate>
        <TextField
          id="setup-code"
          name="setupCode"
          label={t('onboarding.setupCode')}
          type="password"
          autoComplete="off"
          autoCapitalize="none"
          spellCheck={false}
          value={form.setupCode}
          invalid={target === 'setupCode'}
          onChange={form.setSetupCode}
          hintId="setup-code-hint"
          hint={t('onboarding.setupCode.hint')}
        />
        <TextField
          id="household-name"
          name="householdName"
          label={t('onboarding.householdName')}
          type="text"
          autoComplete="off"
          value={form.householdName}
          invalid={target === 'householdName'}
          onChange={form.setHouseholdName}
          placeholder={t('onboarding.householdName.placeholder')}
          labelClassName="font-display text-xl"
        />
        <TextField
          id="onboarding-login"
          name="login"
          label={t('onboarding.login')}
          type="text"
          autoComplete="username"
          autoCapitalize="none"
          spellCheck={false}
          value={form.loginName}
          invalid={target === 'login'}
          onChange={form.setLoginName}
          hintId="onboarding-login-hint"
          hint={t('onboarding.login.hint')}
        />
        <TextField
          id="onboarding-password"
          name="password"
          label={t('onboarding.password')}
          type="password"
          autoComplete="new-password"
          value={form.password}
          invalid={target === 'password'}
          onChange={form.setPassword}
          hintId="onboarding-password-hint"
          hint={t('onboarding.password.hint')}
        />
        <TimezoneField
          value={form.timezone}
          onChange={form.setTimezone}
          invalid={target === 'timezone'}
          zones={form.zones}
        />
        <OnboardingError error={form.error} />
        <OnboardingSubmit pending={form.pending} />
      </form>
    </section>
  );
}
