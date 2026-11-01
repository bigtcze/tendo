import { createContext, useContext, useState } from 'react';
import { NavLink } from './NavLink';
import { useI18n } from '../i18n';

export type HouseholdRoute = 'home' | 'allItems' | 'people' | 'members' | 'account' | 'item';
export type NavigationActions = { home: () => void; allItems: () => void; people: () => void; members: () => void; account: () => void };
export const HouseholdNavigationContext = createContext<{ route: HouseholdRoute; actions: NavigationActions; household: boolean } | null>(null);

export function HouseholdNavigation() {
  const navigation = useContext(HouseholdNavigationContext);
  const { t } = useI18n();
  const [moreOpen, setMoreOpen] = useState(false);
  if (!navigation) return null;
  const links: { route: HouseholdRoute; href: string; label: string; accessibleName?: string; action: () => void }[] = [
    { route: 'home', href: '/', label: t('nav.home'), action: navigation.actions.home },
    ...(navigation.household ? [
      { route: 'allItems' as const, href: '/items', label: t('nav.allItems'), accessibleName: t('home.allItems'), action: navigation.actions.allItems },
      { route: 'people' as const, href: '/people', label: t('nav.people'), accessibleName: t('home.people'), action: navigation.actions.people },
      { route: 'members' as const, href: '/members', label: t('nav.members'), accessibleName: t('home.members'), action: navigation.actions.members },
    ] : []),
    { route: 'account', href: '/account', label: t('nav.account'), accessibleName: t('home.account'), action: navigation.actions.account },
  ];
  const active = navigation.route === 'item' ? 'allItems' : navigation.route;
  const linkClass = (route: HouseholdRoute) => `flex min-h-11 min-w-0 items-center justify-center rounded-xl px-2 text-center text-sm leading-tight transition-colors hover:bg-sand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent ${active === route ? 'bg-sand font-medium text-ink' : 'text-muted'}`;
  // One list for every width: on phones Home, All items and People and things stay visible and "More" reveals the rest as a second row.
  return <nav aria-label={t('nav.label')} className="border-b border-line">
    <ul id="household-nav-links" className="flex flex-wrap gap-1 py-2 sm:flex-nowrap">
      {links.map((link, index) => <li key={link.route} className={index < 3 ? 'min-w-0 flex-1' : `${moreOpen ? 'order-2 basis-full' : 'hidden'} sm:order-none sm:block sm:min-w-0 sm:flex-1 sm:basis-0`}>
        <NavLink href={link.href} onNavigate={() => { setMoreOpen(false); link.action(); }} aria-label={link.accessibleName} aria-current={active === link.route ? 'page' : undefined} className={linkClass(link.route)}>
          {link.label}
        </NavLink>
      </li>)}
      {links.length > 3 ? <li className="order-1 min-w-0 flex-1 sm:hidden">
        <button type="button" aria-expanded={moreOpen} aria-controls="household-nav-links" onClick={() => setMoreOpen((open) => !open)}
          className="flex min-h-11 w-full items-center justify-center rounded-xl px-2 text-sm text-muted hover:bg-sand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent">
          {t('nav.more')}
        </button>
      </li> : null}
    </ul>
  </nav>;
}
