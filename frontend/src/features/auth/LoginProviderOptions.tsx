import type { RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';

const OPTIONS_SECTION = 'border-t border-line bg-sand/40 px-5 py-5 sm:px-7';

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
      <div className={OPTIONS_SECTION}>
        <p role="status" className="text-small text-muted">{t('login.oidc.loadError')}</p>
        <Button
          ref={retryRef}
          type="button"
          variant="quiet"
          size="small"
          aria-disabled={statusPending}
          className="mt-2"
          onClick={retry}
        >
          {statusPending ? t('login.oidc.retrying') : t('app.error.retry')}
        </Button>
      </div>
    );
  }
  if (!provider) return null;
  return (
    <div className={OPTIONS_SECTION}>
      <div className="flex items-center gap-3 text-small text-muted" aria-hidden="true">
        <span className="h-px flex-1 bg-line" />
        {t('login.or')}
        <span className="h-px flex-1 bg-line" />
      </div>
      <Button
        ref={providerRef}
        type="button"
        variant="secondary"
        disabled={passwordPending || oidcPending}
        aria-describedby={oidcError ? 'login-error' : undefined}
        className="mt-4 w-full"
        onClick={signIn}
      >
        {oidcPending ? t('login.oidc.submitting', { provider }) : t('login.oidc.submit', { provider })}
      </Button>
    </div>
  );
}
