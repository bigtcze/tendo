import type { components, operations } from '../../../../api/generated/api';
import { api } from '../../lib/api';
import { archivedPaging } from './archivedPaging';

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

// Item writes that are still in flight. List and summary reads wait for them, so a list opened while a
// write from another screen is pending (e.g. Archive, then Back before the response) loads post-write data:
// navigation unmounts the writing screen, so nothing else would refresh that list later. Callers that read a
// fresh ETag before writing track the read and write together. The wait is bounded per read (fail-open): a
// write slower than the bound can still leave a list stale until it is reloaded.
const pendingItemWrites = new Set<Promise<unknown>>();
export function trackItemWrite<T>(write: Promise<T>): Promise<T> {
  pendingItemWrites.add(write);
  const done = () => { pendingItemWrites.delete(write); };
  write.then(done, done);
  return write;
}
// Bounded so a hung write (fetch has no timeout) delays a list at most this long instead of blocking it.
export const itemWriteWaitLimitMs = 10_000;
async function afterItemWrites(): Promise<void> {
  if (!pendingItemWrites.size) return;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const limit = new Promise<void>((resolve) => { timer = setTimeout(resolve, itemWriteWaitLimitMs); });
  const settled = (async () => { while (pendingItemWrites.size) await Promise.allSettled([...pendingItemWrites]); })();
  await Promise.race([settled, limit]);
  clearTimeout(timer);
}

export type CompletionPage = { kind: 'ok'; completions: Completion[]; nextCursor: string | null } | Failure | { kind: 'notFound' };
export async function listCompletions(
  householdId: string,
  itemId: string,
  cursor?: string,
  options: { order?: 'oldest' | 'newest'; limit?: number } = {},
): Promise<CompletionPage> {
  try {
    const { data, response } = await api.GET(completionPath, {
      params: {
        path: { householdId, itemId },
        query: {
          limit: options.limit ?? 100,
          ...(options.order ? { order: options.order } : {}),
          ...(cursor ? { cursor } : {}),
        },
      },
    });
    if (data && response.status === 200) {
      return { kind: 'ok', completions: data.items, nextCursor: data.nextCursor };
    }
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'notFound' };
  } catch { /* network failure */ }
  return { kind: 'error' };
}

export type ItemPatch = { kind: 'ok'; item: Item } | { kind: 'notFound' | 'changed' } | { kind: 'invalid'; field: string; code: string } | Failure;
export function patchItem(householdId: string, itemId: string, etag: string, body: components['schemas']['UpdateItemRequest']): Promise<ItemPatch> {
  return trackItemWrite(sendPatchItem(householdId, itemId, etag, body));
}
async function sendPatchItem(householdId: string, itemId: string, etag: string, body: components['schemas']['UpdateItemRequest']): Promise<ItemPatch> {
  try {
    const { data, error, response } = await api.PATCH(itemPath, { params: { path: { householdId, itemId }, header: { 'If-Match': etag } }, body });
    if (data && response.status === 200) {
      return { kind: 'ok', item: data };
    }
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'notFound' };
    if (response.status === 412) return { kind: 'changed' };
    if (response.status === 422) {
      const problem = (error ?? {}) as Problem;
      return { kind: 'invalid', field: problem.field ?? 'unknown', code: problem.code ?? 'unknown' };
    }
  } catch { /* network failure */ }
  return { kind: 'error' };
}

export function changeItem(householdId: string, itemId: string, body: components['schemas']['UpdateItemRequest']): Promise<ItemPatch> {
  // Tracked from the ETag read onward, so a list opened between the read and the PATCH also waits.
  return trackItemWrite(readThenPatchItem(householdId, itemId, body));
}
async function readThenPatchItem(householdId: string, itemId: string, body: components['schemas']['UpdateItemRequest']): Promise<ItemPatch> {
  const current = await getItem(householdId, itemId);
  if (current.kind === 'unauthenticated' || current.kind === 'error') return current;
  if (current.kind === 'notFound') return { kind: 'notFound' };
  if (current.kind !== 'ok') return { kind: 'error' };
  return patchItem(householdId, itemId, current.etag, body);
}

