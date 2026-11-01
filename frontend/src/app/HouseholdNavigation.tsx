import { createContext, useContext } from 'react';
import { NavLink } from './NavLink';
import { useI18n } from '../i18n';

export type HouseholdRoute = 'home' | 'allItems' | 'people' | 'members' | 'account' | 'item';
export type NavigationActions = { home: () => void; allItems: () => void; people: () => void; members: () => void; account: () => void };
export const HouseholdNavigationContext = createContext<{ route: HouseholdRoute; actions: NavigationActions; household: boolean } | null>(null);

export function HouseholdNavigation() {
  const navigation = useContext(HouseholdNavigationContext);
  const { t } = useI18n();
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
  return <nav aria-label={t('nav.label')} className="border-b border-line lg:flex lg:justify-end">
    <ul className="flex gap-1 overflow-x-auto py-2 lg:w-auto lg:min-w-[42rem] lg:justify-between">
      {links.map((link) => <li key={link.route} className="shrink-0 lg:flex-1">
        <NavLink href={link.href} onNavigate={link.action} aria-label={link.accessibleName} aria-current={active === link.route ? 'page' : undefined}
          className={`flex min-h-11 min-w-0 items-center justify-center whitespace-nowrap rounded-full px-3 text-center text-sm transition-colors hover:bg-sand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent ${active === link.route ? 'bg-sand font-medium text-ink' : 'text-muted'}`}>
          {link.label}
        </NavLink>
      </li>)}
    </ul>
  </nav>;
}
