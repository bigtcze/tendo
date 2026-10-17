import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, household, installFakeServer, json, session, type RecordedRequest } from '../../test/fakeServer';
import { itemRoutes } from '../../test/fakeItems';
import type { Item, Subject } from './itemsApi';

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const subjectsPath = `GET ${base}/subjects`;
const membersPath = `GET ${base}/members`;
const subject: Subject = { id: 's-1', name: 'Café', type: 'person', linkedUserId: null, archived: false, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' };
const member = { userId: session().userId, login: 'anna', role: 'owner' as const };
function makeItem(index: number, overrides: Partial<Item> = {}): Item {
  return { id: `00000000-0000-4000-8000-${String(index).padStart(12, '0')}`, subjectId: 's-1', responsibleUserId: null, title: `Item ${index}`, notes: null, attentionOn: null, recurrence: null, workflowState: 'open', attention: 'needs_attention', archived: false, done: false, lastCompletedOn: null, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z', ...overrides };
}
const bootRoutes = { 'GET /api/v1/session': json(200, session()), [`GET ${base}`]: json(200, household), [subjectsPath]: json(200, { items: [subject], nextCursor: null }), [membersPath]: json(200, { items: [member], nextCursor: null }) };
function app() { return render(<I18nProvider><App /></I18nProvider>); }
function reqs(requests: RecordedRequest[], path: string) { return requests.filter((request) => request.path === path); }
async function openAllItems(items: Item[] = [makeItem(1)]) {
  const fake = installFakeServer({ ...bootRoutes, ...itemRoutes(base, items) });
  app();
  if (window.location.pathname !== '/items') await userEvent.click(await screen.findByRole('link', { name: 'All items' }));
  await screen.findByRole('heading', { name: 'All items' });
  await waitFor(() => expect(screen.queryByRole('status', { name: 'Loading items…' })).not.toBeInTheDocument());
  return fake;
}
beforeEach(() => { window.history.replaceState(null, '', '/'); vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']); });
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); localStorage.removeItem('tendo.locale'); });

describe('All items', () => {
  it('offers navigation from home, loads summary before the active list, and shows truthful status counts', async () => {
    const all = [makeItem(1), makeItem(2, { workflowState: 'waiting' }), makeItem(3, { done: true }), makeItem(4, { archived: true })];
    const fake = await openAllItems(all);
    const summary = reqs(fake.requests, `${base}/items/summary`).at(-1)!;
    const list = reqs(fake.requests, `${base}/items`).at(-1)!;
    expect([...fake.requests].findIndex((request) => request.path.endsWith('/items/summary'))).toBeLessThan(fake.requests.findIndex((request) => request.path === `${base}/items` && request.query.has('attentionDate')));
    expect(summary.query.toString()).toBe('');
    expect(list.query.toString()).toBe('limit=25&sort=attention_on&attentionDate=2026-01-15&archived=false&done=false');
    const status = screen.getByLabelText('Show');
    expect(within(status).getByRole('option', { name: 'Active (2)' })).toBeInTheDocument();
    expect(within(status).getByRole('option', { name: 'Done (1)' })).toBeInTheDocument();
    expect(within(status).getByRole('option', { name: 'Archived (1)' })).toBeInTheDocument();
  });

  it.each([
    ['needs', { workflowState: 'open', attention: 'needs_attention' }],
    ['inProgress', { workflowState: 'in_progress' }], ['waiting', { workflowState: 'waiting' }],
    ['upcoming', { workflowState: 'open', attention: 'upcoming' }], ['paused', { workflowState: 'paused' }],
    ['done', { done: 'true', archived: 'false' }], ['archived', { archived: 'true' }],
  ])('uses exact query fields for %s and applies combined filters', async (status, expected) => {
    const fake = await openAllItems([makeItem(1)]);
    await userEvent.selectOptions(screen.getByLabelText('Show'), status);
    await userEvent.type(screen.getByRole('searchbox'), '  care  ');
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));
    await waitFor(() => expect(reqs(fake.requests, `${base}/items`).length).toBeGreaterThan(2));
    const summary = reqs(fake.requests, `${base}/items/summary`).at(-1)!;
    const list = reqs(fake.requests, `${base}/items`).at(-1)!;
    expect(summary.query.toString()).toBe('q=care');
    expect(list.query.get('q')).toBe('care');
    expect(list.query.get('status')).toBeNull();
    for (const [key, value] of Object.entries(expected)) expect(list.query.get(key)).toBe(value);
  });

  it('restores URL filters from a direct deep link on initial load', async () => {
    window.history.replaceState(null, '', '/items?status=done&q=care&recurring=false');
    const fake = installFakeServer({ ...bootRoutes, ...itemRoutes(base, [makeItem(1, { done: true, title: 'Care item' })]) });
    app();
    expect(await screen.findByText('Care item')).toBeInTheDocument();
    expect(screen.getByRole('searchbox')).toHaveValue('care');
    expect(screen.getByLabelText('Show')).toHaveValue('done');
    expect(screen.getByLabelText('Repeat')).toHaveValue('false');
    expect(reqs(fake.requests, `${base}/items`).at(-1)?.query.get('done')).toBe('true');
  });

  it('normalizes invalid URL values and restores valid deep-link filters', async () => {
    window.history.replaceState(null, '', '/items?status=bogus&recurring=maybe&q=%20%20&subject=unknown-id');
    const fake = installFakeServer({ ...bootRoutes, ...itemRoutes(base, [makeItem(1)]) });
    app();
    expect(await screen.findByText('No items match.')).toBeInTheDocument();
    expect(window.location.search).toBe('?subject=unknown-id');
    expect(screen.getByLabelText('Show')).toHaveValue('active');
    expect(screen.getByLabelText('For')).toHaveValue('unknown-id');
    expect(reqs(fake.requests, `${base}/items/summary`).at(-1)?.query.toString()).toBe('subjectId=unknown-id');
  });

  it('submits trimmed search, clears it, and blocks more than 200 characters without a request', async () => {
    const fake = await openAllItems([makeItem(1, { title: 'Garden care' })]);
    const input = screen.getByRole('searchbox');
    await userEvent.type(input, ' care ');
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));
    await waitFor(() => expect(window.location.search).toBe('?q=care'));
    const before = reqs(fake.requests, `${base}/items`).length;
    await userEvent.clear(input);
    await userEvent.type(input, 'x'.repeat(201));
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));
    expect(await screen.findByText('Search needs to be 200 characters or fewer.')).toBeInTheDocument();
    expect(reqs(fake.requests, `${base}/items`).length).toBe(before);
  });

  it('sends all combined filter values to both summary and list', async () => {
    const item = makeItem(1, { title: 'Café care', responsibleUserId: member.userId, recurrence: { intervalUnit: 'month', intervalValue: 1, mode: 'fixed' } });
    const fake = await openAllItems([item]);
    await userEvent.type(screen.getByRole('searchbox'), 'cafe');
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));
    await userEvent.selectOptions(screen.getByLabelText('For'), 's-1');
    await userEvent.selectOptions(screen.getByLabelText("Who's looking after it"), member.userId);
    await userEvent.selectOptions(screen.getByLabelText('Repeat'), 'true');
    await waitFor(() => expect(reqs(fake.requests, `${base}/items`).at(-1)?.query.get('recurring')).toBe('true'));
    const summary = reqs(fake.requests, `${base}/items/summary`).at(-1)!;
    const list = reqs(fake.requests, `${base}/items`).at(-1)!;
    for (const request of [summary, list]) {
      expect(request.query.get('q')).toBe('cafe');
      expect(request.query.get('subjectId')).toBe('s-1');
      expect(request.query.get('responsibleUserId')).toBe(member.userId);
      expect(request.query.get('recurring')).toBe('true');
    }
  });

  it('reaches done and archived records from their status choices', async () => {
    const fake = await openAllItems([makeItem(1, { done: true }), makeItem(2, { archived: true })]);
    await userEvent.selectOptions(screen.getByLabelText('Show'), 'done');
    expect(await screen.findByText('Item 1')).toBeInTheDocument();
    await userEvent.selectOptions(screen.getByLabelText('Show'), 'archived');
    expect(await screen.findByText('Item 2')).toBeInTheDocument();
    expect(reqs(fake.requests, `${base}/items`).some((request) => request.query.get('done') === 'true')).toBe(true);
    expect(reqs(fake.requests, `${base}/items`).some((request) => request.query.get('archived') === 'true')).toBe(true);
  });

  it('replaces page rows, preserves previous cursors, and focuses results only after paging', async () => {
    const items = Array.from({ length: 100 }, (_, i) => makeItem(i + 1));
    await openAllItems(items);
    const search = screen.getByRole('searchbox');
    search.focus();
    await userEvent.selectOptions(screen.getByLabelText('Show'), 'active');
    expect(screen.getByLabelText('Show')).toHaveFocus();
    for (let page = 0; page < 3; page++) await userEvent.click(await screen.findByRole('button', { name: 'Next page' }));
    expect(await screen.findByText('Item 76')).toBeInTheDocument();
    expect(document.activeElement).toHaveAttribute('tabindex', '-1');
    expect(document.querySelectorAll('[data-item-id]').length).toBeLessThanOrEqual(25);
    const thirdPageTitles = [...document.querySelectorAll('[data-item-id]')].map((row) => row.textContent);
    await userEvent.click(screen.getByRole('button', { name: 'Previous page' }));
    expect(document.querySelectorAll('[data-item-id]').length).toBe(25);
    expect([...document.querySelectorAll('[data-item-id]')].map((row) => row.textContent)).not.toEqual(thirdPageTitles);
    await userEvent.click(screen.getByRole('button', { name: 'Next page' }));
    await userEvent.selectOptions(screen.getByLabelText('Repeat'), 'true');
    await waitFor(() => expect(new URLSearchParams(window.location.search).has('cursor')).toBe(false));
  });

  it('distinguishes household-empty and no-match states, and Clear resets filters', async () => {
    await openAllItems([]);
    expect(await screen.findByText('There are no items in this household yet.')).toBeInTheDocument();
    cleanup();
    window.history.replaceState(null, '', '/');
    const fake = await openAllItems([makeItem(1)]);
    await userEvent.type(screen.getByRole('searchbox'), 'missing');
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));
    expect(await screen.findByText('No items match.')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Clear search and filters' }));
    await screen.findByText('Item 1');
    expect(window.location.pathname + window.location.search).toBe('/items');
    expect(fake.requests.length).toBeGreaterThan(0);
  });

  it('returns from item detail to the filtered page', async () => {
    await openAllItems([makeItem(1)]);
    await userEvent.type(screen.getByRole('searchbox'), 'Item');
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));
    await userEvent.click(await screen.findByRole('link', { name: 'Item 1' }));
    expect(await screen.findByRole('link', { name: 'Back to all items' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('link', { name: 'Back to all items' }));
    expect(await screen.findByText('Item 1')).toBeInTheDocument();
    expect(window.location.search).toBe('?q=Item');
  });

  it('restarts a stale cursor once, shows a notice, and errors if the first-page retry is stale too', async () => {
    let staleCount = 0;
    const fake = installFakeServer({ ...bootRoutes, ...itemRoutes(base, Array.from({ length: 30 }, (_, i) => makeItem(i + 1))), [`GET ${base}/items`]: (request) => {
      if (request.query.has('attentionDate') && request.query.has('cursor')) { staleCount++; return json(400, { parameter: 'cursor' }); }
      if (request.query.has('attentionDate')) { staleCount++; return json(400, { parameter: 'attentionDate' }); }
      return json(200, { items: [makeItem(1)], nextCursor: 'c:1' });
    } });
    window.history.replaceState({ allItemsCursors: ['c:0'] }, '', '/items?cursor=c:25');
    app();
    expect(await screen.findByText('The list changed, so we started from the top.')).toBeInTheDocument();
    expect(await screen.findByRole('button', { name: 'Try again' })).toBeInTheDocument();
    expect(staleCount).toBeGreaterThanOrEqual(2);
    expect(fake.requests.filter((request) => request.path === `${base}/items`).length).toBeGreaterThanOrEqual(2);
  });

  it('ignores stale responses after a quick filter change', async () => {
    let release: ((response: Response) => void) | undefined;
    let first = true;
    const fake = installFakeServer({ ...bootRoutes, ...itemRoutes(base, () => [makeItem(1, { title: 'Current new result' })]), [`GET ${base}/items/summary`]: (request) => {
      if (request.query.get('q') === 'old' && first) { first = false; return new Promise<Response>((resolve) => { release = resolve; }); }
      return json(200, { evaluatedOn: '2026-01-15', total: 1, needsAttention: 1, inProgress: 0, waiting: 0, upcoming: 0, paused: 0, done: 0, archived: 0 });
    } });
    app();
    await userEvent.click(await screen.findByRole('link', { name: 'All items' }));
    await screen.findByRole('heading', { name: 'All items' });
    const search = screen.getByRole('searchbox');
    await userEvent.type(search, 'old');
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));
    await waitFor(() => expect(release).toBeDefined());
    await userEvent.clear(search);
    await userEvent.type(search, 'new');
    const resolveOld = release!;
    await act(async () => {
      await userEvent.click(screen.getByRole('button', { name: 'Search' }));
      resolveOld(json(200, { evaluatedOn: '2026-01-15', total: 0, needsAttention: 0, inProgress: 0, waiting: 0, upcoming: 0, paused: 0, done: 0, archived: 0 }));
    });
    await screen.findByText('Current new result');
    await waitFor(() => expect(screen.getByText('Current new result')).toBeInTheDocument());
    expect(reqs(fake.requests, `${base}/items`).at(-1)?.query.get('q')).toBe('new');
  });

  it('signs out when the list endpoint returns 401', async () => {
    installFakeServer({ ...bootRoutes, [`GET ${base}/items/summary`]: json(200, { evaluatedOn: '2026-01-15', total: 1, needsAttention: 1, inProgress: 0, waiting: 0, upcoming: 0, paused: 0, done: 0, archived: 0 }), [`GET ${base}/items`]: json(401, {}) });
    app();
    await userEvent.click(await screen.findByRole('link', { name: 'All items' }));
    expect(await screen.findByLabelText('Password')).toBeInTheDocument();
  });

  it('supports Czech copy', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    const fake = installFakeServer({ ...bootRoutes, ...itemRoutes(base, [makeItem(1)]) });
    app();
    expect(await screen.findByRole('link', { name: 'Všechny položky' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('link', { name: 'Všechny položky' }));
    expect(await screen.findByRole('heading', { name: 'Všechny položky' })).toBeInTheDocument();
    expect(fake.requests.length).toBeGreaterThan(0);
  });
});
