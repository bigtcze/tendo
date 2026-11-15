import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, household, instanceAdminSession, installFakeServer, json, memberHousehold, session } from '../../test/fakeServer';
import { itemRoutes } from '../../test/fakeItems';
import type { components } from '../../../../api/generated/api';

type AdminHousehold = components['schemas']['AdminHousehold'];

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const listPath = 'GET /api/v1/admin/households';
const problem = (status: number, code?: string) => json(status, { type: 'about:blank', title: 'x', status, ...(code ? { code } : {}) });

const veseli: AdminHousehold = { id: '0198a2f0-0000-7000-8000-000000000011', name: 'Veselí', timezone: 'Europe/Prague', createdAt: '2026-10-06T10:00:00Z' };
const chata: AdminHousehold = { id: '0198a2f0-0000-7000-8000-000000000012', name: 'Chata', timezone: 'Europe/Prague', createdAt: '2026-11-01T09:00:00Z' };

function renderAt(routes: Parameters<typeof installFakeServer>[0], path = '/admin/households', viewer = instanceAdminSession()) {
  const fake = installFakeServer({
    'GET /api/v1/session': json(200, viewer),
    [`GET ${base}`]: json(200, household),
    ...itemRoutes(base, []),
    ...routes,
  });
  window.history.replaceState(null, '', path);
  render(<I18nProvider><App /></I18nProvider>);
  return fake;
}

function rows() {
  return [...screen.getByRole('main').querySelectorAll<HTMLElement>('li[data-install-household-id]')];
}
function requests() {
  return screen.queryAllByRole('main');
}

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

