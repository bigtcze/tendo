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
const page = (items: Item[], nextCursor: string | null = null) => json(200, { items, nextCursor });
function item(overrides: Partial<Item> = {}): Item {
  return { id: 'i-1', subjectId: 's-1', title: 'Renew passport', notes: null, attentionOn: null, recurrence: null, workflowState: 'open', attention: 'needs_attention', archived: false, done: false, lastCompletedOn: null, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z', ...overrides };
}
function app() { return render(<I18nProvider><App /></I18nProvider>); }
function requestsOf(requests: RecordedRequest[], method: string, path: string) { return requests.filter((request) => request.method === method && request.path === path); }
async function openForm() {
  await screen.findByRole('heading', { name: 'Veselí' });
  await userEvent.click(await screen.findByRole('button', { name: 'Add item' }));
  return screen.findByRole('form', { name: 'Add an item' });
}
beforeEach(() => vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']));
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.unstubAllEnvs(); vi.restoreAllMocks(); localStorage.removeItem('tendo.locale'); });

describe('item creation', () => {
  it('defaults Repeat off and sends an exact one-off body', async () => {
    const fake = installFakeServer({ ...routes, [itemList]: page([]), [subjectList]: subjects, [`POST ${base}/items`]: (request) => json(201, item({ title: JSON.parse(request.body).title })) });
    app();
    const form = await openForm();
    expect(within(form).getByRole('switch', { name: 'Repeat' })).not.toBeChecked();
    expect(within(form).queryByLabelText('Every')).not.toBeInTheDocument();
    expect(within(form).queryByRole('switch', { name: /Count the next repeat/ })).not.toBeInTheDocument();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Renew passport');
    await userEvent.click(within(form).getByRole('button', { name: 'Add item' }));
    await screen.findByText('“Renew passport” was added.');
    const post = requestsOf(fake.requests, 'POST', `${base}/items`)[0]!;
    expect(JSON.parse(post.body)).toEqual({ title: 'Renew passport', subjectId: 's-1', recurrence: null });
  });

  it.each([{ fluid: false, mode: 'fixed' }, { fluid: true, mode: 'after_completion' }])('creates repeat mode $mode with chosen interval', async ({ fluid, mode }) => {
    const fake = installFakeServer({ ...routes, [itemList]: page([]), [subjectList]: subjects, [`POST ${base}/items`]: (request) => json(201, item({ title: JSON.parse(request.body).title })) });
    app();
    const form = await openForm();
    await userEvent.type(within(form).getByLabelText('What needs doing?'), 'Check smoke alarm');
    await userEvent.click(within(form).getByRole('switch', { name: 'Repeat' }));
    expect(within(form).getByRole('spinbutton')).toBeVisible();
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

  it('maps a 422 title error to a focused accessible field', async () => {
    installFakeServer({ ...routes, [itemList]: page([]), [subjectList]: subjects, [`POST ${base}/items`]: json(422, { type: 'about:blank', title: 'Validation Failed', status: 422, field: 'title', code: 'invalid_length' }) });
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
    expect(rows.map((row) => row.querySelector('p')?.textContent)).toEqual(['Need now', 'Started', 'Waiting item', 'Future item', 'Paused item']);
  });

  it('completes with fresh ETag and one idempotency key; retry reuses the key after network failure', async () => {
    let listCalls = 0;
    let postCalls = 0;
    const fake = installFakeServer({
      ...routes,
      [itemList]: () => page(++listCalls === 1 ? [item({ recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' } })] : [item({ attention: 'upcoming', attentionOn: '2027-01-01', recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' } })]),
      [subjectList]: subjects,
      [`GET ${base}/items/i-1`]: json(200, item({ recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' } }), { ETag: '"7"' }),
      [`POST ${donePath}`]: () => ++postCalls === 1 ? Promise.reject(new Error('network')) : json(201, { id: 'c-1', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: { intervalValue: 1, intervalUnit: 'year', mode: 'fixed' }, nextAttentionOn: '2027-01-01', createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null }),
    });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    expect(await screen.findByText('That didn’t work. Check your connection and try again.')).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByText(/Next time: Jan 1, 2027/)).toBeVisible();
    const posts = requestsOf(fake.requests, 'POST', donePath);
    expect(posts).toHaveLength(2);
    expect(JSON.parse(posts[0]!.body)).toEqual({});
    expect(posts[0]!.headers.get('If-Match')).toBe('"7"');
    const key = posts[0]!.headers.get('Idempotency-Key')!;
    expect(key).toMatch(/^[0-9a-f]{32}$/);
    expect(posts[1]!.headers.get('Idempotency-Key')).toBe(key);
    expect(requestsOf(fake.requests, 'GET', `${base}/items/i-1`)).toHaveLength(2);
    expect(listCalls).toBeGreaterThanOrEqual(2);
  });

  it('undoes latest completion with fresh ETag and exact body, then clears the notice', async () => {
    const fake = installFakeServer({
      ...routes,
      [itemList]: page([item()]),
      [subjectList]: subjects,
      [`GET ${base}/items/i-1`]: json(200, item(), { ETag: '"9"' }),
      [`POST ${donePath}`]: json(201, { id: 'c-undo', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: null, undoneByUserId: null }),
      [`PATCH ${donePath}/c-undo`]: json(200, { id: 'c-undo', itemId: 'i-1', completedOn: '2026-01-01', completedByUserId: 'u-1', cycleAttentionOn: null, recurrence: null, nextAttentionOn: null, createdAt: '2026-01-01T00:00:00Z', undoneAt: '2026-01-01T00:00:00Z', undoneByUserId: 'u-1' }),
    });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    expect(await screen.findByText('“Renew passport” is done.')).toBeVisible();
    // The Done button that had focus is gone with the row; focus lands on the notice, not the page body.
    expect(document.activeElement).not.toBe(document.body);
    await userEvent.click(screen.getByRole('button', { name: 'Undo' }));
    await waitFor(() => expect(screen.queryByText('“Renew passport” is done.')).not.toBeInTheDocument());
    const patch = requestsOf(fake.requests, 'PATCH', `${donePath}/c-undo`)[0]!;
    expect(JSON.parse(patch.body)).toEqual({ undone: true });
    expect(patch.headers.get('If-Match')).toBe('"9"');
  });

  it.each([{ status: 412, message: 'That changed just now.' }, { status: 409, message: 'already done or can’t be completed' }])('shows a calm $status completion notice and refreshes', async ({ status, message }) => {
    let lists = 0;
    installFakeServer({ ...routes, [itemList]: () => { lists++; return page([item()]); }, [subjectList]: (request) => request.query.get('archived') === 'true' ? json(200, { items: [], nextCursor: null }) : subjects, [`GET ${base}/items/i-1`]: json(200, item(), { ETag: '"3"' }), [`POST ${donePath}`]: json(status), 'GET /api/v1/auth/setup': json(200, { required: false }) });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    expect(await screen.findByText(new RegExp(message))).toBeVisible();
    expect(lists).toBeGreaterThanOrEqual(2);
  });

  it('returns to sign-in on 401 while completing', async () => {
    installFakeServer({ ...routes, [itemList]: page([item()]), [subjectList]: subjects, [`GET ${base}/items/i-1`]: json(401), 'GET /api/v1/auth/setup': json(200, { required: false }) });
    app();
    await userEvent.click(await screen.findByRole('button', { name: 'Done' }));
    expect(await screen.findByRole('heading', { name: 'Sign in to Tendo' })).toBeVisible();
  });

  it('shows the item copy in Czech', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer({ ...routes, [itemList]: page([]), [subjectList]: subjects });
    app();
    await screen.findByRole('heading', { name: 'Veselí' });
    await waitFor(() => expect(screen.getByRole('button', { name: 'Přidat položku' })).toBeVisible());
  });
});
