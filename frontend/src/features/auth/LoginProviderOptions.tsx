import type { RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';

export function LoginProviderOptions({
  provider,
  status,
  statusPending,
  passwordPending,
  oidcPending,
  oidcError,
  retryRef,
  providerRef,
  retry,
  signIn,
}: {
  provider: string | null;
  status: 'loading' | 'enabled' | 'disabled' | 'error';
  statusPending: boolean;
  passwordPending: boolean;
  oidcPending: boolean;
  oidcError: boolean;
  retryRef: RefObject<HTMLButtonElement | null>;
  providerRef: RefObject<HTMLButtonElement | null>;
  retry: () => void;
  signIn: () => void;
}) {
  const { t } = useI18n();
  if (status === 'error') {
    return (
      <div className="mt-8">
        <p role="status" className="text-sm text-muted">{t('login.oidc.loadError')}</p>
        <Button
          ref={retryRef}
          type="button"
          variant="quiet"
          size="small"
          aria-disabled={statusPending}
          className="mt-2"
          onClick={retry}
        >
          {t('app.error.retry')}
        </Button>
      </div>
    );
  }
  if (!provider) return null;
  return (
    <div className="mt-8">
      <div className="flex items-center gap-3 text-sm text-muted" aria-hidden="true">
        <span className="h-px flex-1 bg-line" />
        {t('login.or')}
        <span className="h-px flex-1 bg-line" />
      </div>
      <Button
        ref={providerRef}
        type="button"
        variant="quiet"
        disabled={passwordPending || oidcPending}
        aria-describedby={oidcError ? 'login-error' : undefined}
        className="mt-5 w-full border border-line sm:w-auto"
        onClick={signIn}
      >
        {oidcPending ? t('login.oidc.submitting', { provider }) : t('login.oidc.submit', { provider })}
      </Button>
    </div>
  );
}
