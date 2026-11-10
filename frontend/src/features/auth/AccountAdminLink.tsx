import { useContext } from 'react';
import { useViewerRole } from '../../app/HouseholdData';
import { HouseholdNavigationContext } from '../../app/HouseholdNavigation';
import { NavLink } from '../../app/NavLink';
import { useI18n } from '../../i18n';

const LINK = [
  'mt-4 inline-flex min-h-11 items-center rounded-xl border border-control-border bg-raised px-5 text-control font-semibold text-ink',
  'transition-colors hover:bg-sand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus',
].join(' ');

/** Administrators reach the Admin center from Account at every width, since the phone bar keeps five fixed items. */
export function AccountAdminLink() {
  const { t } = useI18n();
  const role = useViewerRole();
  const navigation = useContext(HouseholdNavigationContext);
  if (role !== 'admin' || !navigation) return null;
  return <section aria-labelledby="account-admin-heading" className="rounded-2xl border border-line bg-raised p-5 sm:p-6">
    <h2 id="account-admin-heading" className="text-lg font-bold text-ink">{t('admin.account.title')}</h2>
    <p className="mt-1 max-w-prose text-small text-muted">{t('admin.account.body')}</p>
    <NavLink href="/admin" onNavigate={navigation.actions.admin} className={LINK}>{t('admin.account.link')}</NavLink>
  </section>;
}
