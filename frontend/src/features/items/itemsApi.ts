import type { components, operations } from '../../../../api/generated/api';
import { api } from '../../lib/api';

export type Item = components['schemas']['Item'];
export type Subject = components['schemas']['Subject'];
export type Completion = components['schemas']['Completion'];
export type Recurrence = components['schemas']['ItemRecurrence'];
export type ItemField = 'title' | 'subjectId' | 'attentionOn' | 'recurrence' | 'notes' | 'historicalCompletedOn' | 'responsibleUserId';
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

type Problem = { code?: string; field?: string; parameter?: string };
type ConflictCode = 'item_done' | 'item_archived' | 'completion_not_latest' | 'unknown';
export type ItemSummary = components['schemas']['ItemSummary'];
export type ItemGroup = 'needs' | 'inProgress' | 'waiting' | 'upcoming' | 'paused';
export type ItemGroupPage = { kind: 'ok'; items: Item[]; nextCursor: string | null } | { kind: 'stale'; parameter?: string } | Failure | { kind: 'noHousehold' };
export type AllItemsFilters = { q?: string; subjectId?: string; responsibleUserId?: string; recurring?: boolean };
export type AllItemsStatus = 'active' | 'needs' | 'inProgress' | 'waiting' | 'upcoming' | 'paused' | 'done' | 'archived';
export type FilteredSummaryResult = { kind: 'ok'; summary: ItemSummary } | { kind: 'unauthenticated' | 'error' } | { kind: 'noHousehold' };
export type AllItemsPageResult = { kind: 'ok'; items: Item[]; nextCursor: string | null } | { kind: 'stale'; parameter?: string } | { kind: 'invalidQuery' } | { kind: 'unauthenticated' | 'error' } | { kind: 'noHousehold' };
export async function getFilteredItemsSummary(householdId: string, filters: AllItemsFilters): Promise<FilteredSummaryResult> {
  try {
    const { data, response } = await api.GET(summaryPath, { params: { path: { householdId }, query: { ...(filters.q ? { q: filters.q } : {}), ...(filters.subjectId ? { subjectId: filters.subjectId } : {}), ...(filters.responsibleUserId ? { responsibleUserId: filters.responsibleUserId } : {}), ...(filters.recurring !== undefined ? { recurring: filters.recurring ? 'true' : 'false' } : {}) } } });
    if (data && response.status === 200) return { kind: 'ok' as const, summary: data };
    if (response.status === 401) return { kind: 'unauthenticated' as const };
    if (response.status === 404) return { kind: 'noHousehold' as const };
  } catch { /* network failure */ }
  return { kind: 'error' as const };
}
export async function listAllItems(householdId: string, status: AllItemsStatus, filters: AllItemsFilters, attentionDate: string, cursor?: string): Promise<AllItemsPageResult> {
  const common = {
    sort: 'attention_on' as const,
    attentionDate,
    ...(filters.q ? { q: filters.q } : {}),
    ...(filters.subjectId ? { subjectId: filters.subjectId } : {}),
    ...(filters.responsibleUserId ? { responsibleUserId: filters.responsibleUserId } : {}),
    ...(filters.recurring !== undefined ? { recurring: filters.recurring ? 'true' as const : 'false' as const } : {}),
  };

  async function fetchStream(query: operations['listItems']['parameters']['query']) {
    const { data, error, response } = await api.GET(listPath, { params: { path: { householdId }, query } });
    if (data && response.status === 200) return { kind: 'ok' as const, items: data.items, nextCursor: data.nextCursor };
    if (response.status === 401) return { kind: 'unauthenticated' as const };
    if (response.status === 404) return { kind: 'noHousehold' as const };
    if (response.status === 400 && ['attentionDate', 'cursor'].includes(((error ?? {}) as Problem).parameter ?? '')) return { kind: 'stale' as const, parameter: ((error ?? {}) as Problem).parameter };
    if (response.status === 400 && ((error ?? {}) as Problem).parameter === 'q') return { kind: 'invalidQuery' as const };
    return { kind: 'error' as const };
  }

  try {
    if (status === 'archived') {
      // UI cursor wraps two server streams: 'a:<c>' continues unfinished archived items,
      // 'b:' starts completed archived items and 'b:<c>' continues them. Anything else is stale.
      const token = cursor ?? '';
      const archivedDone = token.startsWith('b:');
      if (token && !archivedDone && !(token.startsWith('a:') && token.length > 2)) return { kind: 'stale', parameter: 'cursor' };
      const serverCursor = token.slice(2);
      const first = await fetchStream({ limit: 25, ...common, archived: true, done: archivedDone, ...(serverCursor ? { cursor: serverCursor } : {}) });
      if (first.kind !== 'ok') return first;
      if (archivedDone) return { kind: 'ok', items: first.items, nextCursor: first.nextCursor ? `b:${first.nextCursor}` : null };
      if (first.nextCursor) return { kind: 'ok', items: first.items, nextCursor: `a:${first.nextCursor}` };
      if (first.items.length === 25) {
        // Only offer the boundary page when completed archived items actually exist.
        const probe = await fetchStream({ limit: 1, ...common, archived: true, done: true });
        if (probe.kind !== 'ok') return probe;
        return { kind: 'ok', items: first.items, nextCursor: probe.items.length ? 'b:' : null };
      }
      const second = await fetchStream({ limit: 25 - first.items.length, ...common, archived: true, done: true });
      if (second.kind !== 'ok') return second;
      return { kind: 'ok', items: [...first.items, ...second.items], nextCursor: second.nextCursor ? `b:${second.nextCursor}` : null };
    }

    const query = {
      limit: 25 as const,
      ...common,
      ...(status === 'active' ? { archived: false as const, done: false as const } : {}),
      ...(status === 'needs' ? { workflowState: 'open' as const, attention: 'needs_attention' as const } : {}),
      ...(status === 'inProgress' ? { workflowState: 'in_progress' as const } : {}),
      ...(status === 'waiting' ? { workflowState: 'waiting' as const } : {}),
      ...(status === 'upcoming' ? { workflowState: 'open' as const, attention: 'upcoming' as const } : {}),
      ...(status === 'paused' ? { workflowState: 'paused' as const } : {}),
      ...(status === 'done' ? { done: true as const, archived: false as const } : {}),
      ...(cursor ? { cursor } : {}),
    };
    return await fetchStream(query);
  } catch { /* network failure */ }
  return { kind: 'error' };
}
const summaryPath = '/api/v1/households/{householdId}/items/summary';
export async function getItemsSummary(householdId: string): Promise<{ kind: 'ok'; summary: ItemSummary } | { kind: 'noHousehold' } | Failure> {
  try {
    const { data, response } = await api.GET(summaryPath, { params: { path: { householdId } } });
    if (data && response.status === 200) return { kind: 'ok', summary: data };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'noHousehold' };
  } catch { /* network failure */ }
  return { kind: 'error' };
}
export async function listItemGroup(householdId: string, group: ItemGroup, attentionDate: string, cursor?: string): Promise<ItemGroupPage> {
  const query = {
    limit: 25 as const, sort: 'attention_on' as const, attentionDate,
    ...(group === 'needs' ? { workflowState: 'open' as const, attention: 'needs_attention' as const } : {}),
    ...(group === 'inProgress' ? { workflowState: 'in_progress' as const } : {}),
    ...(group === 'waiting' ? { workflowState: 'waiting' as const } : {}),
    ...(group === 'upcoming' ? { workflowState: 'open' as const, attention: 'upcoming' as const } : {}),
    ...(group === 'paused' ? { workflowState: 'paused' as const } : {}),
    ...(cursor ? { cursor } : {}),
  };
  try {
    const { data, error, response } = await api.GET(listPath, { params: { path: { householdId }, query } });
    if (data && response.status === 200) return { kind: 'ok', items: data.items, nextCursor: data.nextCursor };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'noHousehold' };
    if (response.status === 400 && ['attentionDate', 'cursor'].includes(((error ?? {}) as Problem).parameter ?? '')) return { kind: 'stale' };
  } catch { /* network failure */ }
  return { kind: 'error' };
}

