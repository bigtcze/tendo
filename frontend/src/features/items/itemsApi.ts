import type { components } from '../../../../api/generated/api';
import { api } from '../../lib/api';

export type Item = components['schemas']['Item'];
export type Subject = components['schemas']['Subject'];
export type Completion = components['schemas']['Completion'];
export type Recurrence = components['schemas']['ItemRecurrence'];
export type ItemField = 'title' | 'subjectId' | 'attentionOn' | 'recurrence' | 'notes';
export type Invalid = { kind: 'invalid'; field: ItemField; code: string };
type Failure = { kind: 'unauthenticated' } | { kind: 'error' };
const listPath = '/api/v1/households/{householdId}/items';
const itemPath = '/api/v1/households/{householdId}/items/{itemId}';
const completionPath = '/api/v1/households/{householdId}/items/{itemId}/completions';
const undoPath = '/api/v1/households/{householdId}/items/{itemId}/completions/{completionId}';

export type CompletionPage = { kind: 'ok'; completions: Completion[]; nextCursor: string | null } | Failure | { kind: 'notFound' };
export async function listCompletions(householdId: string, itemId: string, cursor?: string): Promise<CompletionPage> {
  try {
    const { data, response } = await api.GET(completionPath, { params: { path: { householdId, itemId }, query: { limit: 100, ...(cursor ? { cursor } : {}) } } });
    if (data && response.status === 200) return { kind: 'ok', completions: data.items, nextCursor: data.nextCursor };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'notFound' };
  } catch { /* network failure */ }
  return { kind: 'error' };
}

export type ItemPatch = { kind: 'ok'; item: Item } | { kind: 'notFound' | 'changed' } | { kind: 'invalid'; field: string; code: string } | Failure;
export async function patchItem(householdId: string, itemId: string, etag: string, body: components['schemas']['UpdateItemRequest']): Promise<ItemPatch> {
  try {
    const { data, error, response } = await api.PATCH(itemPath, { params: { path: { householdId, itemId }, header: { 'If-Match': etag } }, body });
    if (data && response.status === 200) return { kind: 'ok', item: data };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'notFound' };
    if (response.status === 412) return { kind: 'changed' };
    if (response.status === 422) { const problem = (error ?? {}) as Problem; return { kind: 'invalid', field: problem.field ?? 'unknown', code: problem.code ?? 'unknown' }; }
  } catch { /* network failure */ }
  return { kind: 'error' };
}

export async function changeItem(householdId: string, itemId: string, body: components['schemas']['UpdateItemRequest']): Promise<ItemPatch> {
  const current = await getItem(householdId, itemId);
  if (current.kind === 'unauthenticated' || current.kind === 'error') return current;
  if (current.kind === 'notFound') return { kind: 'notFound' };
  if (current.kind !== 'ok') return { kind: 'error' };
  return patchItem(householdId, itemId, current.etag, body);
}

type Problem = { code?: string; field?: string };
type ConflictCode = 'item_done' | 'item_archived' | 'completion_not_latest' | 'unknown';
export type ItemPage = { kind: 'ok'; items: Item[]; nextCursor: string | null } | { kind: 'noHousehold' } | Failure;
export async function listItems(householdId: string, cursor?: string): Promise<ItemPage> {
  try {
    const { data, response } = await api.GET(listPath, {
      params: { path: { householdId }, query: { limit: 100, archived: false, done: false, ...(cursor ? { cursor } : {}) } },
    });
    if (data && response.status === 200) return { kind: 'ok', items: data.items, nextCursor: data.nextCursor };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'noHousehold' };
  } catch { /* network failure */ }
  return { kind: 'error' };
}

export type SubjectsResult = { kind: 'ok'; subjects: Subject[]; nextCursor: string | null } | { kind: 'unauthenticated' } | { kind: 'error' } | { kind: 'noHousehold' };
export async function listActiveSubjects(householdId: string, archived = false, cursor?: string): Promise<SubjectsResult> {
  try {
    const { data, response } = await api.GET('/api/v1/households/{householdId}/subjects', {
      params: { path: { householdId }, query: { limit: 100, archived, ...(cursor ? { cursor } : {}) } },
    });
    if (data && response.status === 200) return { kind: 'ok', subjects: data.items, nextCursor: data.nextCursor };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'noHousehold' };
  } catch { /* network failure */ }
  return { kind: 'error' };
}

