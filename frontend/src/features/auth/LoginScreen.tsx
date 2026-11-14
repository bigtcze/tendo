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
  unauthenticated: 'oidc.start.unavailable',
  alreadySignedIn: 'oidc.error.session_changed',
  disabled: 'oidc.start.disabled',
  rateLimited: 'oidc.start.rateLimited',
  providerUnavailable: 'oidc.start.providerUnavailable',
  invalidInvitation: 'oidc.start.unavailable',
  unavailable: 'oidc.start.unavailable',
};

function LoginForm({
  notice, loginName, setLoginName, password, setPassword, pending, oidcPending, message, submit, passwordRef,
}: {
  notice?: string;
  loginName: string;
  setLoginName: (value: string) => void;
  password: string;
  setPassword: (value: string) => void;
  pending: boolean;
  oidcPending: boolean;
  message: string | null;
  submit: (event: React.FormEvent<HTMLFormElement>) => void;
  passwordRef: React.RefObject<HTMLInputElement | null>;
}) {
  const { t } = useI18n();
  return (
    <div className="p-5 sm:p-7">
      <NoticeRegion message={notice} className={notice ? 'mb-5' : undefined} />
      <form aria-label={t('login.heading')} onSubmit={(event) => void submit(event)} className="grid gap-5" noValidate>
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
            aria-describedby={message ? 'login-error' : undefined}
          />
        </div>
        <div id="login-error" role="alert" className="empty:contents">
          {message ? (
            <div className="rounded-xl border border-danger bg-danger-bg px-4 py-3 text-small text-danger">
              {message}
            </div>
          ) : null}
        </div>
        <Button type="submit" disabled={pending || oidcPending} className="w-full">
          {pending ? t('login.submitting') : t('login.submit')}
        </Button>
      </form>
    </div>
  );
}

export function LoginScreen({ onSignedIn, notice, initialLogin }: {
  onSignedIn: (session: Session) => void;
  notice?: string;
  initialLogin?: string;
}) {
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
    initialLogin,
    focusPassword: () => passwordRef.current?.focus(),
    focusProvider: () => oidcButtonRef.current?.focus(),
    onProviderDisabled: status.disableProvider,
  });
  const { loginName, setLoginName, password, setPassword, pending, error, oidcPending, oidcError } = actions;
  const message = error ? t(errorKeys[error]) : oidcError ? t(startErrorKeys[oidcError], { provider: provider ?? '' }) : null;

  return (
    <section className="settle mx-auto w-full max-w-md lg:mt-10">
      <Heading className="text-center text-h1 font-semibold text-ink">{t('login.welcome')}</Heading>
      <div className="mt-8 w-full overflow-hidden rounded-2xl border border-line bg-raised">
        <LoginForm
          notice={notice}
          loginName={loginName}
          setLoginName={setLoginName}
          password={password}
          setPassword={setPassword}
          pending={pending}
          oidcPending={oidcPending}
          message={message}
          submit={actions.submit}
          passwordRef={passwordRef}
        />
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
