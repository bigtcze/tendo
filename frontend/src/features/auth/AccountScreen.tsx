import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { useI18n } from '../../i18n';
import { type OidcOutcome } from './oidcApi';
import { AccountOidcSectionView } from './AccountOidcSectionView';
import { useAccountOidcSection } from './useAccountOidcSection';
import { AccountPreferences } from './AccountPreferences';

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
      <section className="settle mx-auto w-full max-w-6xl">
        <Heading className="font-display text-h1 leading-tight">{t('account.title')}</Heading>
        <div role="status" className="mt-5 empty:mt-0 max-w-3xl">
          {outcomeText ? <p className="rounded-xl border border-line bg-sand px-4 py-3 text-base text-ink">{outcomeText}</p> : null}
        </div>
        <div className="mt-8 grid gap-8 lg:grid-cols-[minmax(0,1.2fr)_minmax(18rem,0.8fr)] lg:gap-x-12">
          <div>
            <section aria-labelledby="account-identity-heading">
              <h2 id="account-identity-heading" className="font-display text-h2">{t('account.identity.title')}</h2>
              <p
                ref={signedInRef}
                tabIndex={-1}
                className="mt-3 break-words text-lg text-ink outline-none"
              >
                {t('account.signedInAs', { login })}
              </p>
            </section>
            <AccountOidcSectionView state={sectionState} onSignedOut={onSignedOut} />
          </div>
          <AccountPreferences />
        </div>
      </section>
    </Layout>
  );
}
