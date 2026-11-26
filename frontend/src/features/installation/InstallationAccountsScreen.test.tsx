import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, household, instanceAdminSession, installFakeServer, json, memberHousehold, session } from '../../test/fakeServer';
import { itemRoutes } from '../../test/fakeItems';
import type { components } from '../../../../api/generated/api';

type DirectoryAccount = components['schemas']['DirectoryAccount'];

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const directoryPath = 'GET /api/v1/admin/accounts';
const householdsPath = 'GET /api/v1/admin/households';
const problem = (status: number, code?: string) => json(status, { type: 'about:blank', title: 'x', status, ...(code ? { code } : {}) });

const anna: DirectoryAccount = { userId: session().userId, login: 'anna' };
const petr: DirectoryAccount = { userId: '0198a2f0-0000-7000-8000-000000000002', login: 'petr' };
const jana: DirectoryAccount = { userId: '0198a2f0-0000-7000-8000-000000000003', login: 'jana' };

/** The Admin center's account directory, on its own address, with every route a test does not override already here. */
function renderAt(routes: Parameters<typeof installFakeServer>[0], path = '/admin/accounts', viewer = instanceAdminSession()) {
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
  return [...screen.getByRole('main').querySelectorAll<HTMLElement>('li[data-install-directory-id]')];
}
function logins() {
  return [...screen.getByRole('main').querySelectorAll('[data-install-login]')].map((node) => node.textContent);
}
const field = () => screen.getByLabelText('Login');
const asks = (fake: ReturnType<typeof installFakeServer>) =>
  fake.requests.filter((request) => request.method === 'GET' && request.path === '/api/v1/admin/accounts');
const sections = () => screen.getByRole('navigation', { name: 'Admin center sections' });

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

