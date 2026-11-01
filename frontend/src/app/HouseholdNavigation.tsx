import { useState, createContext, useContext } from 'react';
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
  const links: { route: HouseholdRoute; href: string; label: string; accessibleName?: string; action: () => void; secondary?: boolean }[] = [
    { route: 'home', href: '/', label: t('nav.home'), action: navigation.actions.home },
    ...(navigation.household ? [
      { route: 'allItems' as const, href: '/items', label: t('nav.allItems'), accessibleName: t('home.allItems'), action: navigation.actions.allItems },
      { route: 'people' as const, href: '/people', label: t('nav.people'), accessibleName: t('home.people'), action: navigation.actions.people },
      { route: 'members' as const, href: '/members', label: t('nav.members'), accessibleName: t('home.members'), action: navigation.actions.members, secondary: true },
    ] : []),
    { route: 'account', href: '/account', label: t('nav.account'), accessibleName: t('home.account'), action: navigation.actions.account, secondary: true },
  ];
  const active = navigation.route === 'item' ? 'allItems' : navigation.route;
  const visibleMobile = (route: HouseholdRoute) => route === 'home' || route === 'allItems' || route === 'people' || active === route;
  return <nav aria-label={t('nav.label')} className="app-nav border-b border-line">
    <ul className="app-nav-list flex flex-wrap gap-x-1 py-2">
      {links.map((link) => <li key={link.route} data-secondary={link.secondary ? 'true' : undefined} data-mobile-visible={visibleMobile(link.route) ? 'true' : undefined} className="nav-link min-w-0 flex-1 basis-[30%] sm:basis-0">
        <NavLink href={link.href} onNavigate={link.action} aria-label={link.accessibleName} aria-current={active === link.route ? 'page' : undefined}
          className={`flex min-h-11 min-w-0 items-center justify-center rounded-xl px-2 text-center text-sm leading-tight transition-colors hover:bg-sand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent ${active === link.route ? 'bg-sand font-medium text-ink' : 'text-muted'}`}>
          {link.label}
        </NavLink>
      </li>)}
      {navigation.household ? <li className="nav-link more-link" data-mobile-visible="true">
        <button type="button" aria-expanded={moreOpen} onClick={() => setMoreOpen((open) => !open)} className="more-link-anchor flex min-h-11 min-w-0 w-full items-center justify-center rounded-xl px-2 text-center text-sm text-muted">{moreOpen ? t('nav.more') : <><span aria-hidden="true">···</span><span className="sr-only">{t('nav.more')}</span></>}</button>
        {moreOpen ? <div className="more-menu" role="group" aria-label={t('nav.more')}>
          <NavLink href="/members" onNavigate={navigation.actions.members}>{t('nav.members')}</NavLink>
          <NavLink href="/account" onNavigate={navigation.actions.account}>{t('nav.account')}</NavLink>
        </div> : null}
      </li> : null}
    </ul>
  </nav>;
}
