import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, household, installFakeServer, json, memberHousehold, session } from '../../test/fakeServer';
import { itemRoutes } from '../../test/fakeItems';

/**
 * One Admin center entry, one highlight: the viewer's own household role wins the target, the installation grant
 * covers an account without it, and neither offers anything when the account holds neither.
 */
type Viewer = { role: 'admin' | 'member'; instanceAdmin?: boolean; withHousehold?: boolean };

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const listPath = 'GET /api/v1/admin/households';
const households = [{ id: HOUSEHOLD_ID, name: 'Veselí', timezone: 'Europe/Prague', createdAt: '2026-10-06T10:00:00Z' }];

function routesFor({ role, instanceAdmin = false, withHousehold = true }: Viewer, extra: Parameters<typeof installFakeServer>[0] = {}) {
  return {
    'GET /api/v1/session': json(200, { ...session(withHousehold), instanceAdmin }),
    ...(withHousehold ? {
      [`GET ${base}`]: json(200, role === 'admin' ? household : memberHousehold),
      ...itemRoutes(base, []),
    } : {}),
    ...extra,
  };
}

function app(path = '/') {
  window.history.replaceState(null, '', path);
  return render(<I18nProvider><App /></I18nProvider>);
}
function nav() {
  return screen.getByRole('navigation', { name: 'Household navigation' });
}

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

describe('Admin center entry', () => {
  it('opens this household for a household administrator and stays current on the installation list', async () => {
    installFakeServer(routesFor({ role: 'admin' }, {
      [`GET ${base}/accounts`]: json(200, { items: [], nextCursor: null }),
      [listPath]: json(200, { items: households, nextCursor: null }),
    }));
    app('/');
    const entry = await waitFor(() => within(nav()).getByRole('link', { name: 'Admin center' }));
    expect(entry).toHaveAttribute('href', '/admin');
    await userEvent.click(entry);
    expect(await screen.findByRole('heading', { level: 1, name: 'Admin center' })).toBeVisible();
    expect(within(nav()).getByRole('link', { name: 'Admin center' })).toHaveAttribute('aria-current', 'page');

    // The same entry stays current on the installation side of the Admin center; there is never a second one.
    window.history.replaceState(null, '', '/admin/households');
    window.dispatchEvent(new PopStateEvent('popstate'));
    expect(await screen.findByRole('heading', { level: 2, name: 'Households' })).toBeVisible();
    expect(within(nav()).getAllByRole('link', { name: 'Admin center' })).toHaveLength(1);
    expect(within(nav()).getByRole('link', { name: 'Admin center' })).toHaveAttribute('aria-current', 'page');
  });

  it('opens the household list for an installation administrator who is only a member here', async () => {
    installFakeServer(routesFor({ role: 'member', instanceAdmin: true }, {
      [listPath]: json(200, { items: households, nextCursor: null }),
    }));
    app('/');
    const entry = await waitFor(() => within(nav()).getByRole('link', { name: 'Admin center' }));
    expect(entry).toHaveAttribute('href', '/admin/households');
    await userEvent.click(entry);
    expect(window.location.pathname).toBe('/admin/households');
    expect(await screen.findByRole('heading', { level: 2, name: 'Households' })).toBeVisible();
    expect(within(nav()).getByRole('link', { name: 'Admin center' })).toHaveAttribute('aria-current', 'page');
  });

  it('offers no entry to a plain member', async () => {
    installFakeServer(routesFor({ role: 'member' }));
    app('/');
    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toBeVisible();
    await waitFor(() => expect(within(nav()).getAllByRole('link')).toHaveLength(6));
    expect(within(nav()).queryByRole('link', { name: 'Admin center' })).not.toBeInTheDocument();
  });

  it('names the single entry the same way in Czech', async () => {
    // One neutral word for the whole Admin center, matching the English "Admin center", and no second entry.
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer(routesFor({ role: 'admin' }, {
      [`GET ${base}/accounts`]: json(200, { items: [], nextCursor: null }),
    }));
    app('/');
    const czechNav = () => screen.getByRole('navigation', { name: 'Navigace domácnosti' });
    const entry = await waitFor(() => within(czechNav()).getByRole('link', { name: 'Správa' }));
    expect(entry).toHaveAttribute('href', '/admin');
    expect(within(czechNav()).getAllByRole('link', { name: 'Správa' })).toHaveLength(1);
    expect(within(czechNav()).queryByRole('link', { name: 'Správa domácnosti' })).not.toBeInTheDocument();
  });

  it('keeps the entry in the phone More sheet instead of the bar', async () => {
    installFakeServer(routesFor({ role: 'admin' }));
    app('/');
    // The desktop rail holds one entry; the sheet is closed, so nothing second is in the document.
    const railEntry = await waitFor(() => within(nav()).getByRole('link', { name: 'Admin center' }));
    expect(railEntry).toHaveAttribute('href', '/admin');
    await userEvent.click(within(nav()).getByRole('button', { name: 'More' }));
    expect(within(nav()).getByRole('button', { name: 'More' })).toHaveAttribute('aria-expanded', 'true');
    const entries = within(nav()).getAllByRole('link', { name: 'Admin center' });
    expect(entries).toHaveLength(2);
    for (const entry of entries) expect(entry).toHaveAttribute('href', '/admin');
  });

  it('shows the installation center in a household-less shell', async () => {
    installFakeServer(routesFor({ role: 'member', instanceAdmin: true, withHousehold: false }, {
      [listPath]: json(200, { items: households, nextCursor: null }),
    }));
    app('/');
    await waitFor(() => expect(within(nav()).getAllByRole('link')).toHaveLength(3));
    const entry = within(nav()).getByRole('link', { name: 'Admin center' });
    expect(entry).toHaveAttribute('href', '/admin/households');
    await userEvent.click(entry);
    expect(await screen.findByRole('heading', { level: 2, name: 'Households' })).toBeVisible();
    expect(within(nav()).getByRole('link', { name: 'Admin center' })).toHaveAttribute('aria-current', 'page');
    expect(within(nav()).getByRole('link', { name: 'Home' })).not.toHaveAttribute('aria-current');
  });

  it('offers no Admin center entry to a household-less account without the grant', async () => {
    installFakeServer(routesFor({ role: 'member', withHousehold: false }));
    app('/');
    await waitFor(() => expect(within(nav()).getAllByRole('link')).toHaveLength(2));
    expect(within(nav()).queryByRole('link', { name: 'Admin center' })).not.toBeInTheDocument();
  });
});

