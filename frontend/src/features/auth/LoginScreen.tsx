import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Button } from '../../components/ui/button';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { useI18n, type MessageKey } from '../../i18n';
import { login, type Session } from './sessionApi';
import { browserNavigation, fetchOidcStatus, startOidc, type StartResult } from './oidcApi';
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
  const [provider, setProvider] = useState<string | null>(null);
  const [oidcPending, setOidcPending] = useState(false);
  const [oidcError, setOidcError] = useState<StartFailure | null>(null);
  const oidcPendingRef = useRef(false);
  const oidcButtonRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    let cancelled = false;
    fetchOidcStatus().then((status) => {
      if (!cancelled && status.kind === 'enabled') setProvider(status.displayName);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  async function signInWithProvider() {
    if (oidcPendingRef.current || pending) return;
    oidcPendingRef.current = true;
    setOidcPending(true);
    setOidcError(null);
    setError(null);
    const result = await startOidc({ purpose: 'login' });
    if (result.kind === 'redirect') {
      // Leave the button disabled while the browser navigates to the provider.
      browserNavigation.assign(result.url);
      return;
    }
    if (result.kind === 'disabled') setProvider(null);
    oidcPendingRef.current = false;
    setOidcPending(false);
    setOidcError(result.kind);
    if (result.kind !== 'disabled') oidcButtonRef.current?.focus();
  }
  const [loginName, setLoginName] = useState('');
  const [password, setPassword] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<keyof typeof errorKeys | null>(null);
  const passwordRef = useRef<HTMLInputElement>(null);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (pending || oidcPendingRef.current) return;
    setPending(true);
    setError(null);
    setOidcError(null);
    const result = await login(loginName, password);
    if (result.kind === 'ok') {
      onSignedIn(result.session);
      return;
    }
    setPassword('');
    setError(result.kind);
    setPending(false);
    passwordRef.current?.focus();
  }

  return (
    <section className="settle">
      <Heading className="font-display text-3xl leading-tight sm:text-4xl">{t('login.title')}</Heading>
      {notice ? <p role="status" className="mt-4 rounded-xl bg-sand px-4 py-3">{notice}</p> : null}
      <form onSubmit={submit} className="mt-8 space-y-5" noValidate>
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
      {provider ? (
        <div className="mt-8">
          <div className="flex items-center gap-3 text-sm text-muted" aria-hidden="true">
            <span className="h-px flex-1 bg-line" />
            {t('login.or')}
            <span className="h-px flex-1 bg-line" />
          </div>
          <Button
            ref={oidcButtonRef}
            type="button"
            variant="quiet"
            disabled={oidcPending || pending}
            aria-describedby={oidcError ? 'login-error' : undefined}
            onClick={() => void signInWithProvider()}
            className="mt-5 w-full border border-line sm:w-auto"
          >
            {oidcPending ? t('login.oidc.submitting', { provider }) : t('login.oidc.submit', { provider })}
          </Button>
        </div>
      ) : null}
    </section>
  );
}
