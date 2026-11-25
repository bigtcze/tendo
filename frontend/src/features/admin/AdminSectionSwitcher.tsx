import { useContext } from 'react';
import { useViewerRole } from '../../app/HouseholdData';
import { HouseholdNavigationContext } from '../../app/HouseholdNavigation';
import { NavLink } from '../../app/NavLink';
import { ADMIN_ACCOUNTS_PATH, ADMIN_AI_APPS_PATH, ADMIN_HOUSEHOLDS_PATH, ADMIN_PATH } from '../../app/routes';
import { useI18n, type MessageKey } from '../../i18n';

const LINK = 'flex min-h-11 min-w-11 items-center justify-center border-b-2 px-1 text-small transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus';
const CURRENT = `${LINK} border-accent font-semibold text-ink`;
const QUIET = `${LINK} border-transparent text-muted hover:text-ink`;

type Section = 'household' | 'households' | 'accounts' | 'aiApps';
type SectionLink = {
  key: Section;
  href: string;
  run: 'admin' | 'adminHouseholds' | 'adminAccounts' | 'adminAiApps';
  label: MessageKey;
};

/** A household administrator manages one household; the installation grant opens the installation-wide sections. */
function sectionsFor(householdAdmin: boolean, instanceAdmin: boolean): SectionLink[] {
  const household: SectionLink[] = householdAdmin
    ? [{ key: 'household', href: ADMIN_PATH, run: 'admin', label: 'admin.sections.household' }]
    : [];
  const installation: SectionLink[] = instanceAdmin
    ? [
      { key: 'households', href: ADMIN_HOUSEHOLDS_PATH, run: 'adminHouseholds', label: 'admin.sections.households' },
      { key: 'accounts', href: ADMIN_ACCOUNTS_PATH, run: 'adminAccounts', label: 'admin.sections.accounts' },
      { key: 'aiApps', href: ADMIN_AI_APPS_PATH, run: 'adminAiApps', label: 'admin.sections.aiApps' },
    ]
    : [];
  return [...household, ...installation];
}

/**
 * The Admin center's sections. They follow the viewer's own grants only — never the household a screen is showing —
 * so a household administrator sees this household, and the installation grant alone is already three sections:
 * every household, every account and the installation's AI apps. With fewer than two sections there is nothing to
 * move between and the switcher stays out of the way: quiet links under the page title, never a framed tab strip.
 */
export function AdminSectionSwitcher({ current }: { current: Section }) {
  const { t } = useI18n();
  const role = useViewerRole();
  const navigation = useContext(HouseholdNavigationContext);
  if (!navigation) return null;
  const sections = sectionsFor(role === 'admin', navigation.instanceAdmin);
  if (sections.length < 2) return null;

  return <nav aria-label={t('admin.sections.label')} className="mt-5 border-b border-line sm:mt-6">
    <ul className="flex flex-wrap gap-x-6 gap-y-1">
      {sections.map((section) => <li key={section.key}>
        <NavLink href={section.href} onNavigate={navigation.actions[section.run]}
          aria-current={current === section.key ? 'page' : undefined}
          className={current === section.key ? CURRENT : QUIET}>
          {t(section.label)}
        </NavLink>
      </li>)}
    </ul>
  </nav>;
}
