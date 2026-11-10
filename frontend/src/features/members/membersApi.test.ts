import { beforeEach, describe, expect, it, vi, type Mock } from 'vitest';
import { api } from '../../lib/api';
import { createInvitation, listInvitations, listMembers, revokeInvitation } from './membersApi';

vi.mock('../../lib/api', () => ({ api: { GET: vi.fn(), POST: vi.fn(), DELETE: vi.fn() } }));

const get = api.GET as unknown as Mock;
const post = api.POST as unknown as Mock;
const del = api.DELETE as unknown as Mock;

function forbidden(code?: string) {
  return {
    data: undefined,
    error: { type: 'about:blank', title: 'Forbidden', status: 403, ...(code ? { code } : {}) },
    response: { status: 403 },
  };
}

beforeEach(() => { get.mockReset(); post.mockReset(); del.mockReset(); });

// Invitation routes still answer the legacy `owner_required`; admin-only routes answer `admin_required`.
describe.each([
  ['owner_required'],
  ['admin_required'],
])('403 %s', (code) => {
  it('listMembers maps to forbidden', async () => {
    get.mockResolvedValue(forbidden(code));
    expect(await listMembers('household')).toEqual({ kind: 'forbidden' });
  });

  it('listInvitations maps to forbidden', async () => {
    get.mockResolvedValue(forbidden(code));
    expect(await listInvitations('household')).toEqual({ kind: 'forbidden' });
  });

  it('createInvitation maps to forbidden', async () => {
    post.mockResolvedValue(forbidden(code));
    expect(await createInvitation('household', 'key')).toEqual({ kind: 'forbidden' });
  });

  it('revokeInvitation maps to forbidden', async () => {
    del.mockResolvedValue(forbidden(code));
    expect(await revokeInvitation('household', 'invitation')).toBe('forbidden');
  });
});

describe('403 without a recognised admin code', () => {
  it('listMembers maps to rejected', async () => {
    get.mockResolvedValue(forbidden('forbidden'));
    expect(await listMembers('household')).toEqual({ kind: 'rejected' });
  });

  it('listInvitations maps to rejected', async () => {
    get.mockResolvedValue(forbidden());
    expect(await listInvitations('household')).toEqual({ kind: 'rejected' });
  });

  it('createInvitation maps to rejected', async () => {
    post.mockResolvedValue(forbidden('forbidden_origin'));
    expect(await createInvitation('household', 'key')).toEqual({ kind: 'rejected' });
  });

  it('revokeInvitation maps to rejected', async () => {
    del.mockResolvedValue(forbidden());
    expect(await revokeInvitation('household', 'invitation')).toBe('rejected');
  });
});

describe('listInvitations query', () => {
  it('asks the server to hide expired invitations on every page', async () => {
    get.mockResolvedValue({ data: { items: [], nextCursor: null }, response: { status: 200 } });
    await listInvitations('household');
    await listInvitations('household', 'next-page');
    expect(get.mock.calls.map(([, init]) => (init as { params: { query: unknown } }).params.query)).toEqual([
      { limit: 50, includeExpired: false },
      { limit: 50, includeExpired: false, cursor: 'next-page' },
    ]);
  });
});
