import { act, renderHook, waitFor } from '@testing-library/react';
import type { MouseEvent } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { useCursorList, type PageResult } from './useCursorList';

type Row = { id: string };
const rows = (...ids: string[]): Row[] => ids.map((id) => ({ id }));
/** A pager click: the list reads only which control was pressed. */
const click = () => ({ currentTarget: document.body }) as unknown as MouseEvent<HTMLButtonElement>;

/**
 * A server whose pages are read from a mutable table keyed by cursor (`''` is the first page), so a test can delete
 * rows between reads exactly the way another request would.
 */
function pagedServer(pages: Record<string, PageResult<Row>>) {
  const asked: (string | undefined)[] = [];
  const load = vi.fn(async (cursor?: string) => {
    asked.push(cursor);
    return pages[cursor ?? ''] ?? { kind: 'error' as const };
  });
  return { pages, asked, load };
}

function renderList(load: (cursor?: string) => Promise<PageResult<Row>>) {
  return renderHook(() => useCursorList<Row>({ load, onSignedOut: vi.fn(), keyOf: (row) => row.id }));
}

/** Opens the list and walks forward to the third page: first → c2 → c3. */
async function onThirdPage(server: ReturnType<typeof pagedServer>) {
  const view = renderList(server.load);
  await waitFor(() => expect(view.result.current.items).toEqual(rows('a')));
  act(() => { view.result.current.next(click()); });
  await waitFor(() => expect(view.result.current.items).toEqual(rows('b')));
  act(() => { view.result.current.next(click()); });
  await waitFor(() => expect(view.result.current.items).toEqual(rows('c')));
  expect(view.result.current.stack).toEqual(['', 'c2']);
  server.asked.length = 0;
  return view;
}

