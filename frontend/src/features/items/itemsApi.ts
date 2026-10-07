import type { components } from '../../../../api/generated/api';
import { api } from '../../lib/api';

export type Item = components['schemas']['Item'];
export type Subject = components['schemas']['Subject'];
export type Completion = components['schemas']['Completion'];
export type Recurrence = components['schemas']['ItemRecurrence'];
export type ItemField = 'title' | 'subjectId' | 'attentionOn' | 'recurrence';
export type Invalid = { kind: 'invalid'; field: ItemField; code: string };

type Failure = { kind: 'unauthenticated' } | { kind: 'error' };
const listPath = '/api/v1/households/{householdId}/items';
const itemPath = '/api/v1/households/{householdId}/items/{itemId}';
const completionPath = '/api/v1/households/{householdId}/items/{itemId}/completions';
const undoPath = '/api/v1/households/{householdId}/items/{itemId}/completions/{completionId}';

export type ItemPage = { kind: 'ok'; items: Item[]; nextCursor: string | null } | { kind: 'noHousehold' } | Failure;
export async function listItems(householdId: string, cursor?: string): Promise<ItemPage> {
  try {
    const { data, response } = await api.GET(listPath, {
      params: {
        path: { householdId },
        query: { limit: 100, archived: false, done: false, ...(cursor ? { cursor } : {}) },
      },
    });
    if (data && response.status === 200) return { kind: 'ok', items: data.items, nextCursor: data.nextCursor };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'noHousehold' };
  } catch { /* network failure */ }
  return { kind: 'error' };
}

export type SubjectsResult = { kind: 'ok'; subjects: Subject[] } | { kind: 'unauthenticated' } | { kind: 'error' } | { kind: 'noHousehold' };
export async function listActiveSubjects(householdId: string, archived = false): Promise<SubjectsResult> {
  try {
    const { data, response } = await api.GET('/api/v1/households/{householdId}/subjects', {
      params: { path: { householdId }, query: { limit: 100, archived } },
    });
    if (data && response.status === 200) return { kind: 'ok', subjects: data.items };
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
      const value = (error ?? {}) as { field?: string; code?: string };
      const fields: ItemField[] = ['title', 'subjectId', 'attentionOn', 'recurrence'];
      return {
        kind: 'invalid',
        field: fields.includes(value.field as ItemField) ? value.field as ItemField : 'title',
        code: value.code ?? 'unknown',
      };
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

export type CompleteResult = { kind: 'ok'; completion: Completion } | { kind: 'notFound' | 'conflict' | 'changed' } | Failure;
export async function completeItem(householdId: string, itemId: string, etag: string, key: string): Promise<CompleteResult> {
  try {
    const { data, response } = await api.POST(completionPath, {
      params: { path: { householdId, itemId }, header: { 'If-Match': etag, 'Idempotency-Key': key } },
      body: {},
    });
    if (data && response.status === 201) return { kind: 'ok', completion: data };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'notFound' };
    if (response.status === 409) return { kind: 'conflict' };
    if (response.status === 412) return { kind: 'changed' };
  } catch { /* retry retains the same idempotency key */ }
  return { kind: 'error' };
}

export type UndoResult = { kind: 'ok'; completion: Completion } | { kind: 'notFound' | 'conflict' | 'changed' } | Failure;
export async function undoCompletion(householdId: string, itemId: string, completionId: string, etag: string): Promise<UndoResult> {
  try {
    const { data, response } = await api.PATCH(undoPath, {
      params: { path: { householdId, itemId, completionId }, header: { 'If-Match': etag } },
      body: { undone: true },
    });
    if (data && response.status === 200) return { kind: 'ok', completion: data };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'notFound' };
    if (response.status === 409) return { kind: 'conflict' };
    if (response.status === 412) return { kind: 'changed' };
  } catch { /* network failure */ }
  return { kind: 'error' };
}
