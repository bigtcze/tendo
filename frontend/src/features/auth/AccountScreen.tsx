import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { useI18n } from '../../i18n';
import { AccountPreferences } from './AccountPreferences';
import { AccountAdminLink } from './AccountAdminLink';

/**
 * Account is a stack of flat settings sections separated by a top rule, matching Members and People. The header
 * already shows the account's initial, so the identity section states the login alone.
 */
export function AccountScreen({ login, onSignedOut }: { login: string; onSignedOut: () => void }) {
  const { t } = useI18n();

  return (
    <Layout
      actions={<AccountActions login={login} onSignedOut={onSignedOut} />}
    >
      <section className="settle">
        <Heading className="text-h1 font-semibold text-ink">{t('account.title')}</Heading>
        <div className="mt-6 max-w-3xl space-y-6 sm:mt-8">
          <section aria-labelledby="account-identity-heading" className="border-t border-line pt-6">
            <h2 id="account-identity-heading" className="text-h2 text-ink">{t('account.identity.title')}</h2>
            <p className="mt-1 break-words text-body text-ink">
              {t('account.signedInAs', { login })}
            </p>
          </section>
          <AccountAdminLink />
          <AccountPreferences />
        </div>
      </section>
    </Layout>
  );
}
