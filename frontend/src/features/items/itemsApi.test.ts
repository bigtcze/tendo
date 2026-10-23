import { afterEach, describe, expect, it, vi } from 'vitest';
import { HOUSEHOLD_ID, installFakeServer, json } from '../../test/fakeServer';
import { completeItem, createItem, getItemsSummary, itemWriteWaitLimitMs, listItemGroup, patchItem } from './itemsApi';

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const itemId = '00000000-0000-4000-8000-000000000001';
const summary = { needsAttention: 0, inProgress: 0, waiting: 0, upcoming: 0, noDate: 0, done: 0, archived: 0 };
const item = { id: itemId, subjectId: 's-1', responsibleUserId: null, title: 'Item', notes: null, attentionOn: null, recurrence: null, workflowState: 'open', attention: 'needs_attention', archived: false, done: false, lastCompletedOn: null, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' };
function deferred() { let resolve!: (response: Response) => void; const promise = new Promise<Response>((r) => { resolve = r; }); return { promise, resolve }; }
const flush = async () => { for (let i = 0; i < 10; i += 1) await Promise.resolve(); };
const reads = (requests: { method: string }[]) => requests.filter((request) => request.method === 'GET');

afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe('item write barrier', () => {
  it('reads immediately when no item write is pending', async () => {
    const fake = installFakeServer({ [`GET ${base}/items/summary`]: json(200, summary) });
    expect((await getItemsSummary(HOUSEHOLD_ID)).kind).toBe('ok');
    expect(fake.requests).toHaveLength(1);
  });

  it('holds list and summary reads until every overlapping write settles, including failed ones', async () => {
    const patch = deferred(); const complete = deferred(); const create = deferred();
    const fake = installFakeServer({
      [`PATCH ${base}/items/${itemId}`]: () => patch.promise,
      [`POST ${base}/items/${itemId}/completions`]: () => complete.promise,
      [`POST ${base}/items`]: () => create.promise,
      [`GET ${base}/items/summary`]: json(200, summary),
      [`GET ${base}/items`]: json(200, { items: [], nextCursor: null }),
    });
    const writes: Promise<unknown>[] = [patchItem(HOUSEHOLD_ID, itemId, '"1"', { archived: true }), completeItem(HOUSEHOLD_ID, itemId, '"1"', 'k'.repeat(32))];
    const summaryRead = getItemsSummary(HOUSEHOLD_ID);
    const listRead = listItemGroup(HOUSEHOLD_ID, 'needs', '2026-01-15');
    await flush();
    expect(reads(fake.requests)).toHaveLength(0);
    patch.resolve(json(200, { ...item, archived: true }, { ETag: '"2"' }));
    await flush();
    // A write that starts while the barrier is waiting is also awaited.
    writes.push(createItem(HOUSEHOLD_ID, { title: 'New', subjectId: 's-1', recurrence: null }, 'c'.repeat(32)));
    complete.resolve(json(500, { title: 'boom' }));
    await flush();
    expect(reads(fake.requests)).toHaveLength(0);
    create.resolve(json(201, item));
    await Promise.all(writes);
    expect((await summaryRead).kind).toBe('ok');
    expect((await listRead).kind).toBe('ok');
    expect(reads(fake.requests)).toHaveLength(2);
  });

  it('stops waiting for a hung write after the bound so lists still load', async () => {
    vi.useFakeTimers();
    const fake = installFakeServer({
      [`PATCH ${base}/items/${itemId}`]: () => new Promise<Response>(() => {}),
      [`GET ${base}/items/summary`]: json(200, summary),
    });
    void patchItem(HOUSEHOLD_ID, itemId, '"1"', { archived: true });
    const summaryRead = getItemsSummary(HOUSEHOLD_ID);
    await vi.advanceTimersByTimeAsync(itemWriteWaitLimitMs - 1);
    expect(reads(fake.requests)).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1);
    expect((await summaryRead).kind).toBe('ok');
    expect(reads(fake.requests)).toHaveLength(1);
  });
});
