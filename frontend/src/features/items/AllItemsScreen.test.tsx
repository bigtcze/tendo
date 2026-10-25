import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
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
// The persistent outcome region is the only atomic status on the screen; loading and stale notices are not.
function outcomeRegion() { const region = document.querySelector<HTMLElement>('main [role="status"][aria-atomic="true"]'); if (!region) throw new Error('outcome region missing'); return region; }
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
    ['active', { archived: 'false', done: 'false' }],
    ['needs', { workflowState: 'open', attention: 'needs_attention' }],
    ['inProgress', { workflowState: 'in_progress' }], ['waiting', { workflowState: 'waiting' }],
    ['upcoming', { workflowState: 'open', attention: 'upcoming' }], ['paused', { workflowState: 'paused' }],
    ['done', { done: 'true', archived: 'false' }], ['archived', { archived: 'true' }],
  ])('uses exact query fields for %s with all common filters; summary excludes status', async (status, expected) => {
    window.history.replaceState(null, '', `/items?status=${status}&q=care&subject=s-1&responsible=${member.userId}&recurring=false`);
    const fake = installFakeServer({ ...bootRoutes, ...itemRoutes(base, [makeItem(1)]) });
    app();
    await screen.findByRole('heading', { name: 'All items' });
    await waitFor(() => expect(reqs(fake.requests, `${base}/items`).at(-1)?.query.has('attentionDate')).toBe(true));
    const summary = reqs(fake.requests, `${base}/items/summary`).at(-1)!;
    const list = reqs(fake.requests, `${base}/items`).at(-1)!;
    expect(summary.query.toString()).toBe(`q=care&subjectId=s-1&responsibleUserId=${member.userId}&recurring=false`);
    expect(summary.query.has('workflowState')).toBe(false);
    expect(summary.query.has('done')).toBe(false);
    expect(summary.query.has('archived')).toBe(false);
    expect(list.query.get('limit')).toBe('25');
    expect(list.query.get('sort')).toBe('attention_on');
    expect(list.query.get('attentionDate')).toBe('2026-01-15');
    expect(list.query.get('q')).toBe('care');
    expect(list.query.get('subjectId')).toBe('s-1');
    expect(list.query.get('responsibleUserId')).toBe(member.userId);
    expect(list.query.get('recurring')).toBe('false');
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
    expect(await screen.findByText('No items match.', { selector: 'div p.font-display' })).toBeInTheDocument();
    expect(window.location.search).toBe('?subject=unknown-id');
    expect(screen.getByLabelText('Show')).toHaveValue('active');
    expect(screen.getByLabelText('For')).toHaveValue('unknown-id');
    expect(reqs(fake.requests, `${base}/items/summary`).at(-1)?.query.toString()).toBe('subjectId=unknown-id');
  });

  it('hides failed facets while search still works and keeps unknown subject filters clearable', async () => {
    const routes = { ...bootRoutes, [subjectsPath]: json(500, {}), [membersPath]: json(500, {}) };
    const fake = installFakeServer({ ...routes, ...itemRoutes(base, [makeItem(1)]) });
    app();
    await userEvent.click(await screen.findByRole('link', { name: 'All items' }));
    expect(await screen.findByRole('heading', { name: 'All items' })).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByLabelText('For')).not.toBeInTheDocument());
    expect(screen.queryByLabelText("Who's looking after it")).not.toBeInTheDocument();
    await userEvent.type(screen.getByRole('searchbox'), 'Item');
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));
    expect(await screen.findByText('Item 1')).toBeInTheDocument();
    await waitFor(() => expect(outcomeRegion()).toHaveTextContent('Showing 1–1 of 1'));
    expect(reqs(fake.requests, `${base}/items`).at(-1)?.query.get('q')).toBe('Item');

    cleanup();
    window.history.replaceState(null, '', '/');
    window.history.replaceState(null, '', '/items?subject=unknown-subject');
    const unknown = installFakeServer({ ...bootRoutes, ...itemRoutes(base, [makeItem(1)]) });
    app();
    expect(await screen.findByRole('option', { name: 'Unknown' })).toBeInTheDocument();
    expect(screen.getByLabelText('For')).toHaveValue('unknown-subject');
    await userEvent.click(screen.getByRole('button', { name: 'Clear search and filters' }));
    expect(window.location.pathname + window.location.search).toBe('/items');
    expect(unknown.requests.length).toBeGreaterThan(0);
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
    expect(await screen.findByRole('alert')).toHaveTextContent('Search needs to be 200 characters or fewer.');
    expect(input).toHaveFocus();
    expect(input).toHaveAttribute('aria-invalid', 'true');
    expect(input).toHaveAttribute('aria-describedby', 'all-items-search-error');
    const firstAlert = screen.getByRole('alert');
    await userEvent.type(input, '{Enter}');
    expect(screen.getByRole('alert')).not.toBe(firstAlert);
    expect(screen.getByRole('alert')).toHaveTextContent('Search needs to be 200 characters or fewer.');
    expect(reqs(fake.requests, `${base}/items`).length).toBe(before);
  });

  it('shows the repeat mode next to the interval on result rows', async () => {
    await openAllItems([
      makeItem(1, { title: 'Fixed result', recurrence: { intervalUnit: 'week', intervalValue: 2, mode: 'fixed' } }),
      makeItem(2, { title: 'Fluid result', recurrence: { intervalUnit: 'day', intervalValue: 1, mode: 'after_completion' } }),
    ]);
    const main = screen.getByRole('main');
    expect(within(within(main).getByText('Fixed result').closest('li')!).getByText('Repeats every 2 weeks · keeps planned dates')).toBeVisible();
    expect(within(within(main).getByText('Fluid result').closest('li')!).getByText('Repeats every 1 day · counted from when it’s done')).toBeVisible();
  });

  it('names the search landmark and the results region', async () => {
    await openAllItems([makeItem(1, { title: 'Named result' })]);
    await waitFor(() => expect(document.title).toBe('All items — Tendo'));
    const search = screen.getByRole('search', { name: 'Search items' });
    expect(within(search).getByRole('searchbox')).toBeVisible();
    expect(within(screen.getByRole('region', { name: 'Results' })).getByText('Named result')).toBeVisible();
  });

  it('words the attention date by state, never repeating "Needs attention" without "since"', async () => {
    await openAllItems([
      makeItem(1, { title: 'Overdue result', attentionOn: '2025-11-18', attention: 'needs_attention' }),
      makeItem(2, { title: 'Upcoming result', attentionOn: '2026-02-15', attention: 'upcoming' }),
      makeItem(3, { title: 'Paused result', attentionOn: '2026-03-01', attention: 'upcoming', workflowState: 'paused' }),
    ]);
    const row = (title: string) => within(within(screen.getByRole('main')).getByText(title).closest('li')!);
    expect(row('Overdue result').getByText('Needs attention since Nov 18, 2025')).toBeVisible();
    expect(row('Upcoming result').getByText('Needs attention Feb 15, 2026')).toBeVisible();
    expect(row('Paused result').getByText('Attention date Mar 1, 2026')).toBeVisible();
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

  it('restores page three after reload and Previous returns to page two', async () => {
    const items = Array.from({ length: 80 }, (_, index) => makeItem(index + 1));
    await openAllItems(items);
    await userEvent.click(screen.getByRole('button', { name: 'Next page' }));
    await userEvent.click(screen.getByRole('button', { name: 'Next page' }));
    const pageThree = [...document.querySelectorAll('[data-item-id]')].map((row) => row.textContent);
    expect(pageThree.some((row) => row.startsWith('Item 51'))).toBe(true);
    const state = window.history.state;
    const href = window.location.pathname + window.location.search;
    cleanup();
    window.history.replaceState(state, '', href);
    const fake = installFakeServer({ ...bootRoutes, ...itemRoutes(base, items) });
    app();
    await waitFor(() => expect([...document.querySelectorAll('[data-item-id]')].map((row) => row.textContent)).toEqual(pageThree));
    await userEvent.click(screen.getByRole('button', { name: 'Previous page' }));
    expect(await screen.findByText('Item 26')).toBeInTheDocument();
    expect(window.location.search).toContain('cursor=c%3A25');
    expect(fake.requests.some((request) => request.query.get('cursor') === 'c:50')).toBe(true);
  });

  it('shows First page when a deep-linked cursor has no cursor stack', async () => {
    window.history.replaceState(null, '', '/items?cursor=c%3A25');
    const fake = installFakeServer({ ...bootRoutes, ...itemRoutes(base, Array.from({ length: 40 }, (_, index) => makeItem(index + 1))) });
    app();
    expect(await screen.findByText('Item 26')).toBeInTheDocument();
    expect(await screen.findByRole('button', { name: 'First page' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'First page' }));
    expect(await screen.findByText('Item 1')).toBeInTheDocument();
    expect(fake.requests.some((request) => request.query.get('cursor') === 'c:0')).toBe(false);
  });

  it('reaches done and archived records from their status choices', async () => {
    const fake = await openAllItems([makeItem(1, { done: true }), makeItem(2, { archived: true })]);
    await userEvent.selectOptions(screen.getByLabelText('Show'), 'done');
    expect(await screen.findByText('Item 1')).toBeInTheDocument();
    await userEvent.selectOptions(screen.getByLabelText('Show'), 'archived');
    expect(await screen.findByText('Item 2')).toBeInTheDocument();
    await waitFor(() => expect(outcomeRegion()).toHaveTextContent('Showing 1–1 of 1'));
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
    const rowIds = () => [...document.querySelectorAll('[data-item-id]')].map((row) => row.getAttribute('data-item-id'));
    expect(await screen.findByRole('heading', { level: 2, name: 'Showing 1–25 of 100' })).toBeInTheDocument();
    const pages = [rowIds()];
    for (const range of ['26–50', '51–75', '76–100']) {
      await userEvent.click(await screen.findByRole('button', { name: 'Next page' }));
      expect(await screen.findByRole('heading', { level: 2, name: `Showing ${range} of 100` })).toBeInTheDocument();
      expect(screen.getByRole('button', { name: 'Previous page' })).toBeInTheDocument();
      expect(document.querySelectorAll('[data-item-id]').length).toBeLessThanOrEqual(25);
      pages.push(rowIds());
    }
    expect(new Set(pages.flat()).size).toBe(100);
    expect(document.activeElement).toHaveAttribute('tabindex', '-1');
    for (const [index, range] of [[2, '51–75'], [1, '26–50'], [0, '1–25']] as const) {
      await userEvent.click(screen.getByRole('button', { name: 'Previous page' }));
      expect(await screen.findByRole('heading', { level: 2, name: `Showing ${range} of 100` })).toBeInTheDocument();
      expect(rowIds()).toEqual(pages[index]);
    }
    expect(screen.queryByRole('button', { name: 'Previous page' })).not.toBeInTheDocument();
    expect(new URLSearchParams(window.location.search).has('cursor')).toBe(false);
    await userEvent.click(screen.getByRole('button', { name: 'Next page' }));
    await userEvent.selectOptions(screen.getByLabelText('Repeat'), 'true');
    await waitFor(() => expect(new URLSearchParams(window.location.search).has('cursor')).toBe(false));
  });

  it('distinguishes household-empty and no-match states, and Clear resets filters', async () => {
    await openAllItems([]);
    expect(await screen.findByText('There are no items in this household yet.', { selector: 'p.font-display' })).toBeInTheDocument();
    await waitFor(() => expect(outcomeRegion()).toHaveTextContent('There are no items in this household yet.'));
    cleanup();
    window.history.replaceState(null, '', '/');
    const fake = await openAllItems([makeItem(1)]);
    await userEvent.type(screen.getByRole('searchbox'), 'missing');
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));
    expect(await screen.findByText('No items match.', { selector: 'div p.font-display' })).toBeInTheDocument();
    await waitFor(() => expect(outcomeRegion()).toHaveTextContent('No items match.'));
    await userEvent.click(screen.getByRole('button', { name: 'Clear search and filters' }));
    await screen.findByText('Item 1');
    expect(window.location.pathname + window.location.search).toBe('/items');
    expect(fake.requests.length).toBeGreaterThan(0);
  });

  it('holds the filtered All items reads until an archive in flight on detail settles, then shows the post-archive list', async () => {
    const archivedItem = makeItem(90, { title: 'Unique archive target' });
    let isArchived = false;
    let releasePatch!: () => void;
    const routes = itemRoutes(base, () => [{ ...archivedItem, archived: isArchived }]);
    const fake = installFakeServer({ ...bootRoutes, ...routes,
      [`GET ${base}/items/${archivedItem.id}`]: () => json(200, { ...archivedItem, archived: isArchived }, { ETag: '"1"' }),
      [`GET ${base}/items/${archivedItem.id}/completions`]: json(200, { items: [], nextCursor: null }),
      [`GET ${base}/subjects/s-1`]: json(200, subject),
      [`PATCH ${base}/items/${archivedItem.id}`]: () => new Promise<Response>((resolve) => { releasePatch = () => { isArchived = true; resolve(json(200, { ...archivedItem, archived: true }, { ETag: '"2"' })); }; }),
    });
    app();
    await userEvent.click(await screen.findByRole('link', { name: 'All items' }));
    await userEvent.type(screen.getByRole('searchbox'), 'Unique archive target');
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));
    await userEvent.click(await screen.findByRole('link', { name: 'Unique archive target' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Archive' }));
    await waitFor(() => expect(releasePatch).toBeDefined());
    const readsBeforeBack = fake.requests.length;
    await userEvent.click(await screen.findByRole('link', { name: 'Back to all items' }));
    await screen.findByRole('heading', { name: 'All items' });
    const filteredUrl = window.location.pathname + window.location.search;
    expect(filteredUrl).toContain('q=Unique+archive+target');
    // No list or summary read may leave while the archive is pending, and the pre-archive row never appears.
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(fake.requests.slice(readsBeforeBack).filter((request) => request.path === `${base}/items` || request.path === `${base}/items/summary`)).toHaveLength(0);
    expect(screen.queryByRole('link', { name: 'Unique archive target' })).not.toBeInTheDocument();
    expect(screen.getByRole('searchbox')).toHaveValue('Unique archive target');

    await act(async () => releasePatch());
    expect(await screen.findByText('No items match.', { selector: 'div p.font-display' })).toBeInTheDocument();
    await waitFor(() => expect(outcomeRegion()).toHaveTextContent('No items match.'));
    expect(window.location.pathname + window.location.search).toBe(filteredUrl);
    expect(screen.getByRole('option', { name: 'Archived (1)' })).toBeInTheDocument();
    expect(fake.requests.slice(readsBeforeBack).some((request) => request.path === `${base}/items/summary` && request.query.get('q') === 'Unique archive target')).toBe(true);
    expect(screen.queryByRole('link', { name: 'Unique archive target' })).not.toBeInTheDocument();
  });

  it('holds Home reads until an archive in flight on detail settles, so the archived item never reappears', async () => {
    const archivedItem = makeItem(91, { title: 'Home archive target' });
    let isArchived = false;
    let releasePatch!: () => void;
    const routes = itemRoutes(base, () => [{ ...archivedItem, archived: isArchived }]);
    const fake = installFakeServer({ ...bootRoutes, ...routes,
      [`GET ${base}/items/${archivedItem.id}`]: () => json(200, { ...archivedItem, archived: isArchived }, { ETag: '"1"' }),
      [`GET ${base}/items/${archivedItem.id}/completions`]: json(200, { items: [], nextCursor: null }),
      [`GET ${base}/subjects/s-1`]: json(200, subject),
      [`PATCH ${base}/items/${archivedItem.id}`]: () => new Promise<Response>((resolve) => { releasePatch = () => { isArchived = true; resolve(json(200, { ...archivedItem, archived: true }, { ETag: '"2"' })); }; }),
    });
    app();
    await userEvent.click(await screen.findByRole('link', { name: 'Open item Home archive target' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Archive' }));
    await waitFor(() => expect(releasePatch).toBeDefined());
    const readsBeforeHome = fake.requests.length;
    await userEvent.click(screen.getByRole('link', { name: 'Home' }));
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(fake.requests.slice(readsBeforeHome).filter((request) => request.path === `${base}/items` || request.path === `${base}/items/summary`)).toHaveLength(0);
    expect(screen.queryByRole('link', { name: 'Open item Home archive target' })).not.toBeInTheDocument();

    await act(async () => releasePatch());
    await waitFor(() => expect(fake.requests.slice(readsBeforeHome).some((request) => request.path === `${base}/items/summary`)).toBe(true));
    await waitFor(() => expect(screen.queryByRole('status', { name: /Loading/ })).not.toBeInTheDocument());
    expect(screen.queryByRole('link', { name: 'Open item Home archive target' })).not.toBeInTheDocument();
  });

  it('returns from detail to the exact filtered second page and restores its rows', async () => {
    const items = Array.from({ length: 60 }, (_, index) => makeItem(index + 1));
    const fake = await openAllItems(items);
    await userEvent.type(screen.getByRole('searchbox'), 'Item');
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));
    await userEvent.click(screen.getByRole('button', { name: 'Next page' }));
    const pageRows = [...document.querySelectorAll('[data-item-id]')].map((row) => row.textContent);
    const listUrl = window.location.pathname + window.location.search;
    await userEvent.click(await screen.findByRole('link', { name: 'Item 26' }));
    expect(await screen.findByRole('link', { name: 'Back to all items' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('link', { name: 'Back to all items' }));
    await screen.findByText('Item 26');
    expect(window.location.pathname + window.location.search).toBe(listUrl);
    expect([...document.querySelectorAll('[data-item-id]')].map((row) => row.textContent)).toEqual(pageRows);
    expect(fake.requests.some((request) => request.query.get('cursor') === 'c:25')).toBe(true);
  });

  it('All items navigation starts a clean first page and browser Back restores the filtered page', async () => {
    const items = Array.from({ length: 60 }, (_, index) => makeItem(index + 1));
    const fake = await openAllItems(items);
    await userEvent.type(screen.getByRole('searchbox'), 'Item');
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));
    await userEvent.selectOptions(screen.getByLabelText('Repeat'), 'false');
    await userEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(window.location.search).toContain('cursor=');
    const filteredUrl = window.location.pathname + window.location.search;

    const requestStart = fake.requests.length;
    await userEvent.click(screen.getByRole('link', { name: 'All items' }));
    await waitFor(() => expect(window.location.pathname + window.location.search).toBe('/items'));
    expect(screen.getByRole('searchbox')).toHaveValue('');
    expect(screen.getByLabelText('Show')).toHaveValue('active');
    expect(screen.getByLabelText('For')).toHaveValue('');
    expect(screen.getByLabelText('Repeat')).toHaveValue('');
    await screen.findByText('Item 1');
    const freshListRequests = reqs(fake.requests.slice(requestStart), `${base}/items`);
    expect(freshListRequests.length).toBeGreaterThan(0);
    expect(freshListRequests.at(-1)!.query.has('cursor')).toBe(false);
    expect(freshListRequests.at(-1)!.query.has('q')).toBe(false);
    expect(freshListRequests.at(-1)!.query.has('subjectId')).toBe(false);
    expect(freshListRequests.at(-1)!.query.has('responsibleUserId')).toBe(false);
    expect(freshListRequests.at(-1)!.query.has('recurring')).toBe(false);
    expect(freshListRequests.at(-1)!.query.get('archived')).toBe('false');
    expect(freshListRequests.at(-1)!.query.get('done')).toBe('false');

    await act(async () => { window.history.back(); });
    await waitFor(() => expect(window.location.pathname + window.location.search).toBe(filteredUrl));
    expect(await screen.findByRole('searchbox')).toHaveValue('Item');
    expect(screen.getByLabelText('Repeat')).toHaveValue('false');
    expect(await screen.findByText('Item 26')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('link', { name: 'All items' }));
    await screen.findByText('Item 1');
    await userEvent.click(screen.getByRole('link', { name: 'Item 1' }));
    await screen.findByRole('link', { name: 'Back to all items' });
    await userEvent.click(screen.getByRole('link', { name: 'Back to all items' }));
    await screen.findByText('Item 1');
    expect(window.location.pathname + window.location.search).toBe('/items');
    expect(screen.getByRole('searchbox')).toHaveValue('');
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

  it('never shows previous page rows under the next page range while it loads', async () => {
    const items = Array.from({ length: 60 }, (_, i) => makeItem(i + 1));
    const routes = itemRoutes(base, items);
    const listItems = routes[`GET ${base}/items`];
    let release: (() => void) | undefined;
    installFakeServer({ ...bootRoutes, ...routes, [`GET ${base}/items`]: (request: RecordedRequest) => {
      if (!request.query.has('cursor')) return listItems(request);
      return new Promise<Response>((resolve) => { release = () => resolve(listItems(request)); });
    } });
    app();
    await userEvent.click(await screen.findByRole('link', { name: 'All items' }));
    expect(await screen.findByRole('heading', { level: 2, name: 'Showing 1–25 of 60' })).toBeInTheDocument();
    // Synchronous click: assert the very first render after the context change, before the deferred load starts.
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(screen.queryByRole('heading', { level: 2, name: /Showing 26–50/ })).not.toBeInTheDocument();
    expect(screen.queryByText('Item 1')).not.toBeInTheDocument();
    await waitFor(() => expect(release).toBeDefined());
    expect(screen.queryByRole('heading', { level: 2, name: /Showing 26–50/ })).not.toBeInTheDocument();
    expect(screen.queryByText('Item 1')).not.toBeInTheDocument();
    await act(async () => { release!(); });
    expect(await screen.findByRole('heading', { level: 2, name: 'Showing 26–50 of 60' })).toBeInTheDocument();
    expect(screen.getByText('Item 26')).toBeInTheDocument();
    expect(screen.queryByText('Item 1')).not.toBeInTheDocument();
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
    await waitFor(() => expect(document.title).toBe('Všechny položky — Tendo'));
    expect(fake.requests.length).toBeGreaterThan(0);
    await userEvent.click(screen.getByRole('button', { name: 'English' }));
    await screen.findByRole('heading', { name: 'All items' });
    await waitFor(() => expect(document.title).toBe('All items — Tendo'));
  });

  it('does not rewrite a popped home URL when leaving the list route', async () => {
    await openAllItems([makeItem(1)]);
    window.history.pushState(null, '', '/');
    window.dispatchEvent(new PopStateEvent('popstate'));
    await screen.findByRole('heading', { name: 'Veselí' });
    expect(window.location.pathname).toBe('/');
    expect(screen.queryByRole('heading', { name: 'All items' })).not.toBeInTheDocument();
  });

  it('submits trimmed search and blocks more than 200 characters without a request', async () => {
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

  it('keeps a count-only heading while paging from a deep-linked cursor', async () => {
    window.history.replaceState(null, '', '/items?cursor=c%3A25');
    const items = Array.from({ length: 100 }, (_, index) => makeItem(index + 1));
    const fake = installFakeServer({ ...bootRoutes, ...itemRoutes(base, items) });
    app();
    await screen.findByRole('heading', { name: 'All items' });
    await waitFor(() => expect(screen.queryByText('Loading items…')).not.toBeInTheDocument());
    expect(screen.getByRole('heading', { name: '100 items' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(screen.getByRole('heading', { name: '100 items' })).toBeInTheDocument();
    expect(screen.queryByText(/Showing 26–50/)).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Previous page' }));
    expect(screen.getByRole('heading', { name: '100 items' })).toBeInTheDocument();
    expect(reqs(fake.requests, `${base}/items`).some((request) => request.query.get('cursor') === 'c:50')).toBe(true);
  });

  it('reaches archived completed items over multiple UI pages, topping up the first stream', async () => {
    const fixtures = Array.from({ length: 27 }, (_, index) => makeItem(index + 1, { archived: true, done: index >= 20 }));
    const fake = await openAllItems(fixtures);
    await userEvent.selectOptions(screen.getByLabelText('Show'), 'archived');
    await waitFor(() => expect(screen.getByRole('option', { name: 'Archived (27)' })).toBeInTheDocument());
    const ids = new Set<string>();
    for (let page = 0; page < 2; page++) {
      await waitFor(() => expect(document.querySelectorAll('[data-item-id]').length).toBeGreaterThan(0));
      const pageIds = [...document.querySelectorAll('[data-item-id]')].map((row) => row.getAttribute('data-item-id')!);
      expect(pageIds.length).toBeLessThanOrEqual(25);
      pageIds.forEach((id) => ids.add(id));
      if (page === 0) await userEvent.click(screen.getByRole('button', { name: 'Next page' }));
    }
    expect(ids.size).toBe(27);
    const archivedCalls = reqs(fake.requests, `${base}/items`).filter((request) => request.query.get('archived') === 'true');
    expect(archivedCalls.some((request) => request.query.get('done') === 'false')).toBe(true);
    expect(archivedCalls.some((request) => request.query.get('done') === 'true')).toBe(true);
  });

  it('invalidates a stale cursor response as soon as a filter changes', async () => {
    let release: ((response: Response) => void) | undefined;
    let pendingCursor = false;
    const items = Array.from({ length: 30 }, (_, i) => makeItem(i + 1));
    const routeSet = itemRoutes(base, items);
    const normalList = routeSet[`GET ${base}/items`]!;
    const fake = installFakeServer({ ...bootRoutes, ...routeSet, [`GET ${base}/items`]: (request) => {
      if (request.query.has('cursor') && !pendingCursor) {
        pendingCursor = true;
        return new Promise<Response>((resolve) => { release = resolve; });
      }
      return normalList(request);
    } });
    app();
    await userEvent.click(await screen.findByRole('link', { name: 'All items' }));
    await screen.findByRole('heading', { name: 'All items' });
    await waitFor(() => expect(screen.queryByText('Loading items…')).not.toBeInTheDocument());
    await userEvent.click(screen.getByRole('button', { name: 'Next page' }));
    await waitFor(() => expect(reqs(fake.requests, `${base}/items`).some((request) => request.query.has('cursor'))).toBe(true));
    await waitFor(() => expect(release).toBeDefined());
    fireEvent.change(screen.getByLabelText('Repeat'), { target: { value: 'true' } });
    const changedUrl = window.location.pathname + window.location.search;
    await act(async () => { release!(json(400, { parameter: 'cursor' })); });
    expect(window.location.pathname + window.location.search).toBe(changedUrl);
    expect(changedUrl).toContain('recurring=true');
    expect(screen.queryByText('The list changed, so we started from the top.')).not.toBeInTheDocument();
    expect(screen.queryByText('Item 1')).not.toBeInTheDocument();
  });

  it('does not steal focus if the user moves focus while paging is pending', async () => {
    const items = Array.from({ length: 30 }, (_, i) => makeItem(i + 1));
    const routes = itemRoutes(base, items);
    const listItems = routes[`GET ${base}/items`]!;
    let release: (() => void) | undefined;
    const fake = installFakeServer({ ...bootRoutes, ...routes, [`GET ${base}/items`]: (request: RecordedRequest) => request.query.has('cursor')
      ? new Promise<Response>((resolve) => { release = () => resolve(listItems(request)); })
      : listItems(request) });
    app();
    await userEvent.click(await screen.findByRole('link', { name: 'All items' }));
    await screen.findByRole('heading', { name: 'All items' });
    await waitFor(() => expect(screen.queryByText('Loading items…')).not.toBeInTheDocument());
    await userEvent.click(screen.getByRole('button', { name: 'Next page' }));
    await waitFor(() => expect(release).toBeDefined());
    screen.getByRole('searchbox').focus();
    await act(async () => { release!(); });
    await screen.findByText('Item 26');
    expect(screen.getByRole('searchbox')).toHaveFocus();
    expect(fake.requests.some((request) => request.query.get('cursor') === 'c:0')).toBe(false);
  });

  it('shows error without a loading status after a failed initial request, and recovers on Try again', async () => {
    let fail = true;
    const routeSet = itemRoutes(base, [makeItem(1)]);
    const normalList = routeSet[`GET ${base}/items`]!;
    installFakeServer({ ...bootRoutes, ...routeSet, [`GET ${base}/items`]: (request) => fail ? json(500, {}) : normalList(request) });
    app();
    await userEvent.click(await screen.findByRole('link', { name: 'All items' }));
    await screen.findByRole('heading', { name: 'All items' });
    expect(await screen.findByRole('button', { name: 'Try again' })).toBeInTheDocument();
    await waitFor(() => expect(outcomeRegion()).toHaveTextContent('Check your connection and try again.'));
    expect(screen.queryByText('Loading items…')).not.toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByText('Item 1')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument();
    expect(screen.queryByText('Loading items…')).not.toBeInTheDocument();
  });

  it('shows error without a loading status after a failed filter request', async () => {
    const routeSet = itemRoutes(base, [makeItem(1)]);
    const normalList = routeSet[`GET ${base}/items`]!;
    installFakeServer({ ...bootRoutes, ...routeSet, [`GET ${base}/items`]: (request) => request.query.get('q') === 'fail' ? json(500, {}) : normalList(request) });
    app();
    await userEvent.click(await screen.findByRole('link', { name: 'All items' }));
    expect(await screen.findByText('Item 1')).toBeInTheDocument();
    await userEvent.type(screen.getByRole('searchbox'), 'fail');
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));
    expect(await screen.findByRole('button', { name: 'Try again' })).toBeInTheDocument();
    expect(screen.queryByText('Loading items…')).not.toBeInTheDocument();
    expect(screen.queryByText('Item 1')).not.toBeInTheDocument();
  });

  it.each([25, 50])('offers no phantom Next page when exactly %i unfinished archived items exist and none are completed', async (count) => {
    const items = Array.from({ length: count }, (_, i) => makeItem(i + 1, { archived: true }));
    await openAllItems(items);
    await userEvent.selectOptions(screen.getByLabelText('Show'), 'archived');
    expect(await screen.findByRole('heading', { level: 2, name: `Showing 1–25 of ${count}` })).toBeInTheDocument();
    if (count === 50) {
      await userEvent.click(screen.getByRole('button', { name: 'Next page' }));
      expect(await screen.findByRole('heading', { level: 2, name: 'Showing 26–50 of 50' })).toBeInTheDocument();
    }
    expect(screen.queryByRole('button', { name: 'Next page' })).not.toBeInTheDocument();
  });

  it('treats a malformed archived cursor as stale and restarts from the first page', async () => {
    window.history.replaceState(null, '', '/items?status=archived&cursor=garbage');
    const fake = installFakeServer({ ...bootRoutes, ...itemRoutes(base, [makeItem(1, { archived: true })]) });
    app();
    expect(await screen.findByText('The list changed, so we started from the top.')).toBeInTheDocument();
    expect(await screen.findByText('Item 1')).toBeInTheDocument();
    expect(new URLSearchParams(window.location.search).has('cursor')).toBe(false);
    expect(reqs(fake.requests, `${base}/items`).some((request) => request.query.get('cursor') === 'garbage')).toBe(false);
  });
});
