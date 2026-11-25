import type { HouseholdRoute } from './HouseholdNavigation';

export const PEOPLE_PATH = '/people';
export const MEMBERS_PATH = '/members';
export const INVITE_PATH = '/invite';
export const RESET_PASSWORD_PATH = '/reset-password';
export const LOGIN_PATH = '/login';
export const ACCOUNT_PATH = '/account';
export const ADMIN_PATH = '/admin';
export const ADMIN_HOUSEHOLDS_PATH = '/admin/households';
export const ADMIN_ACCOUNTS_PATH = '/admin/accounts';
export const ADMIN_AI_APPS_PATH = '/admin/ai-apps';
export const CONNECT_PATH = '/connect';

export type Route =
  | { kind: 'home' | 'people' | 'members' | 'invite' | 'resetPassword' | 'login' | 'connect' | 'account' | 'admin' | 'adminHouseholds' | 'adminAccounts' | 'adminAiApps' | 'allItems' | 'notFound' }
  | { kind: 'item'; itemId: string }
  | { kind: 'invalidItem' }
  /** The installation-wide administration screens; they belong to the account, not to a household. */
  | { kind: 'adminHousehold'; householdId: string }
  /** One account's AI apps, reached from its row in the account directory. */
  | { kind: 'adminAccountAiApps'; userId: string };

const SIMPLE_ROUTES = new Map<string, Route>([
  [PEOPLE_PATH, { kind: 'people' }],
  [MEMBERS_PATH, { kind: 'members' }],
  [INVITE_PATH, { kind: 'invite' }],
  [RESET_PASSWORD_PATH, { kind: 'resetPassword' }],
  [LOGIN_PATH, { kind: 'login' }],
  [CONNECT_PATH, { kind: 'connect' }],
  [ACCOUNT_PATH, { kind: 'account' }],
  [ADMIN_PATH, { kind: 'admin' }],
  [ADMIN_HOUSEHOLDS_PATH, { kind: 'adminHouseholds' }],
  [ADMIN_ACCOUNTS_PATH, { kind: 'adminAccounts' }],
  [ADMIN_AI_APPS_PATH, { kind: 'adminAiApps' }],
  ['/', { kind: 'home' }],
  ['/items', { kind: 'allItems' }],
]);

/** Identifiers are opaque to the client; the same shape gate as an item keeps a stray address out of a request. */
const UUID = /^[0-9a-f-]{36}$/i;

export function readRoute(path = window.location.pathname): Route {
  const simple = SIMPLE_ROUTES.get(path);
  if (simple) return simple;
  const match = /^\/items\/([^/]+)$/.exec(path);
  if (match) {
    const itemId = match[1];
    return itemId && UUID.test(itemId) ? { kind: 'item', itemId } : { kind: 'invalidItem' };
  }
  if (path.startsWith('/items/')) {
    const segments = path.slice('/items/'.length).split('/');
    return segments.length === 1 ? { kind: 'invalidItem' } : { kind: 'notFound' };
  }
  return readInstallationTarget(path);
}

/**
 * One household's accounts, or one account's AI apps. A malformed identifier and any other address are both simply
 * not a page; there is no partial screen for them.
 */
function readInstallationTarget(path: string): Route {
  if (path.startsWith(`${ADMIN_HOUSEHOLDS_PATH}/`)) {
    const [householdId, ...rest] = path.slice(ADMIN_HOUSEHOLDS_PATH.length + 1).split('/');
    if (rest.length === 0 && householdId && UUID.test(householdId)) return { kind: 'adminHousehold', householdId };
  }
  if (path.startsWith(`${ADMIN_ACCOUNTS_PATH}/`)) {
    const [userId, page, ...rest] = path.slice(ADMIN_ACCOUNTS_PATH.length + 1).split('/');
    if (rest.length === 0 && page === 'ai-apps' && userId && UUID.test(userId)) return { kind: 'adminAccountAiApps', userId };
  }
  return { kind: 'notFound' };
}

/** The address of one account's AI apps. */
export function adminAccountAiAppsPath(userId: string) {
  return `${ADMIN_ACCOUNTS_PATH}/${encodeURIComponent(userId)}/ai-apps`;
}

/** The installation-wide Admin center screens, which an account without any household still reaches. */
const INSTALLATION_KINDS: ReadonlySet<Route['kind']> = new Set([
  'adminHouseholds', 'adminAccounts', 'adminAiApps', 'adminHousehold', 'adminAccountAiApps',
]);

export function householdNavRoute(route: Route, hasHousehold: boolean): HouseholdRoute {
  if (route.kind === 'notFound') return 'home';
  if (route.kind === 'account') return 'account';
  if (route.kind === 'resetPassword' || route.kind === 'connect') return 'home';
  // The installation screens are the Admin center for an account that may hold no household at all.
  if (INSTALLATION_KINDS.has(route.kind)) return 'admin';
  if (!hasHousehold) return 'home';
  if (route.kind === 'allItems') return 'allItems';
  if (route.kind === 'people') return 'people';
  if (route.kind === 'members') return 'members';
  if (route.kind === 'admin') return 'admin';
  if (route.kind === 'item' || route.kind === 'invalidItem') return 'item';
  return 'home';
}
