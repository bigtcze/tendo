import { describe, expect, it } from 'vitest';
import { householdNavRoute, readRoute } from './routes';

const householdId = '0198a2f0-0000-7000-8000-000000000002';

describe('readRoute', () => {
  it.each([
    ['/', { kind: 'home' }],
    ['/items', { kind: 'allItems' }],
    ['/items/0198a2f0-0000-7000-8000-000000000002', { kind: 'item', itemId: '0198a2f0-0000-7000-8000-000000000002' }],
    ['/items/0198A2F0-0000-7000-8000-000000000002', { kind: 'item', itemId: '0198A2F0-0000-7000-8000-000000000002' }],
    ['/items/not-a-uuid', { kind: 'invalidItem' }],
    ['/items/a/b', { kind: 'notFound' }],
    ['/people', { kind: 'people' }],
    ['/members', { kind: 'members' }],
    ['/invite', { kind: 'invite' }],
    ['/reset-password', { kind: 'resetPassword' }],
    ['/login', { kind: 'login' }],
    ['/connect', { kind: 'connect' }],
    ['/account', { kind: 'account' }],
    ['/admin', { kind: 'admin' }],
    ['/unknown', { kind: 'notFound' }],
  ] as const)('parses %s', (path, route) => {
    expect(readRoute(path)).toEqual(route);
  });
});

describe('readRoute for the installation Admin center', () => {
  it.each([
    ['/admin/households', { kind: 'adminHouseholds' }],
    [`/admin/households/${householdId}`, { kind: 'adminHousehold', householdId }],
    [`/admin/households/${householdId.toUpperCase()}`, { kind: 'adminHousehold', householdId: householdId.toUpperCase() }],
    // A malformed identifier and a deeper address are pages that do not exist, not partial screens.
    ['/admin/households/not-a-uuid', { kind: 'notFound' }],
    ['/admin/households/', { kind: 'notFound' }],
    [`/admin/households/${householdId}/accounts`, { kind: 'notFound' }],
    ['/admin/household', { kind: 'notFound' }],
  ] as const)('parses %s', (path, route) => {
    expect(readRoute(path)).toEqual(route);
  });
});

describe('readRoute for the installation account list', () => {
  it.each([
    ['/admin/accounts', { kind: 'adminAccounts' }],
    // The list is flat: a deeper address is a page that does not exist, not a detail screen.
    ['/admin/accounts/x', { kind: 'notFound' }],
    ['/admin/accounts/', { kind: 'notFound' }],
    ['/admin/account', { kind: 'notFound' }],
  ] as const)('parses %s', (path, route) => {
    expect(readRoute(path)).toEqual(route);
  });
});

describe('householdNavRoute', () => {
  it.each([
    [{ kind: 'home' }, true, 'home'],
    [{ kind: 'allItems' }, true, 'allItems'],
    [{ kind: 'people' }, true, 'people'],
    [{ kind: 'members' }, true, 'members'],
    [{ kind: 'account' }, true, 'account'],
    [{ kind: 'admin' }, true, 'admin'],
    [{ kind: 'login' }, true, 'home'],
    [{ kind: 'notFound' }, true, 'home'],
    [{ kind: 'invite' }, true, 'home'],
    [{ kind: 'resetPassword' }, true, 'home'],
    [{ kind: 'connect' }, true, 'home'],
    [{ kind: 'item', itemId: 'x' }, true, 'item'],
    [{ kind: 'invalidItem' }, true, 'item'],
    [{ kind: 'allItems' }, false, 'home'],
    [{ kind: 'account' }, false, 'account'],
    [{ kind: 'admin' }, false, 'home'],
  ] as const)('maps route to current navigation page', (route, hasHousehold, expected) => {
    expect(householdNavRoute(route, hasHousehold)).toBe(expected);
  });

  it.each([
    [{ kind: 'adminHouseholds' }, true, 'admin'],
    [{ kind: 'adminHouseholds' }, false, 'admin'],
    [{ kind: 'adminAccounts' }, true, 'admin'],
    [{ kind: 'adminAccounts' }, false, 'admin'],
    [{ kind: 'adminHousehold', householdId }, true, 'admin'],
    [{ kind: 'adminHousehold', householdId }, false, 'admin'],
  ] as const)('marks the single Admin center entry current for %o', (route, hasHousehold, expected) => {
    expect(householdNavRoute(route, hasHousehold)).toBe(expected);
  });
});
