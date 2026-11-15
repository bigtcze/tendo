import { useContext } from 'react';
import { useViewerRole } from '../../app/HouseholdData';
import { HouseholdNavigationContext } from '../../app/HouseholdNavigation';
import { NavLink } from '../../app/NavLink';
import { useI18n } from '../../i18n';

const LINK = [
  'mt-4 inline-flex min-h-11 items-center rounded-xl border border-control-border bg-raised px-5 text-control font-semibold text-ink',
  'transition-colors hover:bg-sand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus',
].join(' ');

/**
 * Administrators reach the Admin center from Account at every width, since the phone bar keeps five fixed items.
 * The same entry serves the installation grant: a household administrator opens this household's accounts, and
 * an account that only holds the installation grant opens the household list.
 */
export function AccountAdminLink({ instanceAdmin }: { instanceAdmin: boolean }) {
  const { t } = useI18n();
  const role = useViewerRole();
  const navigation = useContext(HouseholdNavigationContext);
  const householdAdmin = role === 'admin';
  if (!navigation || (!householdAdmin && !instanceAdmin)) return null;
  return <section aria-labelledby="account-admin-heading" className="border-t border-line pt-6">
    <h2 id="account-admin-heading" className="text-h2 text-ink">
      {t(householdAdmin ? 'admin.account.title' : 'install.account.title')}
    </h2>
    <p className="mt-1 max-w-prose text-small text-muted">
      {t(householdAdmin ? 'admin.account.body' : 'install.account.body')}
    </p>
    <NavLink
      href={householdAdmin ? '/admin' : '/admin/households'}
      onNavigate={householdAdmin ? navigation.actions.admin : navigation.actions.adminHouseholds}
      className={LINK}
    >
      {t(householdAdmin ? 'admin.account.link' : 'install.account.link')}
    </NavLink>
  </section>;
}
