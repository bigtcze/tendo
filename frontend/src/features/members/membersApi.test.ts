import { beforeEach, describe, expect, it, vi, type Mock } from 'vitest';
import { api } from '../../lib/api';
import { acceptInvitationAsCurrentUser, createInvitation, listInvitations, listMembers, revokeInvitation } from './membersApi';

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

describe('acceptInvitationAsCurrentUser', () => {
  const problem = (status: number, code: string) => ({
    data: undefined,
    error: { type: 'about:blank', title: 'Problem', status, code },
    response: { status },
  });

  it.each([
    [201, 'ok'],
    [404, 'invalid'],
    [401, 'unauthenticated'],
    [429, 'rateLimited'],
  ] as const)('maps status %i to %s', async (status, expected) => {
    post.mockResolvedValue({ data: status === 201 ? {} : undefined, error: undefined, response: { status } });
    expect(await acceptInvitationAsCurrentUser('token')).toBe(expected);
  });

  it('keeps already_member as the only invitation conflict', async () => {
    post.mockResolvedValue(problem(409, 'already_member'));
    expect(await acceptInvitationAsCurrentUser('token')).toBe('alreadyMember');
  });

  // The server no longer emits the retired conflict code; a stray one must not revive the removed "sign out" path.
  it('treats a stray household_conflict as the generic error', async () => {
    post.mockResolvedValue(problem(409, 'household_conflict'));
    expect(await acceptInvitationAsCurrentUser('token')).toBe('error');
  });

  it('treats an unrecognised 409 as the generic error', async () => {
    post.mockResolvedValue(problem(409, 'cannot_reset_self'));
    expect(await acceptInvitationAsCurrentUser('token')).toBe('error');
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
