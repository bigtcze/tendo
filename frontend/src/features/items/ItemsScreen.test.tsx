import { StrictMode } from 'react';
import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, household, installFakeServer, json, session, type RecordedRequest } from '../../test/fakeServer';
import type { Item, Subject } from './itemsApi';
import { clearPendingItemCreates } from './pendingItemCreate';
import { itemRoutes } from '../../test/fakeItems';

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const subjectList = `GET ${base}/subjects`;
const memberList = `GET ${base}/members`;
const ownerMember = { userId: session().userId, login: 'anna', role: 'owner' as const };
const secondMember = { userId: 'u-2', login: 'petr', role: 'member' as const };
const memberPage = (items = [ownerMember, secondMember]) => json(200, { items, nextCursor: null });
const donePath = `${base}/items/i-1/completions`;
const routes = {
  'GET /api/v1/session': json(200, session()),
  [`GET ${base}`]: json(200, household),
};
const subject: Subject = { id: 's-1', name: 'Anna', type: 'person', linkedUserId: null, archived: false, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' };
const subjects = json(200, { items: [subject], nextCursor: null });
const subjectPages = (request: RecordedRequest) => json(200, { items: request.query.get('archived') === 'true' ? [] : [subject], nextCursor: null });
function item(overrides: Partial<Item> = {}): Item {
  return { id: 'i-1', subjectId: 's-1', responsibleUserId: null, title: 'Renew passport', notes: null, attentionOn: null, recurrence: null, workflowState: 'open', attention: 'needs_attention', archived: false, done: false, lastCompletedOn: null, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z', ...overrides };
}
function app() { return render(<I18nProvider><App /></I18nProvider>); }
function requestsOf(requests: RecordedRequest[], method: string, path: string) { return requests.filter((request) => request.method === method && request.path === path); }
async function openForm() {
  await screen.findByRole('heading', { name: 'Veselí' });
  await userEvent.click(await screen.findByRole('button', { name: 'Add item' }));
  return screen.findByRole('form', { name: 'Add an item' });
}
beforeEach(() => { clearPendingItemCreates(); sessionStorage.clear(); vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']); });
afterEach(() => { cleanup(); clearPendingItemCreates(); vi.unstubAllGlobals(); vi.unstubAllEnvs(); vi.restoreAllMocks(); localStorage.removeItem('tendo.locale'); });

describe('item creation', () => {
  it('defaults Repeat off and sends an exact one-off body', async () => {
    const source: Item[] = [];
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, () => source), [subjectList]: subjectPages, [memberList]: memberPage(), [`POST ${base}/items`]: (request) => { const created = item({ id: 'created-live', title: JSON.parse(request.body).title }); source.push(created); return json(201, created); } });
    app();
    const form = await openForm();
    expect(screen.queryByText('Renew passport')).not.toBeInTheDocument();
    expect(within(form).getByRole('switch', { name: 'Repeat' })).not.toBeChecked();
    expect(within(form).queryByRole('spinbutton', { name: 'Repeat every' })).not.toBeInTheDocument();
    expect(within(form).queryByRole('switch', { name: /Count the next repeat/ })).not.toBeInTheDocument();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Renew passport');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await screen.findByText('“Renew passport” was added.');
    expect(await screen.findByText('Renew passport')).toBeVisible();
    const post = requestsOf(fake.requests, 'POST', `${base}/items`)[0]!;
    expect(JSON.parse(post.body)).toEqual({ title: 'Renew passport', subjectId: 's-1', recurrence: null });
    expect(post.headers.get('Idempotency-Key')).toMatch(/^[0-9a-f]{32}$/);
    expect(Object.hasOwn(JSON.parse(post.body), 'responsibleUserId')).toBe(false);
  });

  it('pending-create recovery renders immediately and retries unchanged despite failed subject fetches', async () => {
    const key = 'a'.repeat(32);
    const body = { title: 'Frozen recovery', subjectId: 'missing', recurrence: null };
    sessionStorage.setItem(`tendo.pendingItemCreate.v1:${session().userId}:${HOUSEHOLD_ID}`, JSON.stringify({ version: 1, key, body }));
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: json(503), [memberList]: memberPage(), [`GET ${base}/subjects/missing`]: json(503), [`POST ${base}/items`]: () => json(201, item({ title: body.title, subjectId: body.subjectId })) });
    app();
    const form = await screen.findByRole('form', { name: 'Add an item' });
    expect(within(form).getByRole('button', { name: 'Check again' })).toBeEnabled();
    await userEvent.click(within(form).getByRole('button', { name: 'Check again' }));
    await screen.findByText('“Frozen recovery” was added.');
    const post = requestsOf(fake.requests, 'POST', `${base}/items`)[0]!;
    expect(post.headers.get('Idempotency-Key')).toBe(key);
    expect(post.body).toBe(JSON.stringify(body));
  });

  it('retries an uncertain item create with its stored key and exact body while inputs are frozen', async () => {
    let posts = 0;
    let persistedBeforeRequest: { key: string; body: string } | undefined;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: memberPage(), [`POST ${base}/items`]: (request) => {
      if (posts++ === 0) {
        const stored = sessionStorage.getItem(`tendo.pendingItemCreate.v1:${session().userId}:${HOUSEHOLD_ID}`);
        if (stored) { const parsed = JSON.parse(stored); persistedBeforeRequest = { key: parsed.key, body: JSON.stringify(parsed.body) }; }
        return Promise.reject(new Error('offline'));
      }
      return json(201, item({ title: JSON.parse(request.body).title }));
    } });
    app();
    const form = await openForm();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Retry item');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    expect(await within(form).findByText('We couldn’t confirm this was saved.')).toBeVisible();
    const first = requestsOf(fake.requests, 'POST', `${base}/items`)[0]!;
    expect(first.headers.get('Idempotency-Key')).toMatch(/^[0-9a-f]{32}$/);
    expect(persistedBeforeRequest).toEqual({ key: first.headers.get('Idempotency-Key'), body: first.body });
    expect(within(form).getByLabelText('What needs doing?')).toBeDisabled();
    await userEvent.click(within(form).getByRole('button', { name: 'Check again' }));
    await screen.findByText('“Retry item” was added.');
    const postsSent = requestsOf(fake.requests, 'POST', `${base}/items`);
    expect(postsSent).toHaveLength(2);
    expect(postsSent[1]!.headers.get('Idempotency-Key')).toBe(first.headers.get('Idempotency-Key'));
    expect(postsSent[1]!.body).toBe(first.body);
  });

  it('uses a fresh key for a deliberate create after success', async () => {
    let posts = 0;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: memberPage(), [`POST ${base}/items`]: (request) => json(201, item({ id: `i-${++posts}`, title: JSON.parse(request.body).title })) });
    app();
    let form = await openForm();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'First');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await screen.findByText('“First” was added.');
    await userEvent.click(await screen.findByRole('button', { name: 'Add item' }));
    form = await screen.findByRole('form', { name: 'Add an item' });
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Second');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await screen.findByText('“Second” was added.');
    const postsSent = requestsOf(fake.requests, 'POST', `${base}/items`);
    expect(postsSent[1]!.headers.get('Idempotency-Key')).not.toBe(postsSent[0]!.headers.get('Idempotency-Key'));
  });

  it('restores a pending create after App remount and retries the stored body', async () => {
    let posts = 0;
    const archivedSubject = { ...subject, archived: true };
    let archived = false;
    const server = { ...routes, ...itemRoutes(base, []), [subjectList]: (request: RecordedRequest) => json(200, { items: request.query.get('archived') === 'true' ? (archived ? [archivedSubject] : []) : (archived ? [] : [subject]), nextCursor: null }), [memberList]: memberPage(), [`POST ${base}/items`]: (request: RecordedRequest) => { if (++posts === 1) { archived = true; return Promise.reject(new Error('offline')); } return json(201, item({ title: JSON.parse(request.body).title })); } };
    const first = installFakeServer(server);
    app();
    const form = await openForm();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Reload item');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await within(form).findByText('We couldn’t confirm this was saved.');
    const firstPost = requestsOf(first.requests, 'POST', `${base}/items`)[0]!;
    cleanup();
    const reloaded = installFakeServer(server);
    app();
    const restored = await screen.findByRole('form', { name: 'Add an item' });
    expect(within(restored).getByLabelText('What needs doing?')).toHaveValue('Reload item');
    expect(within(restored).getByLabelText('What needs doing?')).toBeDisabled();
    expect(await within(restored).findByRole('button', { name: 'Check again' })).toBeVisible();
    await waitFor(() => expect(within(restored).getByText('We couldn’t confirm this was saved.')).toHaveFocus());
    await userEvent.click(within(restored).getByRole('button', { name: 'Check again' }));
    await screen.findByText('“Reload item” was added.');
    const resent = requestsOf(reloaded.requests, 'POST', `${base}/items`)[0]!;
    expect(resent.headers.get('Idempotency-Key')).toBe(firstPost.headers.get('Idempotency-Key'));
    expect(resent.body).toBe(firstPost.body);
  });

  it('Add loads subject pages only when opened and shows chooser only after full paging', async () => {
    const second = { ...subject, id: 's-2', name: 'Garage' };
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: (request) => {
      if (request.query.has('id')) return json(200, { items: [], nextCursor: null });
      return request.query.get('cursor') === 'subject-cursor' ? json(200, { items: [second], nextCursor: null }) : json(200, { items: [subject], nextCursor: 'subject-cursor' });
    }, [memberList]: memberPage() });
    app();
    await screen.findByRole('heading', { name: 'Veselí' });
    await screen.findByRole('button', { name: 'Add item' });
    expect(requestsOf(fake.requests, 'GET', `${base}/subjects`)).toHaveLength(0);
    await userEvent.click(screen.getByRole('button', { name: 'Add item' }));
    const form = await screen.findByRole('form', { name: 'Add an item' });
    expect(within(form).getByRole('option', { name: 'Garage' })).toBeVisible();
    expect(requestsOf(fake.requests, 'GET', `${base}/subjects`).map((request) => request.query.get('cursor'))).toEqual([null, 'subject-cursor']);
  });

  it('ignores older member pages resolved out of order', async () => {
    let resolveFirst!: (response: Response) => void;
    let resolveSecond!: (response: Response) => void;
    const first = new Promise<Response>((resolve) => { resolveFirst = resolve; });
    const second = new Promise<Response>((resolve) => { resolveSecond = resolve; });
    let calls = 0;
    installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: () => (++calls === 1 ? memberPage() : calls === 2 ? first : second) });
    app(); await openForm();
    await userEvent.click(within(await screen.findByRole('form', { name: 'Add an item' })).getByRole('button', { name: 'Cancel' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Add item' }));
    await waitFor(() => expect(calls).toBe(3));
    await act(async () => resolveSecond(json(200, { items: [ownerMember, secondMember], nextCursor: null })));
    await waitFor(() => expect(screen.getByLabelText("Who's looking after it")).toBeVisible());
    await act(async () => resolveFirst(json(200, { items: [ownerMember], nextCursor: null })));
    expect(screen.getByRole('option', { name: 'petr' })).toBeInTheDocument();
  });

  it('caps Add catalogue paging without exposing a partial chooser and allows retry', async () => {
    let failWalk = false;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: (request) => {
      if (failWalk) return json(503);
      return json(200, { items: [subject], nextCursor: `cursor-${request.query.get('cursor') ?? 'start'}` });
    }, [memberList]: memberPage() });
    app();
    await screen.findByRole('button', { name: 'Add item' });
    await userEvent.click(screen.getByRole('button', { name: 'Add item' }));
    expect(await screen.findByText('People and things could not be loaded. Try again.')).toBeVisible();
    expect(screen.queryByRole('form', { name: 'Add an item' })).not.toBeInTheDocument();
    expect(requestsOf(fake.requests, 'GET', `${base}/subjects`)).toHaveLength(20);
    failWalk = true;
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByText('People and things could not be loaded. Try again.')).toBeVisible();
    expect(requestsOf(fake.requests, 'GET', `${base}/subjects`)).toHaveLength(21);
    expect(screen.queryByRole('form', { name: 'Add an item' })).not.toBeInTheDocument();
  });

  it('closes and reopens Add without accepting the earlier subject catalogue response', async () => {
    let release!: (response: Response) => void;
    let calls = 0;
    const pending = new Promise<Response>((resolve) => { release = resolve; });
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: () => ++calls === 1 ? pending : json(200, { items: [subject], nextCursor: null }), [memberList]: memberPage() });
    app();
    await screen.findByRole('heading', { name: 'Veselí' });
    await screen.findByRole('button', { name: 'Add item' });
    await userEvent.click(screen.getByRole('button', { name: 'Add item' }));
    await waitFor(() => expect(requestsOf(fake.requests, 'GET', `${base}/subjects`)).toHaveLength(1));
    await userEvent.click(await screen.findByRole('button', { name: 'Cancel' }));
    await userEvent.click(screen.getByRole('button', { name: 'Add item' }));
    const form = await screen.findByRole('form', { name: 'Add an item' });
    await waitFor(() => expect(requestsOf(fake.requests, 'GET', `${base}/subjects`)).toHaveLength(2));
    await act(async () => { release(json(200, { items: [{ ...subject, name: 'Obsolete' }], nextCursor: null })); });
    expect(within(form).getByRole('option', { name: 'Anna' })).toBeInTheDocument();
    expect(within(form).queryByRole('option', { name: 'Obsolete' })).not.toBeInTheDocument();
  });

  it('signs out on a current label 401 and ignores a stale response after unmount', async () => {
    let release!: (response: Response) => void;
    const deferred = new Promise<Response>((resolve) => { release = resolve; });
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, [item()]), [subjectList]: () => deferred, [memberList]: memberPage(), 'GET /api/v1/auth/setup': json(200, { required: false }) });
    app();
    await screen.findByRole('heading', { name: 'Veselí' });
    await waitFor(() => expect(requestsOf(fake.requests, 'GET', `${base}/subjects`)).toHaveLength(2));
    cleanup();
    await act(async () => { release(json(401)); });
    expect(screen.queryByRole('heading', { name: 'Sign in to Tendo' })).not.toBeInTheDocument();

    installFakeServer({ ...routes, ...itemRoutes(base, [item()]), [subjectList]: json(401), [memberList]: memberPage(), 'GET /api/v1/auth/setup': json(200, { required: false }) });
    app();
    expect(await screen.findByRole('heading', { name: 'Sign in to Tendo' })).toBeVisible();
  });

  it('ignores a stale 401 member response after add is cancelled', async () => {
    let resolveMembers!: (response: Response) => void;
    const pending = new Promise<Response>((resolve) => { resolveMembers = resolve; });
    let calls = 0;
    installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: () => ++calls === 1 ? memberPage() : pending });
    app();
    await screen.findByRole('heading', { name: 'Veselí' });
    await userEvent.click(await screen.findByRole('button', { name: 'Add item' }));
    const form = await screen.findByRole('form', { name: 'Add an item' });
    await userEvent.click(within(form).getByRole('button', { name: 'Cancel' }));
    await act(async () => resolveMembers(json(401)));
    expect(screen.getByRole('heading', { name: 'Veselí' })).toBeVisible();
    expect(screen.queryByRole('heading', { name: 'Sign in to Tendo' })).not.toBeInTheDocument();
    expect(screen.queryByRole('form', { name: 'Add an item' })).not.toBeInTheDocument();
  });

  it('creates an item with the selected responsible member', async () => {
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: memberPage(), [`POST ${base}/items`]: (request) => json(201, item({ title: JSON.parse(request.body).title, responsibleUserId: JSON.parse(request.body).responsibleUserId })) });
    app();
    const form = await openForm();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Call dentist');
    await userEvent.selectOptions(within(form).getByLabelText("Who's looking after it"), 'u-2');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await screen.findByText('“Call dentist” was added.');
    expect(JSON.parse(requestsOf(fake.requests, 'POST', `${base}/items`)[0]!.body)).toEqual({ title: 'Call dentist', subjectId: 's-1', recurrence: null, responsibleUserId: 'u-2' });
  });

  it('hides the responsible field for a single-member household', async () => {
    installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: memberPage([ownerMember]) });
    app();
    const form = await openForm();
    expect(within(form).queryByLabelText("Who's looking after it")).not.toBeInTheDocument();
  });

  it.each([{ fluid: false, mode: 'fixed' }, { fluid: true, mode: 'after_completion' }])('creates repeat mode $mode with chosen interval', async ({ fluid, mode }) => {
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: memberPage(), [`POST ${base}/items`]: (request) => json(201, item({ title: JSON.parse(request.body).title })) });
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

  it('loads active subject pages only after opening Add; the chooser preserves full paging', async () => {
    const second: Subject = { ...subject, id: 's-2', name: 'Garage' };
    const created = item({ id: 'i-2', subjectId: 's-2', title: 'Check the roof' });
    const source: Item[] = [];
    const fake = installFakeServer({
      ...routes,
      ...itemRoutes(base, () => source),
      [subjectList]: (request) => {
        if (request.query.get('archived') === 'true') return json(200, { items: [], nextCursor: null });
        return request.query.get('cursor') === 'subject-cursor'
          ? json(200, { items: [second], nextCursor: null })
          : json(200, { items: [subject], nextCursor: 'subject-cursor' });
      },
      [`POST ${base}/items`]: (request) => { const saved = { ...created, title: JSON.parse(request.body).title }; source.push(saved); return json(201, saved); },
    });
    app();
    await screen.findByRole('heading', { name: 'Veselí' });
    await screen.findByRole('button', { name: 'Add item' });
    expect(requestsOf(fake.requests, 'GET', `${base}/subjects`)).toHaveLength(0);
    const form = await openForm();
    expect(screen.queryByText('Check the roof')).not.toBeInTheDocument();
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

  it('retains and retries an item create attempt after 503 with the same key and body', async () => {
    let posts = 0;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: memberPage(), [`POST ${base}/items`]: (request) => ++posts === 1 ? json(503) : json(201, item({ title: JSON.parse(request.body).title })) });
    app();
    const form = await openForm();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Retry after 503');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await within(form).findByText('We couldn’t confirm this was saved.');
    const first = requestsOf(fake.requests, 'POST', `${base}/items`)[0]!;
    expect(sessionStorage.getItem(`tendo.pendingItemCreate.v1:${session().userId}:${HOUSEHOLD_ID}`)).not.toBeNull();
    await userEvent.click(within(form).getByRole('button', { name: 'Check again' }));
    await screen.findByText('“Retry after 503” was added.');
    const retry = requestsOf(fake.requests, 'POST', `${base}/items`)[1]!;
    expect(retry.headers.get('Idempotency-Key')).toBe(first.headers.get('Idempotency-Key'));
    expect(retry.body).toBe(first.body);
  });

  it('clears a definitive create rejection and uses a fresh key on the next deliberate submit', async () => {
    let posts = 0;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: memberPage(), [`POST ${base}/items`]: (request) => ++posts === 1 ? json(400) : json(201, item({ title: JSON.parse(request.body).title })) });
    app();
    const form = await openForm();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Definitive rejection');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await within(form).findByRole('alert');
    const first = requestsOf(fake.requests, 'POST', `${base}/items`)[0]!;
    expect(sessionStorage.getItem(`tendo.pendingItemCreate.v1:${session().userId}:${HOUSEHOLD_ID}`)).toBeNull();
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await screen.findByText('“Definitive rejection” was added.');
    const second = requestsOf(fake.requests, 'POST', `${base}/items`)[1]!;
    expect(second.headers.get('Idempotency-Key')).not.toBe(first.headers.get('Idempotency-Key'));
  });

  it('clears an idempotency-key reuse response and offers no retry', async () => {
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: memberPage(), [`POST ${base}/items`]: json(422, { status: 422, code: 'idempotency_key_reused' }) });
    app();
    const form = await openForm();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Reused key');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    expect(await screen.findByText('This wasn’t saved. Please add it again.')).toBeVisible();
    await waitFor(() => expect(screen.getByText('This wasn’t saved. Please add it again.').closest('[tabindex="-1"]')).toHaveFocus());
    expect(screen.queryByRole('button', { name: 'Check again' })).not.toBeInTheDocument();
    expect(sessionStorage.getItem(`tendo.pendingItemCreate.v1:${session().userId}:${HOUSEHOLD_ID}`)).toBeNull();
    expect(requestsOf(fake.requests, 'POST', `${base}/items`)).toHaveLength(1);
  });

  it('maps a 422 title error to a focused accessible field', async () => {
    installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: memberPage(), [`POST ${base}/items`]: json(422, { type: 'about:blank', title: 'Validation Failed', status: 422, field: 'title', code: 'invalid_length' }) });
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
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: json(200, { items: [], nextCursor: null }) });
    app();
    await screen.findByRole('heading', { name: 'Veselí' });
    await userEvent.click(await screen.findByRole('button', { name: 'Add item' }));
    expect(await screen.findByText('Add a person or thing before adding an item.')).toBeVisible();
    expect(screen.getByRole('button', { name: 'People and things' })).toBeVisible();
    expect(fake.requests.some((request) => request.method === 'GET' && request.path === `${base}/items/summary`)).toBe(true);

    cleanup();
    cleanup();
    const failed = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: json(503) });
    app();
    await screen.findByRole('heading', { name: 'Veselí' });
    await userEvent.click(await screen.findByRole('button', { name: 'Add item' }));
    expect(await screen.findByText('People and things could not be loaded. Try again.')).toBeVisible();
    expect(screen.queryByText('Add a person or thing before adding an item.')).not.toBeInTheDocument();
    expect(failed.requests.filter((request) => request.method === 'GET' && request.path === `${base}/subjects`)).toHaveLength(1);
  });
});