describe('installation household list', () => {
  it('holds the list place while the households load', async () => {
    let release!: (response: Response) => void;
    const pending = new Promise<Response>((resolve) => { release = resolve; });
    renderAt({ [listPath]: () => pending });
    const label = await screen.findByText('Loading households…');
    expect(label.closest('[role="status"]')).not.toBeNull();
    await act(async () => { release(json(200, { items: [], nextCursor: null })); await pending; });
    expect(await screen.findByText('No households to show.')).toBeVisible();
  });

  it('lists every household as a name link with its timezone and creation day', async () => {
    renderAt({ [listPath]: json(200, { items: [veseli, chata], nextCursor: null }) });
    await waitFor(() => expect(rows()).toHaveLength(2));
    const [first, second] = rows();
    const link = within(first).getByRole('link', { name: 'Veselí' });
    expect(link).toHaveAttribute('href', `/admin/households/${veseli.id}`);
    expect(first.querySelector('[data-install-household-meta]')).toHaveTextContent('Europe/Prague');
    expect(first.querySelector('[data-install-household-meta]')).toHaveTextContent(/Oct 6, 2026/);
    expect(second.querySelector('[data-install-household-meta]')).toHaveTextContent(/Nov 1, 2026/);
    expect(screen.getByRole('heading', { level: 2, name: 'Households' })).toBeVisible();
    expect(screen.getByRole('main')).toHaveTextContent('Every household in this installation. Open one to see who belongs to it.');
    // Read-only: the only controls on the page are the pager's own.
    expect(within(screen.getByRole('main')).queryAllByRole('button')).toHaveLength(0);
  });

  it('says there are no households as plain muted text', async () => {
    renderAt({ [listPath]: json(200, { items: [], nextCursor: null }) });
    const empty = await screen.findByText('No households to show.');
    expect(empty).toHaveClass('text-muted');
    expect(rows()).toHaveLength(0);
  });

  it('keeps a failed first read recoverable', async () => {
    let attempt = 0;
    const fake = renderAt({
      [listPath]: () => (++attempt === 1 ? problem(503) : json(200, { items: [veseli], nextCursor: null })),
    });
    expect(await screen.findByText('Households couldn’t be loaded. Check your connection and try again.')).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    await waitFor(() => expect(rows()).toHaveLength(1));
    expect(fake.requests.filter((request) => request.path === '/api/v1/admin/households')).toHaveLength(2);
  });

  it('pages forward and back without losing the household it showed', async () => {
    const fake = renderAt({
      [listPath]: (request) => (request.query.get('cursor')
        ? json(200, { items: [chata], nextCursor: null })
        : json(200, { items: [veseli], nextCursor: 'h2' })),
    });
    await waitFor(() => expect(rows()).toHaveLength(1));
    await userEvent.click(screen.getByRole('button', { name: 'Next page' }));
    await waitFor(() => expect(screen.getByRole('link', { name: 'Chata' })).toBeVisible());
    await userEvent.click(screen.getByRole('button', { name: 'First page' }));
    await waitFor(() => expect(screen.getByRole('link', { name: 'Veselí' })).toBeVisible());
    expect(fake.requests
      .filter((request) => request.path === '/api/v1/admin/households')
      .map((request) => request.query.get('cursor'))).toEqual([null, 'h2', null]);
    // The list heading takes focus after a page settles, so a keyboard reader is placed on the new page.
    await waitFor(() => expect(screen.getByRole('heading', { level: 2, name: 'Households' })).toHaveFocus());
  });

  it('keeps the rows it has when a pager click fails, and retries that page', async () => {
    let nextAttempts = 0;
    renderAt({
      [listPath]: (request) => {
        if (!request.query.get('cursor')) return json(200, { items: [veseli], nextCursor: 'h2' });
        return ++nextAttempts === 1 ? problem(500) : json(200, { items: [chata], nextCursor: null });
      },
    });
    await waitFor(() => expect(rows()).toHaveLength(1));
    await userEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(await screen.findByText('Households couldn’t be loaded. Check your connection and try again.')).toBeVisible();
    expect(rows()).toHaveLength(1);
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    await waitFor(() => expect(screen.getByRole('link', { name: 'Chata' })).toBeVisible());
    expect(nextAttempts).toBe(2);
  });

  it('drops the list on an authoritative refusal and lets the session correct the navigation hint', async () => {
    let sessionReads = 0;
    renderAt({
      'GET /api/v1/session': () => {
        sessionReads += 1;
        return sessionReads === 1 ? json(200, instanceAdminSession()) : json(200, session());
      },
      [`GET ${base}`]: json(200, memberHousehold),
      [listPath]: json(403, { type: 'about:blank', title: 'Forbidden', status: 403, code: 'instance_admin_required' }),
    });
    expect(await screen.findByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' })).toBeVisible();
    expect(screen.getByRole('main')).toHaveTextContent('Ask whoever set up Tendo if you need access.');
    expect(rows()).toHaveLength(0);
    // The stale grant is re-read once, and the Admin center entry follows the session the server just confirmed.
    await waitFor(() => expect(sessionReads).toBe(2));
    await waitFor(() => expect(within(screen.getByRole('navigation', { name: 'Household navigation' }))
      .queryByRole('link', { name: 'Admin center' })).not.toBeInTheDocument());
    expect(requests()).toHaveLength(1);
  });

  it('does not mistake a request-origin refusal for the calm grant refusal', async () => {
    renderAt({ [listPath]: problem(403, 'forbidden_origin') });
    expect(await screen.findByText('Households couldn’t be loaded. Check your connection and try again.')).toBeVisible();
    expect(screen.queryByRole('heading', { name: 'Only an installation administrator can see this.' })).not.toBeInTheDocument();
  });
});

describe('Admin center sections', () => {
  it('offers both sections to an account that holds both grants, and says which one it shows', async () => {
    renderAt({
      [listPath]: json(200, { items: [veseli], nextCursor: null }),
      [`GET ${base}/accounts`]: json(200, { items: [], nextCursor: null }),
    });
    const sections = await screen.findByRole('navigation', { name: 'Admin center sections' });
    expect(within(sections).getByRole('link', { name: 'Households' })).toHaveAttribute('aria-current', 'page');
    const householdSection = within(sections).getByRole('link', { name: 'This household' });
    expect(householdSection).toHaveAttribute('href', '/admin');
    expect(householdSection).not.toHaveAttribute('aria-current');

    await userEvent.click(householdSection);
    expect(await screen.findByRole('heading', { level: 1, name: 'Admin center' })).toBeVisible();
    expect(window.location.pathname).toBe('/admin');
    const known = screen.getByRole('navigation', { name: 'Admin center sections' });
    expect(within(known).getByRole('link', { name: 'This household' })).toHaveAttribute('aria-current', 'page');
  });

  it('stays out of the way when only one section applies', async () => {
    // A household administrator without the installation grant sees no section switcher at all.
    renderAt({
      [`GET ${base}/accounts`]: json(200, { items: [], nextCursor: null }),
    }, '/admin', session());
    expect(await screen.findByRole('heading', { level: 1, name: 'Admin center' })).toBeVisible();
    expect(screen.queryByRole('navigation', { name: 'Admin center sections' })).not.toBeInTheDocument();
  });

  it('stays out of the way for an installation administrator on the household list', async () => {
    // The account holds the installation grant only (a member here), so only the Households section applies.
    const viewer = { ...session(), instanceAdmin: true };
    installFakeServer({
      'GET /api/v1/session': json(200, viewer),
      [`GET ${base}`]: json(200, { ...household, viewerRole: 'member' }),
      ...itemRoutes(base, []),
      [listPath]: json(200, { items: [veseli], nextCursor: null }),
    });
    window.history.replaceState(null, '', '/admin/households');
    render(<I18nProvider><App /></I18nProvider>);
    await waitFor(() => expect(rows()).toHaveLength(1));
    expect(screen.queryByRole('navigation', { name: 'Admin center sections' })).not.toBeInTheDocument();
  });
});
