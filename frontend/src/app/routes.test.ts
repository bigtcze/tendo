import { describe, expect, it } from 'vitest';
import { householdNavRoute, readRoute } from './routes';

describe('readRoute', () => {
  it.each([
    ['/', { kind: 'home' }],
    ['/items', { kind: 'allItems' }],
    ['/items/0198a2f0-0000-7000-8000-000000000002', { kind: 'item', itemId: '0198a2f0-0000-7000-8000-000000000002' }],
    ['/items/0198A2F0-0000-7000-8000-000000000002', { kind: 'item', itemId: '0198A2F0-0000-7000-8000-000000000002' }],
    ['/items/not-a-uuid', { kind: 'invalidItem' }],
    ['/items/a/b', { kind: 'home' }],
    ['/people', { kind: 'people' }],
    ['/members', { kind: 'members' }],
    ['/invite', { kind: 'invite' }],
    ['/login', { kind: 'login' }],
    ['/account', { kind: 'account' }],
    ['/unknown', { kind: 'home' }],
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
    [{ kind: 'login' }, true, 'home'],
    [{ kind: 'invite' }, true, 'home'],
    [{ kind: 'item', itemId: 'x' }, true, 'item'],
    [{ kind: 'invalidItem' }, true, 'item'],
    [{ kind: 'allItems' }, false, 'home'],
    [{ kind: 'account' }, false, 'account'],
  ] as const)('maps route to current navigation page', (route, hasHousehold, expected) => {
    expect(householdNavRoute(route, hasHousehold)).toBe(expected);
  });
});