type Problem = { code?: string; field?: string; parameter?: string };
type ConflictCode = 'item_done' | 'item_archived' | 'completion_not_latest' | 'unknown';
function completeConflictCode(code: string | undefined): ConflictCode {
  return code === 'item_done' || code === 'item_archived' ? code : 'unknown';
}
function undoConflictCode(code: string | undefined): ConflictCode {
  return code === 'item_archived' || code === 'completion_not_latest' ? code : 'unknown';
}
export type ItemSummary = components['schemas']['ItemSummary'];
export type ItemGroup = 'needs' | 'inProgress' | 'waiting' | 'upcoming' | 'paused';
export type ItemGroupPage =
  | { kind: 'ok'; items: Item[]; nextCursor: string | null }
  | { kind: 'stale'; parameter?: string }
  | Failure
  | { kind: 'noHousehold' };
export type AllItemsFilters = { q?: string; subjectId?: string; responsibleUserId?: string; recurring?: boolean };
export type AllItemsStatus = 'active' | 'needs' | 'inProgress' | 'waiting' | 'upcoming' | 'paused' | 'done' | 'archived';
export type FilteredSummaryResult = { kind: 'ok'; summary: ItemSummary } | { kind: 'unauthenticated' | 'error' } | { kind: 'noHousehold' };
export type AllItemsPageResult =
  | { kind: 'ok'; items: Item[]; nextCursor: string | null }
  | { kind: 'stale'; parameter?: string }
  | { kind: 'invalidQuery' }
  | { kind: 'unauthenticated' | 'error' }
  | { kind: 'noHousehold' };
function filterQuery(filters: AllItemsFilters) {
  return {
    ...(filters.q ? { q: filters.q } : {}),
    ...(filters.subjectId ? { subjectId: filters.subjectId } : {}),
    ...(filters.responsibleUserId ? { responsibleUserId: filters.responsibleUserId } : {}),
    ...(filters.recurring !== undefined ? { recurring: filters.recurring ? 'true' as const : 'false' as const } : {}),
  };
}

function groupQuery(
  status: ItemGroup | Exclude<AllItemsStatus, 'archived'>,
  attentionDate: string,
  filters: AllItemsFilters,
  cursor?: string,
) {
  return {
    limit: 25 as const,
    sort: 'attention_on' as const,
    attentionDate,
    ...filterQuery(filters),
    ...(status === 'active' ? { archived: false as const, done: false as const } : {}),
    ...(status === 'needs' ? { workflowState: 'open' as const, attention: 'needs_attention' as const } : {}),
    ...(status === 'inProgress' ? { workflowState: 'in_progress' as const } : {}),
    ...(status === 'waiting' ? { workflowState: 'waiting' as const } : {}),
    ...(status === 'upcoming' ? { workflowState: 'open' as const, attention: 'upcoming' as const } : {}),
    ...(status === 'paused' ? { workflowState: 'paused' as const } : {}),
    ...(status === 'done' ? { done: true as const, archived: false as const } : {}),
    ...(cursor ? { cursor } : {}),
  };
}

