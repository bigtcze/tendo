import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, household, installFakeServer, json, session, type RecordedRequest } from '../../test/fakeServer';
import type { Completion, Item, Subject } from './itemsApi';

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const itemId = '0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b80';
const itemIdB = '0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b81';
const path = `${base}/items/${itemId}`;
const pathB = `${base}/items/${itemIdB}`;
const historyPath = `${path}/completions`;
const subject: Subject = { id: 's-1', name: 'Anna', type: 'person', archived: false, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' };
const baseRoutes = { 'GET /api/v1/session': json(200, session()), [`GET ${base}`]: json(200, household) };
function item(overrides: Partial<Item> = {}): Item { return { id: itemId, subjectId: subject.id, title: 'Renew passport', notes: 'Bring old passport', attentionOn: '2026-10-10', recurrence: { intervalValue: 2, intervalUnit: 'year', mode: 'fixed' }, workflowState: 'open', attention: 'upcoming', archived: false, done: false, lastCompletedOn: '2025-10-10', createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z', ...overrides }; }
function completion(id: string, completedOn: string, overrides: Partial<Completion> = {}): Completion { return { id, itemId, completedOn, completedByUserId: 'secret-user', cycleAttentionOn: '2024-10-10', recurrence: null, nextAttentionOn: '2026-10-10', createdAt: `${completedOn}T00:00:00Z`, undoneAt: null, undoneByUserId: null, ...overrides }; }
function app() { return render(<I18nProvider><App /></I18nProvider>); }
function requestsOf(requests: RecordedRequest[], method: string, requestPath: string) { return requests.filter((request) => request.method === method && request.path === requestPath); }
function common(routes: Record<string, Response | ((request: RecordedRequest) => Response | Promise<Response>)>) { return { ...baseRoutes, [`GET ${base}/items`]: json(200, { items: [item()], nextCursor: null }), [`GET ${base}/subjects/s-1`]: json(200, subject, { ETag: '"subject"' }), [`GET ${path}`]: json(200, item(), { ETag: '"1"' }), [`GET ${historyPath}`]: json(200, { items: [], nextCursor: null }), ...routes }; }
beforeEach(() => { vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']); window.history.replaceState(null, '', '/'); });
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); localStorage.removeItem('tendo.locale'); window.history.replaceState(null, '', '/'); });

describe('item details', () => {
  it('opens from a home title, starts with a fresh ETag and shows detail content', async () => {
    let etag = 1;
    const fake = installFakeServer(common({ [`GET ${path}`]: () => json(200, item(), { ETag: `"${etag++}"` }), [`PATCH ${path}`]: (req) => json(200, item({ workflowState: JSON.parse(req.body).workflowState }), { ETag: '"3"' }) }));
    app();
    await userEvent.click(await screen.findByRole('link', { name: 'Open item Renew passport' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Renew passport' })).toBeVisible();
    expect(screen.getByText('Anna')).toBeVisible();
    expect(screen.getByText('Bring old passport')).toBeVisible();
    expect(screen.getByText(/Repeats every 2 years/)).toBeVisible();
    await waitFor(() => expect(screen.getByRole('heading', { level: 1, name: 'Renew passport' })).toHaveFocus());
    await userEvent.click(screen.getByRole('button', { name: 'Start' }));
    await waitFor(() => expect(requestsOf(fake.requests, 'PATCH', path)).toHaveLength(1));
    const patch = requestsOf(fake.requests, 'PATCH', path)[0]!;
    expect(JSON.parse(patch.body)).toEqual({ workflowState: 'in_progress' });
    const itemGets = requestsOf(fake.requests, 'GET', path);
    expect(itemGets.length).toBeGreaterThanOrEqual(2);
    expect(patch.headers.get('If-Match')).toBe('"2"');
    expect(await screen.findByText('Item updated.')).toHaveFocus();
  });

  it('pauses, resumes, and waits with only workflow state changes', async () => {
    let state: Item['workflowState'] = 'open';
    const fake = installFakeServer(common({ [`GET ${path}`]: () => json(200, item({ workflowState: state }), { ETag: `"${state === 'open' ? 1 : 2}"` }), [`PATCH ${path}`]: (req) => { const body = JSON.parse(req.body); state = body.workflowState; return json(200, item({ workflowState: state }), { ETag: '"3"' }); } }));
    window.history.replaceState(null, '', `/items/${itemId}`); app();
    await userEvent.click(await screen.findByRole('button', { name: 'Pause' }));
    expect(await screen.findByText('Paused')).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Resume' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Waiting' }));
    await waitFor(() => expect(requestsOf(fake.requests, 'PATCH', path)).toHaveLength(3));
    expect(requestsOf(fake.requests, 'PATCH', path).map((request) => JSON.parse(request.body))).toEqual([{ workflowState: 'paused' }, { workflowState: 'open' }, { workflowState: 'waiting' }]);
  });

  it('uses the ETag from the immediately preceding GET and blocks double submission', async () => {
    let reads = 0; let release!: () => void;
    const hold = new Promise<void>((resolve) => { release = resolve; });
    const fake = installFakeServer(common({ [`GET ${path}`]: () => json(200, item(), { ETag: `"${++reads}"` }), [`PATCH ${path}`]: async () => { await hold; return json(200, item({ workflowState: 'in_progress' })); } }));
    app(); await userEvent.click(await screen.findByRole('link', { name: 'Open item Renew passport' }));
    const heading = await screen.findByRole('heading', { level: 1, name: 'Renew passport' });
    await waitFor(() => expect(heading).toHaveFocus());
    const button = await screen.findByRole('button', { name: 'Start' });
    await userEvent.dblClick(button);
    await waitFor(() => expect(requestsOf(fake.requests, 'PATCH', path)).toHaveLength(1));
    const patch = requestsOf(fake.requests, 'PATCH', path)[0]!;
    expect(JSON.parse(patch.body)).toEqual({ workflowState: 'in_progress' });
    expect(patch.headers.get('If-Match')).toBe('"2"');
    expect(button).toBeDisabled();
    release();
  });

  it('reloads after a stale write and shows latest values and notice', async () => {
    let reads = 0; const latest = item({ title: 'New title', workflowState: 'waiting', notes: 'Latest note' });
    installFakeServer(common({ [`GET ${path}`]: () => json(200, ++reads === 1 ? item() : latest, { ETag: `"${reads}"` }), [`PATCH ${path}`]: json(412) }));
    window.history.replaceState(null, '', `/items/${itemId}`); app();
    await userEvent.click(await screen.findByRole('button', { name: 'Start' }));
    expect(await screen.findByRole('heading', { name: 'New title' })).toBeVisible();
    expect(screen.getByText('Latest note')).toBeVisible();
    expect(screen.getByText('That changed just now. Here is the latest.')).toBeVisible();
  });

  it('archives and undoes using exact patch bodies and the latest ETags', async () => {
    let archived = false; let version = 1;
    const fake = installFakeServer(common({ [`GET ${path}`]: () => json(200, item({ archived }), { ETag: `"${version}"` }), [`PATCH ${path}`]: (req) => { archived = JSON.parse(req.body).archived; version++; return json(200, item({ archived }), { ETag: `"${version}"` }); } }));
    window.history.replaceState(null, '', `/items/${itemId}`); app();
    await userEvent.click(await screen.findByRole('button', { name: 'Archive' }));
    expect(await screen.findByText(/Archived\. This item stays here/)).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Undo' }));
    await waitFor(() => expect(requestsOf(fake.requests, 'PATCH', path)).toHaveLength(2));
    expect(requestsOf(fake.requests, 'PATCH', path).map((request) => JSON.parse(request.body))).toEqual([{ archived: true }, { archived: false }]);
    expect(requestsOf(fake.requests, 'PATCH', path).map((request) => request.headers.get('If-Match'))).toEqual(['"1"', '"2"']);
  });

  it('keeps archived item details/history and hides workflow controls', async () => {
    installFakeServer(common({ [`GET ${path}`]: json(200, item({ archived: true, done: true } ), { ETag: '"1"' }) }));
    window.history.replaceState(null, '', `/items/${itemId}`); app();
    expect(await screen.findByText(/Archived\. This item stays here/)).toBeVisible();
    expect(screen.getByRole('button', { name: 'Restore' })).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Start' })).not.toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'History' })).toBeVisible();
  });

  it('shows history newest first across two pages and mutes undone entries', async () => {
    const older = completion('c-old', '2024-03-01'); const newer = completion('c-new', '2025-04-02', { undoneAt: '2025-04-03T00:00:00Z' });
    const fake = installFakeServer(common({ [`GET ${historyPath}`]: (request) => request.query.has('cursor') ? json(200, { items: [newer], nextCursor: null }) : json(200, { items: [older], nextCursor: 'next' }) }));
    window.history.replaceState(null, '', `/items/${itemId}`); app();
    const newestText = await screen.findByText(/Done Apr 2, 2025/); const olderText = await screen.findByText(/Done Mar 1, 2024/);
    expect(newestText.closest('li')!.compareDocumentPosition(olderText.closest('li')!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(newestText.closest('li')).toHaveClass('opacity-65'); expect(newestText.closest('li')).toHaveTextContent('Undone');
    expect(screen.queryByText('secret-user')).not.toBeInTheDocument();
    expect(requestsOf(fake.requests, 'GET', historyPath)).toHaveLength(2);
  });

  it('loads details via deep link, shows gone for 404/malformed ids and Back returns home', async () => {
    const first = installFakeServer(common({ [`GET ${path}`]: json(404), [`GET ${base}/items`]: json(200, { items: [], nextCursor: null }) }));
    window.history.replaceState(null, '', `/items/${itemId}`); app();
    expect(await screen.findByRole('heading', { name: 'That item is gone' })).toBeVisible();
    await userEvent.click(screen.getByRole('link', { name: 'Back to home' }));
    expect(await screen.findByRole('heading', { name: 'Veselí' })).toBeVisible();
    cleanup(); window.history.replaceState(null, '', '/items/not-an-id');
    const malformed = installFakeServer(baseRoutes); app();
    expect(await screen.findByRole('heading', { name: 'That item is gone' })).toBeVisible();
    expect(requestsOf(first.requests, 'GET', path)).toHaveLength(1);
    expect(malformed.requests.some((request) => request.method === 'GET' && request.path.includes('/items/'))).toBe(false);
  });

  it('ignores a late response from an item after popstate navigates to another detail', async () => {
    let resolveA!: (response: Response) => void;
    const waitingA = new Promise<Response>((resolve) => { resolveA = resolve; });
    const itemB = { ...item(), id: itemIdB, title: 'Different item' };
    installFakeServer(common({ [`GET ${path}`]: () => waitingA, [`GET ${pathB}`]: () => json(200, itemB, { ETag: '"b"' }), [`GET ${base}/subjects/s-1`]: json(200, subject, { ETag: '"subject"' }), [`GET ${pathB}/completions`]: json(200, { items: [], nextCursor: null }) }));
    window.history.replaceState(null, '', `/items/${itemId}`); app();
    await waitFor(() => expect(window.location.pathname).toBe(`/items/${itemId}`));
    await act(async () => { window.history.pushState(null, '', `/items/${itemIdB}`); window.dispatchEvent(new PopStateEvent('popstate')); });
    expect(await screen.findByRole('heading', { name: 'Different item' })).toBeVisible();
    await act(async () => { resolveA(json(200, item(), { ETag: '"a"' })); });
    expect(screen.queryByRole('heading', { name: 'Renew passport' })).not.toBeInTheDocument();
  });

  it('handles mutation 404 and 401, plus failed restore and undo with visible notices', async () => {
    const fake = installFakeServer(common({ [`GET ${path}`]: json(200, item({ archived: true }), { ETag: '"1"' }), [`PATCH ${path}`]: json(503) }));
    window.history.replaceState(null, '', `/items/${itemId}`); app();
    await userEvent.click(await screen.findByRole('button', { name: 'Restore' }));
    expect(await screen.findByText('That didn’t work. Check your connection and try again.')).toBeVisible();
    expect(screen.getByText(/Archived\. This item stays here/)).toBeVisible();
    expect(requestsOf(fake.requests, 'PATCH', path)).toHaveLength(1);

    cleanup(); window.history.replaceState(null, '', `/items/${itemId}`);
    installFakeServer(common({ [`GET ${path}`]: json(200, item(), { ETag: '"1"' }), [`PATCH ${path}`]: (request) => JSON.parse(request.body).archived ? json(200, item({ archived: true })) : json(503) })); app();
    await userEvent.click(await screen.findByRole('button', { name: 'Archive' }));
    const undo = await screen.findByRole('button', { name: 'Undo' });
    await userEvent.click(undo);
    expect(await screen.findByText('That didn’t work. Check your connection and try again.')).toBeVisible();
    expect(screen.getByText(/Archived\. This item stays here/)).toBeVisible();

    cleanup(); window.history.replaceState(null, '', `/items/${itemId}`);
    installFakeServer(common({ [`GET ${path}`]: json(404) })); app();
    await screen.findByRole('heading', { name: 'That item is gone' });
    cleanup(); window.history.replaceState(null, '', `/items/${itemId}`);
    installFakeServer(common({ [`GET ${path}`]: json(401) })); app();
    expect(await screen.findByRole('heading', { name: 'Sign in to Tendo' })).toBeVisible();
  });

  it('prefills edit values and patches only changed fields with the ETag from edit-open GET', async () => {
    let reads = 0;
    const fake = installFakeServer(common({
      [`GET ${path}`]: () => json(200, item(), { ETag: `"${++reads}"` }),
      [`GET ${base}/subjects`]: json(200, { items: [subject], nextCursor: null }),
      [`PATCH ${path}`]: (request) => json(200, item({ title: JSON.parse(request.body).title }), { ETag: '"saved"' }),
    }));
    window.history.replaceState(null, '', `/items/${itemId}`); app();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit' }));
    const form = await screen.findByRole('form', { name: 'Edit item' });
    expect(screen.getByLabelText('What needs doing?')).toHaveValue('Renew passport');
    expect(screen.getByLabelText('For')).toHaveValue('s-1');
    expect(screen.getByLabelText('When should this need attention?')).toHaveValue('2026-10-10');
    expect(screen.getByLabelText('Notes')).toHaveValue('Bring old passport');
    await userEvent.clear(screen.getByLabelText('What needs doing?'));
    await userEvent.type(screen.getByLabelText('What needs doing?'), 'Renew passport soon');
    await userEvent.click(within(form).getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(requestsOf(fake.requests, 'PATCH', path)).toHaveLength(1));
    const patch = requestsOf(fake.requests, 'PATCH', path)[0]!;
    expect(JSON.parse(patch.body)).toEqual({ title: 'Renew passport soon' });
    expect(patch.headers.get('If-Match')).toBe('"2"');
    expect(await screen.findByText('Item updated.')).toBeVisible();
  });

  it('sends a complete changed recurrence only, and sends null when Repeat is turned off', async () => {
    const fake = installFakeServer(common({ [`GET ${base}/subjects`]: json(200, { items: [subject], nextCursor: null }), [`PATCH ${path}`]: json(200, item()) }));
    window.history.replaceState(null, '', `/items/${itemId}`); app();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit' }));
    const form = await screen.findByRole('form', { name: 'Edit item' });
    await userEvent.click(screen.getByRole('switch', { name: 'Count the next repeat from when I complete this' }));
    await userEvent.click(within(form).getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(requestsOf(fake.requests, 'PATCH', path)).toHaveLength(1));
    expect(JSON.parse(requestsOf(fake.requests, 'PATCH', path)[0]!.body)).toEqual({ recurrence: { intervalValue: 2, intervalUnit: 'year', mode: 'after_completion' } });
    cleanup(); window.history.replaceState(null, '', `/items/${itemId}`);
    const second = installFakeServer(common({ [`GET ${base}/subjects`]: json(200, { items: [subject], nextCursor: null }), [`PATCH ${path}`]: json(200, item()) })); app();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit' }));
    const secondForm = await screen.findByRole('form', { name: 'Edit item' });
    await userEvent.click(screen.getByRole('switch', { name: 'Repeat' }));
    await userEvent.click(within(secondForm).getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(requestsOf(second.requests, 'PATCH', path)).toHaveLength(1));
    expect(JSON.parse(requestsOf(second.requests, 'PATCH', path)[0]!.body)).toEqual({ recurrence: null });
  });

  it('sends null when the attention date and notes are cleared', async () => {
    const fake = installFakeServer(common({ [`GET ${base}/subjects`]: json(200, { items: [subject], nextCursor: null }), [`PATCH ${path}`]: json(200, item()) }));
    window.history.replaceState(null, '', `/items/${itemId}`); app();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit' }));
    const form = await screen.findByRole('form', { name: 'Edit item' });
    await userEvent.clear(screen.getByLabelText('When should this need attention?'));
    await userEvent.clear(screen.getByLabelText('Notes'));
    await userEvent.click(within(form).getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(requestsOf(fake.requests, 'PATCH', path)).toHaveLength(1));
    expect(JSON.parse(requestsOf(fake.requests, 'PATCH', path)[0]!.body)).toEqual({ attentionOn: null, notes: null });
  });

  it('keeps edits after a 412 and retries with the newly fetched ETag', async () => {
    let reads = 0; let writes = 0;
    const latest = item({ title: 'Saved elsewhere' });
    const fake = installFakeServer(common({
      [`GET ${path}`]: () => json(200, ++reads <= 2 ? item() : latest, { ETag: `"${reads}"` }),
      [`GET ${base}/subjects`]: json(200, { items: [subject], nextCursor: null }),
      [`PATCH ${path}`]: (request) => { writes++; return writes === 1 ? json(412) : json(200, item({ title: JSON.parse(request.body).title })); },
    }));
    window.history.replaceState(null, '', `/items/${itemId}`); app();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit' }));
    const form = await screen.findByRole('form', { name: 'Edit item' });
    const titleInput = screen.getByLabelText('What needs doing?');
    await userEvent.clear(titleInput); await userEvent.type(titleInput, 'My unsaved title');
    await userEvent.click(within(form).getByRole('button', { name: 'Save changes' }));
    expect(await screen.findByText(/Someone else changed this item/)).toBeVisible();
    expect(screen.getByText('Latest saved: Saved elsewhere')).toBeVisible();
    expect(screen.getByLabelText('What needs doing?')).toHaveValue('My unsaved title');
    await userEvent.click(screen.getByRole('button', { name: 'Save changes' }));
    await waitFor(() => expect(requestsOf(fake.requests, 'PATCH', path)).toHaveLength(2));
    expect(requestsOf(fake.requests, 'PATCH', path).map((request) => request.headers.get('If-Match'))).toEqual(['"2"', '"3"']);
    expect(await screen.findByText('Item updated.')).toBeVisible();
  });

  it('does not patch unchanged values and offers Edit in Czech', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    const fake = installFakeServer(common({ [`GET ${base}/subjects`]: json(200, { items: [subject], nextCursor: null }) }));
    window.history.replaceState(null, '', `/items/${itemId}`); app();
    await userEvent.click(await screen.findByRole('button', { name: 'Upravit' }));
    const form = await screen.findByRole('form', { name: 'Upravit položku' });
    expect(screen.getByRole('button', { name: 'Uložit změny' })).toBeVisible();
    await userEvent.click(within(form).getByRole('button', { name: 'Uložit změny' }));
    await waitFor(() => expect(screen.queryByRole('form', { name: 'Upravit položku' })).not.toBeInTheDocument());
    expect(requestsOf(fake.requests, 'PATCH', path)).toHaveLength(0);
  });

  it('renders Czech copy and browser Back returns home', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer(common({ [`GET ${base}/items`]: json(200, { items: [item()], nextCursor: null }) })); app();
    await userEvent.click(await screen.findByRole('link', { name: 'Otevřít položku Renew passport' }));
    expect(await screen.findByRole('heading', { name: 'Renew passport' })).toBeVisible();
    expect(screen.getByText('Naposledy hotovo 10. 10. 2025')).toBeVisible();
    window.history.back(); await waitFor(() => expect(window.location.pathname).toBe('/'));
    expect(await screen.findByRole('heading', { name: 'Veselí' })).toBeVisible();
  });
});
