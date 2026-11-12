import { createContext, useContext, useId, useRef, useState } from 'react';
import { useI18n, type MessageKey } from '../i18n';
import { useViewerRole } from './HouseholdData';
import { NavIcon, type NavIconRoute } from './NavIcons';
import { NavLink } from './NavLink';
import { useDismiss } from './useDismiss';

export type HouseholdRoute = 'home' | 'allItems' | 'people' | 'members' | 'account' | 'admin' | 'item';
export type NavigationActions = {
  home: () => void;
  allItems: (subjectId?: string) => void;
  archive: () => void;
  people: () => void;
  members: () => void;
  account: () => void;
  admin: () => void;
};
export const HouseholdNavigationContext = createContext<{ route: HouseholdRoute; actions: NavigationActions; household: boolean } | null>(null);

type Destination = Exclude<HouseholdRoute, 'item'>;
type LinkSpec = {
  key: string;
  route: Destination;
  href: string;
  icon: NavIconRoute;
  label: MessageKey;
  compactLabel: MessageKey;
  /** Accessible name; always the full destination, never the compact phone label alone (WCAG 2.5.3). */
  name: MessageKey;
  /** Hidden in the phone bar; reached from the More sheet instead. */
  phoneSheet?: boolean;
  adminOnly?: boolean;
};

const HOME: LinkSpec = { key: 'home', route: 'home', href: '/', icon: 'home', label: 'nav.home', compactLabel: 'nav.home', name: 'nav.home' };
const ALL_ITEMS: LinkSpec = { key: 'items', route: 'allItems', href: '/items', icon: 'allItems', label: 'nav.allItems', compactLabel: 'nav.compact.allItems', name: 'home.allItems' };
const ARCHIVE: LinkSpec = { key: 'archive', route: 'allItems', href: '/items?status=archived', icon: 'archive', label: 'nav.archive', compactLabel: 'nav.archive', name: 'nav.archive', phoneSheet: true };
const PEOPLE: LinkSpec = { key: 'people', route: 'people', href: '/people', icon: 'people', label: 'nav.people', compactLabel: 'nav.compact.people', name: 'home.people' };
const MEMBERS: LinkSpec = { key: 'members', route: 'members', href: '/members', icon: 'members', label: 'nav.members', compactLabel: 'nav.members', name: 'home.members', phoneSheet: true };
const ACCOUNT: LinkSpec = { key: 'account', route: 'account', href: '/account', icon: 'account', label: 'nav.account', compactLabel: 'nav.account', name: 'home.account', phoneSheet: true };
const ADMIN: LinkSpec = { key: 'admin', route: 'admin', href: '/admin', icon: 'admin', label: 'nav.admin', compactLabel: 'nav.admin', name: 'nav.admin', phoneSheet: true, adminOnly: true };

type NavContext = {
  navigation: NonNullable<React.ContextType<typeof HouseholdNavigationContext>>;
  admin: boolean;
  archivedView: boolean;
};

function isActive(spec: LinkSpec, { navigation, archivedView }: NavContext) {
  if (spec.key === 'archive') return archivedView;
  // An open item detail belongs to the All items list.
  if (spec.key === 'items') return (navigation.route === 'allItems' || navigation.route === 'item') && !archivedView;
  return navigation.route === spec.route;
}

function navigate(spec: LinkSpec, { navigation }: NavContext) {
  (spec.key === 'archive' ? navigation.actions.archive : navigation.actions[spec.route])();
}

/** One destination; the sidebar shows the full label, the phone bar its short label, the name keeps both. */
function NavItem({ spec, className, context, onSelect, plain = false }: {
  spec: LinkSpec; className: string; context: NavContext; onSelect?: () => void; plain?: boolean;
}) {
  const { t } = useI18n();
  const active = isActive(spec, context);
  return <NavLink
    href={spec.href}
    onNavigate={() => { onSelect?.(); navigate(spec, context); }}
    aria-label={t(spec.name)}
    aria-current={active ? 'page' : undefined}
    className={`${className} ${active ? SELECTED : QUIET}`}
  >
    <NavIcon route={spec.icon} className="size-5 shrink-0 lg:size-[1.125rem]" />
    {plain ? <span className="max-w-full">{t(spec.label)}</span> : <>
      <span className="max-w-full lg:hidden">{t(spec.compactLabel)}</span>
      <span className="hidden lg:inline">{t(spec.label)}</span>
    </>}
  </NavLink>;
}

function MoreButton({ open, panelId, onToggle, buttonRef }: {
  open: boolean; panelId: string; onToggle: () => void; buttonRef: React.RefObject<HTMLButtonElement | null>;
}) {
  const { t } = useI18n();
  return <button
    ref={buttonRef}
    type="button"
    aria-expanded={open}
    aria-controls={panelId}
    onClick={onToggle}
    className={`${BAR_LINK} w-full ${open ? 'text-ink' : 'text-muted'}`}
  >
    <svg aria-hidden="true" focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth="1.75" strokeLinecap="round" className="size-5 shrink-0">
      <circle cx="5" cy="12" r="1.4" /><circle cx="12" cy="12" r="1.4" /><circle cx="19" cy="12" r="1.4" />
    </svg>
    <span>{t('nav.more')}</span>
  </button>;
}

/**
 * One navigation landmark with one set of links. It is a fixed bottom bar on phones (static after
 * main on short viewports, so 200–400% zoom never covers content) and a quiet left column from `lg`.
 * Archive, Members, Account and Admin move into the phone More sheet, so Archive is never buried and
 * the bar keeps four comfortable 44 px slots.
 */