describe('Home items', () => {
  it('avoids catalogue requests without rows and batch-loads only unique row subjects across groups', async () => {
    const noRows = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjects });
    app();
    await screen.findByRole('heading', { name: 'Veselí' });
    expect(requestsOf(noRows.requests, 'GET', `${base}/subjects`)).toHaveLength(0);
    cleanup();

    const states: Item['workflowState'][] = ['open', 'in_progress', 'waiting', 'open', 'paused'];
    const rows = Array.from({ length: 125 }, (_, index) => item({ id: `id-${index}`, subjectId: `s-${index}`, title: `Row ${index}`, workflowState: states[Math.floor(index / 25)]!, attention: index >= 75 && index < 100 ? 'upcoming' : 'needs_attention', attentionOn: index >= 75 && index < 100 ? '2027-01-01' : null }));
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, rows), [subjectList]: (request) => {
      const ids = request.query.getAll('id');
      expect(ids.length).toBeLessThanOrEqual(100);
      return json(200, { items: ids.map((id) => ({ ...subject, id, name: `Name ${id}` })), nextCursor: null });
    } });
    app();
    await screen.findByText('Name s-0');
    await waitFor(() => expect(requestsOf(fake.requests, 'GET', `${base}/subjects`)).toHaveLength(4));
    const labelCalls = requestsOf(fake.requests, 'GET', `${base}/subjects`);
    expect(labelCalls.map((request) => request.query.getAll('id').length).sort((a, b) => a - b)).toEqual([25, 25, 100, 100]);
    expect(labelCalls.every((request) => request.query.get('limit') === '100' && request.query.get('cursor') === null)).toBe(true);
  });

  it('Show more requests only newly visible subject IDs and locale changes reuse labels', async () => {
    const rows = Array.from({ length: 26 }, (_, index) => item({ id: `walk-${index}`, subjectId: `s-${index}`, title: `Walk ${index}`, workflowState: 'open', attention: 'needs_attention' }));
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, rows), [subjectList]: (request) => json(200, { items: request.query.getAll('id').map((id) => ({ ...subject, id, name: `Name ${id}` })), nextCursor: null }) });
    app();
    await screen.findByText('Name s-0');
    await waitFor(() => expect(requestsOf(fake.requests, 'GET', `${base}/subjects`)).toHaveLength(2));
    expect(screen.getByRole('button', { name: 'Show more (1 remaining)' })).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Show more (1 remaining)' }));
    await screen.findByText('Name s-25');
    const calls = requestsOf(fake.requests, 'GET', `${base}/subjects`);
    expect(calls).toHaveLength(4);
    expect(calls.slice(2).map((request) => request.query.getAll('id'))).toEqual([['s-9'], ['s-9']]);
    await userEvent.click(screen.getByRole('button', { name: 'Čeština' }));
    await waitFor(() => expect(screen.getByRole('heading', { name: /Veselí/ })).toBeVisible());
    expect(requestsOf(fake.requests, 'GET', `${base}/subjects`)).toHaveLength(4);
  });

  it('requests each shared row subject in both partitions and displays archived names', async () => {
    const archivedSubject = { ...subject, id: 'archived-id', archived: true, name: 'Archived name' };
    const same = [item({ id: 'same-needs', title: 'Needs row', subjectId: 'archived-id', workflowState: 'open' }), item({ id: 'same-progress', title: 'Progress row', subjectId: 'archived-id', workflowState: 'in_progress' })];
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, same), [subjectList]: (request) => request.query.get('archived') === 'true' ? json(200, { items: [archivedSubject], nextCursor: null }) : json(200, { items: [], nextCursor: null }) });
    app();
    await screen.findByText('Needs row');
    await waitFor(() => expect(requestsOf(fake.requests, 'GET', `${base}/subjects`)).toHaveLength(2));
    const rowsWithName = await screen.findAllByText('Archived name');
    expect(rowsWithName).toHaveLength(2);
    const calls = requestsOf(fake.requests, 'GET', `${base}/subjects`);
    expect(calls).toHaveLength(2);
    expect(calls.map((request) => request.query.get('archived')).sort()).toEqual(['false', 'true']);
    expect(calls.every((request) => request.query.getAll('id').join() === 'archived-id')).toBe(true);
  });

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
    installFakeServer({ ...routes, ...itemRoutes(base, entries), [subjectList]: subjects });
    app();
    await screen.findByText('Need now');
    const headings = screen.getAllByRole('heading', { level: 2 }).map((heading) => heading.textContent);
    expect(headings).toEqual(['Needs attention 1 item', 'In progress 1 item', 'Waiting 1 item', 'Coming up 1 item', 'Paused 1 item']);
    expect(screen.getByText(/Nov 1, 2026/)).toBeVisible();
    const rows = screen.getByRole('main').querySelectorAll('ul.divide-y > li');
    expect([...rows].filter((row) => row.querySelector('a > span[aria-hidden="true"]')).map((row) => row.querySelector('a > span[aria-hidden="true"]')?.textContent)).toEqual(['Need now', 'Started', 'Waiting item', 'Future item', 'Paused item']);
  });

  it('shows calm row date, last-done, and assignee metadata and loads members once', async () => {
    const entries = [
      item({ id: 'needs-date', title: 'Needs date', attentionOn: '2025-12-03', lastCompletedOn: '2025-12-01', responsibleUserId: session().userId }),
      item({ id: 'progress-date', title: 'In progress date', workflowState: 'in_progress', attentionOn: '2026-03-04', responsibleUserId: 'u-2' }),
      item({ id: 'waiting-date', title: 'Waiting date', workflowState: 'waiting', attentionOn: '2026-03-05' }),
      item({ id: 'paused-date', title: 'Paused date', workflowState: 'paused', attentionOn: '2026-03-06' }),
      item({ id: 'upcoming-date', title: 'Upcoming date', attention: 'upcoming', attentionOn: '2026-02-03' }),
      item({ id: 'no-date', title: 'No attention date' }),
    ];
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, entries), [subjectList]: subjects, [memberList]: memberPage() });
    app();
    const needs = await screen.findByText('Needs date');
    const needsRow = needs.closest('li')!;
    await waitFor(() => expect(needsRow).toHaveTextContent('Looked after by you'));
    expect(needsRow).toHaveTextContent('Needs attention since Dec 3, 2025');
    expect(needsRow).toHaveTextContent('Last done Dec 1, 2025');
    const progressRow = screen.getByText('In progress date').closest('li')!;
    expect(progressRow).toHaveTextContent('Attention date Mar 4, 2026');
    expect(progressRow).toHaveTextContent('petr');
    expect(screen.getByText('Waiting date').closest('li')).toHaveTextContent('Attention date Mar 5, 2026');
    expect(screen.getByText('Paused date').closest('li')).toHaveTextContent('Attention date Mar 6, 2026');
    expect(screen.getByText('Upcoming date').closest('li')).toHaveTextContent('Needs attention Feb 3, 2026');
    const noDateRow = screen.getByText('No attention date').closest('li')!;
    expect(noDateRow).not.toHaveTextContent('Attention date');
    expect(noDateRow).not.toHaveTextContent('Needs attention since');
    await waitFor(() => expect(requestsOf(fake.requests, 'GET', `${base}/members`)).toHaveLength(1));
  });

  it('retries only failed label IDs while retaining names from the successful partition', async () => {
    let failArchived = true;
    const two = [item({ id: 'label-one', subjectId: 's-1' }), item({ id: 'label-two', subjectId: 's-2', workflowState: 'in_progress' })];
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, two), [subjectList]: (request) => {
      if (request.query.get('archived') === 'false') return json(200, { items: [{ ...subject, id: 's-1', name: 'Known one' }], nextCursor: null });
      return failArchived ? json(503) : json(200, { items: [{ ...subject, id: 's-2', name: 'Known two' }], nextCursor: null });
    } });
    app();
    await screen.findByText('Known one');
    expect(await screen.findByText('Couldn’t load some names. Try again')).toBeVisible();
    expect(screen.getByText('Person or thing')).toBeVisible();
    expect(screen.queryByText('s-1')).not.toBeInTheDocument();
    const before = requestsOf(fake.requests, 'GET', `${base}/subjects`).length;
    failArchived = false;
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    await screen.findByText('Known two');
    expect(requestsOf(fake.requests, 'GET', `${base}/subjects`)).toHaveLength(before + 2);
    expect(requestsOf(fake.requests, 'GET', `${base}/subjects`).slice(before).every((request) => request.query.getAll('id').join() === 's-2')).toBe(true);
  });

  it('keeps home rows visible without assignee names when member loading fails', async () => {
    installFakeServer({ ...routes, ...itemRoutes(base, [item({ responsibleUserId: 'u-2' })]), [subjectList]: subjects, [memberList]: json(503) });
    app();
    const title = await screen.findByText('Renew passport');
    const row = title.closest('li')!;
    await waitFor(() => expect(row).not.toHaveTextContent('Unknown member'));
    expect(row).not.toHaveTextContent('petr');
    expect(row).toBeVisible();
  });

  it('signs out when the home members request is unauthorized', async () => {
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, [item()]), [subjectList]: subjects, [memberList]: json(401), 'GET /api/v1/auth/setup': json(200, { required: false }) });
    app();
    expect(await screen.findByRole('heading', { name: 'Sign in to Tendo' })).toBeVisible();
    expect(requestsOf(fake.requests, 'GET', `${base}/members`)).toHaveLength(1);
  });

  it('ignores a stale home members 401 after navigating to People and things', async () => {
    let release!: (response: Response) => void;
    const deferred = new Promise<Response>((resolve) => { release = resolve; });
    let memberCalls = 0;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjects, [memberList]: () => ++memberCalls === 1 ? deferred : memberPage(), 'GET /api/v1/auth/setup': json(200, { required: false }) });
    app();
    await screen.findByRole('heading', { name: 'Veselí' });
    await userEvent.click(screen.getByRole('link', { name: 'People and things' }));
    await screen.findByRole('heading', { name: 'People and things' });
    await act(async () => { release(json(401)); });
    expect(screen.queryByRole('heading', { name: 'Sign in to Tendo' })).not.toBeInTheDocument();
    expect(requestsOf(fake.requests, 'GET', `${base}/members`)).toHaveLength(2);
  });

  it('ignores an obsolete successful home members response from a StrictMode generation', async () => {
    let releaseFirst!: (response: Response) => void;
    const first = new Promise<Response>((resolve) => { releaseFirst = resolve; });
    let calls = 0;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, [item({ responsibleUserId: 'u-2' })]), [subjectList]: subjects, [memberList]: () => ++calls === 1 ? first : memberPage() });
    render(<StrictMode><I18nProvider><App /></I18nProvider></StrictMode>);
    const title = await screen.findByText('Renew passport');
    const row = title.closest('li')!;
    await waitFor(() => expect(requestsOf(fake.requests, 'GET', `${base}/members`)).toHaveLength(2));
    await waitFor(() => expect(row).toHaveTextContent('petr'));
    await act(async () => { releaseFirst(json(200, { items: [{ userId: 'u-2', login: 'obsolete', role: 'member' }], nextCursor: null })); });
    expect(row).toHaveTextContent('petr');
    expect(row).not.toHaveTextContent('obsolete');
  });

  it('resolves home assignees from a later members page', async () => {
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, [item({ responsibleUserId: 'u-2' })]), [subjectList]: subjects, [memberList]: (request: RecordedRequest) => request.query.get('cursor') === 'later' ? json(200, { items: [secondMember], nextCursor: null }) : json(200, { items: [ownerMember], nextCursor: 'later' }) });
    app();
    const title = await screen.findByText('Renew passport');
    await waitFor(() => expect(title.closest('li')).toHaveTextContent('petr'));
    expect(requestsOf(fake.requests, 'GET', `${base}/members`).map((request) => request.query.get('cursor'))).toEqual([null, 'later']);
  });

  it('stops an endless home members cursor chain at 20 pages without unknown metadata', async () => {
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, [item({ responsibleUserId: 'u-2' })]), [subjectList]: subjects, [memberList]: (request: RecordedRequest) => json(200, { items: [], nextCursor: `cursor-${request.query.get('cursor') ?? 'start'}` }) });
    app();
    const title = await screen.findByText('Renew passport');
    const row = title.closest('li')!;
    await waitFor(() => expect(requestsOf(fake.requests, 'GET', `${base}/members`)).toHaveLength(20));
    expect(row).not.toHaveTextContent('Unknown member');
    expect(row).not.toHaveTextContent('petr');
  });

  it('does not restart the home members walk after Done refresh or locale change', async () => {
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, [item()]), [subjectList]: subjects, [memberList]: memberPage(), [`GET ${base}/items/i-1`]: json(200, item(), { ETag: '"7"' }), [`POST ${donePath}`]: json(201, { id: 'c-locale', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null }) });
    app();
    await screen.findByText('Renew passport');
    await waitFor(() => expect(requestsOf(fake.requests, 'GET', `${base}/members`)).toHaveLength(1));
    await userEvent.click(screen.getByRole('button', { name: /Done:/ }));
    await screen.findByText('“Renew passport” is done.');
    await waitFor(() => expect(requestsOf(fake.requests, 'GET', `${base}/items/summary`)).toHaveLength(2));
    await userEvent.click(screen.getByRole('button', { name: 'Čeština' }));
    expect(screen.getByRole('heading', { name: 'Veselí' })).toBeVisible();
    expect(requestsOf(fake.requests, 'GET', `${base}/members`)).toHaveLength(1);
  });

  it('gives each secondary Done button a title-inclusive accessible name', async () => {
    installFakeServer({ ...routes, ...itemRoutes(base, [item({ title: 'Renew passport' })]), [subjectList]: subjects });
    app();
    const title = await screen.findByText('Renew passport');
    const done = within(title.closest('li')!).getByRole('button', { name: /Done:/ });
    expect(done).toHaveTextContent('Done');
    expect(done).toHaveAttribute('data-variant', 'secondary');
  });

  it('reuses the unresolved key when the server committed but the response was lost', async () => {
    let posts = 0;
    const fake = installFakeServer({
      ...routes,
      ...itemRoutes(base, [item()]),
      [subjectList]: subjects,
      [`GET ${base}/items/i-1`]: json(200, item(), { ETag: '"7"' }),
      [`POST ${donePath}`]: (request) => {
        posts++;
        if (posts === 1) { JSON.parse(request.body); throw new Error('response lost after commit'); }
        return json(201, { id: 'c-replayed', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null });
      },
    });
    app();
    const done = await screen.findByRole('button', { name: /Done:/ });
    await userEvent.click(done);
    await screen.findByRole('button', { name: 'Try again' });
    await userEvent.click(screen.getByRole('button', { name: /Done:/ }));
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
      ...itemRoutes(base, [item({ recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' } })]),
      [subjectList]: subjects,
      [`GET ${base}/items/i-1`]: () => json(200, item({ recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' } }), { ETag: `"${7 + getCount++}"` }),
      [`POST ${donePath}`]: () => json(201, { id: `c-${postCount++}`, itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' }, nextAttentionOn: '2027-01-01', createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null }),
    });
    app();
    await userEvent.click(await screen.findByRole('button', { name: /Done:/ }));
    await screen.findByRole('button', { name: 'Undo' });
    await userEvent.click(screen.getByRole('button', { name: /Done:/ }));
    await waitFor(() => expect(requestsOf(fake.requests, 'POST', donePath)).toHaveLength(2));
    const posts = requestsOf(fake.requests, 'POST', donePath);
    expect(posts[0]!.headers.get('Idempotency-Key')).not.toBe(posts[1]!.headers.get('Idempotency-Key'));
    expect(posts.map((request) => request.headers.get('If-Match'))).toEqual(['"7"', '"8"']);
  });

  it('keeps a completion key when the item GET fails during retry', async () => {
    let gets = 0;
    const fake = installFakeServer({
      ...routes,
      ...itemRoutes(base, [item()]),
      [subjectList]: subjects,
      [`GET ${base}/items/i-1`]: () => ++gets === 1 ? json(200, item(), { ETag: '"7"' }) : gets === 2 ? json(503) : json(200, item(), { ETag: '"8"' }),
      [`POST ${donePath}`]: (request) => request.headers.get('If-Match') === '"7"' ? json(503) : json(503),
    });
    app();
    await userEvent.click(await screen.findByRole('button', { name: /Done:/ }));
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
      ...itemRoutes(base, [item()]),
      [subjectList]: json(200, { items: [], nextCursor: null }),
      [`GET ${base}/items/i-1`]: json(200, item(), { ETag: '"7"' }),
      [`POST ${donePath}`]: () => { if (++posts === 1) throw new Error('response lost'); return json(201, { id: 'c-nav', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null }); },
    });
    app();
    await userEvent.click(await screen.findByRole('button', { name: /Done:/ }));
    const key = requestsOf(fake.requests, 'POST', donePath)[0]!.headers.get('Idempotency-Key');
    await screen.findByRole('button', { name: 'Try again' });
    await userEvent.click(screen.getByRole('link', { name: 'People and things' }));
    await screen.findByRole('heading', { name: 'People and things' });
    await userEvent.click(screen.getByRole('link', { name: /^Home$/ }));
    await userEvent.click(await screen.findByRole('button', { name: /Done:/ }));
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
      ...itemRoutes(base, [item()]),
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
    await userEvent.click(await screen.findByRole('button', { name: /Done:/ }));
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
      ...itemRoutes(base, [item()]),
      [subjectList]: json(200, { items: [], nextCursor: null }),
      [`GET ${base}/items/i-1`]: json(200, item(), { ETag: '"7"' }),
      [`POST ${donePath}`]: () => { if (++posts === 1) throw new Error('response lost'); return json(201, { id: 'c-reload', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null }); },
    };
    const first = installFakeServer(server);
    app();
    await userEvent.click(await screen.findByRole('button', { name: /Done:/ }));
    const key = requestsOf(first.requests, 'POST', donePath)[0]!.headers.get('Idempotency-Key');
    cleanup();
    const reloaded = installFakeServer(server);
    app();
    await userEvent.click(await screen.findByRole('button', { name: /Done:/ }));
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
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, [item()]), [subjectList]: subjects, 'DELETE /api/v1/session': json(204), 'GET /api/v1/auth/setup': json(200, { required: false }) });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Sign out' }));
    expect(await screen.findByRole('heading', { name: 'Sign in to Tendo' })).toBeVisible();
    expect(sessionStorage.getItem(`tendo.pendingCompletion:${session().userId}:${HOUSEHOLD_ID}:i-1`)).toBeNull();
    expect(requestsOf(fake.requests, 'DELETE', '/api/v1/session')).toHaveLength(1);
  });

  it('keeps Add item focused after an undone receipt is replayed', async () => {
    installFakeServer({ ...routes, ...itemRoutes(base, [item()]), [subjectList]: subjects, [`GET ${base}/items/i-1`]: json(200, item(), { ETag: '"7"' }), [`POST ${donePath}`]: json(201, { id: 'c-undone', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: '2026-01-02T00:00:00Z', undoneByUserId: 'u-1' }) });
    app();
    await userEvent.click(await screen.findByRole('button', { name: /Done:/ }));
    expect(await screen.findByText('That was already undone. The list is up to date.')).toBeVisible();
    await waitFor(() => expect(screen.getByText('That was already undone. The list is up to date.').closest('[tabindex="-1"]')).toHaveFocus());
    expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument();
    expect(sessionStorage.getItem(`tendo.pendingCompletion:${session().userId}:${HOUSEHOLD_ID}:i-1`)).toBeNull();
  });

  it('keeps Add and Done available but form submit disabled while Done is pending', async () => {
    let release!: () => void;
    const waiting = new Promise<void>((resolve) => { release = resolve; });
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, [item()]), [subjectList]: subjects, [`GET ${base}/items/i-1`]: async () => { await waiting; return json(200, item(), { ETag: '"7"' }); }, [`POST ${donePath}`]: json(409, { type: 'about:blank', title: 'Conflict', status: 409, code: 'item_done' }), [`POST ${base}/items`]: json(201, item()) });
    app();
    await userEvent.click(await screen.findByRole('button', { name: /Done:/ }));
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
    expect(screen.getByRole('button', { name: /Done:/ })).toBeDisabled();
    release();
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Add item' })).toBeEnabled());
  });

  it('restores the row and focuses its Done action after undo', async () => {
    const fake = installFakeServer({
      ...routes,
      ...itemRoutes(base, [item()]),
      [subjectList]: subjects,
      [`GET ${base}/items/i-1`]: () => {
        const mutations = requestsOf(fake.requests, 'POST', donePath).length + requestsOf(fake.requests, 'PATCH', `${donePath}/c-focus`).length;
        return json(200, item(), { ETag: mutations === 0 ? '"7"' : '"8"' });
      },
      [`POST ${donePath}`]: json(201, { id: 'c-focus', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null }),
      [`PATCH ${donePath}/c-focus`]: json(200, { id: 'c-focus', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: '2026-01-01T00:00:00Z', undoneByUserId: 'u-1' }),
    });
    app();
    await userEvent.click(await screen.findByRole('button', { name: /Done:/ }));
    await userEvent.click(await screen.findByRole('button', { name: 'Undo' }));
    const done = await screen.findByRole('button', { name: /Done:/ });
    await waitFor(() => expect(done).toHaveFocus());
    const gets = requestsOf(fake.requests, 'GET', `${base}/items/i-1`);
    const patch = requestsOf(fake.requests, 'PATCH', `${donePath}/c-focus`)[0]!;
    expect(gets).toHaveLength(2);
    expect(patch.headers.get('If-Match')).toBe('"8"');
  });

  it('completes with fresh ETag and one idempotency key; retry reuses the key after network failure', async () => {
    let postCalls = 0;
    let itemReads = 0;
    const fake = installFakeServer({
      ...routes,
      ...itemRoutes(base, [item({ recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' } })]),
      [subjectList]: subjects,
      [`GET ${base}/items/i-1`]: () => json(200, item({ recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' } }), { ETag: `"${7 + itemReads++}"` }),
      [`POST ${donePath}`]: () => ++postCalls === 1 ? Promise.reject(new Error('network')) : json(201, { id: 'c-1', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' }, nextAttentionOn: '2027-01-01', createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null }),
    });
    app();
    await userEvent.click(await screen.findByRole('button', { name: /Done:/ }));
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
    expect(requestsOf(fake.requests, 'GET', `${base}/items/summary`).length).toBeGreaterThanOrEqual(2);
  });

  it('undoes latest completion with fresh ETag and exact body, then clears the notice', async () => {
    const fake = installFakeServer({
      ...routes,
      ...itemRoutes(base, [item()]),
      [subjectList]: subjects,
      [`GET ${base}/items/i-1`]: () => {
        const previousMutation = requestsOf(fake.requests, 'POST', donePath).length + requestsOf(fake.requests, 'PATCH', `${donePath}/c-undo`).length;
        return json(200, item(), { ETag: previousMutation === 0 ? '"9"' : '"10"' });
      },
      [`POST ${donePath}`]: json(201, { id: 'c-undo', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null }),
      [`PATCH ${donePath}/c-undo`]: json(200, { id: 'c-undo', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: '2026-01-01T00:00:00Z', undoneByUserId: 'u-1' }),
    });
    app();
    const doneButton = await screen.findByRole('button', { name: /Done:/ });
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
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, [item()]), [subjectList]: (request) => request.query.get('archived') === 'true' ? json(200, { items: [], nextCursor: null }) : subjects, [`GET ${base}/items/i-1`]: json(200, item(), { ETag: '"3"' }), [`POST ${donePath}`]: status === 409 ? json(409, { type: 'about:blank', title: 'Conflict', status: 409, code: 'item_done' }) : json(status), 'GET /api/v1/auth/setup': json(200, { required: false }) });
    app();
    await userEvent.click(await screen.findByRole('button', { name: /Done:/ }));
    expect(await screen.findByText(new RegExp(message))).toBeVisible();
    expect(requestsOf(fake.requests, 'GET', `${base}/items/summary`).length).toBeGreaterThanOrEqual(2);
    if (status === 409) expect(sessionStorage.getItem(`tendo.pendingCompletion:${session().userId}:${HOUSEHOLD_ID}:i-1`)).toBeNull();
  });

  it('clears the pending key when the item is gone during Done', async () => {
    installFakeServer({ ...routes, ...itemRoutes(base, [item()]), [subjectList]: subjects, [`GET ${base}/items/i-1`]: json(404) });
    app();
    await userEvent.click(await screen.findByRole('button', { name: /Done:/ }));
    await screen.findByText('That item is gone. The list is up to date.');
    expect(sessionStorage.getItem(`tendo.pendingCompletion:${session().userId}:${HOUSEHOLD_ID}:i-1`)).toBeNull();
  });

  it('clears a pending item create on sign out', async () => {
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: memberPage(), [`POST ${base}/items`]: () => Promise.reject(new Error('offline')), 'DELETE /api/v1/session': json(204), 'GET /api/v1/auth/setup': json(200, { required: false }) });
    app();
    const form = await openForm();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Signout pending');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await screen.findByRole('button', { name: 'Check again' });
    expect(sessionStorage.getItem(`tendo.pendingItemCreate.v1:${session().userId}:${HOUSEHOLD_ID}`)).not.toBeNull();
    await userEvent.click(await screen.findByRole('button', { name: 'Sign out' }));
    await screen.findByRole('heading', { name: 'Sign in to Tendo' });
    expect(sessionStorage.getItem(`tendo.pendingItemCreate.v1:${session().userId}:${HOUSEHOLD_ID}`)).toBeNull();
    expect(requestsOf(fake.requests, 'DELETE', '/api/v1/session')).toHaveLength(1);
  });

  it('clears an item create attempt when the server returns 401', async () => {
    installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: memberPage(), [`POST ${base}/items`]: json(401), 'GET /api/v1/auth/setup': json(200, { required: false }) });
    app();
    const form = await openForm();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Expired session');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await screen.findByRole('heading', { name: 'Sign in to Tendo' });
    expect(sessionStorage.getItem(`tendo.pendingItemCreate.v1:${session().userId}:${HOUSEHOLD_ID}`)).toBeNull();
  });

  it('returns to sign-in on 401 while completing', async () => {
    installFakeServer({ ...routes, ...itemRoutes(base, [item()]), [subjectList]: subjects, [`GET ${base}/items/i-1`]: json(401), 'GET /api/v1/auth/setup': json(200, { required: false }) });
    app();
    await userEvent.click(await screen.findByRole('button', { name: /Done:/ }));
    expect(await screen.findByRole('heading', { name: 'Sign in to Tendo' })).toBeVisible();
  });

  it('a terminal 422 shows a calm notice without a retry action', async () => {
    installFakeServer({ ...routes, ...itemRoutes(base, [item()]), [subjectList]: subjects, [`GET ${base}/items/i-1`]: json(200, item(), { ETag: '"7"' }), [`POST ${donePath}`]: json(422, { type: 'about:blank', title: 'Validation Failed', status: 422, code: 'future_date' }) });
    app();
    await userEvent.click(await screen.findByRole('button', { name: /Done:/ }));
    expect(await screen.findByText('A completion date cannot be in the future.')).toBeVisible();
    const notice = screen.getByText('A completion date cannot be in the future.').closest<HTMLElement>('[aria-live="polite"]')!;
    expect(within(notice).queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument();
    expect(sessionStorage.getItem(`tendo.pendingCompletion:${session().userId}:${HOUSEHOLD_ID}:i-1`)).toBeNull();
  });

  it('removes Undo when the completion is no longer latest', async () => {
    let itemReads = 0;
    const fake = installFakeServer({
      ...routes,
      ...itemRoutes(base, [item()]),
      [subjectList]: subjects,
      [`GET ${base}/items/i-1`]: () => {
        const reads = itemReads++;
        return json(200, item(), { ETag: reads === 0 ? '"7"' : '"8"' });
      },
      [`POST ${donePath}`]: json(201, { id: 'c-old', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null }),
      [`PATCH ${donePath}/c-old`]: json(409, { type: 'about:blank', title: 'Conflict', status: 409, code: 'completion_not_latest' }),
    });
    app();
    await userEvent.click(await screen.findByRole('button', { name: /Done:/ }));
    await userEvent.click(await screen.findByRole('button', { name: 'Undo' }));
    expect(await screen.findByText('Only the latest completion can be undone.')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Undo' })).not.toBeInTheDocument();
    const patch = requestsOf(fake.requests, 'PATCH', `${donePath}/c-old`)[0]!;
    expect(patch.headers.get('If-Match')).toBe('"8"');
  });

  it('maps a notes 422 to the expanded, focused accessible notes field', async () => {
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: memberPage(), [`POST ${base}/items`]: json(422, { type: 'about:blank', title: 'Validation Failed', status: 422, field: 'notes', code: 'invalid_characters' }) });
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

  it('resends the stored historical completion date verbatim after an uncertain create', async () => {
    let posts = 0;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: memberPage(), [`POST ${base}/items`]: (request) => ++posts === 1 ? Promise.reject(new Error('offline')) : json(201, item({ title: JSON.parse(request.body).title })) });
    app();
    const form = await openForm();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Historical retry');
    await userEvent.click(within(form).getByRole('switch', { name: 'Repeat' }));
    await userEvent.click(within(form).getByRole('button', { name: 'Add a previous completion' }));
    await userEvent.type(within(form).getByLabelText('Last completed on'), '2025-06-12');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await within(form).findByText('We couldn’t confirm this was saved.');
    const first = requestsOf(fake.requests, 'POST', `${base}/items`)[0]!;
    expect(JSON.parse(first.body).historicalCompletedOn).toBe('2025-06-12');
    await userEvent.click(within(form).getByRole('button', { name: 'Check again' }));
    await screen.findByText('“Historical retry” was added.');
    const postsSent = requestsOf(fake.requests, 'POST', `${base}/items`);
    expect(postsSent[1]!.body).toBe(first.body);
    expect(postsSent[1]!.headers.get('Idempotency-Key')).toBe(first.headers.get('Idempotency-Key'));
  });

  it('reveals historical completion only for repeating add forms and sends only the entered date', async () => {
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: memberPage(), [`POST ${base}/items`]: (request) => json(201, item({ title: JSON.parse(request.body).title })) });
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
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: memberPage(), [`POST ${base}/items`]: (request) => json(201, item({ title: JSON.parse(request.body).title })) });
    app();
    const form = await openForm();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Clean filter');
    await userEvent.click(within(form).getByRole('switch', { name: 'Repeat' }));
    await userEvent.click(within(form).getByRole('button', { name: 'Add a previous completion' }));
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await screen.findByText('“Clean filter” was added.');
    expect(JSON.parse(requestsOf(fake.requests, 'POST', `${base}/items`)[0]!.body)).not.toHaveProperty('historicalCompletedOn');

    cleanup();
    const second = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: memberPage(), [`POST ${base}/items`]: (request) => json(201, item({ title: JSON.parse(request.body).title })) });
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
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: memberPage(), [`POST ${base}/items`]: json(422, { type: 'about:blank', title: 'Validation Failed', status: 422, field: 'historicalCompletedOn', code }) });
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

  it('shows server summary counts and requests only populated groups with exact attention query params', async () => {
    const entries = Array.from({ length: 101 }, (_, index) => item({ id: `old-${index}`, title: `Created ${index}`, attention: 'upcoming', attentionOn: '2027-01-01' }));
    entries.push(item({ id: 'late-needs', title: 'Late needs item', attention: 'needs_attention', attentionOn: null }));
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, entries, { evaluatedOn: '2026-01-15' }), [subjectList]: subjects });
    app();
    await screen.findByText('Late needs item');
    const requests = requestsOf(fake.requests, 'GET', `${base}/items`);
    expect(requests).toHaveLength(2);
    expect(requests.map((request) => Object.fromEntries(request.query.entries()))).toEqual([
      { limit: '25', sort: 'attention_on', attentionDate: '2026-01-15', workflowState: 'open', attention: 'needs_attention' },
      { limit: '25', sort: 'attention_on', attentionDate: '2026-01-15', workflowState: 'open', attention: 'upcoming' },
    ]);
    expect(screen.getByRole('heading', { level: 2, name: 'Coming up 101 items' })).toBeVisible();
    expect(screen.getByRole('heading', { level: 2, name: 'Needs attention 1 item' })).toBeVisible();
    expect(requestsOf(fake.requests, 'GET', `${base}/items/summary`)[0]!.query.size).toBe(0);
  });

  it.each([false, true])('does not steal focus from another group while Show more resolves (final page %s)', async (finalPage) => {
    const aItems = Array.from({ length: finalPage ? 26 : 51 }, (_, index) => item({ id: `focus-a-${index}`, title: `Focus A ${index}` }));
    const bItems = Array.from({ length: 27 }, (_, index) => item({ id: `focus-b-${index}`, title: `Focus B ${index}`, workflowState: 'in_progress' }));
    const source = [...aItems, ...bItems];
    let resolveA!: (response: Response) => void;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, source), [subjectList]: subjects,
      [`GET ${base}/items`]: (request) => {
        if (request.query.get('workflowState') === 'open' && request.query.has('cursor')) return new Promise<Response>((resolve) => { resolveA = resolve; });
        if (request.query.get('workflowState') === 'open') return json(200, { items: aItems.slice(0, 25), nextCursor: 'c:25' });
        if (request.query.get('workflowState') === 'in_progress') return json(200, { items: bItems.slice(0, 25), nextCursor: 'c:25' });
        return json(200, { items: [], nextCursor: null });
      },
    });
    app();
    await screen.findByText('Focus A 0');
    const sectionA = screen.getByRole('heading', { level: 2, name: /^Needs attention/ }).closest('section')!;
    const sectionB = screen.getByRole('heading', { level: 2, name: /^In progress/ }).closest('section')!;
    const buttonA = within(sectionA).getByRole('button', { name: /Show more/ });
    const buttonB = within(sectionB).getByRole('button', { name: /Show more/ });
    buttonA.focus();
    await userEvent.keyboard('{Enter}');
    await waitFor(() => expect(buttonA).toHaveAttribute('aria-disabled', 'true'));
    buttonB.focus();
    await act(async () => resolveA(json(200, { items: finalPage ? aItems.slice(25) : aItems.slice(25, 50), nextCursor: finalPage ? null : 'c:50' })));
    await waitFor(() => expect(buttonB).toHaveFocus());
    expect(requestsOf(fake.requests, 'GET', `${base}/items`).filter((request) => request.query.has('cursor'))).toHaveLength(1);
  });

  it('blocks repeated click and Enter activation while a group page is loading', async () => {
    const entries = Array.from({ length: 27 }, (_, index) => item({ id: `repeat-${index}`, title: `Repeat ${index}` }));
    let resolvePage!: (response: Response) => void;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, entries), [subjectList]: subjects,
      [`GET ${base}/items`]: (request) => request.query.has('cursor') ? new Promise<Response>((resolve) => { resolvePage = resolve; }) : itemRoutes(base, entries)[`GET ${base}/items`]!(request),
    });
    app();
    await screen.findByText('Repeat 0');
    const button = screen.getByRole('button', { name: 'Show more (2 remaining)' });
    await userEvent.click(button);
    await waitFor(() => expect(button).toHaveAttribute('aria-disabled', 'true'));
    await userEvent.click(button);
    button.focus();
    await userEvent.keyboard('{Enter}');
    expect(requestsOf(fake.requests, 'GET', `${base}/items`).filter((request) => request.query.has('cursor'))).toHaveLength(1);
    await act(async () => resolvePage(json(200, { items: entries.slice(25), nextCursor: null })));
  });

  it('Done refresh moves a repeating item into a still-populated Coming up group', async () => {
    const completed = item({ recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' } });
    const alreadyUpcoming = item({ id: 'existing-up', title: 'Existing upcoming', attention: 'upcoming', attentionOn: '2027-02-01' });
    let completedNow = false;
    const source = () => [
      ...(completedNow ? [item({ ...completed, attention: 'upcoming', attentionOn: '2027-01-15' })] : [completed]),
      alreadyUpcoming,
    ];
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, source), [subjectList]: subjects,
      [`GET ${base}/items/i-1`]: json(200, completed, { ETag: '"7"' }),
      [`POST ${donePath}`]: () => { completedNow = true; return json(201, { id: 'c-moving', itemId: 'i-1', completedOn: '2026-01-15', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: completed.recurrence, nextAttentionOn: '2027-01-15', createdAt: '2026-01-15T00:00:00Z', undoneAt: null, undoneByUserId: null }); },
    });
    app();
    await screen.findByText('Renew passport');
    await userEvent.click(within(screen.getByText('Renew passport').closest('li')!).getByRole('button', { name: /Done:/ }));
    await screen.findByText('Renew passport');
    const heading = screen.getByRole('heading', { level: 2, name: 'Coming up 2 items' });
    expect(heading.closest('section')).toHaveTextContent('Existing upcoming');
    expect(heading.closest('section')).toHaveTextContent('Renew passport');
    const summaries = requestsOf(fake.requests, 'GET', `${base}/items/summary`);
    expect(summaries).toHaveLength(2);
    const upcoming = requestsOf(fake.requests, 'GET', `${base}/items`).find((request) => request.query.get('attention') === 'upcoming' && request.query.has('attentionDate'))!;
    expect(Object.fromEntries(upcoming.query.entries())).toEqual({ limit: '25', sort: 'attention_on', attentionDate: '2026-01-15', workflowState: 'open', attention: 'upcoming' });
  });

  it('deduplicates a continuation page containing an already loaded ID and updates page-two remaining count', async () => {
    const entries = Array.from({ length: 52 }, (_, index) => item({ id: `dedup-${index}`, title: `Dedup ${index}` }));
    let page = 0;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, entries), [subjectList]: subjects,
      [`GET ${base}/items`]: (request) => {
        if (!request.query.has('cursor')) return json(200, { items: entries.slice(0, 25), nextCursor: 'c:25' });
        if (request.query.get('cursor') === 'c:25') { page++; return json(200, { items: [entries[24]!, ...entries.slice(25, 49)], nextCursor: 'c:49' }); }
        page++; return json(200, { items: entries.slice(49), nextCursor: null });
      },
    });
    app();
    await screen.findByText('Dedup 0');
    let needs = screen.getByRole('heading', { level: 2, name: 'Needs attention 52 items' }).closest('section')!;
    await userEvent.click(within(needs).getByRole('button', { name: 'Show more (27 remaining)' }));
    await screen.findByText('Dedup 48');
    expect(within(needs).getAllByRole('listitem')).toHaveLength(49);
    expect(within(needs).getAllByText('Dedup 24')).toHaveLength(1);
    await userEvent.click(within(needs).getByRole('button', { name: 'Show more (3 remaining)' }));
    await screen.findByText('Dedup 51');
    needs = screen.getByRole('heading', { level: 2, name: 'Needs attention 52 items' }).closest('section')!;
    expect(within(needs).getAllByRole('listitem')).toHaveLength(52);
    expect(page).toBe(2);
    expect(requestsOf(fake.requests, 'GET', `${base}/items`).filter((request) => request.query.has('cursor'))).toHaveLength(2);
  });

  it('restarts a stale first page once with the fresh evaluatedOn and renders rows', async () => {
    let summaries = 0;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, [item()]), [subjectList]: subjects,
      [`GET ${base}/items/summary`]: () => json(200, { evaluatedOn: ++summaries === 1 ? '2026-01-15' : '2026-01-16', total: 1, needsAttention: 1, inProgress: 0, waiting: 0, upcoming: 0, paused: 0 }),
      [`GET ${base}/items`]: (request) => request.query.get('attentionDate') === '2026-01-15' ? json(400, { status: 400, code: 'invalid_query', parameter: 'attentionDate' }) : json(200, { items: [item()], nextCursor: null }),
    });
    app();
    await screen.findByText('Renew passport');
    const requests = requestsOf(fake.requests, 'GET', `${base}/items`);
    expect(requests.map((request) => request.query.get('attentionDate'))).toEqual(['2026-01-15', '2026-01-16']);
    expect(requestsOf(fake.requests, 'GET', `${base}/items/summary`)).toHaveLength(2);
  });

  it('fails after the second stale first-page response without making a third attempt', async () => {
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, [item()]), [subjectList]: subjects,
      [`GET ${base}/items/summary`]: (request) => json(200, { evaluatedOn: request.query.get('unused') ?? '2026-01-15', total: 1, needsAttention: 1, inProgress: 0, waiting: 0, upcoming: 0, paused: 0 }),
      [`GET ${base}/items`]: () => json(400, { status: 400, code: 'invalid_query', parameter: 'attentionDate' }),
    });
    app();
    await screen.findByText('Check your connection and try again.');
    expect(requestsOf(fake.requests, 'GET', `${base}/items/summary`)).toHaveLength(2);
    expect(requestsOf(fake.requests, 'GET', `${base}/items`)).toHaveLength(2);
  });

  it('stale during Show more starts one fresh load and recovers', async () => {
    const entries = Array.from({ length: 27 }, (_, index) => item({ id: `stale-more-${index}`, title: `Stale more ${index}` }));
    let firstCursor = true;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, entries), [subjectList]: subjects,
      [`GET ${base}/items`]: (request) => {
        if (request.query.has('cursor') && firstCursor) { firstCursor = false; return json(400, { status: 400, code: 'invalid_query', parameter: 'cursor' }); }
        return itemRoutes(base, entries)[`GET ${base}/items`]!(request);
      },
    });
    app();
    await screen.findByText('Stale more 0');
    await userEvent.click(screen.getByRole('button', { name: 'Show more (2 remaining)' }));
    await screen.findByText('Stale more 26');
    expect(requestsOf(fake.requests, 'GET', `${base}/items/summary`)).toHaveLength(2);
    expect(requestsOf(fake.requests, 'GET', `${base}/items`)).toHaveLength(3);
  });

  it('stale during Show more then stale on fresh load reports error with exact request counts', async () => {
    const entries = Array.from({ length: 27 }, (_, index) => item({ id: `stale-error-${index}`, title: `Stale error ${index}` }));
    let listCalls = 0;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, entries), [subjectList]: subjects,
      [`GET ${base}/items`]: (request) => { listCalls++; if (request.query.has('cursor')) return json(400, { status: 400, code: 'invalid_query', parameter: 'cursor' }); if (listCalls > 2) return json(400, { status: 400, code: 'invalid_query', parameter: 'attentionDate' }); return itemRoutes(base, entries)[`GET ${base}/items`]!(request); },
    });
    app();
    await screen.findByText('Stale error 0');
    await userEvent.click(screen.getByRole('button', { name: 'Show more (2 remaining)' }));
    await screen.findByText('Check your connection and try again.');
    expect(requestsOf(fake.requests, 'GET', `${base}/items/summary`)).toHaveLength(2);
    expect(requestsOf(fake.requests, 'GET', `${base}/items`)).toHaveLength(3);
  });

  it('signs out before restarting when group batch contains both stale and unauthorized results', async () => {
    let lists = 0;
    let summaries = 0;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, [item(), item({ id: 'up', attention: 'upcoming', attentionOn: '2027-01-01' })]), [subjectList]: subjects,
      [`GET ${base}/items/summary`]: () => { summaries++; return json(200, { evaluatedOn: '2026-01-15', total: 2, needsAttention: 1, upcoming: 1, inProgress: 0, waiting: 0, paused: 0, done: 0, archived: 0 }); },
      [`GET ${base}/items`]: (request) => { lists++; return request.query.get('attention') === 'upcoming' ? json(400, { status: 400, code: 'invalid_query', parameter: 'attentionDate' }) : json(401); },
    });
    const unsigned = await import('../../features/auth/sessionApi');
    vi.spyOn(unsigned, 'fetchSession').mockResolvedValue({ kind: 'signedIn', session: session() });
    render(<I18nProvider><App /></I18nProvider>);
    await waitFor(() => expect(requestsOf(fake.requests, 'GET', `${base}/items`)).toHaveLength(2));
    await waitFor(() => expect(screen.queryByRole('heading', { name: 'Sign in to Tendo' })).toBeVisible());
    expect(summaries).toBe(1);
    expect(lists).toBe(2);
    expect(requestsOf(fake.requests, 'GET', `${base}/items/summary`)).toHaveLength(1);
  });

  it('shows load failure without partial rows and retries successfully', async () => {
    let calls = 0;
    installFakeServer({ ...routes, ...itemRoutes(base, [item(), item({ id: 'other', workflowState: 'in_progress', attention: 'needs_attention' })]), [subjectList]: subjects,
      [`GET ${base}/items`]: (request) => request.query.get('workflowState') === 'in_progress' && calls++ === 0 ? json(503) : json(200, { items: request.query.get('workflowState') === 'in_progress' ? [item({ id: 'other', workflowState: 'in_progress', attention: 'needs_attention' })] : [item()], nextCursor: null }),
    });
    app();
    await screen.findByText('Check your connection and try again.');
    expect(screen.queryByText('Renew passport')).not.toBeInTheDocument();
    await userEvent.click(screen.getAllByRole('button', { name: 'Try again' })[0]!);
    await waitFor(() => expect(within(screen.getByRole('main')).getAllByRole('listitem')).toHaveLength(2));
    const rows = within(screen.getByRole('main')).getAllByRole('listitem');
    expect(rows).toHaveLength(2);
    rows.forEach((row) => expect(row).toHaveTextContent('Renew passport'));
  });

  it('keeps Show more focus during loading, then moves to the heading on the final page', async () => {
    const entries = Array.from({ length: 51 }, (_, index) => item({ id: `focus-${index}`, title: `Focus ${index}`, attention: 'upcoming', attentionOn: '2027-01-01' }));
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, entries), [subjectList]: subjects });
    app();
    await screen.findByText('Focus 0');
    const heading = screen.getByRole('heading', { level: 2, name: 'Coming up 51 items' });
    const button = screen.getByRole('button', { name: 'Show more (26 remaining)' });
    button.focus();
    await userEvent.keyboard('{Enter}');
    await waitFor(() => expect(screen.getByRole('button', { name: 'Show more (1 remaining)' })).toHaveFocus());
    await userEvent.keyboard('{Enter}');
    await waitFor(() => expect(heading).toHaveFocus());
    expect(requestsOf(fake.requests, 'GET', `${base}/items`).filter((request) => request.query.has('cursor'))).toHaveLength(2);
  });

  it.each([false, true])('keeps group B focus when group A resolves (final page: %s)', async (finalPage) => {
    const entries = [
      ...Array.from({ length: finalPage ? 26 : 51 }, (_, index) => item({ id: `a-${index}`, title: `Group A ${index}`, attention: 'needs_attention', attentionOn: null })),
      ...Array.from({ length: 27 }, (_, index) => item({ id: `b-${index}`, title: `Group B ${index}`, workflowState: 'in_progress', attention: 'needs_attention' })),
    ];
    let releaseA!: (response: Response) => void;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, entries), [subjectList]: subjects,
      [`GET ${base}/items`]: (request) => request.query.get('workflowState') === 'open' && request.query.has('cursor') ? new Promise<Response>((resolve) => { releaseA = resolve; }) : itemRoutes(base, entries)[`GET ${base}/items`]!(request),
    });
    app();
    await screen.findByText('Group A 0');
    const sectionA = screen.getByRole('heading', { level: 2, name: /^Needs attention/ }).closest('section')!;
    const sectionB = screen.getByRole('heading', { level: 2, name: /^In progress/ }).closest('section')!;
    const buttonA = within(sectionA).getByRole('button', { name: /Show more/ });
    const buttonB = within(sectionB).getByRole('button', { name: /Show more/ });
    buttonA.focus();
    await userEvent.keyboard('{Enter}');
    await waitFor(() => expect(buttonA).toHaveAttribute('aria-disabled', 'true'));
    buttonB.focus();
    await act(async () => releaseA(json(200, { items: finalPage ? [{ ...entries[0]!, id: 'a-25', title: 'Group A 25' }] : entries.slice(25, 50), nextCursor: finalPage ? null : 'c:50' })));
    await waitFor(() => expect(buttonB).toHaveFocus());
    expect(requestsOf(fake.requests, 'GET', `${base}/items`).filter((request) => request.query.has('cursor'))).toHaveLength(1);
  });

  it('ignores a deferred summary 401 after navigating away from home', async () => {
    let resolveSummary!: (response: Response) => void;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjects, [`GET ${base}/items/summary`]: () => new Promise<Response>((resolve) => { resolveSummary = resolve; }) });
    app();
    await waitFor(() => expect(resolveSummary).toBeDefined());
    await userEvent.click(screen.getByRole('link', { name: 'People and things' }));
    await act(async () => resolveSummary(json(401)));
    expect(await screen.findByRole('heading', { level: 1, name: 'People and things' })).toBeVisible();
    expect(screen.queryByRole('heading', { name: 'Sign in to Tendo' })).not.toBeInTheDocument();
    expect(requestsOf(fake.requests, 'GET', `${base}/items`)).toHaveLength(0);
  });

  it('ignores a deferred group 401 after navigating away from home', async () => {
    let resolveItems!: (response: Response) => void;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, [item()]), [subjectList]: subjects, [`GET ${base}/items`]: () => new Promise<Response>((resolve) => { resolveItems = resolve; }) });
    app();
    await waitFor(() => expect(resolveItems).toBeDefined());
    await userEvent.click(screen.getByRole('link', { name: 'People and things' }));
    await act(async () => resolveItems(json(401)));
    expect(await screen.findByRole('heading', { level: 1, name: 'People and things' })).toBeVisible();
    expect(screen.queryByRole('heading', { name: 'Sign in to Tendo' })).not.toBeInTheDocument();
    expect(requestsOf(fake.requests, 'GET', `${base}/items/summary`)).toHaveLength(1);
  });

  it('clears a stale Show more request on a newer failed load and ignores its late page', async () => {
    const entries = Array.from({ length: 27 }, (_, index) => item({ id: index === 0 ? 'i-1' : `race-${index}`, title: `Race ${index}` }));
    let releaseMore!: (response: Response) => void;
    let failReload = false;
    let readCalls = 0;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, entries), [subjectList]: subjects,
      [`GET ${base}/items`]: (request) => {
        if (request.query.has('cursor')) return new Promise<Response>((resolve) => { releaseMore = resolve; });
        if (failReload && ++readCalls === 1) return json(503);
        return itemRoutes(base, entries)[`GET ${base}/items`]!(request);
      },
      [`GET ${base}/items/i-1`]: json(200, item(), { ETag: '"7"' }),
      [`POST ${donePath}`]: json(201, { id: 'c-race', itemId: 'i-1', completedOn: '2026-01-15', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-15T00:00:00Z', undoneAt: null, undoneByUserId: null }),
    });
    app();
    await screen.findByText('Race 0');
    const more = screen.getByRole('button', { name: 'Show more (2 remaining)' });
    await userEvent.click(more);
    await waitFor(() => expect(more).toHaveAttribute('aria-disabled', 'true'));
    failReload = true;
    await userEvent.click(within(screen.getByText('Race 0').closest('li')!).getByRole('button', { name: 'Done: Race 0' }));
    await waitFor(() => expect(requestsOf(fake.requests, 'GET', `${base}/items/summary`)).toHaveLength(2));
    await act(async () => releaseMore(json(200, { items: entries.slice(25), nextCursor: null })));
    await waitFor(() => expect(more).toHaveAttribute('aria-disabled', 'false'));
    expect(within(more.closest('section')!).getAllByRole('listitem')).toHaveLength(25);
    await userEvent.click(screen.getAllByRole('button', { name: 'Try again' })[0]!);
    await screen.findByText('Race 26');
  });

  it('ignores a deferred Show more 401 after navigating away from home', async () => {
    const entries = Array.from({ length: 27 }, (_, index) => item({ id: `more401-${index}`, title: `More401 ${index}` }));
    let resolveMore!: (response: Response) => void;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, entries), [subjectList]: subjects, [`GET ${base}/items`]: (request) => request.query.has('cursor') ? new Promise<Response>((resolve) => { resolveMore = resolve; }) : itemRoutes(base, entries)[`GET ${base}/items`]!(request) });
    app();
    await screen.findByText('More401 0');
    await userEvent.click(screen.getByRole('button', { name: 'Show more (2 remaining)' }));
    await waitFor(() => expect(resolveMore).toBeDefined());
    await userEvent.click(screen.getByRole('link', { name: 'People and things' }));
    await act(async () => resolveMore(json(401)));
    expect(await screen.findByRole('heading', { level: 1, name: 'People and things' })).toBeVisible();
    expect(screen.queryByRole('heading', { name: 'Sign in to Tendo' })).not.toBeInTheDocument();
    expect(requestsOf(fake.requests, 'GET', `${base}/items`)).toHaveLength(2);
  });

  it('drops stale pagination response after a newer generation finishes', async () => {
    const entries = Array.from({ length: 27 }, (_, index) => item({ id: index === 0 ? 'i-1' : `generation-${index}`, title: `Generation ${index}` }));
    let resolveMore!: (response: Response) => void;
    let done = false;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, entries), [subjectList]: subjects,
      [`GET ${base}/items`]: (request) => request.query.has('cursor') ? new Promise<Response>((resolve) => { resolveMore = resolve; }) : json(200, { items: done ? entries.filter((entry) => entry.id !== 'i-1').slice(0, 25) : entries.slice(0, 25), nextCursor: 'c:25' }),
      [`GET ${base}/items/summary`]: () => json(200, { evaluatedOn: '2026-01-15', total: done ? 26 : 27, needsAttention: done ? 26 : 27, inProgress: 0, waiting: 0, upcoming: 0, paused: 0, done: 0, archived: 0 }),
      [`GET ${base}/items/i-1`]: json(200, item(), { ETag: '"7"' }),
      [`POST ${donePath}`]: () => { done = true; return json(201, { id: 'c-old-refresh', itemId: 'i-1', completedOn: '2026-01-15', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-15T00:00:00Z', undoneAt: null, undoneByUserId: null }); },
    });
    app();
    await screen.findByText('Generation 0');
    const more = screen.getByRole('button', { name: 'Show more (2 remaining)' });
    await userEvent.click(more);
    await waitFor(() => expect(more).toHaveAttribute('aria-disabled', 'true'));
    await userEvent.click(within(screen.getByText('Generation 0').closest('li')!).getByRole('button', { name: 'Done: Generation 0' }));
    await waitFor(() => expect(requestsOf(fake.requests, 'GET', `${base}/items/summary`)).toHaveLength(2));
    await act(async () => resolveMore(json(200, { items: entries.slice(25), nextCursor: null })));
    expect(within(more.closest('section')!).getAllByRole('listitem')).toHaveLength(25);
    expect(more).toHaveAttribute('aria-disabled', 'false');
  });

  it('calls the sign-out flow when the summary request is unauthorized', async () => {
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjects, [`GET ${base}/items/summary`]: json(401) });
    const sessionApi = await import('../../features/auth/sessionApi');
    vi.spyOn(sessionApi, 'fetchSession').mockResolvedValue({ kind: 'signedIn', session: session() });
    app();
    await screen.findByRole('heading', { name: 'Sign in to Tendo' });
    expect(requestsOf(fake.requests, 'GET', `${base}/items/summary`)).toHaveLength(1);
  });

  it('uses the summary zero counts to render the empty state', async () => {
    installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjects });
    app();
    expect(await screen.findByText('Nothing needs attention right now.')).toBeVisible();
  });

  it.each([{ count: 1, expected: '1 položka' }, { count: 3, expected: '3 položky' }, { count: 5, expected: '5 položek' }])('pluralizes Czech summary count $count', async ({ count, expected }) => {
    localStorage.setItem('tendo.locale', 'cs');
    const entries = Array.from({ length: count }, (_, index) => item({ id: `cs-${index}`, title: `Czech ${index}` }));
    installFakeServer({ ...routes, ...itemRoutes(base, entries), [subjectList]: subjects });
    app();
    expect(await screen.findByRole('heading', { level: 2, name: `Vyžaduje pozornost ${expected}` })).toBeVisible();
  });

  it('reloads summary and groups after Done mutation', async () => {
    let done = false;
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, [item()]), [subjectList]: subjects,
      [`GET ${base}/items/summary`]: () => json(200, { evaluatedOn: '2026-01-15', total: done ? 0 : 1, needsAttention: done ? 0 : 1, inProgress: 0, waiting: 0, upcoming: 0, paused: 0 }),
      [`GET ${base}/items`]: () => json(200, { items: done ? [] : [item()], nextCursor: null }),
      [`GET ${base}/items/i-1`]: json(200, item(), { ETag: '"7"' }),
      [`POST ${donePath}`]: () => { done = true; return json(201, { id: 'c-done', itemId: 'i-1', completedOn: '2026-01-15', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-15T00:00:00Z', undoneAt: null, undoneByUserId: null }); },
    });
    app();
    await userEvent.click(await screen.findByRole('button', { name: /Done:/ }));
    await screen.findByText('Nothing needs attention right now.');
    expect(requestsOf(fake.requests, 'GET', `${base}/items/summary`)).toHaveLength(2);
  });

  it('creates an item and focuses Add after success', async () => {
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages, [memberList]: memberPage(), [`POST ${base}/items`]: (request) => json(201, item({ title: JSON.parse(request.body).title })) });
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
    installFakeServer({ ...routes, ...itemRoutes(base, []), [subjectList]: subjectPages });
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
