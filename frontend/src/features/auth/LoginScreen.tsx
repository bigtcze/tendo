import { useRef } from 'react';
import { Button } from '../../components/ui/button';
import { LoginProviderOptions } from './LoginProviderOptions';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { useI18n, type MessageKey } from '../../i18n';
import type { Session } from './sessionApi';
import { type StartResult } from './oidcApi';
import { useLoginActions } from './useLoginActions';
import { useOidcProviderStatus } from './useOidcProviderStatus';
import { Heading } from '../../app/Heading';

const errorKeys: Record<'invalid' | 'rateLimited' | 'unavailable', MessageKey> = {
  invalid: 'login.error.invalid',
  rateLimited: 'login.error.rateLimited',
  unavailable: 'login.error.unavailable',
};

type StartFailure = Exclude<StartResult['kind'], 'redirect'>;
const startErrorKeys: Record<StartFailure, MessageKey> = {
  wrongPassword: 'oidc.start.unavailable',
  unauthenticated: 'oidc.start.unavailable',
  alreadySignedIn: 'oidc.error.session_changed',
  disabled: 'oidc.start.disabled',
  rateLimited: 'oidc.start.rateLimited',
  providerUnavailable: 'oidc.start.providerUnavailable',
  unavailable: 'oidc.start.unavailable',
};

export function LoginScreen({ onSignedIn, notice }: { onSignedIn: (session: Session) => void; notice?: string }) {
  const { t } = useI18n();
  const oidcButtonRef = useRef<HTMLButtonElement>(null);
  const passwordRef = useRef<HTMLInputElement>(null);
  const status = useOidcProviderStatus(
    () => passwordRef.current?.focus(),
    () => oidcButtonRef.current?.focus(),
  );
  const { provider, status: oidcStatus, pending: oidcStatusPending, retryRef: statusRetryRef, retry: loadOidcStatus } = status;
  const actions = useLoginActions({
    onSignedIn,
    focusPassword: () => passwordRef.current?.focus(),
    focusProvider: () => oidcButtonRef.current?.focus(),
    onProviderDisabled: status.disableProvider,
  });
  const { loginName, setLoginName, password, setPassword, pending, error, oidcPending, oidcError } = actions;

  return (
    <section className="settle">
      <Heading className="font-display text-3xl leading-tight sm:text-4xl">{t('login.title')}</Heading>
      {notice ? <p role="status" className="mt-4 rounded-xl bg-sand px-4 py-3">{notice}</p> : null}
      <form onSubmit={(event) => void actions.submit(event)} className="mt-8 space-y-5" noValidate>
        <div className="space-y-2">
          <Label htmlFor="login">{t('login.login')}</Label>
          <Input
            id="login"
            name="login"
            type="text"
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
            required
            value={loginName}
            onChange={(e) => setLoginName(e.target.value)}
            aria-invalid={error === 'invalid'}
            aria-describedby={error ? 'login-error' : undefined}
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="password">{t('login.password')}</Label>
          <Input
            id="password"
            name="password"
            type="password"
            autoComplete="current-password"
            required
            ref={passwordRef}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            aria-invalid={error === 'invalid'}
            aria-describedby={error ? 'login-error' : undefined}
          />
        </div>
        <div id="login-error" role="alert" className="text-base text-danger">
          {error ? t(errorKeys[error]) : oidcError ? t(startErrorKeys[oidcError], { provider: provider ?? '' }) : null}
        </div>
        <Button type="submit" disabled={pending || oidcPending} className="w-full sm:w-auto">
          {pending ? t('login.submitting') : t('login.submit')}
        </Button>
      </form>
      <LoginProviderOptions
        provider={provider}
        status={oidcStatus}
        statusPending={oidcStatusPending}
        passwordPending={pending}
        oidcPending={oidcPending}
        oidcError={oidcError !== null}
        retryRef={statusRetryRef}
        providerRef={oidcButtonRef}
        retry={loadOidcStatus}
        signIn={() => void actions.signInWithProvider()}
      />
    </section>
  );
}
