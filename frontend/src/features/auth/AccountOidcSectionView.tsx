import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import { AccountOidcLinkForm } from './AccountOidcLinkForm';
import type { AccountSectionState } from './useAccountOidcSection';

export function AccountOidcSectionView({
  state,
  onSignedOut,
}: {
  state: AccountSectionState;
  onSignedOut: () => void;
}) {
  const { t } = useI18n();
  const { section, showRetry, retry, hide, retryRef, sectionHeadingRef } = state;
  const connected = section.kind === 'linked' || section.kind === 'notLinked';
  return (
    <>
      {section.kind === 'loading' ? <p role="status" className="text-small text-muted">{t('account.oidc.loading')}</p> : null}
      {section.kind === 'error' ? <p role="status" className="text-small text-muted">{t('account.oidc.loadError')}</p> : null}
      {section.kind === 'error' || (showRetry && section.kind === 'loading') ? (
        <Button ref={retryRef} type="button" variant="quiet" size="small" className="self-start" onClick={retry}>
          {t('app.error.retry')}
        </Button>
      ) : null}
      {connected ? (
        <section aria-labelledby="account-oidc-heading" className="rounded-2xl border border-line bg-raised p-5 sm:p-6">
          <h2 id="account-oidc-heading" ref={sectionHeadingRef} tabIndex={-1} className="break-words text-lg font-bold text-ink outline-none">
            {t('account.oidc.title', { provider: section.provider })}
          </h2>
          <p className="mt-2 text-base text-ink">
            {section.kind === 'linked'
              ? t('account.oidc.linked', { provider: section.provider })
              : t('account.oidc.intro', { provider: section.provider })}
          </p>
          {section.kind === 'notLinked'
            ? <AccountOidcLinkForm provider={section.provider} onSignedOut={onSignedOut} onDisabled={hide} />
            : null}
        </section>
      ) : null}
    </>
  );
}
