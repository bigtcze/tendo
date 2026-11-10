import { useEffect, useRef, type FormEvent } from 'react';
import { Button } from '../../components/ui/button';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { useI18n } from '../../i18n';
import type { ContinueFailure } from './useOidcContinue';

const failureKeys = {
  invalid: 'oidc.continue.error.invalid',
  rateLimited: 'oidc.continue.error.rateLimited',
  unavailable: 'oidc.continue.error.unavailable',
} as const;

type Props = {
  pending: boolean;
  failure: ContinueFailure;
  loginName: string;
  setLoginName: (value: string) => void;
  password: string;
  setPassword: (value: string) => void;
  submit: () => void;
  cancel: () => void;
};

/** Local-password confirmation for the "I already have a Tendo account" branch of the continue screen. */
export function OidcContinueLinkForm({
  pending, failure, loginName, setLoginName, password, setPassword, submit, cancel,
}: Props) {
  const { t } = useI18n();
  const loginRef = useRef<HTMLInputElement>(null);
  const passwordRef = useRef<HTMLInputElement>(null);
  const invalid = failure === 'invalid';
  const message = invalid || failure === 'rateLimited' || failure === 'unavailable'
    ? t(failureKeys[failure])
    : null;

  // The form replaces the button that revealed it; land the caret on the first field.
  useEffect(() => {
    if (!invalid) loginRef.current?.focus();
  }, [invalid]);

  // A rejected password is cleared by the flow; returning focus to it keeps the retry one keystroke away.
  useEffect(() => {
    if (invalid) passwordRef.current?.focus();
  }, [invalid]);

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    submit();
  }

  return (
    <form aria-label={t('oidc.continue.link.title')} onSubmit={onSubmit} className="space-y-4" noValidate>
      <h2 id="oidc-link-heading" className="text-h2 text-ink">{t('oidc.continue.link.title')}</h2>
      <p className="text-body text-muted">{t('oidc.continue.link.body')}</p>
      <div className="space-y-2">
        <Label htmlFor="oidc-link-login">{t('oidc.continue.link.login')}</Label>
        <Input
          id="oidc-link-login"
          name="login"
          type="text"
          autoComplete="username"
          autoCapitalize="none"
          spellCheck={false}
          required
          ref={loginRef}
          value={loginName}
          onChange={(event) => setLoginName(event.target.value)}
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="oidc-link-password">{t('oidc.continue.link.password')}</Label>
        <Input
          ref={passwordRef}
          id="oidc-link-password"
          name="password"
          type="password"
          autoComplete="current-password"
          required
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          aria-invalid={invalid}
          aria-describedby={message ? 'oidc-continue-link-error' : undefined}
        />
      </div>
      <div id="oidc-continue-link-error" role="alert" className="empty:contents">
        {message ? (
          <div className="rounded-xl border border-danger bg-danger-bg px-4 py-3 text-small text-danger">{message}</div>
        ) : null}
      </div>
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={pending}>{pending ? t('oidc.continue.link.submitting') : t('oidc.continue.link.submit')}</Button>
        <Button type="button" variant="quiet" disabled={pending} onClick={cancel}>{t('oidc.continue.link.cancel')}</Button>
      </div>
    </form>
  );
}
