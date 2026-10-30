import { afterEach, describe, expect, it, vi } from 'vitest';
import { HOUSEHOLD_ID, installFakeServer, json } from '../../test/fakeServer';
import { changeItem, completeItem, createItem, getItemsSummary, getFilteredItemsSummary, itemWriteWaitLimitMs, listAllItems, listCompletions, listItemGroup, patchItem, undoCompletion } from './itemsApi';

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const itemId = '00000000-0000-4000-8000-000000000001';
const summary = { needsAttention: 0, inProgress: 0, waiting: 0, upcoming: 0, noDate: 0, done: 0, archived: 0 };
const item = { id: itemId, subjectId: 's-1', responsibleUserId: null, title: 'Item', notes: null, attentionOn: null, recurrence: null, workflowState: 'open', attention: 'needs_attention', archived: false, done: false, lastCompletedOn: null, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' };
function deferred() { let resolve!: (response: Response) => void; const promise = new Promise<Response>((r) => { resolve = r; }); return { promise, resolve }; }
const flush = async () => { for (let i = 0; i < 10; i += 1) await Promise.resolve(); };
const reads = (requests: { method: string }[]) => requests.filter((request) => request.method === 'GET');

afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe('completion history API', () => {
  it('sends requested order and limit while preserving legacy defaults', async () => {
    const path = `${base}/items/${itemId}/completions`;
    const fake = installFakeServer({ [`GET ${path}`]: json(200, { items: [], nextCursor: null }) });
    await listCompletions(HOUSEHOLD_ID, itemId, undefined, { order: 'newest', limit: 25 });
    expect(fake.requests[0]!.query.get('order')).toBe('newest');
    expect(fake.requests[0]!.query.get('limit')).toBe('25');
    await listCompletions(HOUSEHOLD_ID, itemId);
    expect(fake.requests[1]!.query.has('order')).toBe(false);
    expect(fake.requests[1]!.query.get('limit')).toBe('100');
  });
});

describe('item query characterization', () => {
  it.each([
    ['needs', 'workflowState=open&attention=needs_attention'],
    ['inProgress', 'workflowState=in_progress'],
    ['waiting', 'workflowState=waiting'],
    ['upcoming', 'workflowState=open&attention=upcoming'],
    ['paused', 'workflowState=paused'],
  ] as const)('lists %s using its exact group query', async (group, expected) => {
    const fake = installFakeServer({ [`GET ${base}/items`]: json(200, { items: [], nextCursor: null }) });
    await listItemGroup(HOUSEHOLD_ID, group, '2026-01-15');
    expect(fake.requests[0]!.query.toString()).toBe(`limit=25&sort=attention_on&attentionDate=2026-01-15&${expected}`);
  });

  it('preserves group stale and invalid-query behavior', async () => {
    const fake = installFakeServer({ [`GET ${base}/items`]: json(400, { parameter: 'cursor' }) });
    expect(await listItemGroup(HOUSEHOLD_ID, 'needs', '2026-01-15', 'c')).toEqual({ kind: 'stale' });
    expect(fake.requests[0]!.query.get('cursor')).toBe('c');
    for (const parameter of ['attentionDate', 'cursor']) {
      installFakeServer({ [`GET ${base}/items`]: json(400, { parameter }) });
      expect(await listItemGroup(HOUSEHOLD_ID, 'needs', '2026-01-15')).toEqual({ kind: 'stale' });
    }
    installFakeServer({ [`GET ${base}/items`]: json(400, { parameter: 'q' }) });
    expect(await listItemGroup(HOUSEHOLD_ID, 'needs', '2026-01-15')).toEqual({ kind: 'error' });
    expect(await listAllItems(HOUSEHOLD_ID, 'needs', {}, '2026-01-15')).toEqual({ kind: 'invalidQuery' });
  });

  it.each([401, 404, 500])('maps list group HTTP %s', async (status) => {
    installFakeServer({ [`GET ${base}/items`]: json(status, {}) });
    expect(await listItemGroup(HOUSEHOLD_ID, 'needs', '2026-01-15')).toEqual(
      status === 401 ? { kind: 'unauthenticated' } : status === 404 ? { kind: 'noHousehold' } : { kind: 'error' },
    );
  });

  it('preserves all-items status/filter query values and archived page boundary requests', async () => {
    const fake = installFakeServer({ [`GET ${base}/items`]: json(200, { items: [], nextCursor: null }) });
    await listAllItems(HOUSEHOLD_ID, 'active', { q: 'word', subjectId: 's', responsibleUserId: 'u', recurring: false }, '2026-01-15');
    expect(fake.requests[0]!.query.toString()).toBe('limit=25&sort=attention_on&attentionDate=2026-01-15&q=word&subjectId=s&responsibleUserId=u&recurring=false&archived=false&done=false');

    const page = { items: Array.from({ length: 25 }, (_, index) => ({ ...item, id: `${index}` })), nextCursor: null };
    const archived = installFakeServer({ [`GET ${base}/items`]: (request) => json(200, request.query.get('limit') === '1' ? { items: [item], nextCursor: null } : page) });
    expect(await listAllItems(HOUSEHOLD_ID, 'archived', {}, '2026-01-15')).toMatchObject({ kind: 'ok', nextCursor: 'b:' });
    expect(archived.requests.map((request) => request.query.get('limit'))).toEqual(['25', '1']);
    expect(archived.requests.map((request) => request.query.get('done'))).toEqual(['false', 'true']);
  });

  it.each([
    ['active', 'archived=false&done=false'],
    ['needs', 'workflowState=open&attention=needs_attention'],
    ['inProgress', 'workflowState=in_progress'],
    ['waiting', 'workflowState=waiting'],
    ['upcoming', 'workflowState=open&attention=upcoming'],
    ['paused', 'workflowState=paused'],
    ['done', 'done=true&archived=false'],
  ] as const)('lists all-items status %s after filters', async (status, statusQuery) => {
    const fake = installFakeServer({ [`GET ${base}/items`]: json(200, { items: [], nextCursor: null }) });
    await listAllItems(HOUSEHOLD_ID, status, { q: 'q', subjectId: 's', responsibleUserId: 'u', recurring: true }, '2026-01-15', 'cursor');
    expect(fake.requests[0]!.query.toString()).toBe(`limit=25&sort=attention_on&attentionDate=2026-01-15&q=q&subjectId=s&responsibleUserId=u&recurring=true&${statusQuery}&cursor=cursor`);
  });

  it.each(['x', 'a:', 'c:1'])('rejects invalid archived cursor %s without a request', async (cursor) => {
    const fake = installFakeServer({});
    expect(await listAllItems(HOUSEHOLD_ID, 'archived', {}, '2026-01-15', cursor)).toEqual({ kind: 'stale', parameter: 'cursor' });
    expect(fake.requests).toHaveLength(0);
  });

  it('continues archived first and completed streams with wrapped cursors', async () => {
    const fake = installFakeServer({ [`GET ${base}/items`]: json(200, { items: [], nextCursor: 'next' }) });
    expect(await listAllItems(HOUSEHOLD_ID, 'archived', {}, '2026-01-15', 'a:start')).toMatchObject({ kind: 'ok', nextCursor: 'a:next' });
    expect(fake.requests[0]!.query.toString()).toBe('limit=25&sort=attention_on&attentionDate=2026-01-15&archived=true&done=false&cursor=start');
    const done = installFakeServer({ [`GET ${base}/items`]: json(200, { items: [], nextCursor: 'later' }) });
    expect(await listAllItems(HOUSEHOLD_ID, 'archived', {}, '2026-01-15', 'b:before')).toMatchObject({ kind: 'ok', nextCursor: 'b:later' });
    expect(done.requests[0]!.query.toString()).toBe('limit=25&sort=attention_on&attentionDate=2026-01-15&archived=true&done=true&cursor=before');
  });

  it('propagates archived failures from first, probe and top-up calls', async () => {
    installFakeServer({ [`GET ${base}/items`]: json(500, {}) });
    expect(await listAllItems(HOUSEHOLD_ID, 'archived', {}, '2026-01-15')).toEqual({ kind: 'error' });
    const fullPage = { items: Array.from({ length: 25 }, (_, index) => ({ ...item, id: `${index}` })), nextCursor: null };
    let calls = 0;
    installFakeServer({ [`GET ${base}/items`]: () => ++calls === 1 ? json(200, fullPage) : json(503, {}) });
    expect(await listAllItems(HOUSEHOLD_ID, 'archived', {}, '2026-01-15')).toEqual({ kind: 'error' });
    calls = 0;
    installFakeServer({ [`GET ${base}/items`]: () => ++calls === 1 ? json(200, { items: [item], nextCursor: null }) : json(500, {}) });
    expect(await listAllItems(HOUSEHOLD_ID, 'archived', {}, '2026-01-15')).toEqual({ kind: 'error' });
  });

  it('maps all-items stale parameters and other bad requests', async () => {
    for (const parameter of ['attentionDate', 'cursor']) {
      installFakeServer({ [`GET ${base}/items`]: json(400, { parameter }) });
      expect(await listAllItems(HOUSEHOLD_ID, 'needs', {}, 'bad')).toEqual({ kind: 'stale', parameter });
    }
    installFakeServer({ [`GET ${base}/items`]: json(400, { parameter: 'workflowState' }) });
    expect(await listAllItems(HOUSEHOLD_ID, 'needs', {}, 'bad')).toEqual({ kind: 'error' });
  });

  it.each(Array.from({ length: 16 }, (_, bits) => bits))('omits only empty or undefined filters for combination %s', async (bits) => {
    const filters = {
      q: bits & 1 ? 'q' : '',
      subjectId: bits & 2 ? 's' : '',
      responsibleUserId: bits & 4 ? 'u' : '',
      recurring: bits & 8 ? false : undefined,
    };
    const fake = installFakeServer({ [`GET ${base}/items`]: json(200, { items: [], nextCursor: null }) });
    await listAllItems(HOUSEHOLD_ID, 'active', filters, '2026-01-15');
    const expected = ['limit=25', 'sort=attention_on', 'attentionDate=2026-01-15',
      ...(filters.q ? ['q=q'] : []), ...(filters.subjectId ? ['subjectId=s'] : []),
      ...(filters.responsibleUserId ? ['responsibleUserId=u'] : []),
      ...(filters.recurring !== undefined ? ['recurring=false'] : []), 'archived=false', 'done=false'].join('&');
    expect(fake.requests[0]!.query.toString()).toBe(expected);
  });

  it('tops up short archived first pages using only the remaining limit', async () => {
    const fake = installFakeServer({ [`GET ${base}/items`]: (request) => request.query.get('done') === 'false'
      ? json(200, { items: [item], nextCursor: null })
      : json(200, { items: [item], nextCursor: 'done-next' }) });
    expect(await listAllItems(HOUSEHOLD_ID, 'archived', {}, '2026-01-15')).toMatchObject({ kind: 'ok', items: [item, item], nextCursor: 'b:done-next' });
    expect(fake.requests.map((request) => request.query.get('limit'))).toEqual(['25', '24']);
  });

  it('starts the completed archived stream from the boundary cursor without a server cursor', async () => {
    const fake = installFakeServer({ [`GET ${base}/items`]: json(200, { items: [item], nextCursor: null }) });
    expect(await listAllItems(HOUSEHOLD_ID, 'archived', {}, '2026-01-15', 'b:')).toEqual({ kind: 'ok', items: [item], nextCursor: null });
    expect(fake.requests).toHaveLength(1);
    expect(fake.requests[0]!.query.toString()).toBe('limit=25&sort=attention_on&attentionDate=2026-01-15&archived=true&done=true');
  });

  it('keeps filters on archived probe and top-up requests and orders unfinished before completed', async () => {
    const filters = { q: 'q', subjectId: 's', responsibleUserId: 'u', recurring: true };
    const filterQuery = 'sort=attention_on&attentionDate=2026-01-15&q=q&subjectId=s&responsibleUserId=u&recurring=true';
    const full = { items: Array.from({ length: 25 }, (_, index) => ({ ...item, id: `u${index}` })), nextCursor: null };
    const probe = installFakeServer({ [`GET ${base}/items`]: (request) => json(200, request.query.get('limit') === '1' ? { items: [], nextCursor: null } : full) });
    expect(await listAllItems(HOUSEHOLD_ID, 'archived', filters, '2026-01-15')).toMatchObject({ kind: 'ok', nextCursor: null });
    expect(probe.requests.map((request) => request.query.toString())).toEqual([
      `limit=25&${filterQuery}&archived=true&done=false`,
      `limit=1&${filterQuery}&archived=true&done=true`,
    ]);

    const unfinished = { ...item, id: 'unfinished' };
    const completed = { ...item, id: 'completed', done: true };
    const topUp = installFakeServer({ [`GET ${base}/items`]: (request) => json(200, request.query.get('done') === 'false'
      ? { items: [unfinished], nextCursor: null }
      : { items: [completed], nextCursor: null }) });
    expect(await listAllItems(HOUSEHOLD_ID, 'archived', filters, '2026-01-15')).toEqual({ kind: 'ok', items: [unfinished, completed], nextCursor: null });
    expect(topUp.requests.map((request) => request.query.toString())).toEqual([
      `limit=25&${filterQuery}&archived=true&done=false`,
      `limit=24&${filterQuery}&archived=true&done=true`,
    ]);
  });

  it('maps create and patch response statuses and defaults', async () => {
    const fake = installFakeServer({
      [`POST ${base}/items`]: (request) => request.headers.get('Idempotency-Key') === 'reused' ? json(422, { code: 'idempotency_key_reused' })
        : request.headers.get('Idempotency-Key') === 'field' ? json(422, { field: 'subjectId', code: 'bad' })
          : request.headers.get('Idempotency-Key') === 'unknown' ? json(422, { field: 'unexpected' })
            : request.headers.get('Idempotency-Key') === 'denied' ? json(403, {})
              : request.headers.get('Idempotency-Key') === 'unauth' ? json(401, {})
              : request.headers.get('Idempotency-Key') === 'server' ? json(500, {})
                : json(201, item),
      [`PATCH ${base}/items/${itemId}`]: (request) => request.headers.get('If-Match') === 'bad' ? json(422, {}) : json(200, item, { ETag: 'next' }),
    });
    expect((await createItem(HOUSEHOLD_ID, { title: 'T', subjectId: 's', recurrence: null }, 'ok')).kind).toBe('ok');
    expect(await createItem(HOUSEHOLD_ID, { title: 'T', subjectId: 's', recurrence: null }, 'reused')).toEqual({ kind: 'reused' });
    expect(await createItem(HOUSEHOLD_ID, { title: 'T', subjectId: 's', recurrence: null }, 'field')).toEqual({ kind: 'invalid', field: 'subjectId', code: 'bad' });
    expect(await createItem(HOUSEHOLD_ID, { title: 'T', subjectId: 's', recurrence: null }, 'unknown')).toEqual({ kind: 'invalid', field: 'title', code: 'unknown' });
    expect((await createItem(HOUSEHOLD_ID, { title: 'T', subjectId: 's', recurrence: null }, 'denied')).kind).toBe('definitive');
    expect((await createItem(HOUSEHOLD_ID, { title: 'T', subjectId: 's', recurrence: null }, 'server')).kind).toBe('error');
    expect((await createItem(HOUSEHOLD_ID, { title: 'T', subjectId: 's', recurrence: null }, 'unauth')).kind).toBe('unauthenticated');
    expect((await patchItem(HOUSEHOLD_ID, itemId, 'bad', { title: 'T' })).kind).toBe('invalid');
    expect(fake.requests).toHaveLength(8);
  });

  it.each([
    ['complete', 401, 'unauthenticated'], ['complete', 404, 'notFound'],
    ['undo', 401, 'unauthenticated'], ['undo', 404, 'notFound'],
  ] as const)('maps %s HTTP %s', async (action, status, kind) => {
    installFakeServer({
      [`POST ${base}/items/${itemId}/completions`]: json(status, {}),
      [`PATCH ${base}/items/${itemId}/completions/c`]: json(status, {}),
    });
    const result = action === 'complete'
      ? await completeItem(HOUSEHOLD_ID, itemId, 'etag', 'key')
      : await undoCompletion(HOUSEHOLD_ID, itemId, 'c', 'etag');
    expect(result).toEqual({ kind });
  });

  it.each([[401, 'unauthenticated'], [404, 'notFound'], [412, 'changed']] as const)(
    'maps patch HTTP %s to %s', async (status, kind) => {
      installFakeServer({ [`PATCH ${base}/items/${itemId}`]: json(status, {}) });
      expect(await patchItem(HOUSEHOLD_ID, itemId, 'etag', { title: 'T' })).toEqual({ kind });
    },
  );

  it('maps complete/undo responses and whitelists conflicts', async () => {
    const fake = installFakeServer({
      [`POST ${base}/items/${itemId}/completions`]: (request) => request.headers.get('If-Match') === 'conflict' ? json(409, { code: 'completion_not_latest' }) : json(500, {}),
      [`PATCH ${base}/items/${itemId}/completions/c`]: (request) => request.headers.get('If-Match') === 'conflict' ? json(409, { code: 'item_done' }) : json(500, {}),
    });
    expect(await completeItem(HOUSEHOLD_ID, itemId, 'conflict', 'key')).toEqual({ kind: 'conflict', code: 'unknown' });
    expect(await undoCompletion(HOUSEHOLD_ID, itemId, 'c', 'conflict')).toEqual({ kind: 'conflict', code: 'unknown' });
    expect(await completeItem(HOUSEHOLD_ID, itemId, 'server', 'key')).toEqual({ kind: 'unavailable', code: 'unavailable' });
    expect(await undoCompletion(HOUSEHOLD_ID, itemId, 'c', 'server')).toEqual({ kind: 'unavailable', code: 'unavailable' });
    expect(fake.requests).toHaveLength(4);
  });

  it.each([
    ['complete item_done whitelist', 'complete', 'item_done', 'item_done'],
    ['complete item_archived whitelist', 'complete', 'item_archived', 'item_archived'],
    ['undo item_archived whitelist', 'undo', 'item_archived', 'item_archived'],
    ['undo completion_not_latest whitelist', 'undo', 'completion_not_latest', 'completion_not_latest'],
  ] as const)('maps %s', async (_name, action, code, expected) => {
    installFakeServer({
      [`POST ${base}/items/${itemId}/completions`]: json(409, { code }),
      [`PATCH ${base}/items/${itemId}/completions/c`]: json(409, { code }),
    });
    const result = action === 'complete'
      ? await completeItem(HOUSEHOLD_ID, itemId, 'etag', 'key')
      : await undoCompletion(HOUSEHOLD_ID, itemId, 'c', 'etag');
    expect(result).toEqual({ kind: 'conflict', code: expected });
  });

  it('maps complete 412/422 and undo 412', async () => {
    const fake = installFakeServer({
      [`POST ${base}/items/${itemId}/completions`]: (request) => request.headers.get('If-Match') === 'changed' ? json(412, {}) : json(422, { code: 'bad' }),
      [`PATCH ${base}/items/${itemId}/completions/c`]: json(412, {}),
    });
    expect(await completeItem(HOUSEHOLD_ID, itemId, 'changed', 'key')).toEqual({ kind: 'changed' });
    expect(await completeItem(HOUSEHOLD_ID, itemId, 'invalid', 'key')).toEqual({ kind: 'invalid', code: 'bad' });
    expect(await undoCompletion(HOUSEHOLD_ID, itemId, 'c', 'etag')).toEqual({ kind: 'changed' });
    expect(fake.requests).toHaveLength(3);
  });

  it('maps network failures for item writes to error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')));
    expect((await createItem(HOUSEHOLD_ID, { title: 'T', subjectId: 's', recurrence: null }, 'network')).kind).toBe('error');
    expect((await completeItem(HOUSEHOLD_ID, itemId, 'e', 'k')).kind).toBe('error');
    expect((await undoCompletion(HOUSEHOLD_ID, itemId, 'c', 'e')).kind).toBe('error');
    expect((await patchItem(HOUSEHOLD_ID, itemId, 'e', { title: 'T' })).kind).toBe('error');
  });

  it('passes changeItem unauthenticated and transport failures through', async () => {
    installFakeServer({ [`GET ${base}/items/${itemId}`]: json(401, {}) });
    expect(await changeItem(HOUSEHOLD_ID, itemId, { title: 'new' })).toEqual({ kind: 'unauthenticated' });
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')));
    expect(await changeItem(HOUSEHOLD_ID, itemId, { title: 'new' })).toEqual({ kind: 'error' });
  });

  it('preserves changeItem read failures without issuing a patch', async () => {
    const fake = installFakeServer({ [`GET ${base}/items/${itemId}`]: json(404, {}) });
    expect(await changeItem(HOUSEHOLD_ID, itemId, { title: 'new' })).toEqual({ kind: 'notFound' });
    expect(fake.requests).toHaveLength(1);
  });

  it('builds filtered summary query and maps error statuses', async () => {
    const fake = installFakeServer({ [`GET ${base}/items/summary`]: json(200, summary) });
    expect((await getFilteredItemsSummary(HOUSEHOLD_ID, { q: '', subjectId: 's', recurring: true })).kind).toBe('ok');
    expect(fake.requests[0]!.query.toString()).toBe('subjectId=s&recurring=true');
  });
});

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
    const hung = deferred();
    const fake = installFakeServer({
      [`PATCH ${base}/items/${itemId}`]: () => hung.promise,
      [`GET ${base}/items/summary`]: json(200, summary),
    });
    const write = patchItem(HOUSEHOLD_ID, itemId, '"1"', { archived: true });
    const summaryRead = getItemsSummary(HOUSEHOLD_ID);
    await vi.advanceTimersByTimeAsync(itemWriteWaitLimitMs - 1);
    expect(reads(fake.requests)).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1);
    expect((await summaryRead).kind).toBe('ok');
    expect(reads(fake.requests)).toHaveLength(1);
    // Settle the write so no tracked promise leaks into later tests.
    hung.resolve(json(200, { ...item, archived: true }, { ETag: '"2"' }));
    await write;
  });
});
