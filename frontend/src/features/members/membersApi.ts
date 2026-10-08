import type { components } from '../../../../api/generated/api';
import { api } from '../../lib/api';

export type Member = components['schemas']['Member'];
export type Invitation = components['schemas']['Invitation'];
export type InvitationCreated = components['schemas']['InvitationCreated'];
const PAGE_SIZE = 50;
const membersPath = '/api/v1/households/{householdId}/members';
const invitationsPath = '/api/v1/households/{householdId}/invitations';

export type PageResult<T> = { kind: 'ok'; items: T[]; nextCursor: string | null } | { kind: 'unauthenticated' | 'forbidden' | 'rejected' | 'notFound' | 'error' };

function problemCode(error: unknown): string | undefined { return (error as { code?: string } | undefined)?.code; }

export async function listMembers(householdId: string, cursor?: string): Promise<PageResult<Member>> {
  try {
    const { data, error, response } = await api.GET(membersPath, { params: { path: { householdId }, query: { limit: PAGE_SIZE, ...(cursor ? { cursor } : {}) } } });
    if (data && response.status === 200) return { kind: 'ok', items: data.items, nextCursor: data.nextCursor };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 403) return problemCode(error) === 'owner_required' ? { kind: 'forbidden' } : { kind: 'rejected' };
    if (response.status === 404) return { kind: 'notFound' };
  } catch { /* show the common unavailable state */ }
  return { kind: 'error' };
}

export async function listInvitations(householdId: string, cursor?: string): Promise<PageResult<Invitation>> {
  try {
    const { data, error, response } = await api.GET(invitationsPath, { params: { path: { householdId }, query: { limit: PAGE_SIZE, ...(cursor ? { cursor } : {}) } } });
    if (data && response.status === 200) return { kind: 'ok', items: data.items, nextCursor: data.nextCursor };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 403) return problemCode(error) === 'owner_required' ? { kind: 'forbidden' } : { kind: 'rejected' };
    if (response.status === 404) return { kind: 'notFound' };
  } catch { /* show the common unavailable state */ }
  return { kind: 'error' };
}

export type CreateInvitationResult = { kind: 'created'; invitation: InvitationCreated } | { kind: 'existing'; invitation: Invitation } | { kind: 'unauthenticated' | 'forbidden' | 'rejected' | 'notFound' | 'error' };
export async function createInvitation(householdId: string, idempotencyKey: string): Promise<CreateInvitationResult> {
  try {
    const { data, error, response } = await api.POST(invitationsPath, {
      params: { path: { householdId }, header: { 'Idempotency-Key': idempotencyKey } }, body: {},
    });
    if (response.status === 201 && data && 'token' in data) return { kind: 'created', invitation: data };
    if (response.status === 200 && data) return { kind: 'existing', invitation: data };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 403) return problemCode(error) === 'owner_required' ? { kind: 'forbidden' } : { kind: 'rejected' };
    if (response.status === 404) return { kind: 'notFound' };
  } catch { /* key can be replayed to recover an uncertain outcome */ }
  return { kind: 'error' };
}

export async function revokeInvitation(householdId: string, invitationId: string): Promise<'ok' | 'unauthenticated' | 'forbidden' | 'rejected' | 'notFound' | 'error'> {
  try {
    const { error, response } = await api.DELETE('/api/v1/households/{householdId}/invitations/{invitationId}', { params: { path: { householdId, invitationId } } });
    if (response.status === 204) return 'ok';
    if (response.status === 401) return 'unauthenticated';
    if (response.status === 403) return problemCode(error) === 'owner_required' ? 'forbidden' : 'rejected';
    if (response.status === 404) return 'notFound';
  } catch { /* fall through */ }
  return 'error';
}

export type AcceptResult = 'ok' | 'invalid' | 'alreadyMember' | 'householdConflict' | 'unauthenticated' | 'rateLimited' | 'error';
function conflictCode(error: unknown): string | undefined { return (error as { code?: string } | undefined)?.code; }
export async function acceptInvitationAsCurrentUser(token: string): Promise<AcceptResult> {
  try {
    const { error, response } = await api.POST('/api/v1/invitations/accept', { body: { token } });
    if (response.status === 201) return 'ok';
    if (response.status === 404) return 'invalid';
    if (response.status === 401) return 'unauthenticated';
    if (response.status === 429) return 'rateLimited';
    if (response.status === 409) {
      const code = conflictCode(error);
      if (code === 'already_member') return 'alreadyMember';
      if (code === 'household_conflict') return 'householdConflict';
    }
  } catch { /* fall through */ }
  return 'error';
}

export async function acceptNewAccount(token: string, login: string, password: string): Promise<{ kind: 'ok' | 'invalid' | 'loginTaken' | 'invalidField' | 'rateLimited' | 'unavailable' | 'error'; field?: 'login' | 'password' }> {
  try {
    const { data, error, response } = await api.POST('/api/v1/auth/invitations/accept', { body: { token, login, password } });
    if (data && response.status === 201) return { kind: 'ok' };
    if (response.status === 404) return { kind: 'invalid' };
    if (response.status === 409 && conflictCode(error) === 'login_unavailable') return { kind: 'loginTaken' };
    if (response.status === 422) {
      const field = (error as { field?: unknown } | undefined)?.field;
      if (field === 'login' || field === 'password') return { kind: 'invalidField', field };
      return { kind: 'error' };
    }
    if (response.status === 429) return { kind: 'rateLimited' };
    return { kind: 'unavailable' };
  } catch { return { kind: 'unavailable' }; }
}