describe('Account entry to the Admin center', () => {
  it.each([
    [{ role: 'admin' } as Viewer, 'Open Admin center', '/admin'],
    [{ role: 'member', instanceAdmin: true } as Viewer, 'Open households', '/admin/households'],
    [{ role: 'admin', instanceAdmin: true } as Viewer, 'Open Admin center', '/admin'],
  ])('points %o at the center that account can use', async (viewer, name, href) => {
    installFakeServer(routesFor(viewer, { [listPath]: json(200, { items: households, nextCursor: null }) }));
    app('/account');
    const link = await waitFor(() => within(screen.getByRole('main')).getByRole('link', { name }));
    expect(link).toHaveAttribute('href', href);
    expect(within(screen.getByRole('main')).queryByRole('heading', { name: 'Only administrators can open the Admin center.' })).not.toBeInTheDocument();
  });

  it('offers nothing to a plain member', async () => {
    installFakeServer(routesFor({ role: 'member' }));
    app('/account');
    expect(await screen.findByRole('heading', { level: 1, name: 'Your account' })).toBeVisible();
    const main = screen.getByRole('main');
    expect(within(main).queryByRole('link', { name: 'Open Admin center' })).not.toBeInTheDocument();
    expect(within(main).queryByRole('link', { name: 'Open households' })).not.toBeInTheDocument();
  });

  it('offers nothing to a household-less member', async () => {
    installFakeServer(routesFor({ role: 'member', withHousehold: false }));
    app('/account');
    expect(await screen.findByRole('heading', { level: 1, name: 'Your account' })).toBeVisible();
    expect(within(screen.getByRole('main')).queryByRole('link', { name: 'Open households' })).not.toBeInTheDocument();
  });
});

describe('household-less /admin address', () => {
  it('is corrected to the household list for an installation administrator', async () => {
    installFakeServer(routesFor({ role: 'member', instanceAdmin: true, withHousehold: false }, {
      [listPath]: json(200, { items: households, nextCursor: null }),
    }));
    app('/admin');
    expect(await screen.findByRole('heading', { level: 2, name: 'Households' })).toBeVisible();
    await waitFor(() => expect(window.location.pathname).toBe('/admin/households'));
    // The replaced address also replaces the route, so the entry stays current instead of falling back to Home.
    expect(within(nav()).getByRole('link', { name: 'Admin center' })).toHaveAttribute('aria-current', 'page');
    expect(within(nav()).getByRole('link', { name: 'Home' })).not.toHaveAttribute('aria-current');
  });

  it('is corrected to Home for every other household-less account', async () => {
    installFakeServer(routesFor({ role: 'member', withHousehold: false }));
    app('/admin');
    expect(await screen.findByText('Join your household by invitation')).toBeVisible();
    await waitFor(() => expect(window.location.pathname).toBe('/'));
  });
});