describe('installation account directory', () => {
  it('lists every account as a login, marks the viewer, and offers deletion only for others', async () => {
    const fake = renderAt({ [directoryPath]: json(200, { items: [anna, petr], nextCursor: null }) });
    await waitFor(() => expect(rows()).toHaveLength(2));

    expect(logins()).toEqual(['anna', 'petr']);
    expect(screen.getByRole('heading', { level: 1, name: 'Admin center' })).toBeVisible();
    expect(screen.getByRole('heading', { level: 2, name: 'Accounts' })).toBeVisible();
    expect(screen.getByRole('region', { name: 'Accounts' })).toHaveTextContent(
      'Every account in Tendo, including people who belong to no household. Search by login to find one.',
    );

    // No account deletes itself: the viewer's own row says so and carries no action at all.
    const [own, other] = rows();
    expect(own).toHaveTextContent('(you)');
    expect(within(own).queryByRole('button')).not.toBeInTheDocument();
    expect(within(other).getByRole('button', { name: 'Delete account: petr' })).toBeEnabled();

    // The empty search is the directory itself: no `q`, and the dialog's own page size.
    expect(asks(fake)).toHaveLength(1);
    expect(asks(fake)[0].query.has('q')).toBe(false);
    expect(asks(fake)[0].query.get('limit')).toBe('20');
  });

  it('holds the list place while the accounts load', async () => {
    let release!: (response: Response) => void;
    const pending = new Promise<Response>((resolve) => { release = resolve; });
    renderAt({ [directoryPath]: () => pending });

    const label = await screen.findByText('Loading accounts…');
    expect(label.closest('[role="status"]')).not.toBeNull();
    await act(async () => { release(json(200, { items: [petr], nextCursor: null })); await pending; });
    expect(await screen.findByText('petr')).toBeVisible();
  });

  it('searches only when asked, and Enter searches without leaving the field', async () => {
    const fake = renderAt({
      [directoryPath]: (request) => json(200, {
        items: request.query.get('q') ? [petr] : [anna, petr],
        nextCursor: null,
      }),
    });
    await waitFor(() => expect(rows()).toHaveLength(2));

    await userEvent.type(field(), 'pet');
    expect(asks(fake)).toHaveLength(1);

    await userEvent.type(field(), '{Enter}');
    await waitFor(() => expect(logins()).toEqual(['petr']));
    expect(asks(fake)).toHaveLength(2);
    expect(asks(fake).at(-1)!.query.get('q')).toBe('pet');
    expect(asks(fake).at(-1)!.query.get('limit')).toBe('20');
    expect(field()).toHaveFocus();
  });

  it('answers an over-long search on the field, counting code points as the server does', async () => {
    const fake = renderAt({ [directoryPath]: json(200, { items: [petr], nextCursor: null }) });
    await waitFor(() => expect(rows()).toHaveLength(1));
    const search = screen.getByRole('button', { name: 'Search' });

    // 64 emoji are 128 UTF-16 units but 64 code points, which the server accepts as a whole search.
    await userEvent.click(field());
    await userEvent.paste('😀'.repeat(64));
    await userEvent.click(search);
    await waitFor(() => expect(asks(fake)).toHaveLength(2));
    expect(asks(fake).at(-1)!.query.get('q')).toBe('😀'.repeat(64));

    // One more is one too many: the field says so instead of asking for a search the server would refuse.
    await userEvent.click(field());
    await userEvent.paste('😀');
    await userEvent.click(search);
    await waitFor(() => expect(field()).toHaveAttribute('aria-invalid', 'true'));
    const describedBy = field().getAttribute('aria-describedby');
    expect(document.getElementById(describedBy!)).toHaveTextContent(
      'That search can’t be used. Use plain text and 64 characters or fewer.',
    );
    await waitFor(() => expect(field()).toHaveFocus());
    expect(asks(fake)).toHaveLength(2);
  });

  it('answers a search the server refuses on the field, without offering a retry', async () => {
    renderAt({
      [directoryPath]: (request) => (request.query.get('q')
        ? problem(400, 'invalid_query')
        : json(200, { items: [petr], nextCursor: null })),
    });
    await waitFor(() => expect(rows()).toHaveLength(1));

    await userEvent.type(field(), 'pet{Enter}');
    await waitFor(() => expect(field()).toHaveAttribute('aria-invalid', 'true'));
    const describedBy = field().getAttribute('aria-describedby');
    expect(document.getElementById(describedBy!)).toHaveTextContent(
      'That search can’t be used. Use plain text and 64 characters or fewer.',
    );
    // The field the reader has to fix takes the focus, and nothing offers a retry that could never work.
    await waitFor(() => expect(field()).toHaveFocus());
    expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument();
    expect(rows()).toHaveLength(0);

    // The same refused text searched again is a new answer: the reader is put back on the field once more.
    await userEvent.click(screen.getByRole('button', { name: 'Search' }));
    await waitFor(() => expect(field()).toHaveFocus());
  });

  it('keeps the refused search on the field when an earlier search lands after it', async () => {
    let release!: (response: Response) => void;
    const held = new Promise<Response>((resolve) => { release = resolve; });
    const fake = renderAt({
      [directoryPath]: (request) => {
        const q = request.query.get('q');
        if (!q) return json(200, { items: [petr], nextCursor: null });
        return q === 'ann' ? held : problem(400, 'invalid_query');
      },
    });
    await waitFor(() => expect(rows()).toHaveLength(1));

    await userEvent.clear(field());
    await userEvent.type(field(), 'ann{Enter}');
    await waitFor(() => expect(asks(fake)).toHaveLength(2));

    await userEvent.clear(field());
    await userEvent.type(field(), 'petr{Enter}');
    await waitFor(() => expect(field()).toHaveAttribute('aria-invalid', 'true'));

    // The search nobody waits for any more lands now: it answers itself, not the refusal the field is showing.
    await act(async () => { release(json(200, { items: [anna], nextCursor: null })); await held; });

    expect(field()).toHaveAttribute('aria-invalid', 'true');
    expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument();
    expect(rows()).toHaveLength(0);
    expect(asks(fake)).toHaveLength(3);
  });

  it('says an empty directory and an empty search differently', async () => {
    renderAt({ [directoryPath]: json(200, { items: [], nextCursor: null }) });
    const empty = await screen.findByText('No accounts to show.');
    expect(empty).toHaveClass('text-muted');

    await userEvent.type(field(), 'zzz{Enter}');
    expect(await screen.findByText('No account matches “zzz”.')).toBeVisible();
  });

  it('keeps a failed first read recoverable', async () => {
    let attempt = 0;
    const fake = renderAt({
      [directoryPath]: () => (++attempt === 1 ? problem(503) : json(200, { items: [petr], nextCursor: null })),
    });
    expect(await screen.findByText('Accounts couldn’t be loaded. Try again in a moment.')).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    await waitFor(() => expect(logins()).toEqual(['petr']));
    expect(asks(fake)).toHaveLength(2);
  });

  it('pages forward and back, and puts the reader on the heading of the new page', async () => {
    const fake = renderAt({
      [directoryPath]: (request) => (request.query.get('cursor')
        ? json(200, { items: [jana], nextCursor: null })
        : json(200, { items: [petr], nextCursor: 'a2' })),
    });
    await waitFor(() => expect(logins()).toEqual(['petr']));

    await userEvent.click(screen.getByRole('button', { name: 'Next page' }));
    await waitFor(() => expect(logins()).toEqual(['jana']));
    await userEvent.click(screen.getByRole('button', { name: 'First page' }));
    await waitFor(() => expect(logins()).toEqual(['petr']));
    expect(asks(fake).map((request) => request.query.get('cursor'))).toEqual([null, 'a2', null]);
    // The list heading takes focus after a page settles, so a keyboard reader is placed on the new page.
    await waitFor(() => expect(screen.getByRole('heading', { level: 2, name: 'Accounts' })).toHaveFocus());
  });

  it('keeps the page it has when a pager click fails, and retries that page', async () => {
    let nextAttempts = 0;
    renderAt({
      [directoryPath]: (request) => {
        if (!request.query.get('cursor')) return json(200, { items: [petr], nextCursor: 'a2' });
        return ++nextAttempts === 1 ? problem(500) : json(200, { items: [jana], nextCursor: null });
      },
    });
    await waitFor(() => expect(logins()).toEqual(['petr']));

    await userEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(await screen.findByText('Accounts couldn’t be loaded. Try again in a moment.')).toBeVisible();
    expect(logins()).toEqual(['petr']);

    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    await waitFor(() => expect(logins()).toEqual(['jana']));
    expect(nextAttempts).toBe(2);
  });

  it('drops the list on an authoritative refusal and lets the session correct the navigation hint', async () => {
    let sessionReads = 0;
    const fake = renderAt({
      'GET /api/v1/session': () => {
        sessionReads += 1;
        return sessionReads === 1 ? json(200, instanceAdminSession()) : json(200, session());
      },
      [`GET ${base}`]: json(200, memberHousehold),
      [directoryPath]: problem(403, 'instance_admin_required'),
    });
    expect(await screen.findByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' })).toBeVisible();
    expect(screen.getByRole('main')).toHaveTextContent('Ask whoever set up Tendo if you need access.');
    expect(rows()).toHaveLength(0);

    // The stale grant is re-read once, and both the section switcher and the Admin center entry follow it.
    await waitFor(() => expect(sessionReads).toBe(2));
    await waitFor(() => expect(screen.queryByRole('navigation', { name: 'Admin center sections' })).not.toBeInTheDocument());
    await waitFor(() => expect(within(screen.getByRole('navigation', { name: 'Household navigation' }))
      .queryByRole('link', { name: 'Admin center' })).not.toBeInTheDocument());
    // A refusal is an answer, not a failure to retry: the directory is read exactly once.
    expect(asks(fake)).toHaveLength(1);
  });

  it('does not mistake a request-origin refusal for the calm grant refusal', async () => {
    renderAt({ [directoryPath]: problem(403, 'forbidden_origin') });
    expect(await screen.findByText('Accounts couldn’t be loaded. Try again in a moment.')).toBeVisible();
    expect(screen.queryByRole('heading', { name: 'Only an installation administrator can see this.' })).not.toBeInTheDocument();
  });

  it('returns to sign-in when the directory reports an ended session', async () => {
    renderAt({
      [directoryPath]: problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
    });
    expect(await screen.findByLabelText('Password')).toBeVisible();
    expect(screen.getByRole('status')).toHaveTextContent('You were signed out. Sign in again to continue. Saved changes are kept.');
  });
});

describe('account directory sections', () => {
  it('opens the account directory from the household list, and returns with Back', async () => {
    renderAt({
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [householdsPath]: json(200, { items: [], nextCursor: null }),
    }, '/admin/households');
    const switcher = await screen.findByRole('navigation', { name: 'Admin center sections' });
    // The household grant is known only once the household read lands, so the third section follows it.
    const householdSection = await within(switcher).findByRole('link', { name: 'Your household' });
    expect(householdSection).toHaveAttribute('href', '/admin');
    expect(within(switcher).getByRole('link', { name: 'Households' })).toHaveAttribute('aria-current', 'page');
    const accountsLink = within(switcher).getByRole('link', { name: 'Accounts' });
    expect(accountsLink).toHaveAttribute('href', '/admin/accounts');

    await userEvent.click(accountsLink);
    expect(await screen.findByRole('heading', { level: 2, name: 'Accounts' })).toBeVisible();
    expect(window.location.pathname).toBe('/admin/accounts');
    expect(within(sections()).getByRole('link', { name: 'Accounts' })).toHaveAttribute('aria-current', 'page');

    // The directory is an ordinary address: Back returns to the household list, not to Home.
    await act(async () => { window.history.back(); await new Promise((resolve) => setTimeout(resolve, 0)); });
    await waitFor(() => expect(window.location.pathname).toBe('/admin/households'));
    expect(await screen.findByRole('heading', { level: 2, name: 'Households' })).toBeVisible();
  });

  it('keeps a deeper account address out of the directory', async () => {
    const fake = renderAt({ [directoryPath]: json(200, { items: [petr], nextCursor: null }) }, '/admin/accounts/x');
    expect(await screen.findByRole('heading', { level: 1, name: 'We couldn’t find that page.' })).toBeVisible();
    // No directory request is made for an address that is not the directory.
    expect(asks(fake)).toHaveLength(0);
  });

  it('offers no section switcher to a household administrator who lacks the installation grant', async () => {
    // The address is not authority: without the grant the directory refuses, and one section is no switcher.
    renderAt({
      [directoryPath]: problem(403, 'instance_admin_required'),
      'GET /api/v1/session': json(200, session()),
    }, '/admin/accounts', session());
    expect(await screen.findByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' })).toBeVisible();
    expect(screen.queryByRole('navigation', { name: 'Admin center sections' })).not.toBeInTheDocument();
  });
});
