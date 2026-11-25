import { useRef } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { useI18n } from '../../i18n';
import { ConnectedAppsSection } from '../mcp/ConnectedAppsSection';
import { OwnAiAppsSection } from '../mcp/OwnAiAppsSection';
import { AccountAdminLink } from './AccountAdminLink';
import { AccountHouseholdSection } from './AccountHouseholdSection';
import { AccountPasswordSection } from './AccountPasswordSection';
import { AccountPreferences } from './AccountPreferences';
import type { Session } from './sessionApi';

/**
 * Account is a stack of flat settings sections separated by a top rule, matching Members and People. The header
 * already shows the account's initial, so the identity section states the login alone. The password section follows
 * the sign-in it belongs to, before the administrative settings; the household membership and display choices come
 * after, quiet and out of the way. AI apps and the connected apps close the page, and only show once they apply.
 */
export function AccountScreen({ session, onSessionChanged, onSignedOut, onSelfLeft }: {
  session: Session;
  onSessionChanged: (session: Session) => void;
  onSignedOut: () => void;
  /** The viewer left the household: the shell adopts the fresh session and opens Home. */
  onSelfLeft: (household: string, alreadyGone: boolean) => void;
}) {
  const { t } = useI18n();
  /** Last-resort focus target when a disconnect leaves the connected-apps section with nothing to focus. */
  const pageHeadingRef = useRef<HTMLHeadingElement>(null);

  return (
    <Layout
      actions={<AccountActions login={session.login} onSignedOut={onSignedOut} />}
    >
      <section className="settle">
        <Heading className="text-h1 font-semibold text-ink" headingRef={pageHeadingRef}>{t('account.title')}</Heading>
        <div className="mt-6 max-w-3xl space-y-6 sm:mt-8">
          <section aria-labelledby="account-identity-heading" className="border-t border-line pt-6">
            <h2 id="account-identity-heading" className="text-h2 text-ink">{t('account.identity.title')}</h2>
            <p className="mt-1 break-words text-body text-ink">
              {t('account.signedInAs', { login: session.login })}
            </p>
          </section>
          <AccountPasswordSection session={session} onChanged={onSessionChanged} onSignedOut={onSignedOut} />
          <AccountAdminLink instanceAdmin={session.instanceAdmin} />
          <AccountPreferences />
          <AccountHouseholdSection session={session} onSignedOut={onSignedOut} onSelfLeft={onSelfLeft} />
        </div>
        <OwnAiAppsSection onSignedOut={onSignedOut} />
        <ConnectedAppsSection onSignedOut={onSignedOut} pageHeadingRef={pageHeadingRef} />
      </section>
    </Layout>
  );
}
