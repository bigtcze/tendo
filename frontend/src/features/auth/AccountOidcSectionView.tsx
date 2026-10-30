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
  return (
    <>
      {section.kind === 'loading' ? <p role="status" className="mt-8 text-muted">{t('account.oidc.loading')}</p> : null}
      {section.kind === 'error' ? <p role="status" className="mt-8 text-muted">{t('account.oidc.loadError')}</p> : null}
      {section.kind === 'error' || (showRetry && section.kind === 'loading') ? (
        <Button ref={retryRef} type="button" variant="quiet" className="mt-3" onClick={retry}>
          {t('app.error.retry')}
        </Button>
      ) : null}
      {section.kind === 'linked' ? (
        <section className="mt-8 border-t border-line pt-6">
          <h2 ref={sectionHeadingRef} tabIndex={-1} className="font-display text-2xl break-words outline-none">
            {t('account.oidc.title', { provider: section.provider })}
          </h2>
          <p className="mt-2 text-muted">{t('account.oidc.linked', { provider: section.provider })}</p>
        </section>
      ) : null}
      {section.kind === 'notLinked' ? (
        <section className="mt-8 border-t border-line pt-6">
          <h2 ref={sectionHeadingRef} tabIndex={-1} className="font-display text-2xl break-words outline-none">
            {t('account.oidc.title', { provider: section.provider })}
          </h2>
          <p className="mt-2 text-muted">{t('account.oidc.intro', { provider: section.provider })}</p>
          <AccountOidcLinkForm provider={section.provider} onSignedOut={onSignedOut} onDisabled={hide} />
        </section>
      ) : null}
    </>
  );
}
