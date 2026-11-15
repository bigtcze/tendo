import { useContext } from 'react';
import { useViewerRole } from '../../app/HouseholdData';
import { HouseholdNavigationContext } from '../../app/HouseholdNavigation';
import { NavLink } from '../../app/NavLink';
import { useI18n } from '../../i18n';

const LINK = 'flex min-h-11 items-center border-b-2 px-1 text-small transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus';
const CURRENT = `${LINK} border-accent font-semibold text-ink`;
const QUIET = `${LINK} border-transparent text-muted hover:text-ink`;

/**
 * The Admin center has two sections, and only an account that holds both grants has a reason to move between
 * them: this household's accounts and every household of the installation. It reads the viewer's own grants
 * only — never the household a screen is showing — and stays out of the way otherwise, as two quiet links under
 * the page title rather than a tab strip with a frame.
 */
export function AdminSectionSwitcher({ current }: { current: 'household' | 'households' }) {
  const { t } = useI18n();
  const role = useViewerRole();
  const navigation = useContext(HouseholdNavigationContext);
  if (!navigation || !navigation.instanceAdmin || role !== 'admin') return null;

  return <nav aria-label={t('admin.sections.label')} className="mt-5 border-b border-line sm:mt-6">
    <ul className="flex gap-6">
      <li>
        <NavLink href="/admin" onNavigate={navigation.actions.admin}
          aria-current={current === 'household' ? 'page' : undefined}
          className={current === 'household' ? CURRENT : QUIET}>
          {t('admin.sections.household')}
        </NavLink>
      </li>
      <li>
        <NavLink href="/admin/households" onNavigate={navigation.actions.adminHouseholds}
          aria-current={current === 'households' ? 'page' : undefined}
          className={current === 'households' ? CURRENT : QUIET}>
          {t('admin.sections.households')}
        </NavLink>
      </li>
    </ul>
  </nav>;
}
