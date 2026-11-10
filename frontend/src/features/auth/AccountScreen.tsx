import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { Notice } from '../../components/ui/notice';
import { useI18n } from '../../i18n';
import { type OidcOutcome } from './oidcApi';
import { AccountOidcSectionView } from './AccountOidcSectionView';
import { useAccountOidcSection } from './useAccountOidcSection';
import { AccountPreferences } from './AccountPreferences';
import { AccountAdminLink } from './AccountAdminLink';

function initialOf(login: string): string {
  return login.trim().charAt(0).toLocaleUpperCase() || '?';
}

export function AccountScreen({ login, outcome, onSignedOut }: { login: string; outcome: OidcOutcome | null; onSignedOut: () => void }) {
  const { t } = useI18n();
  const sectionState = useAccountOidcSection(onSignedOut);
  const { signedInRef } = sectionState;

  const outcomeText = outcome
    ? outcome.kind === 'connected'
      ? t('account.oidc.connected')
      : t(`oidc.error.${outcome.code}`)
    : null;

  return (
    <Layout
      actions={<AccountActions login={login} onSignedOut={onSignedOut} />}
    >
      <section className="settle mx-auto w-full max-w-3xl">
        <p className="text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-muted">{t('account.eyebrow')}</p>
        <Heading className="mt-2 text-h1 leading-tight text-ink">{t('account.title')}</Heading>
        <p className="mt-2 max-w-prose text-base text-muted">{t('account.subtitle')}</p>
        <div role="status" className="mt-5 empty:mt-0">
          {outcomeText ? <Notice role={null}>{outcomeText}</Notice> : null}
        </div>
        <div className="mt-6 space-y-4">
          <section aria-labelledby="account-identity-heading" className="rounded-2xl border border-line bg-raised p-5 sm:p-6">
            <h2 id="account-identity-heading" className="text-lg font-bold text-ink">{t('account.identity.title')}</h2>
            <div className="mt-4 flex items-center gap-3">
              <span aria-hidden="true" className="grid size-11 shrink-0 place-items-center rounded-full bg-nav-selected text-base font-semibold text-ink">
                {initialOf(login)}
              </span>
              <p ref={signedInRef} tabIndex={-1} className="min-w-0 break-words text-base text-ink outline-none">
                {t('account.signedInAs', { login })}
              </p>
            </div>
          </section>
          <AccountAdminLink />
          <AccountOidcSectionView state={sectionState} onSignedOut={onSignedOut} />
          <AccountPreferences />
        </div>
      </section>
    </Layout>
  );
}
