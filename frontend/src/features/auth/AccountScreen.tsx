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
      <section className="settle">
        <Heading className="font-display text-h1 leading-tight">{t('account.title')}</Heading>
        <div role="status" className="mt-5 empty:mt-0">
          {outcomeText ? <p className="rounded-xl bg-sand px-4 py-3">{outcomeText}</p> : null}
        </div>
        <p
          ref={signedInRef}
          tabIndex={-1}
          className="mt-6 break-words text-lg text-muted outline-none"
        >
          {t('account.signedInAs', { login })}
        </p>
        <AccountOidcSectionView state={sectionState} onSignedOut={onSignedOut} />
        <AccountPreferences />
      </section>
    </Layout>
  );
}
