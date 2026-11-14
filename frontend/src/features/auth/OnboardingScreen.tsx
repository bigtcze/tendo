import { Heading } from '../../app/Heading';
import { Button } from '../../components/ui/button';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { Select } from '../../components/ui/select';
import { useI18n } from '../../i18n';
import type { Session } from './sessionApi';
import { errorKey, errorTarget, formatTimezoneLabel, type FormError } from './onboardingModel';
import { useOnboardingForm } from './useOnboardingForm';

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
  hint?: string;
  hintId?: string;
};

function TimezoneField({ value, onChange, invalid, zones, locale }: {
  value: string;
  onChange: (value: string) => void;
  invalid: boolean;
  zones: string[];
  locale: string;
}) {
  const { t } = useI18n();
  return (
    <div className="space-y-2">
      <Label htmlFor="onboarding-timezone">{t('onboarding.timezone')}</Label>
      <Select
        id="onboarding-timezone"
        name="timezone"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        aria-invalid={invalid}
        aria-describedby={describedBy('onboarding-timezone-hint', invalid)}
      >
        {zones.map((zone) => <option key={zone} value={zone}>{formatTimezoneLabel(zone, locale)}</option>)}
      </Select>
      <p id="onboarding-timezone-hint" className="text-small text-muted">
        {t('onboarding.timezone.hint', { timezone: formatTimezoneLabel(value, locale) })}
      </p>
    </div>
  );
}

function TextField({
  id, name, label, type, autoComplete, value, invalid, onChange,
  autoCapitalize, spellCheck, placeholder, hint, hintId,
}: TextFieldProps) {
  return (
    <div className="space-y-2">
      <Label htmlFor={id}>{label}</Label>
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
      {hint ? <p id={hintId} className="text-small text-muted">{hint}</p> : null}
    </div>
  );
}

function OnboardingError({ error }: { error: FormError | null }) {
  const { t } = useI18n();
  return (
    <div
      id="onboarding-error"
      role="alert"
      className={error ? 'rounded-xl border border-danger bg-danger-bg px-4 py-3 text-small text-danger' : ''}
    >
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

function StepHeading({ step, children }: { step: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-3">
      <span aria-hidden="true" className="grid size-7 shrink-0 place-items-center rounded-lg bg-nav-selected text-small font-semibold text-ink">{step}</span>
      <h2 className="text-h2 text-ink">{children}</h2>
    </div>
  );
}

function OnboardingIntroduction() {
  const { t } = useI18n();
  return (
    <Heading className="text-h1 font-semibold text-ink">
      {t('onboarding.title')}
    </Heading>
  );
}

function SetupSection({ form, target }: {
  form: ReturnType<typeof useOnboardingForm>;
  target: ReturnType<typeof errorTarget>;
}) {
  const { t } = useI18n();
  return (
    <section className="p-5 sm:p-7">
      <StepHeading step="1">{t('onboarding.setup.section')}</StepHeading>
      <p className="mt-3 max-w-prose text-small text-muted">{t('onboarding.setup.intro')}</p>
      <div className="mt-5 grid gap-5 sm:grid-cols-2">
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
        />
      </div>
    </section>
  );
}

function OwnerSection({ form, target, locale }: {
  form: ReturnType<typeof useOnboardingForm>;
  target: ReturnType<typeof errorTarget>;
  locale: string;
}) {
  const { t } = useI18n();
  return (
    <section className="border-t border-line p-5 sm:p-7">
      <StepHeading step="2">{t('onboarding.owner.section')}</StepHeading>
      <p className="mt-3 max-w-prose text-small text-muted">{t('onboarding.owner.intro')}</p>
      <div className="mt-5 grid gap-5 sm:grid-cols-2">
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
      </div>
      <div className="mt-5">
        <TimezoneField
          value={form.timezone}
          onChange={form.setTimezone}
          invalid={target === 'timezone'}
          zones={form.zones}
          locale={locale}
        />
      </div>
    </section>
  );
}

export function OnboardingScreen({
  onSignedIn,
  onSetupComplete,
}: {
  onSignedIn: (session: Session) => void;
  onSetupComplete: () => void;
}) {
  const { locale } = useI18n();
  const form = useOnboardingForm({ onSignedIn, onSetupComplete });
  const target = form.error ? errorTarget(form.error) : null;

  return (
    <section className="settle mx-auto w-full max-w-4xl">
      <OnboardingIntroduction />
      <form onSubmit={form.submit} className="mt-8" noValidate>
        <div className="overflow-hidden rounded-2xl border border-line bg-raised">
          <SetupSection form={form} target={target} />
          <OwnerSection form={form} target={target} locale={locale} />
          <div className="flex flex-col gap-4 border-t border-line p-5 sm:flex-row sm:items-center sm:justify-between sm:p-7">
            <OnboardingError error={form.error} />
            <OnboardingSubmit pending={form.pending} />
          </div>
        </div>
      </form>
    </section>
  );
}
