import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, household, installFakeServer, json, session, type RecordedRequest } from '../../test/fakeServer';
import type { Item, Subject } from './itemsApi';

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const itemList = `GET ${base}/items`;
const subjectList = `GET ${base}/subjects`;
const donePath = `${base}/items/i-1/completions`;
const routes = {
  'GET /api/v1/session': json(200, session()),
  [`GET ${base}`]: json(200, household),
};
const subject: Subject = { id: 's-1', name: 'Anna', type: 'person', archived: false, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' };
const subjects = json(200, { items: [subject], nextCursor: null });
const subjectPages = (request: RecordedRequest) => json(200, { items: request.query.get('archived') === 'true' ? [] : [subject], nextCursor: null });
const page = (items: Item[], nextCursor: string | null = null) => json(200, { items, nextCursor });
function item(overrides: Partial<Item> = {}): Item {
  return { id: 'i-1', subjectId: 's-1', title: 'Renew passport', notes: null, attentionOn: null, recurrence: null, workflowState: 'open', attention: 'needs_attention', archived: false, done: false, lastCompletedOn: null, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z', ...overrides };
}
function app() { return render(<I18nProvider><App /></I18nProvider>); }
function requestsOf(requests: RecordedRequest[], method: string, path: string) { return requests.filter((request) => request.method === method && request.path === path); }
async function openForm() {
  await screen.findByRole('heading', { name: 'Veselí' });
  await waitFor(() => expect(screen.queryByText('People and things could not be loaded. Try again.')).not.toBeInTheDocument());
  await userEvent.click(await screen.findByRole('button', { name: 'Add item' }));
  return screen.findByRole('form', { name: 'Add an item' });
}
beforeEach(() => vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']));
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.unstubAllEnvs(); vi.restoreAllMocks(); localStorage.removeItem('tendo.locale'); });

describe('item creation', () => {
  it('defaults Repeat off and sends an exact one-off body', async () => {
    const fake = installFakeServer({ ...routes, [itemList]: page([]), [subjectList]: subjectPages, [`POST ${base}/items`]: (request) => json(201, item({ title: JSON.parse(request.body).title })) });
    app();
    const form = await openForm();
    expect(within(form).getByRole('switch', { name: 'Repeat' })).not.toBeChecked();
    expect(within(form).queryByRole('spinbutton', { name: 'Repeat every' })).not.toBeInTheDocument();
    expect(within(form).queryByRole('switch', { name: /Count the next repeat/ })).not.toBeInTheDocument();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Renew passport');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await screen.findByText('“Renew passport” was added.');
    const post = requestsOf(fake.requests, 'POST', `${base}/items`)[0]!;
    expect(JSON.parse(post.body)).toEqual({ title: 'Renew passport', subjectId: 's-1', recurrence: null });
  });

  it.each([{ fluid: false, mode: 'fixed' }, { fluid: true, mode: 'after_completion' }])('creates repeat mode $mode with chosen interval', async ({ fluid, mode }) => {
    const fake = installFakeServer({ ...routes, [itemList]: page([]), [subjectList]: subjectPages, [`POST ${base}/items`]: (request) => json(201, item({ title: JSON.parse(request.body).title })) });
    app();
    const form = await openForm();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Check smoke alarm');
    await userEvent.click(within(form).getByRole('switch', { name: 'Repeat' }));
    expect(within(form).getByRole('spinbutton', { name: 'Repeat every' })).toBeVisible();
    expect(within(form).getByRole('switch', { name: 'Count the next repeat from when I complete this' })).not.toBeChecked();
    expect(within(form).getByText('Keeps the planned dates, even if you finish late.')).toBeVisible();
    await userEvent.clear(within(form).getByRole('spinbutton'));
    await userEvent.type(within(form).getByRole('spinbutton'), '3');
    await userEvent.selectOptions(within(form).getByRole('combobox', { name: 'Repeat interval' }), 'month');
    if (fluid) {
      await userEvent.click(within(form).getByRole('switch', { name: 'Count the next repeat from when I complete this' }));
      expect(within(form).getByText('The next one is counted from the day you finish.')).toBeVisible();
    }
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await screen.findByText('“Check smoke alarm” was added.');
    expect(JSON.parse(requestsOf(fake.requests, 'POST', `${base}/items`)[0]!.body)).toEqual({ title: 'Check smoke alarm', subjectId: 's-1', recurrence: { intervalValue: 3, intervalUnit: 'month', mode } });
  });

  it('loads the next subject page for selection and resolves its name on an item card', async () => {
    const second: Subject = { ...subject, id: 's-2', name: 'Garage' };
    const created = item({ id: 'i-2', subjectId: 's-2', title: 'Check the roof' });
    let createdOnServer = false;
    const fake = installFakeServer({
      ...routes,
      [itemList]: (request) => request.query.has('cursor') ? page([created]) : page(createdOnServer ? [created] : []),
      [subjectList]: (request) => {
        if (request.query.get('archived') === 'true') return json(200, { items: [], nextCursor: null });
        return request.query.get('cursor') === 'subject-cursor'
          ? json(200, { items: [second], nextCursor: null })
          : json(200, { items: [subject], nextCursor: 'subject-cursor' });
      },
      [`POST ${base}/items`]: (request) => { createdOnServer = true; return json(201, { ...created, title: JSON.parse(request.body).title }); },
    });
    app();
    const form = await openForm();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Check the roof');
    await userEvent.selectOptions(within(form).getByLabelText('For'), 's-2');
    expect(within(form).getByRole('option', { name: 'Garage' })).toBeVisible();
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await screen.findByText('Check the roof');
    const row = screen.getByText('Check the roof').closest('li')!;
    expect(row).toHaveTextContent('Garage');
    expect(JSON.parse(requestsOf(fake.requests, 'POST', `${base}/items`)[0]!.body)).toEqual({ title: 'Check the roof', subjectId: 's-2', recurrence: null });
    const subjectRequests = requestsOf(fake.requests, 'GET', `${base}/subjects`);
    expect(subjectRequests.map((request) => request.query.get('cursor'))).toContain('subject-cursor');
  });

  it('maps a 422 title error to a focused accessible field', async () => {
    installFakeServer({ ...routes, [itemList]: page([]), [subjectList]: subjectPages, [`POST ${base}/items`]: json(422, { type: 'about:blank', title: 'Validation Failed', status: 422, field: 'title', code: 'invalid_length' }) });
    app();
    const form = await openForm();
    const title = within(form).getByLabelText('What needs doing?');
    await userEvent.type(title, 'x');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    expect(await within(form).findByRole('alert')).toHaveTextContent('1 to 200 characters');
    expect(title).toHaveFocus();
    expect(title).toHaveAttribute('aria-invalid', 'true');
    expect(title.getAttribute('aria-describedby')).toBe(within(form).getByRole('alert').id);
  });

  it('distinguishes no subjects from a retryable subjects error', async () => {
    const fake = installFakeServer({ ...routes, [itemList]: page([]), [subjectList]: json(200, { items: [], nextCursor: null }) });
    app();
    await screen.findByRole('heading', { name: 'Veselí' });
    await userEvent.click(await screen.findByRole('button', { name: 'Add item' }));
    expect(await screen.findByText('Add a person or thing before adding an item.')).toBeVisible();
    expect(screen.getByRole('button', { name: 'People and things' })).toBeVisible();
    expect(fake.requests.some((request) => request.method === 'GET' && request.path === `${base}/items`)).toBe(true);

    cleanup();
    cleanup();
    const failed = installFakeServer({ ...routes, [itemList]: page([]), [subjectList]: json(503) });
    app();
    await screen.findByRole('heading', { name: 'Veselí' });
    expect(await screen.findByText('People and things could not be loaded. Try again.')).toBeVisible();
    expect(screen.queryByText('Add a person or thing before adding an item.')).not.toBeInTheDocument();
    expect(failed.requests.filter((request) => request.method === 'GET' && request.path === `${base}/subjects`)).toHaveLength(1);
  });
});

describe('Home items', () => {
  it('groups by server states and formats date-only values without timezone shifting', async () => {
    // Node honours TZ changes at runtime; a negative offset would turn a naive Date('2026-11-01') into Oct 31.
    vi.stubEnv('TZ', 'America/Los_Angeles');
    expect(new Date('2026-11-01').getDate()).toBe(31);
    const entries = [
      item({ id: 'a', title: 'Need now' }),
      item({ id: 'b', title: 'Started', attention: 'needs_attention', workflowState: 'in_progress' }),
      item({ id: 'c', title: 'Waiting item', workflowState: 'waiting' }),
      item({ id: 'd', title: 'Future item', attention: 'upcoming', attentionOn: '2026-11-01' }),
      item({ id: 'e', title: 'Paused item', workflowState: 'paused' }),
    ];
    installFakeServer({ ...routes, [itemList]: page(entries), [subjectList]: subjects });
    app();
    await screen.findByText('Need now');
    const headings = screen.getAllByRole('heading', { level: 2 }).map((heading) => heading.textContent);
    expect(headings).toEqual(['Needs attention', 'In progress', 'Waiting', 'Coming up', 'Paused']);
    expect(screen.getByText(/Nov 1, 2026/)).toBeVisible();
    const rows = screen.getAllByRole('listitem');
    expect(rows.map((row) => row.querySelector('a > span[aria-hidden="true"]')?.textContent)).toEqual(['Need now', 'Started', 'Waiting item', 'Future item', 'Paused item']);
  });

  it('reuses the unresolved key when the server committed but the response was lost', async () => {
    let listCalls = 0;
    let posts = 0;
    const fake = installFakeServer({
      ...routes,
      [itemList]: () => page(++listCalls === 1 ? [item()] : [item({ attention: 'upcoming', attentionOn: '2027-01-01' })]),
      [subjectList]: subjects,
      [`GET ${base}/items/i-1`]: json(200, item(), { ETag: '"7"' }),
      [`POST ${donePath}`]: (request) => {
        posts++;
        if (posts === 1) { JSON.parse(request.body); throw new Error('response lost after commit'); }
        return json(201, { id: 'c-replayed', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null });
      },
    });
    app();
    const done = await screen.findByRole('button', { name: 'Done' });
    await userEvent.click(done);
    await screen.findByRole('button', { name: 'Try again' });
    await userEvent.click(screen.getByRole('button', { name: 'Done' }));
    await screen.findByText('“Renew passport” is done.');
    const sentPosts = requestsOf(fake.requests, 'POST', donePath);
    expect(sentPosts).toHaveLength(2);
    expect(sentPosts[1]!.headers.get('Idempotency-Key')).toBe(sentPosts[0]!.headers.get('Idempotency-Key'));
  });

  it('uses a new idempotency key after successful completion', async () => {
    let getCount = 0;
    let postCount = 0;
    const fake = installFakeServer({
      ...routes,
      [itemList]: () => page([item({ recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' } })]),
      [subjectList]: subjects,
      [`GET ${base}/items/i-1`]: () => json(200, item({ recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' } }), { ETag: `"${7 + getCount++}"` }),
      [`POST ${donePath}`]: () => json(201, { id: `c-${postCount++}`, itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' }, nextAttentionOn: '2027-01-01', createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null }),
    });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    await screen.findByRole('button', { name: 'Undo' });
    await userEvent.click(screen.getByRole('button', { name: 'Done' }));
    await waitFor(() => expect(requestsOf(fake.requests, 'POST', donePath)).toHaveLength(2));
    const posts = requestsOf(fake.requests, 'POST', donePath);
    expect(posts[0]!.headers.get('Idempotency-Key')).not.toBe(posts[1]!.headers.get('Idempotency-Key'));
    expect(posts.map((request) => request.headers.get('If-Match'))).toEqual(['"7"', '"8"']);
  });

  it('keeps a completion key when the item GET fails during retry', async () => {
    let gets = 0;
    const fake = installFakeServer({
      ...routes,
      [itemList]: page([item()]),
      [subjectList]: subjects,
      [`GET ${base}/items/i-1`]: () => ++gets === 1 ? json(200, item(), { ETag: '"7"' }) : gets === 2 ? json(503) : json(200, item(), { ETag: '"8"' }),
      [`POST ${donePath}`]: (request) => request.headers.get('If-Match') === '"7"' ? json(503) : json(503),
    });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    const key = requestsOf(fake.requests, 'POST', donePath)[0]!.headers.get('Idempotency-Key');
    await userEvent.click(await screen.findByRole('button', { name: 'Try again' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Try again' }));
    const posts = requestsOf(fake.requests, 'POST', donePath);
    expect(posts).toHaveLength(2);
    expect(posts[0]!.headers.get('If-Match')).toBe('"7"');
    expect(posts[1]!.headers.get('If-Match')).toBe('"8"');
    expect(posts[1]!.headers.get('Idempotency-Key')).toBe(key);
    expect(sessionStorage.getItem(`tendo.pendingCompletion:${session().userId}:${HOUSEHOLD_ID}:i-1`)).toBe(key);
  });

  it('keeps the unresolved completion key when Home unmounts for People and things', async () => {
    let posts = 0;
    const fake = installFakeServer({
      ...routes,
      [itemList]: page([item()]),
      [subjectList]: json(200, { items: [], nextCursor: null }),
      [`GET ${base}/items/i-1`]: json(200, item(), { ETag: '"7"' }),
      [`POST ${donePath}`]: () => { if (++posts === 1) throw new Error('response lost'); return json(201, { id: 'c-nav', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null }); },
    });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    const key = requestsOf(fake.requests, 'POST', donePath)[0]!.headers.get('Idempotency-Key');
    await screen.findByRole('button', { name: 'Try again' });
    await userEvent.click(screen.getByRole('link', { name: 'People and things' }));
    await screen.findByRole('heading', { name: 'People and things' });
    await userEvent.click(screen.getByRole('link', { name: 'Back to home' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    expect(await screen.findByText('“Renew passport” is done.')).toBeVisible();
    const postsSent = requestsOf(fake.requests, 'POST', donePath);
    expect(postsSent).toHaveLength(2);
    expect(postsSent[0]!.headers.get('If-Match')).toBe('"7"');
    expect(postsSent[1]!.headers.get('If-Match')).toBe('"7"');
    expect(postsSent[1]!.headers.get('Idempotency-Key')).toBe(key);
  });

  it('retries a lost response from the server receipt using the exact stored receipt and advances once', async () => {
    const receipts = new Map<string, Record<string, unknown>>();
    let advanceCount = 0;
    const fake = installFakeServer({
      ...routes,
      [itemList]: () => page([item({ attention: advanceCount ? 'upcoming' : 'needs_attention', attentionOn: advanceCount ? '2027-01-01' : null })]),
      [subjectList]: subjects,
      [`GET ${base}/items/i-1`]: () => json(200, item({ attention: advanceCount ? 'upcoming' : 'needs_attention', attentionOn: advanceCount ? '2027-01-01' : null }), { ETag: `"${1 + advanceCount}"` }),
      [`POST ${donePath}`]: (request) => {
        const key = request.headers.get('Idempotency-Key')!;
        const replay = receipts.get(key);
        if (replay) return json(201, replay);
        expect(request.body).toBe('{}');
        advanceCount++;
        const receipt = { id: `c-${advanceCount}`, itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: session().userId, cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null, requestKey: key };
        receipts.set(key, receipt);
        throw new Error('server committed; response lost');
      },
    });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Try again' }));
    await screen.findByText('“Renew passport” is done.');
    const posts = requestsOf(fake.requests, 'POST', donePath);
    expect(posts).toHaveLength(2);
    expect(posts[1]!.headers.get('Idempotency-Key')).toBe(posts[0]!.headers.get('Idempotency-Key'));
    expect(advanceCount).toBe(1);
    expect(receipts.get(posts[0]!.headers.get('Idempotency-Key')!)?.requestKey).toBe(posts[0]!.headers.get('Idempotency-Key'));
  });

  it('persists an unresolved attempt across a fresh App render', async () => {
    sessionStorage.clear();
    let posts = 0;
    const server = {
      ...routes,
      [itemList]: page([item()]),
      [subjectList]: json(200, { items: [], nextCursor: null }),
      [`GET ${base}/items/i-1`]: json(200, item(), { ETag: '"7"' }),
      [`POST ${donePath}`]: () => { if (++posts === 1) throw new Error('response lost'); return json(201, { id: 'c-reload', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null }); },
    };
    const first = installFakeServer(server);
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    const key = requestsOf(first.requests, 'POST', donePath)[0]!.headers.get('Idempotency-Key');
    cleanup();
    const reloaded = installFakeServer(server);
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    await screen.findByText('“Renew passport” is done.');
    expect(requestsOf(reloaded.requests, 'POST', donePath)[0]!.headers.get('Idempotency-Key')).toBe(key);
  });

  it('clears stale pending completions when bootstrap finds a signed-out session', async () => {
    const storedKey = `tendo.pendingCompletion:${session().userId}:${HOUSEHOLD_ID}:i-1`;
    sessionStorage.setItem(storedKey, 'stale-attempt');
    installFakeServer({ 'GET /api/v1/session': json(401), 'GET /api/v1/auth/setup': json(200, { required: false }) });

    app();

    expect(await screen.findByRole('heading', { name: 'Sign in to Tendo' })).toBeVisible();
    expect(sessionStorage.getItem(storedKey)).toBeNull();
  });

  it('clears stored completion attempts on sign out', async () => {
    const key = 'a'.repeat(32);
    sessionStorage.setItem(`tendo.pendingCompletion:${session().userId}:${HOUSEHOLD_ID}:i-1`, key);
    const fake = installFakeServer({ ...routes, [itemList]: page([item()]), [subjectList]: subjects, 'DELETE /api/v1/session': json(204), 'GET /api/v1/auth/setup': json(200, { required: false }) });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Sign out' }));
    expect(await screen.findByRole('heading', { name: 'Sign in to Tendo' })).toBeVisible();
    expect(sessionStorage.getItem(`tendo.pendingCompletion:${session().userId}:${HOUSEHOLD_ID}:i-1`)).toBeNull();
    expect(requestsOf(fake.requests, 'DELETE', '/api/v1/session')).toHaveLength(1);
  });

  it('keeps Add item focused after an undone receipt is replayed', async () => {
    installFakeServer({ ...routes, [itemList]: page([item()]), [subjectList]: subjects, [`GET ${base}/items/i-1`]: json(200, item(), { ETag: '"7"' }), [`POST ${donePath}`]: json(201, { id: 'c-undone', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: '2026-01-02T00:00:00Z', undoneByUserId: 'u-1' }) });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    expect(await screen.findByText('That was already undone. The list is up to date.')).toBeVisible();
    await waitFor(() => expect(screen.getByText('That was already undone. The list is up to date.').closest('[tabindex="-1"]')).toHaveFocus());
    expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument();
    expect(sessionStorage.getItem(`tendo.pendingCompletion:${session().userId}:${HOUSEHOLD_ID}:i-1`)).toBeNull();
  });

  it('keeps Add and Done available but form submit disabled while Done is pending', async () => {
    let release!: () => void;
    const waiting = new Promise<void>((resolve) => { release = resolve; });
    const fake = installFakeServer({ ...routes, [itemList]: page([item()]), [subjectList]: subjects, [`GET ${base}/items/i-1`]: async () => { await waiting; return json(200, item(), { ETag: '"7"' }); }, [`POST ${donePath}`]: json(409, { type: 'about:blank', title: 'Conflict', status: 409, code: 'item_done' }), [`POST ${base}/items`]: json(201, item()) });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    const add = await screen.findByRole('button', { name: 'Add item' });
    await userEvent.click(add);
    const form = await screen.findByRole('form', { name: 'Add an item' });
    expect(within(form).getByLabelText('What needs doing?')).toBeEnabled();
    const submit = within(form).getByRole('button', { name: 'Add item' });
    expect(submit).toBeDisabled();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Another item');
    await userEvent.click(submit);
    expect(requestsOf(fake.requests, 'POST', `${base}/items`)).toHaveLength(0);
    expect(add).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Done' })).toBeDisabled();
    release();
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Add item' })).toBeEnabled());
  });

  it('restores the row and focuses its Done action after undo', async () => {
    const fake = installFakeServer({
      ...routes,
      [itemList]: page([item()]),
      [subjectList]: subjects,
      [`GET ${base}/items/i-1`]: () => {
        const mutations = requestsOf(fake.requests, 'POST', donePath).length + requestsOf(fake.requests, 'PATCH', `${donePath}/c-focus`).length;
        return json(200, item(), { ETag: mutations === 0 ? '"7"' : '"8"' });
      },
      [`POST ${donePath}`]: json(201, { id: 'c-focus', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null }),
      [`PATCH ${donePath}/c-focus`]: json(200, { id: 'c-focus', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: '2026-01-01T00:00:00Z', undoneByUserId: 'u-1' }),
    });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Undo' }));
    const done = await screen.findByRole('button', { name: 'Done' });
    await waitFor(() => expect(done).toHaveFocus());
    const gets = requestsOf(fake.requests, 'GET', `${base}/items/i-1`);
    const patch = requestsOf(fake.requests, 'PATCH', `${donePath}/c-focus`)[0]!;
    expect(gets).toHaveLength(2);
    expect(patch.headers.get('If-Match')).toBe('"8"');
  });

  it('completes with fresh ETag and one idempotency key; retry reuses the key after network failure', async () => {
    let listCalls = 0;
    let postCalls = 0;
    let itemReads = 0;
    const fake = installFakeServer({
      ...routes,
      [itemList]: () => page(++listCalls === 1 ? [item({ recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' } })] : [item({ attention: 'upcoming', attentionOn: '2027-01-01', recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' } })]),
      [subjectList]: subjects,
      [`GET ${base}/items/i-1`]: () => json(200, item({ recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' } }), { ETag: `"${7 + itemReads++}"` }),
      [`POST ${donePath}`]: () => ++postCalls === 1 ? Promise.reject(new Error('network')) : json(201, { id: 'c-1', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' }, nextAttentionOn: '2027-01-01', createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null }),
    });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    expect(await screen.findByText('That didn’t work. Check your connection and try again.')).toBeVisible();
    expect(requestsOf(fake.requests, 'POST', donePath)[0]!.headers.get('If-Match')).toBe('"7"');
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByText(/Next time: Jan 1, 2027/)).toBeVisible();
    const posts = requestsOf(fake.requests, 'POST', donePath);
    expect(posts).toHaveLength(2);
    expect(JSON.parse(posts[0]!.body)).toEqual({});
    expect(posts[0]!.headers.get('If-Match')).toBe('"7"');
    expect(posts[1]!.headers.get('If-Match')).toBe('"8"');
    const key = posts[0]!.headers.get('Idempotency-Key')!;
    expect(key).toMatch(/^[0-9a-f]{32}$/);
    expect(posts[1]!.headers.get('Idempotency-Key')).toBe(key);
    const itemGets = requestsOf(fake.requests, 'GET', `${base}/items/i-1`);
    expect(itemGets).toHaveLength(2);
    expect(itemReads).toBe(2);
    expect(posts.map((request) => request.headers.get('If-Match'))).toEqual(['"7"', '"8"']);
    expect(listCalls).toBeGreaterThanOrEqual(2);
  });

  it('undoes latest completion with fresh ETag and exact body, then clears the notice', async () => {
    const fake = installFakeServer({
      ...routes,
      [itemList]: page([item()]),
      [subjectList]: subjects,
      [`GET ${base}/items/i-1`]: () => {
        const previousMutation = requestsOf(fake.requests, 'POST', donePath).length + requestsOf(fake.requests, 'PATCH', `${donePath}/c-undo`).length;
        return json(200, item(), { ETag: previousMutation === 0 ? '"9"' : '"10"' });
      },
      [`POST ${donePath}`]: json(201, { id: 'c-undo', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null }),
      [`PATCH ${donePath}/c-undo`]: json(200, { id: 'c-undo', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: '2026-01-01T00:00:00Z', undoneByUserId: 'u-1' }),
    });
    app();
    const doneButton = await screen.findByRole('button', { name: 'Done' });
    await userEvent.click(doneButton);
    expect(await screen.findByText('“Renew passport” is done.')).toBeVisible();
    await waitFor(() => expect(screen.getByText('“Renew passport” is done.').closest('[tabindex="-1"]')).toHaveFocus());
    await userEvent.click(screen.getByRole('button', { name: 'Undo' }));
    await waitFor(() => expect(screen.queryByText('“Renew passport” is done.')).not.toBeInTheDocument());
    const patch = requestsOf(fake.requests, 'PATCH', `${donePath}/c-undo`)[0]!;
    expect(JSON.parse(patch.body)).toEqual({ undone: true });
    expect(patch.headers.get('If-Match')).toBe('"10"');
  });

  it.each([{ status: 412, message: 'That changed just now.' }, { status: 409, message: 'This item is already done. The list is up to date.' }])('shows a calm $status completion notice and refreshes', async ({ status, message }) => {
    let lists = 0;
    installFakeServer({ ...routes, [itemList]: () => { lists++; return page([item()]); }, [subjectList]: (request) => request.query.get('archived') === 'true' ? json(200, { items: [], nextCursor: null }) : subjects, [`GET ${base}/items/i-1`]: json(200, item(), { ETag: '"3"' }), [`POST ${donePath}`]: status === 409 ? json(409, { type: 'about:blank', title: 'Conflict', status: 409, code: 'item_done' }) : json(status), 'GET /api/v1/auth/setup': json(200, { required: false }) });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    expect(await screen.findByText(new RegExp(message))).toBeVisible();
    expect(lists).toBeGreaterThanOrEqual(2);
    if (status === 409) expect(sessionStorage.getItem(`tendo.pendingCompletion:${session().userId}:${HOUSEHOLD_ID}:i-1`)).toBeNull();
  });

  it('clears the pending key when the item is gone during Done', async () => {
    installFakeServer({ ...routes, [itemList]: page([item()]), [subjectList]: subjects, [`GET ${base}/items/i-1`]: json(404) });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    await screen.findByText('That item is gone. The list is up to date.');
    expect(sessionStorage.getItem(`tendo.pendingCompletion:${session().userId}:${HOUSEHOLD_ID}:i-1`)).toBeNull();
  });

  it('returns to sign-in on 401 while completing', async () => {
    installFakeServer({ ...routes, [itemList]: page([item()]), [subjectList]: subjects, [`GET ${base}/items/i-1`]: json(401), 'GET /api/v1/auth/setup': json(200, { required: false }) });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    expect(await screen.findByRole('heading', { name: 'Sign in to Tendo' })).toBeVisible();
  });

  it('a terminal 422 shows a calm notice without a retry action', async () => {
    installFakeServer({ ...routes, [itemList]: page([item()]), [subjectList]: subjects, [`GET ${base}/items/i-1`]: json(200, item(), { ETag: '"7"' }), [`POST ${donePath}`]: json(422, { type: 'about:blank', title: 'Validation Failed', status: 422, code: 'future_date' }) });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    expect(await screen.findByText('A completion date cannot be in the future.')).toBeVisible();
    const notice = screen.getByText('A completion date cannot be in the future.').closest<HTMLElement>('[aria-live="polite"]')!;
    expect(within(notice).queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument();
    expect(sessionStorage.getItem(`tendo.pendingCompletion:${session().userId}:${HOUSEHOLD_ID}:i-1`)).toBeNull();
  });

  it('removes Undo when the completion is no longer latest', async () => {
    let itemReads = 0;
    const fake = installFakeServer({
      ...routes,
      [itemList]: page([item()]),
      [subjectList]: subjects,
      [`GET ${base}/items/i-1`]: () => {
        const reads = itemReads++;
        return json(200, item(), { ETag: reads === 0 ? '"7"' : '"8"' });
      },
      [`POST ${donePath}`]: json(201, { id: 'c-old', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null }),
      [`PATCH ${donePath}/c-old`]: json(409, { type: 'about:blank', title: 'Conflict', status: 409, code: 'completion_not_latest' }),
    });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Undo' }));
    expect(await screen.findByText('Only the latest completion can be undone.')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument();
    const patch = requestsOf(fake.requests, 'PATCH', `${donePath}/c-old`)[0]!;
    expect(patch.headers.get('If-Match')).toBe('"8"');
  });

  it('maps a notes 422 to the expanded, focused accessible notes field', async () => {
    const fake = installFakeServer({ ...routes, [itemList]: page([]), [subjectList]: subjectPages, [`POST ${base}/items`]: json(422, { type: 'about:blank', title: 'Validation Failed', status: 422, field: 'notes', code: 'invalid_characters' }) });
    app();
    await screen.findByRole('heading', { name: 'Veselí' });
    const form = await openForm();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'A note');
    await userEvent.click(within(form).getByRole('button', { name: 'Add notes (optional)' }));
    const notes = within(form).getByLabelText('Notes');
    await userEvent.type(notes, 'details');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    expect(await within(form).findByRole('alert')).toHaveTextContent('These notes cannot be saved.');
    expect(notes).toHaveFocus();
    expect(notes).toHaveAttribute('aria-invalid', 'true');
    expect(notes.getAttribute('aria-describedby')).toBe(within(form).getByRole('alert').id);
    expect(requestsOf(fake.requests, 'POST', `${base}/items`)).toHaveLength(1);
    expect(JSON.parse(requestsOf(fake.requests, 'POST', `${base}/items`)[0]!.body)).toEqual({ title: 'A note', subjectId: 's-1', notes: 'details', recurrence: null });
  });

  it('reveals historical completion only for repeating add forms and sends only the entered date', async () => {
    const fake = installFakeServer({ ...routes, [itemList]: page([]), [subjectList]: subjectPages, [`POST ${base}/items`]: (request) => json(201, item({ title: JSON.parse(request.body).title })) });
    app();
    const form = await openForm();
    expect(within(form).queryByRole('button', { name: 'Add a previous completion' })).not.toBeInTheDocument();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Service boiler');
    await userEvent.click(within(form).getByRole('switch', { name: 'Repeat' }));
    const disclosure = within(form).getByRole('button', { name: 'Add a previous completion' });
    await userEvent.click(disclosure);
    const previousDate = within(form).getByLabelText('Last completed on');
    expect(previousDate).toHaveValue('');
    expect(within(form).getByText("Only enter a date you know. We'll use it to set the first attention date.")).toBeVisible();
    await userEvent.clear(previousDate);
    await userEvent.type(previousDate, '2025-06-12');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await screen.findByText('“Service boiler” was added.');
    expect(JSON.parse(requestsOf(fake.requests, 'POST', `${base}/items`)[0]!.body)).toEqual({ title: 'Service boiler', subjectId: 's-1', recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' }, historicalCompletedOn: '2025-06-12' });
  });

  it('omits a blank historical date and omits a previously entered date after Repeat is turned off', async () => {
    const fake = installFakeServer({ ...routes, [itemList]: page([]), [subjectList]: subjectPages, [`POST ${base}/items`]: (request) => json(201, item({ title: JSON.parse(request.body).title })) });
    app();
    const form = await openForm();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Clean filter');
    await userEvent.click(within(form).getByRole('switch', { name: 'Repeat' }));
    await userEvent.click(within(form).getByRole('button', { name: 'Add a previous completion' }));
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await screen.findByText('“Clean filter” was added.');
    expect(JSON.parse(requestsOf(fake.requests, 'POST', `${base}/items`)[0]!.body)).not.toHaveProperty('historicalCompletedOn');

    cleanup();
    const second = installFakeServer({ ...routes, [itemList]: page([]), [subjectList]: subjectPages, [`POST ${base}/items`]: (request) => json(201, item({ title: JSON.parse(request.body).title })) });
    app();
    const again = await openForm();
    await userEvent.type(within(again).getByLabelText('What needs doing?'), 'Clean filter');
    await userEvent.click(within(again).getByRole('switch', { name: 'Repeat' }));
    await userEvent.click(within(again).getByRole('button', { name: 'Add a previous completion' }));
    await userEvent.type(within(again).getByLabelText('Last completed on'), '2025-06-12');
    await userEvent.click(within(again).getByRole('button', { name: 'Hide previous completion' }));
    await userEvent.click(within(again).getByRole('button', { name: 'Add a previous completion' }));
    expect(within(again).getByLabelText('Last completed on')).toHaveValue('2025-06-12');
    await userEvent.click(within(again).getByRole('button', { name: 'Hide previous completion' }));
    await userEvent.click(within(again).getByRole('switch', { name: 'Repeat' }));
    await userEvent.click(within(again).getByRole('button', { name: 'Add item' }));
    await screen.findByText('“Clean filter” was added.');
    expect(JSON.parse(requestsOf(second.requests, 'POST', `${base}/items`)[0]!.body)).toEqual({ title: 'Clean filter', subjectId: 's-1', recurrence: null });
  });

  it.each([
    { code: 'conflicting_fields', message: 'Choose either a previous completion date or an attention date, not both.', attention: true },
    { code: 'future_date', message: 'A previous completion date cannot be in the future.', attention: false },
  ])('shows $code on the previous date, retaining both entered values', async ({ code, message, attention }) => {
    const fake = installFakeServer({ ...routes, [itemList]: page([]), [subjectList]: subjectPages, [`POST ${base}/items`]: json(422, { type: 'about:blank', title: 'Validation Failed', status: 422, field: 'historicalCompletedOn', code }) });
    app();
    const form = await openForm();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Renew passport');
    if (attention) await userEvent.type(within(form).getByLabelText('When should this need attention?'), '2026-10-20');
    await userEvent.click(within(form).getByRole('switch', { name: 'Repeat' }));
    await userEvent.click(within(form).getByRole('button', { name: 'Add a previous completion' }));
    const previousDate = within(form).getByLabelText('Last completed on');
    await userEvent.type(previousDate, '2025-06-12');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    expect(await within(form).findByRole('alert')).toHaveTextContent(message);
    expect(previousDate).toHaveFocus();
    expect(previousDate).toHaveAttribute('aria-invalid', 'true');
    expect(previousDate.getAttribute('aria-describedby')).toBe(within(form).getByRole('alert').id);
    expect(previousDate).toHaveValue('2025-06-12');
    if (attention) expect(within(form).getByLabelText('When should this need attention?')).toHaveValue('2026-10-20');
    const body = JSON.parse(requestsOf(fake.requests, 'POST', `${base}/items`)[0]!.body);
    expect(body).toHaveProperty('historicalCompletedOn', '2025-06-12');
    if (attention) expect(body).toHaveProperty('attentionOn', '2026-10-20');
    else expect(body).not.toHaveProperty('attentionOn');
  });

  it('creates an item and focuses Add after success', async () => {
    const fake = installFakeServer({ ...routes, [itemList]: page([]), [subjectList]: subjectPages, [`POST ${base}/items`]: (request) => json(201, item({ title: JSON.parse(request.body).title })) });
    app();
    await screen.findByRole('heading', { name: 'Veselí' });
    const form = await openForm();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Call dentist');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await screen.findByText('“Call dentist” was added.');
    await waitFor(() => expect(screen.getByRole('button', { name: 'Add item' })).toHaveFocus());
    expect(requestsOf(fake.requests, 'POST', `${base}/items`)).toHaveLength(1);
  });

  it('shows previous completion labels in Czech', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer({ ...routes, [itemList]: page([]), [subjectList]: subjectPages });
    app();
    await screen.findByRole('heading', { name: 'Veselí' });
    await userEvent.click(await screen.findByRole('button', { name: 'Přidat položku' }));
    const form = await screen.findByRole('form', { name: 'Přidat položku' });
    await userEvent.click(within(form).getByRole('switch', { name: 'Opakovat' }));
    await userEvent.click(within(form).getByRole('button', { name: 'Přidat předchozí dokončení' }));
    expect(within(form).getByLabelText('Naposledy dokončeno dne')).toBeVisible();
    expect(within(form).getByText('Zadejte jen datum, které znáte. Podle něj nastavíme první datum připomenutí.')).toBeVisible();
  });
});