export type SubjectsResult = { kind: 'ok'; subjects: Subject[]; nextCursor: string | null } | { kind: 'unauthenticated' } | { kind: 'error' } | { kind: 'noHousehold' };
export async function listSubjectsByIDs(householdId: string, ids: string[], archived: boolean): Promise<SubjectsResult> {
  try {
    const { data, response } = await api.GET('/api/v1/households/{householdId}/subjects', {
      params: { path: { householdId }, query: { limit: 100, archived, id: ids } },
    });
    if (data && response.status === 200) return { kind: 'ok', subjects: data.items, nextCursor: data.nextCursor };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'noHousehold' };
  } catch { /* network failure */ }
  return { kind: 'error' };
}

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

export type CreateResult = { kind: 'ok'; item: Item } | Invalid | { kind: 'reused' } | { kind: 'definitive' } | Failure;
export async function createItem(householdId: string, body: components['schemas']['CreateItemRequest'], idempotencyKey: string): Promise<CreateResult> {
  try {
    const { data, error, response } = await api.POST(listPath, { params: { path: { householdId }, header: { 'Idempotency-Key': idempotencyKey } }, body });
    if (data && response.status === 201) return { kind: 'ok', item: data };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 422) {
      if ((error as Problem | undefined)?.code === 'idempotency_key_reused') return { kind: 'reused' };
      const problem = (error ?? {}) as Problem;
      const allowed: ItemField[] = ['title', 'subjectId', 'attentionOn', 'recurrence', 'notes', 'historicalCompletedOn', 'responsibleUserId'];
      return { kind: 'invalid', field: allowed.includes(problem.field as ItemField) ? problem.field as ItemField : 'title', code: problem.code ?? 'unknown' };
    }
    if (response.status >= 400 && response.status < 500) return { kind: 'definitive' };
  } catch { /* keep stored attempt after network failure */ }
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