export function HouseholdNavigation({ rail }: { rail: boolean }) {
  const navigation = useContext(HouseholdNavigationContext);
  const role = useViewerRole();
  const { t } = useI18n();
  const [moreOpen, setMoreOpen] = useState(false);
  const panelId = useId();
  const rootRef = useRef<HTMLElement>(null);
  const moreButtonRef = useRef<HTMLButtonElement>(null);
  useDismiss(moreOpen, () => setMoreOpen(false), rootRef, moreButtonRef);
  if (!navigation) return null;

  const context: NavContext = {
    navigation,
    admin: navigation.household && role === 'admin',
    archivedView: navigation.route === 'allItems' && window.location.search.includes('status=archived'),
  };
  if (!navigation.household) {
    return <nav aria-label={t('nav.label')} className={NAV_BAR}>
      <ul className={LIST_ROW}>
        {[HOME, ACCOUNT].map((spec) => <li key={spec.key} className="min-w-0">
          <NavItem spec={spec} className={INLINE_LINK} context={context} plain />
        </li>)}
      </ul>
    </nav>;
  }
  const sheet = [ARCHIVE, MEMBERS, ACCOUNT, ...(context.admin ? [ADMIN] : [])];
  return (
    <nav ref={rootRef} aria-label={t('nav.label')} className={rail ? NAV_RESPONSIVE : NAV_BAR}>
      {moreOpen ? <div id={panelId} className="absolute inset-x-0 bottom-full rounded-t-2xl border-t border-line bg-raised px-3 pb-2 pt-2.5 shadow-[0_-24px_48px_-24px_rgb(20_18_14/0.5)] lg:hidden">
        <ul className="mx-auto max-w-md">
          {sheet.map((spec) => <li key={spec.key}>
            <NavItem spec={spec} className={SHEET_LINK} context={context} onSelect={() => setMoreOpen(false)} />
          </li>)}
        </ul>
      </div> : <div id={panelId} hidden />}
      <ul className={LIST_PRIMARY}>
        <li className="min-w-0 flex-1 lg:flex-none"><NavItem spec={HOME} className={BAR_LINK} context={context} /></li>
        <li className="min-w-0 flex-1 lg:flex-none"><NavItem spec={ALL_ITEMS} className={BAR_LINK} context={context} /></li>
        <li className="hidden min-w-0 flex-1 lg:flex lg:flex-none"><NavItem spec={ARCHIVE} className={BAR_LINK} context={context} /></li>
        <li className="min-w-0 flex-1 lg:mt-4 lg:flex-none lg:border-t lg:border-line lg:pt-4"><NavItem spec={PEOPLE} className={BAR_LINK} context={context} /></li>
        <li className="hidden min-w-0 flex-1 lg:flex lg:flex-none"><NavItem spec={MEMBERS} className={BAR_LINK} context={context} /></li>
        <li className="min-w-0 flex-1 lg:hidden">
          <MoreButton open={moreOpen} panelId={panelId} buttonRef={moreButtonRef} onToggle={() => setMoreOpen(!moreOpen)} />
        </li>
      </ul>
      <ul className={LIST_SECONDARY}>
        <li><NavItem spec={ACCOUNT} className={BAR_LINK} context={context} /></li>
        {context.admin ? <li><NavItem spec={ADMIN} className={BAR_LINK} context={context} /></li> : null}
      </ul>
    </nav>
  );
}

const FOCUS = 'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus';
const SELECTED = 'bg-nav-selected font-medium text-ink';
const QUIET = 'font-medium text-muted hover:bg-sand/70 hover:text-ink';
// One link style, two presentations: a phone tab and a sidebar row.
const BAR_LINK = [
  'flex min-h-14 min-w-0 flex-col items-center justify-center gap-0.5 rounded-lg px-0.5 text-center text-micro leading-tight transition-colors lg:min-h-10 lg:w-full lg:flex-row lg:justify-start lg:gap-3 lg:px-3 lg:text-left lg:text-body',
  FOCUS,
].join(' ');
const SHEET_LINK = `flex min-h-12 items-center gap-3 rounded-lg px-3 text-body text-ink transition-colors hover:bg-sand ${FOCUS}`;
const INLINE_LINK = `flex min-h-11 min-w-0 items-center gap-2 rounded-lg px-3 text-small transition-colors ${FOCUS}`;

const NAV_RESPONSIVE = [
  'fixed inset-x-0 bottom-0 z-20 border-t border-line bg-raised pb-[env(safe-area-inset-bottom)]',
  'short:static short:pb-0',
  'lg:sticky lg:top-16 lg:col-start-1 lg:row-start-1 lg:self-start lg:h-[calc(100dvh-4rem)]',
  'lg:overflow-y-auto lg:border-t-0 lg:border-r lg:border-line lg:bg-transparent lg:px-3 lg:py-7',
].join(' ');
const NAV_BAR = 'fixed inset-x-0 bottom-0 z-20 border-t border-line bg-raised pb-[env(safe-area-inset-bottom)] short:static short:pb-0';
const LIST_PRIMARY = 'mx-auto flex max-w-xl gap-1 px-2 py-1 lg:mx-0 lg:max-w-none lg:flex-col lg:gap-0.5 lg:px-0 lg:py-0';
const LIST_SECONDARY = 'hidden lg:mt-4 lg:flex lg:flex-col lg:gap-0.5 lg:border-t lg:border-line lg:pt-4';
const LIST_ROW = 'mx-auto flex max-w-xl gap-1 px-2 py-1';