describe('useCursorList quiet reload', () => {
  it('steps back to the previous page when the reloaded later page has no rows left', async () => {
    const server = pagedServer({
      '': { kind: 'ok', items: rows('a'), nextCursor: 'c2' },
      c2: { kind: 'ok', items: rows('b'), nextCursor: 'c3' },
      c3: { kind: 'ok', items: rows('c'), nextCursor: null },
    });
    const view = await onThirdPage(server);

    // The only row on the third page went away (deleted, removed, left) — the server now ends the list on page two.
    server.pages.c2 = { kind: 'ok', items: rows('b'), nextCursor: null };
    server.pages.c3 = { kind: 'ok', items: [], nextCursor: null };
    act(() => { view.result.current.reload(); });

    await waitFor(() => expect(view.result.current.items).toEqual(rows('b')));
    expect(view.result.current.stack).toEqual(['']);
    expect(view.result.current.nextCursor).toBeNull();
    expect(view.result.current.busy).toBe(false);
    expect(view.result.current.state).toBe('ready');
    expect(server.asked).toEqual(['c3', 'c2']);
  });

  it('keeps stepping back past every emptied page, down to the first page', async () => {
    const server = pagedServer({
      '': { kind: 'ok', items: rows('a'), nextCursor: 'c2' },
      c2: { kind: 'ok', items: rows('b'), nextCursor: 'c3' },
      c3: { kind: 'ok', items: rows('c'), nextCursor: null },
    });
    const view = await onThirdPage(server);

    server.pages[''] = { kind: 'ok', items: rows('a'), nextCursor: null };
    server.pages.c2 = { kind: 'ok', items: [], nextCursor: null };
    server.pages.c3 = { kind: 'ok', items: [], nextCursor: null };
    act(() => { view.result.current.reload(); });

    await waitFor(() => expect(view.result.current.items).toEqual(rows('a')));
    expect(view.result.current.stack).toEqual([]);
    expect(server.asked).toEqual(['c3', 'c2', undefined]);
  });

  it('shows an empty first page as it is, without reading anything else', async () => {
    const server = pagedServer({ '': { kind: 'ok', items: rows('a'), nextCursor: null } });
    const view = renderList(server.load);
    await waitFor(() => expect(view.result.current.items).toEqual(rows('a')));
    server.asked.length = 0;

    server.pages[''] = { kind: 'ok', items: [], nextCursor: null };
    act(() => { view.result.current.reload(); });

    await waitFor(() => expect(view.result.current.items).toEqual([]));
    expect(view.result.current.stack).toEqual([]);
    expect(view.result.current.state).toBe('ready');
    expect(server.asked).toEqual([undefined]);
  });

  it('keeps an emptied later page that still leads on, rather than skipping rows after it', async () => {
    const server = pagedServer({
      '': { kind: 'ok', items: rows('a'), nextCursor: 'c2' },
      c2: { kind: 'ok', items: rows('b'), nextCursor: 'c3' },
      c3: { kind: 'ok', items: rows('c'), nextCursor: null },
    });
    const view = await onThirdPage(server);

    // A page that is empty but says more rows follow is not the end of the list; the reader pages on from there.
    server.pages.c3 = { kind: 'ok', items: [], nextCursor: 'c4' };
    act(() => { view.result.current.reload(); });

    await waitFor(() => expect(view.result.current.nextCursor).toBe('c4'));
    expect(view.result.current.items).toEqual([]);
    expect(view.result.current.stack).toEqual(['', 'c2']);
    expect(server.asked).toEqual(['c3']);
  });

  it('offers the earlier page again when stepping back to it fails, and keeps the screen ready', async () => {
    const server = pagedServer({
      '': { kind: 'ok', items: rows('a'), nextCursor: 'c2' },
      c2: { kind: 'ok', items: rows('b'), nextCursor: 'c3' },
      c3: { kind: 'ok', items: rows('c'), nextCursor: null },
    });
    const view = await onThirdPage(server);

    server.pages.c3 = { kind: 'ok', items: [], nextCursor: null };
    server.pages.c2 = { kind: 'error' };
    act(() => { view.result.current.reload(); });

    await waitFor(() => expect(view.result.current.pageFailure).toMatchObject({ cursor: 'c2', stack: [''] }));
    expect(view.result.current.state).toBe('ready');
    expect(view.result.current.busy).toBe(false);

    server.pages.c2 = { kind: 'ok', items: rows('b'), nextCursor: null };
    act(() => { view.result.current.retryPage(); });
    await waitFor(() => expect(view.result.current.items).toEqual(rows('b')));
    expect(view.result.current.stack).toEqual(['']);
    expect(view.result.current.pageFailure).toBeNull();
  });

  it('still steps back when the reader retries a reload that failed and the page has emptied meanwhile', async () => {
    const server = pagedServer({
      '': { kind: 'ok', items: rows('a'), nextCursor: 'c2' },
      c2: { kind: 'ok', items: rows('b'), nextCursor: 'c3' },
      c3: { kind: 'ok', items: rows('c'), nextCursor: null },
    });
    const view = await onThirdPage(server);

    server.pages.c3 = { kind: 'error' };
    act(() => { view.result.current.reload(); });
    await waitFor(() => expect(view.result.current.pageFailure).not.toBeNull());

    server.pages.c2 = { kind: 'ok', items: rows('b'), nextCursor: null };
    server.pages.c3 = { kind: 'ok', items: [], nextCursor: null };
    act(() => { view.result.current.retryPage(); });

    await waitFor(() => expect(view.result.current.items).toEqual(rows('b')));
    expect(view.result.current.stack).toEqual(['']);
    expect(view.result.current.pageFailure).toBeNull();
    expect(server.asked).toEqual(['c3', 'c3', 'c2']);
  });

  it('shows the empty page a reader retries after a failed pager click, without stepping back', async () => {
    const server = pagedServer({
      '': { kind: 'ok', items: rows('a'), nextCursor: 'c2' },
      c2: { kind: 'error' },
    });
    const view = renderList(server.load);
    await waitFor(() => expect(view.result.current.items).toEqual(rows('a')));
    act(() => { view.result.current.next(click()); });
    await waitFor(() => expect(view.result.current.pageFailure).not.toBeNull());
    server.asked.length = 0;

    server.pages.c2 = { kind: 'ok', items: [], nextCursor: null };
    act(() => { view.result.current.retryPage(); });

    await waitFor(() => expect(view.result.current.stack).toEqual(['']));
    expect(view.result.current.items).toEqual([]);
    expect(server.asked).toEqual(['c2']);
  });

  it('never steps back on a pager click that lands on an empty page', async () => {
    const server = pagedServer({
      '': { kind: 'ok', items: rows('a'), nextCursor: 'c2' },
      c2: { kind: 'ok', items: [], nextCursor: null },
    });
    const view = renderList(server.load);
    await waitFor(() => expect(view.result.current.items).toEqual(rows('a')));
    server.asked.length = 0;

    // Paging is the reader's own step: it shows the page asked for, even when it is empty.
    act(() => { view.result.current.next(click()); });
    await waitFor(() => expect(view.result.current.stack).toEqual(['']));
    expect(view.result.current.items).toEqual([]);
    expect(server.asked).toEqual(['c2']);
  });
});
