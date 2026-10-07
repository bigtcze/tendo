import type { components } from '../../../../api/generated/api';
import { api } from '../../lib/api';

export type Subject = components['schemas']['Subject'];
export type SubjectType = components['schemas']['SubjectType'];
export type SubjectField = components['schemas']['SubjectValidationProblem']['field'];
export type SubjectCode = components['schemas']['SubjectValidationProblem']['code'];
export type SubjectChanges = components['schemas']['UpdateSubjectRequest'];

export const subjectTypes: readonly SubjectType[] = ['person', 'home', 'vehicle', 'pet', 'custom'];

export const PAGE_SIZE = 50;

const listPath = '/api/v1/households/{householdId}/subjects';
const itemPath = '/api/v1/households/{householdId}/subjects/{subjectId}';

type Failure = { kind: 'unauthenticated' } | { kind: 'error' };
/** `unknown` is a 422 this client does not recognise; it still reads as a problem with the name. */
export type InvalidCode = SubjectCode | 'unknown';
export type Invalid = { kind: 'invalid'; field: SubjectField; code: InvalidCode };

const fields: readonly string[] = ['name', 'type'];
const codes: readonly string[] = ['invalid_characters', 'invalid_length', 'invalid_type'];

function invalidFrom(error: unknown): Invalid {
  const { field, code } = (error ?? {}) as { field?: unknown; code?: unknown };
  return {
    kind: 'invalid',
    field: typeof field === 'string' && fields.includes(field) ? (field as SubjectField) : 'name',
    code: typeof code === 'string' && codes.includes(code) ? (code as SubjectCode) : 'unknown',
  };
}

export type ListResult =
  | { kind: 'ok'; items: Subject[]; nextCursor: string | null }
  | { kind: 'noHousehold' }
  | Failure;

export async function listSubjects(
  householdId: string,
  options: { archived: boolean; cursor?: string },
): Promise<ListResult> {
  try {
    const { data, response } = await api.GET(listPath, {
      params: {
        path: { householdId },
        query: {
          limit: PAGE_SIZE,
          archived: options.archived,
          ...(options.cursor ? { cursor: options.cursor } : {}),
        },
      },
    });
    if (data && response.status === 200) return { kind: 'ok', items: data.items, nextCursor: data.nextCursor };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'noHousehold' };
  } catch {
    // fall through
  }
  return { kind: 'error' };
}

export type CreateResult = { kind: 'ok'; subject: Subject } | Invalid | Failure;

export async function createSubject(
  householdId: string,
  body: { name: string; type: SubjectType },
): Promise<CreateResult> {
  try {
    const { data, error, response } = await api.POST(listPath, { params: { path: { householdId } }, body });
    if (data && response.status === 201) return { kind: 'ok', subject: data };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 422) return invalidFrom(error);
  } catch {
    // fall through
  }
  return { kind: 'error' };
}

export type GetResult = { kind: 'ok'; subject: Subject; etag: string } | { kind: 'notFound' } | Failure;

/** List items carry no version, so a subject is read first to learn its ETag. */
export async function getSubject(householdId: string, subjectId: string): Promise<GetResult> {
  try {
    const { data, response } = await api.GET(itemPath, { params: { path: { householdId, subjectId } } });
    const etag = response.headers.get('ETag');
    if (data && response.status === 200 && etag) return { kind: 'ok', subject: data, etag };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'notFound' };
  } catch {
    // fall through
  }
  return { kind: 'error' };
}

export type PatchResult =
  | { kind: 'ok'; subject: Subject }
  | { kind: 'conflict' }
  | { kind: 'notFound' }
  | Invalid
  | Failure;

export async function patchSubject(
  householdId: string,
  subjectId: string,
  etag: string,
  body: SubjectChanges,
): Promise<PatchResult> {
  try {
    const { data, error, response } = await api.PATCH(itemPath, {
      params: { path: { householdId, subjectId }, header: { 'If-Match': etag } },
      body,
    });
    if (data && response.status === 200) return { kind: 'ok', subject: data };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'notFound' };
    if (response.status === 412) return { kind: 'conflict' };
    if (response.status === 422) return invalidFrom(error);
  } catch {
    // fall through
  }
  return { kind: 'error' };
}

/** Archive or restore: read the current version, then change it. */
export async function setArchived(
  householdId: string,
  subjectId: string,
  archived: boolean,
): Promise<PatchResult> {
  const current = await getSubject(householdId, subjectId);
  if (current.kind !== 'ok') return current;
  if (current.subject.archived === archived) return { kind: 'ok', subject: current.subject };
  return patchSubject(householdId, subjectId, current.etag, { archived });
}
