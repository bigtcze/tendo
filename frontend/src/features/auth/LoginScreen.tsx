import { useRef } from 'react';
import { Button } from '../../components/ui/button';
import { NoticeRegion } from '../../components/ui/notice';
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
    <section className="settle mx-auto w-full max-w-3xl">
      <div className="max-w-2xl">
        <Heading className="font-display text-display leading-tight">{t('login.welcome')}</Heading>
        <p className="mt-3 text-lg text-muted">{t('login.subtitle')}</p>
      </div>
      <div className="mt-8 rounded-2xl border border-line bg-raised p-5 sm:p-8">
        <NoticeRegion message={notice} className={notice ? 'mb-6' : undefined} />
        <form aria-label={t('login.heading')} onSubmit={(event) => void actions.submit(event)} className="grid gap-5 sm:grid-cols-2" noValidate>
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
              aria-invalid={false}
              aria-describedby={error ? 'login-error' : undefined}
            />
          </div>
          <div id="login-error" role="alert" className="text-base text-danger sm:col-span-2">
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
      </div>
    </section>
  );
}