export async function getFilteredItemsSummary(householdId: string, filters: AllItemsFilters): Promise<FilteredSummaryResult> {
  await afterItemWrites();
  try {
    const { data, response } = await api.GET(summaryPath, { params: { path: { householdId }, query: filterQuery(filters) } });
    if (data && response.status === 200) return { kind: 'ok' as const, summary: data };
    if (response.status === 401) return { kind: 'unauthenticated' as const };
    if (response.status === 404) return { kind: 'noHousehold' as const };
  } catch { /* network failure */ }
  return { kind: 'error' as const };
}
export async function listAllItems(
  householdId: string,
  status: AllItemsStatus,
  filters: AllItemsFilters,
  attentionDate: string,
  cursor?: string,
): Promise<AllItemsPageResult> {
  await afterItemWrites();
  const common = { sort: 'attention_on' as const, attentionDate, ...filterQuery(filters) };

  async function fetchStream(query: operations['listItems']['parameters']['query']) {
    const { data, error, response } = await api.GET(listPath, { params: { path: { householdId }, query } });
    if (data && response.status === 200) return { kind: 'ok' as const, items: data.items, nextCursor: data.nextCursor };
    if (response.status === 401) return { kind: 'unauthenticated' as const };
    if (response.status === 404) return { kind: 'noHousehold' as const };
    const parameter = ((error ?? {}) as Problem).parameter;
    if (response.status === 400 && ['attentionDate', 'cursor'].includes(parameter ?? '')) {
      return { kind: 'stale' as const, parameter };
    }
    if (response.status === 400 && parameter === 'q') return { kind: 'invalidQuery' as const };
    return { kind: 'error' as const };
  }

  try {
    if (status === 'archived') return await archivedPaging(common, cursor, fetchStream);
    const query = groupQuery(status, attentionDate, filters, cursor);
    return await fetchStream(query);
  } catch { /* network failure */ }
  return { kind: 'error' };
}
const summaryPath = '/api/v1/households/{householdId}/items/summary';
export async function getItemsSummary(householdId: string): Promise<{ kind: 'ok'; summary: ItemSummary } | { kind: 'noHousehold' } | Failure> {
  await afterItemWrites();
  try {
    const { data, response } = await api.GET(summaryPath, { params: { path: { householdId } } });
    if (data && response.status === 200) return { kind: 'ok', summary: data };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'noHousehold' };
  } catch { /* network failure */ }
  return { kind: 'error' };
}
export async function listItemGroup(householdId: string, group: ItemGroup, attentionDate: string, cursor?: string): Promise<ItemGroupPage> {
  await afterItemWrites();
  const query = groupQuery(group, attentionDate, {}, cursor);
  try {
    const { data, error, response } = await api.GET(listPath, { params: { path: { householdId }, query } });
    if (data && response.status === 200) return { kind: 'ok', items: data.items, nextCursor: data.nextCursor };
    if (response.status === 401) return { kind: 'unauthenticated' };
    if (response.status === 404) return { kind: 'noHousehold' };
    if (response.status === 400 && ['attentionDate', 'cursor'].includes(((error ?? {}) as Problem).parameter ?? '')) {
      return { kind: 'stale' };
    }
  } catch { /* network failure */ }
  return { kind: 'error' };
}

export type SubjectsResult =
  | { kind: 'ok'; subjects: Subject[]; nextCursor: string | null }
  | { kind: 'unauthenticated' }
  | { kind: 'error' }
  | { kind: 'noHousehold' };
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
export function createItem(householdId: string, body: components['schemas']['CreateItemRequest'], idempotencyKey: string): Promise<CreateResult> {
  return trackItemWrite(sendCreateItem(householdId, body, idempotencyKey));
}
async function sendCreateItem(householdId: string, body: components['schemas']['CreateItemRequest'], idempotencyKey: string): Promise<CreateResult> {
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
export function completeItem(householdId: string, itemId: string, etag: string, key: string): Promise<CompleteResult> {
  return trackItemWrite(sendCompleteItem(householdId, itemId, etag, key));
}
async function sendCompleteItem(householdId: string, itemId: string, etag: string, key: string): Promise<CompleteResult> {
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
      return { kind: 'conflict', code: completeConflictCode(code) };
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
export function undoCompletion(householdId: string, itemId: string, completionId: string, etag: string): Promise<UndoResult> {
  return trackItemWrite(sendUndoCompletion(householdId, itemId, completionId, etag));
}
async function sendUndoCompletion(householdId: string, itemId: string, completionId: string, etag: string): Promise<UndoResult> {
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
      return { kind: 'conflict', code: undoConflictCode(code) };
    }
    if (response.status === 412) return { kind: 'changed' };
    if (response.status >= 500) return { kind: 'unavailable', code: (error as Problem | undefined)?.code ?? 'unavailable' };
  } catch { /* keep the Undo action available after a transport failure */ }
  return { kind: 'error' };
}
