import { useEffect, useLayoutEffect, useRef, useState, type FormEvent } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { Button } from '../../components/ui/button';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { useI18n, type MessageKey } from '../../i18n';
import { browserNavigation, fetchOidcIdentity, fetchOidcStatus, startOidc, type OidcOutcome, type StartResult } from './oidcApi';

type Section =
  | { kind: 'loading' }
  | { kind: 'hidden' }
  | { kind: 'error'; provider: string | null }
  | { kind: 'linked'; provider: string }
  | { kind: 'notLinked'; provider: string };

type StartFailure = Exclude<StartResult['kind'], 'redirect' | 'unauthenticated'>;
const startErrorKeys: Record<StartFailure, MessageKey> = {
  wrongPassword: 'account.oidc.wrongPassword',
  alreadySignedIn: 'oidc.start.unavailable',
  disabled: 'oidc.start.disabled',
  rateLimited: 'oidc.start.rateLimited',
  providerUnavailable: 'oidc.start.providerUnavailable',
  unavailable: 'oidc.start.unavailable',
};

export function AccountScreen({ login, outcome, onSignedOut }: { login: string; outcome: OidcOutcome | null; onSignedOut: () => void }) {
  const { t } = useI18n();
  const [section, setSection] = useState<Section>({ kind: 'loading' });
  const [attempt, setAttempt] = useState(0);
  const [showRetry, setShowRetry] = useState(false);
  const [password, setPassword] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<StartFailure | null>(null);
  const pendingRef = useRef(false);
  const passwordRef = useRef<HTMLInputElement>(null);
  const retryRef = useRef<HTMLButtonElement>(null);
  const sectionHeadingRef = useRef<HTMLHeadingElement>(null);
  const signedInRef = useRef<HTMLParagraphElement>(null);
  // Set when a load settles while the Retry button still has focus; the settled content takes focus
  // so keyboard users are not dropped to the document when Retry unmounts.
  const moveFocusAfterRetry = useRef(false);
  const onSignedOutRef = useRef(onSignedOut);
  useEffect(() => {
    onSignedOutRef.current = onSignedOut;
  }, [onSignedOut]);

  useEffect(() => {
    let cancelled = false;
    const settle = (next: Section) => {
      moveFocusAfterRetry.current = next.kind !== 'error' && document.activeElement !== null && document.activeElement === retryRef.current;
      setSection(next);
    };
    (async () => {
      try {
        const status = await fetchOidcStatus();
        if (cancelled) return;
        if (status.kind === 'disabled') return settle({ kind: 'hidden' });
        if (status.kind === 'error') return settle({ kind: 'error', provider: null });
        const identity = await fetchOidcIdentity();
        if (cancelled) return;
        if (identity.kind === 'unauthenticated') return onSignedOutRef.current();
        if (identity.kind === 'disabled') return settle({ kind: 'hidden' });
        if (identity.kind === 'linked') return settle({ kind: 'linked', provider: status.displayName });
        if (identity.kind === 'notLinked') return settle({ kind: 'notLinked', provider: status.displayName });
        settle({ kind: 'error', provider: status.displayName });
      } catch {
        if (!cancelled) setSection({ kind: 'error', provider: null });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [attempt]);

  useLayoutEffect(() => {
    if (!moveFocusAfterRetry.current) return;
    moveFocusAfterRetry.current = false;
    if (section.kind === 'linked' || section.kind === 'notLinked') sectionHeadingRef.current?.focus();
    else if (section.kind === 'hidden') signedInRef.current?.focus();
  }, [section]);

  async function connect(event: FormEvent) {
    event.preventDefault();
    if (pendingRef.current) return;
    pendingRef.current = true;
    setPending(true);
    setError(null);
    const result = await startOidc({ purpose: 'link', currentPassword: password });
    if (result.kind === 'redirect') {
      browserNavigation.assign(result.url);
      return;
    }
    if (result.kind === 'unauthenticated') {
      onSignedOut();
      return;
    }
    pendingRef.current = false;
    setPending(false);
    setPassword('');
    if (result.kind === 'disabled') setSection({ kind: 'hidden' });
    setError(result.kind);
    passwordRef.current?.focus();
  }

  const outcomeText = outcome
    ? outcome.kind === 'connected'
      ? t('account.oidc.connected')
      : t(`oidc.error.${outcome.code}`)
    : null;

  return (
    <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}>
      <section className="settle">
        <Heading className="font-display text-h1 leading-tight">{t('account.title')}</Heading>
        <div role="status" className="mt-5 empty:mt-0">
          {outcomeText ? <p className="rounded-xl bg-sand px-4 py-3">{outcomeText}</p> : null}
        </div>
        <p ref={signedInRef} tabIndex={-1} className="mt-6 break-words text-lg text-muted outline-none">{t('account.signedInAs', { login })}</p>
        {section.kind === 'loading' ? <p role="status" className="mt-8 text-muted">{t('account.oidc.loading')}</p> : null}
        {section.kind === 'error' ? <p role="status" className="mt-8 text-muted">{t('account.oidc.loadError')}</p> : null}
        {section.kind === 'error' || (showRetry && section.kind === 'loading') ? (
          <Button ref={retryRef} type="button" variant="quiet" className="mt-3" onClick={() => { setShowRetry(true); setSection({ kind: 'loading' }); setAttempt((n) => n + 1); }}>
            {t('app.error.retry')}
          </Button>
        ) : null}
        {section.kind === 'linked' ? (
          <section className="mt-8 border-t border-line pt-6">
            <h2 ref={sectionHeadingRef} tabIndex={-1} className="font-display text-2xl break-words outline-none">{t('account.oidc.title', { provider: section.provider })}</h2>
            <p className="mt-2 text-muted">{t('account.oidc.linked', { provider: section.provider })}</p>
          </section>
        ) : null}
        {section.kind === 'notLinked' ? (
          <section className="mt-8 border-t border-line pt-6">
            <h2 ref={sectionHeadingRef} tabIndex={-1} className="font-display text-2xl break-words outline-none">{t('account.oidc.title', { provider: section.provider })}</h2>
            <p className="mt-2 text-muted">{t('account.oidc.intro', { provider: section.provider })}</p>
            <form onSubmit={connect} className="mt-5 space-y-5" noValidate>
              <div className="space-y-2">
                <Label htmlFor="account-current-password">{t('account.oidc.password')}</Label>
                <Input
                  id="account-current-password"
                  name="currentPassword"
                  type="password"
                  autoComplete="current-password"
                  required
                  ref={passwordRef}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  aria-invalid={error === 'wrongPassword'}
                  aria-describedby={error ? 'account-oidc-error' : undefined}
                />
              </div>
              <div id="account-oidc-error" role="alert" className="text-base text-danger">
                {error ? t(startErrorKeys[error], { provider: section.provider }) : null}
              </div>
              <Button type="submit" disabled={pending} className="w-full sm:w-auto">
                {pending ? t('account.oidc.connecting', { provider: section.provider }) : t('account.oidc.connect', { provider: section.provider })}
              </Button>
            </form>
          </section>
        ) : null}
      </section>
    </Layout>
  );
}