export type CreateResult = { kind: 'ok'; item: Item } | Invalid | Failure;
export async function createItem(householdId: string, body: components['schemas']['CreateItemRequest']): Promise<CreateResult> {
  try {
    const { data, error, response } = await api.POST(listPath, { params: { path: { householdId } }, body });
    if (data && response.status === 201) return { kind: 'ok', item: data };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 422) {
      const problem = (error ?? {}) as Problem;
      const allowed: ItemField[] = ['title', 'subjectId', 'attentionOn', 'recurrence', 'notes'];
      return { kind: 'invalid', field: allowed.includes(problem.field as ItemField) ? problem.field as ItemField : 'title', code: problem.code ?? 'unknown' };
    }
  } catch { /* network failure */ }
  return { kind: 'error' };
}

export type ItemRead = { kind: 'ok'; item: Item; etag: string } | { kind: 'notFound' | 'conflict' | 'changed' } | Failure;
export async function getItem(householdId: string, itemId: string): Promise<ItemRead> {
  try {
    const { data, response } = await api.GET(itemPath, { params: { path: { householdId, itemId } } });
    const etag = response.headers.get('ETag');
    if (data && response.status === 200 && etag) return { kind: 'ok', item: data, etag };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'notFound' };
    if (response.status === 409) return { kind: 'conflict' };
    if (response.status === 412) return { kind: 'changed' };
  } catch { /* network failure */ }
  return { kind: 'error' };
}

export type CompleteResult =
  | { kind: 'ok'; completion: Completion }
  | { kind: 'notFound' }
  | { kind: 'conflict'; code: ConflictCode }
  | { kind: 'changed' }
  | { kind: 'invalid'; code: string }
  | { kind: 'unavailable'; code: string }
  | Failure;
export async function completeItem(householdId: string, itemId: string, etag: string, key: string): Promise<CompleteResult> {
  try {
    const { data, error, response } = await api.POST(completionPath, {
      params: { path: { householdId, itemId }, header: { 'If-Match': etag, 'Idempotency-Key': key } },
      body: {},
    });
    if (data && response.status === 201) return { kind: 'ok', completion: data };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'notFound' };
    if (response.status === 409) {
      const code = (error as Problem | undefined)?.code;
      return { kind: 'conflict', code: code === 'item_done' || code === 'item_archived' ? code : 'unknown' };
    }
    if (response.status === 412) return { kind: 'changed' };
    if (response.status === 422) return { kind: 'invalid', code: (error as Problem | undefined)?.code ?? 'unknown' };
    if (response.status >= 500) return { kind: 'unavailable', code: (error as Problem | undefined)?.code ?? 'unavailable' };
  } catch { /* retry retains the same idempotency key */ }
  return { kind: 'error' };
}

export type UndoResult =
  | { kind: 'ok'; completion: Completion }
  | { kind: 'notFound' }
  | { kind: 'conflict'; code: ConflictCode }
  | { kind: 'changed' }
  | { kind: 'unavailable'; code: string }
  | Failure;
export async function undoCompletion(householdId: string, itemId: string, completionId: string, etag: string): Promise<UndoResult> {
  try {
    const { data, error, response } = await api.PATCH(undoPath, {
      params: { path: { householdId, itemId, completionId }, header: { 'If-Match': etag } },
      body: { undone: true },
    });
    if (data && response.status === 200) return { kind: 'ok', completion: data };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'notFound' };
    if (response.status === 409) {
      const code = (error as Problem | undefined)?.code;
      return { kind: 'conflict', code: code === 'item_archived' || code === 'completion_not_latest' ? code : 'unknown' };
    }
    if (response.status === 412) return { kind: 'changed' };
    if (response.status >= 500) return { kind: 'unavailable', code: (error as Problem | undefined)?.code ?? 'unavailable' };
  } catch { /* keep the Undo action available after a transport failure */ }
  return { kind: 'error' };
}
