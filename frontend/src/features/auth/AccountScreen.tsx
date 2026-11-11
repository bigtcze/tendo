import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { Eyebrow } from '../../components/ui/eyebrow';
import { useI18n } from '../../i18n';
import { AccountPreferences } from './AccountPreferences';
import { AccountAdminLink } from './AccountAdminLink';

function initialOf(login: string): string {
  return login.trim().charAt(0).toLocaleUpperCase() || '?';
}

export function AccountScreen({ login, onSignedOut }: { login: string; onSignedOut: () => void }) {
  const { t } = useI18n();

  return (
    <Layout
      actions={<AccountActions login={login} onSignedOut={onSignedOut} />}
    >
      <section className="settle">
        <Eyebrow>{t('account.eyebrow')}</Eyebrow>
        <Heading className="mt-2 text-h1 leading-tight text-ink">{t('account.title')}</Heading>
        <p className="mt-2 max-w-prose text-base text-muted">{t('account.subtitle')}</p>
        <div className="mt-6 max-w-3xl space-y-4">
          <section aria-labelledby="account-identity-heading" className="rounded-2xl border border-line bg-raised p-5 sm:p-6">
            <h2 id="account-identity-heading" className="text-lg font-bold text-ink">{t('account.identity.title')}</h2>
            <div className="mt-4 flex items-center gap-3">
              <span aria-hidden="true" className="grid size-11 shrink-0 place-items-center rounded-full bg-nav-selected text-base font-semibold text-ink">
                {initialOf(login)}
              </span>
              <p className="min-w-0 break-words text-base text-ink">
                {t('account.signedInAs', { login })}
              </p>
            </div>
          </section>
          <AccountAdminLink />
          <AccountPreferences />
        </div>
      </section>
    </Layout>
  );
}
