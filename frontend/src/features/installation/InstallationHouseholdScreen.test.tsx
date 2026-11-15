import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, household, instanceAdminSession, installFakeServer, json, memberHousehold, session } from '../../test/fakeServer';
import { itemRoutes } from '../../test/fakeItems';
import type { components } from '../../../../api/generated/api';

type AdminHousehold = components['schemas']['AdminHousehold'];
type AdminAccount = components['schemas']['AdminAccount'];

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const target = '0198a2f0-0000-7000-8000-000000000011';
const listPath = 'GET /api/v1/admin/households';
const accountsPath = `GET /api/v1/admin/households/${target}/accounts`;
const detailPath = `/admin/households/${target}`;
const problem = (status: number, code?: string) => json(status, { type: 'about:blank', title: 'x', status, ...(code ? { code } : {}) });

const veseli: AdminHousehold = { id: target, name: 'Veselí', timezone: 'Europe/Prague', createdAt: '2026-10-06T10:00:00Z' };
const anna: AdminAccount = { userId: session().userId, login: 'anna', role: 'admin', signInMethods: { password: true, oidc: false } };
const petr: AdminAccount = { userId: '0198a2f0-0000-7000-8000-000000000002', login: 'petr', role: 'member', signInMethods: { password: false, oidc: true } };

function renderAt(routes: Parameters<typeof installFakeServer>[0], path = detailPath, viewer = instanceAdminSession()) {
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

function accountRows() {
  return [...screen.getByRole('main').querySelectorAll<HTMLElement>('li[data-install-account-id]')];
}

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

describe('installation household detail', () => {
  it('drills down from the list under the household’s own name, read-only', async () => {
    renderAt({
      [listPath]: json(200, { items: [veseli], nextCursor: null }),
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
    }, '/admin/households');
    await userEvent.click(await screen.findByRole('link', { name: 'Veselí' }));
    await waitFor(() => expect(screen.getByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible());
    expect(window.location.pathname).toBe(detailPath);

    await waitFor(() => expect(accountRows()).toHaveLength(2));
    const [first, second] = accountRows();
    expect(first).toHaveTextContent('anna');
    expect(first).toHaveTextContent('Administrator');
    expect(first).toHaveTextContent('Password');
    expect(second).toHaveTextContent('petr');
    expect(second).toHaveTextContent('Member');
    expect(second).toHaveTextContent('Single sign-on');
    expect(screen.getByRole('heading', { level: 2, name: 'Accounts' })).toBeVisible();
    const main = screen.getByRole('main');
    expect(main).toHaveTextContent('Everyone who belongs to this household, their role and how they sign in.');
    expect(within(main).getByRole('link', { name: 'Back to households' })).toHaveAttribute('href', '/admin/households');
    // Read-only: no role change, removal or password help, and the rows do not lead anywhere.
    expect(within(main).queryAllByRole('button')).toHaveLength(0);
    expect(within(first).queryAllByRole('link')).toHaveLength(0);
    expect(within(second).queryAllByRole('link')).toHaveLength(0);
    // The household screen is a view of the installation, not a switch into the household: only the target's accounts were read.
    expect(screen.getByRole('navigation', { name: 'Household navigation' })).toBeVisible();
  });

  it('falls back to a generic heading on a direct load instead of reading the list again', async () => {
    const fake = renderAt({ [accountsPath]: json(200, { items: [anna], nextCursor: null }) });
    expect(await screen.findByRole('heading', { level: 1, name: 'Household accounts' })).toBeVisible();
    await waitFor(() => expect(accountRows()).toHaveLength(1));
    expect(fake.requests.filter((request) => request.path === '/api/v1/admin/households')).toHaveLength(0);
  });

  it('says the household is gone when the address names one that no longer exists', async () => {
    renderAt({ [accountsPath]: problem(404) });
    expect(await screen.findByRole('heading', { level: 2, name: 'This household no longer exists.' })).toBeVisible();
    expect(screen.getByRole('main')).toHaveTextContent('It may have been deleted. You can still open the household list.');
    expect(screen.getByRole('link', { name: 'Back to households' })).toHaveAttribute('href', '/admin/households');
    expect(screen.queryByRole('heading', { level: 2, name: 'Accounts' })).not.toBeInTheDocument();
    expect(accountRows()).toHaveLength(0);
  });

  it('shows nothing about the household on an authoritative refusal, and lets the session correct the hint', async () => {
    let sessionReads = 0;
    renderAt({
      'GET /api/v1/session': () => {
        sessionReads += 1;
        return sessionReads === 1 ? json(200, instanceAdminSession()) : json(200, session());
      },
      [`GET ${base}`]: json(200, memberHousehold),
      [accountsPath]: problem(403, 'instance_admin_required'),
    });
    expect(await screen.findByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' })).toBeVisible();
    expect(accountRows()).toHaveLength(0);
    await waitFor(() => expect(sessionReads).toBe(2));
    await waitFor(() => expect(within(screen.getByRole('navigation', { name: 'Household navigation' }))
      .queryByRole('link', { name: 'Admin center' })).not.toBeInTheDocument());
  });

  it('keeps a failed read recoverable', async () => {
    let attempt = 0;
    renderAt({
      [accountsPath]: () => (++attempt === 1 ? problem(503) : json(200, { items: [anna], nextCursor: null })),
    });
    expect(await screen.findByText('Accounts couldn’t be loaded. Check your connection and try again.')).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    await waitFor(() => expect(accountRows()).toHaveLength(1));
    expect(attempt).toBe(2);
  });

  it('pages the accounts and places a keyboard reader on the new page', async () => {
    renderAt({
      [accountsPath]: (request) => json(200, request.query.get('cursor')
        ? { items: [petr], nextCursor: null }
        : { items: [anna], nextCursor: 'a2' }),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(1));
    await userEvent.click(screen.getByRole('button', { name: 'Next page' }));
    await waitFor(() => expect(screen.getByText('petr')).toBeVisible());
    await userEvent.click(screen.getByRole('button', { name: 'First page' }));
    await waitFor(() => expect(screen.getByText('anna')).toBeVisible());
    await waitFor(() => expect(screen.getByRole('heading', { level: 2, name: 'Accounts' })).toHaveFocus());
  });
});
