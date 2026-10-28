import type { HouseholdRoute } from './HouseholdNavigation';

export const PEOPLE_PATH = '/people';
export const MEMBERS_PATH = '/members';
export const INVITE_PATH = '/invite';
export const LOGIN_PATH = '/login';
export const ACCOUNT_PATH = '/account';

export type Route =
  | { kind: 'home' | 'people' | 'members' | 'invite' | 'login' | 'account' | 'allItems' }
  | { kind: 'item'; itemId: string }
  | { kind: 'invalidItem' };

export function readRoute(path = window.location.pathname): Route {
  if (path === PEOPLE_PATH) return { kind: 'people' };
  if (path === MEMBERS_PATH) return { kind: 'members' };
  if (path === INVITE_PATH) return { kind: 'invite' };
  if (path === LOGIN_PATH) return { kind: 'login' };
  if (path === ACCOUNT_PATH) return { kind: 'account' };
  if (path === '/') return { kind: 'home' };
  if (path === '/items') return { kind: 'allItems' };
  const match = /^\/items\/([^/]+)$/.exec(path);
  if (match) {
    const itemId = match[1];
    return itemId && /^[0-9a-f-]{36}$/i.test(itemId)
      ? { kind: 'item', itemId }
      : { kind: 'invalidItem' };
  }
  return { kind: 'home' };
}

export function householdNavRoute(route: Route, hasHousehold: boolean): HouseholdRoute {
  if (route.kind === 'account') return 'account';
  if (!hasHousehold) return 'home';
  if (route.kind === 'allItems') return 'allItems';
  if (route.kind === 'people') return 'people';
  if (route.kind === 'members') return 'members';
  if (route.kind === 'item' || route.kind === 'invalidItem') return 'item';
  return 'home';
}
