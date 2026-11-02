import { createContext, useContext } from 'react';
import { useI18n, type MessageKey } from '../i18n';
import { NavIcon } from './NavIcons';
import { NavLink } from './NavLink';

export type HouseholdRoute = 'home' | 'allItems' | 'people' | 'members' | 'account' | 'item';
export type NavigationActions = { home: () => void; allItems: () => void; people: () => void; members: () => void; account: () => void };
export const HouseholdNavigationContext = createContext<{ route: HouseholdRoute; actions: NavigationActions; household: boolean } | null>(null);

type Destination = Exclude<HouseholdRoute, 'item'>;
type LinkSpec = { route: Destination; href: string; label: MessageKey; compactLabel?: MessageKey; name: MessageKey };

/** Full label on the desktop rail, short label in the phone bar; the accessible name always contains both (WCAG 2.5.3). */
const HOME: LinkSpec = { route: 'home', href: '/', label: 'nav.home', name: 'nav.home' };
const HOUSEHOLD_LINKS: LinkSpec[] = [
  { route: 'allItems', href: '/items', label: 'nav.allItems', compactLabel: 'nav.compact.allItems', name: 'home.allItems' },
  { route: 'people', href: '/people', label: 'nav.people', compactLabel: 'nav.compact.people', name: 'home.people' },
  { route: 'members', href: '/members', label: 'nav.members', name: 'home.members' },
];
const ACCOUNT: LinkSpec = { route: 'account', href: '/account', label: 'nav.account', name: 'home.account' };

/**
 * One navigation landmark whose presentation changes with width: a fixed bottom bar on phone/tablet
 * (static on short viewports such as 200–400% zoom, so it never covers content) and a left rail from `lg`.
 * `rail` is false for the household-less state, which keeps a small inline bar and never reserves rail space.
 */
export function HouseholdNavigation({ rail }: { rail: boolean }) {
  const navigation = useContext(HouseholdNavigationContext);
  const { t } = useI18n();
  if (!navigation) return null;
  const links = [HOME, ...(navigation.household ? HOUSEHOLD_LINKS : []), ACCOUNT];
  const active = navigation.route === 'item' ? 'allItems' : navigation.route;
  return (
    <nav aria-label={t('nav.label')} className={rail ? NAV_RAIL : NAV_INLINE}>
      <ul className={rail ? LIST_RAIL : LIST_INLINE}>
        {links.map((link) => (
          <li key={link.route} className="min-w-0 flex-1 lg:flex-none">
            <NavLink href={link.href} onNavigate={navigation.actions[link.route]} aria-label={t(link.name)}
              aria-current={active === link.route ? 'page' : undefined}
              className={`${rail ? LINK_RAIL : LINK_INLINE} ${active === link.route ? 'bg-sand font-medium text-ink' : 'text-muted hover:bg-sand/60 hover:text-ink'}`}>
              {rail ? <NavIcon route={link.route} /> : null}
              {rail ? <span className="max-w-full text-small lg:hidden">{t(link.compactLabel ?? link.label)}</span> : null}
              <span className={rail ? 'hidden lg:inline' : ''}>{t(link.label)}</span>
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  );
}

const FOCUS = 'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus';
const NAV_RAIL = [
  'fixed inset-x-0 bottom-0 z-20 border-t border-line bg-raised pb-[env(safe-area-inset-bottom)]',
  'short:static short:border-t-0 short:border-b short:pb-0',
  'lg:sticky lg:top-0 lg:col-start-1 lg:row-start-2 lg:self-start lg:border-t-0 lg:bg-transparent lg:pb-0',
].join(' ');
const LIST_RAIL = 'mx-auto flex max-w-xl gap-1 px-1 py-1 lg:mx-0 lg:max-w-none lg:flex-col lg:gap-1 lg:px-4 lg:py-6';
const LINK_RAIL = [
  'flex min-h-14 min-w-0 flex-col items-center justify-center gap-0.5 rounded-xl px-1 text-center leading-tight transition-colors',
  'lg:min-h-11 lg:flex-row lg:justify-start lg:gap-3 lg:px-3 lg:text-left lg:text-body',
  FOCUS,
].join(' ');
const NAV_INLINE = 'border-b border-line';
const LIST_INLINE = 'mx-auto flex max-w-sm gap-1 px-4 py-2';
const LINK_INLINE = `flex min-h-11 items-center justify-center rounded-xl px-3 text-center text-small transition-colors ${FOCUS}`;
