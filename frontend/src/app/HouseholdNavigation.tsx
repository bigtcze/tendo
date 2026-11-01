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
  const primary = links.slice(0, 2);
  const overflow = links.slice(2);
  return <nav aria-label={t('nav.label')} className="border-b border-line">
    <ul className="flex gap-1 py-2 sm:hidden">
      {primary.map((link) => <li key={link.route} className="min-w-0 flex-1">
        <NavLink href={link.href} onNavigate={link.action} aria-label={link.accessibleName} aria-current={active === link.route ? 'page' : undefined}
          className={`flex min-h-11 items-center justify-center rounded-xl px-2 text-center text-sm leading-tight transition-colors hover:bg-sand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent ${active === link.route ? 'bg-sand font-medium text-ink' : 'text-muted'}`}>
          {link.label}
        </NavLink>
      </li>)}
      <li className="relative min-w-0 flex-1">
        <button type="button" aria-expanded={moreOpen} aria-controls="household-nav-more" onClick={() => setMoreOpen((open) => !open)}
          className="flex min-h-11 w-full items-center justify-center rounded-xl px-2 text-sm hover:bg-sand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent">
          {t('nav.more')}
        </button>
        {moreOpen ? <ul id="household-nav-more" className="absolute right-0 top-full z-20 min-w-48 rounded-xl border border-line bg-white p-1 shadow-lg">
          {overflow.map((link) => <li key={link.route}>
            <NavLink href={link.href} onNavigate={() => { setMoreOpen(false); link.action(); }} aria-label={link.accessibleName} aria-current={active === link.route ? 'page' : undefined}
              className={`flex min-h-11 items-center rounded-lg px-3 text-sm hover:bg-sand focus-visible:outline-2 focus-visible:outline-accent ${active === link.route ? 'bg-sand font-medium text-ink' : 'text-muted'}`}>
              {link.label}
            </NavLink>
          </li>)}
        </ul> : null}
      </li>
    </ul>
    <ul className="hidden gap-x-1 py-2 sm:flex">
      {links.map((link) => <li key={link.route} className="min-w-0 flex-1">
        <NavLink href={link.href} onNavigate={link.action} aria-label={link.accessibleName} aria-current={active === link.route ? 'page' : undefined}
          className={`flex min-h-11 min-w-0 items-center justify-center rounded-xl px-2 text-center text-sm leading-tight transition-colors hover:bg-sand focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent ${active === link.route ? 'bg-sand font-medium text-ink' : 'text-muted'}`}>
          {link.label}
        </NavLink>
      </li>)}
    </ul>
  </nav>;
}
