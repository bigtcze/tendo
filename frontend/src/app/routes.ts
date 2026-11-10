import type { HouseholdRoute } from './HouseholdNavigation';

export const PEOPLE_PATH = '/people';
export const MEMBERS_PATH = '/members';
export const INVITE_PATH = '/invite';
export const RESET_PASSWORD_PATH = '/reset-password';
export const LOGIN_PATH = '/login';
export const ACCOUNT_PATH = '/account';
export const ADMIN_PATH = '/admin';

export type Route =
  | { kind: 'home' | 'people' | 'members' | 'invite' | 'resetPassword' | 'login' | 'account' | 'admin' | 'allItems' | 'notFound' }
  | { kind: 'item'; itemId: string }
  | { kind: 'invalidItem' };

const SIMPLE_ROUTES = new Map<string, Route>([
  [PEOPLE_PATH, { kind: 'people' }],
  [MEMBERS_PATH, { kind: 'members' }],
  [INVITE_PATH, { kind: 'invite' }],
  [RESET_PASSWORD_PATH, { kind: 'resetPassword' }],
  [LOGIN_PATH, { kind: 'login' }],
  [ACCOUNT_PATH, { kind: 'account' }],
  [ADMIN_PATH, { kind: 'admin' }],
  ['/', { kind: 'home' }],
  ['/items', { kind: 'allItems' }],
]);

export function readRoute(path = window.location.pathname): Route {
  const simple = SIMPLE_ROUTES.get(path);
  if (simple) return simple;
  const match = /^\/items\/([^/]+)$/.exec(path);
  if (match) {
    const itemId = match[1];
    return itemId && /^[0-9a-f-]{36}$/i.test(itemId)
      ? { kind: 'item', itemId }
      : { kind: 'invalidItem' };
  }
  if (path.startsWith('/items/')) {
    const segments = path.slice('/items/'.length).split('/');
    return segments.length === 1 ? { kind: 'invalidItem' } : { kind: 'notFound' };
  }
  return { kind: 'notFound' };
}

export function householdNavRoute(route: Route, hasHousehold: boolean): HouseholdRoute {
  if (route.kind === 'notFound') return 'home';
  if (route.kind === 'account') return 'account';
  if (route.kind === 'resetPassword') return 'home';
  if (!hasHousehold) return 'home';
  if (route.kind === 'allItems') return 'allItems';
  if (route.kind === 'people') return 'people';
  if (route.kind === 'members') return 'members';
  if (route.kind === 'admin') return 'admin';
  if (route.kind === 'item' || route.kind === 'invalidItem') return 'item';
  return 'home';
}
