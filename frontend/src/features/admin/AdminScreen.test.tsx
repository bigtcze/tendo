import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, household, installFakeServer, json, memberHousehold, session } from '../../test/fakeServer';
import { itemRoutes } from '../../test/fakeItems';
import type { components } from '../../../../api/generated/api';

type Account = components['schemas']['Account'];
const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const accountsPath = `GET ${base}/accounts`;
const accountPath = (userId: string) => `GET ${base}/accounts/${userId}`;
const changePath = (userId: string) => `PATCH ${base}/accounts/${userId}`;
const owner: Account = { userId: session().userId, login: 'anna', role: 'admin', signInMethods: { password: true, oidc: false }, passwordReset: null };
const petr: Account = { userId: '0198a2f0-0000-7000-8000-000000000002', login: 'petr', role: 'member', signInMethods: { password: false, oidc: true }, passwordReset: null };
const both: Account = { userId: '0198a2f0-0000-7000-8000-000000000003', login: 'jana', role: 'member', signInMethods: { password: true, oidc: true }, passwordReset: null };
const localMember: Account = { userId: '0198a2f0-0000-7000-8000-000000000004', login: 'marta', role: 'member', signInMethods: { password: true, oidc: false }, passwordReset: null };

const forbidden = () => json(403, { type: 'about:blank', title: 'Forbidden', status: 403, code: 'admin_required' });

function app(path = '/') {
  window.history.replaceState(null, '', path);
  return render(<I18nProvider><App /></I18nProvider>);
}
function nav() {
  return screen.getByRole('navigation', { name: 'Household navigation' });
}
function rowOf(login: string) {
  const rows = screen.getByRole('main').querySelectorAll('li[data-account-id]');
  return [...rows].find((row) => row.querySelector('[data-account-login]')?.textContent === login) as HTMLElement;
}

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined });
  localStorage.removeItem('tendo.locale');
});

describe('Admin center', () => {
  it('offers the Admin center in the rail to an administrator and lists accounts with roles and sign-in methods', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [owner, petr, both], nextCursor: null }),
    });
    app('/');
    await waitFor(() => expect(within(nav()).getByRole('link', { name: 'Admin center' })).toHaveAttribute('href', '/admin'));
    await userEvent.click(within(nav()).getByRole('link', { name: 'Admin center' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Admin center' })).toHaveFocus();
    expect(window.location.pathname).toBe('/admin');

    const main = screen.getByRole('main');
    const rows = await waitFor(() => {
      const found = main.querySelectorAll('li[data-account-id]');
      expect(found).toHaveLength(3);
      return found;
    });
    expect(rows[0]).toHaveTextContent('anna');
    expect(rows[0]).toHaveTextContent('(you)');
    expect(rows[0]).toHaveTextContent('Administrator');
    expect(rows[0]).toHaveTextContent('Password');
    expect(rows[1]).toHaveTextContent('petr');
    expect(rows[1]).not.toHaveTextContent('(you)');
    expect(rows[1]).toHaveTextContent('Member');
    expect(rows[1]).toHaveTextContent('Single sign-on');
    expect(rows[2]).toHaveTextContent('Password and single sign-on');
    expect(within(rows[1] as HTMLElement).getByRole('button', { name: 'Make administrator: petr' })).toHaveTextContent('Make administrator');
    expect(within(rows[0] as HTMLElement).getByRole('button', { name: 'Make member: anna' })).toBeVisible();
    expect(main).toHaveTextContent('Everyone here can use the household. Only administrators can invite people, change roles or help with passwords.');
  });

  it('keeps the Admin center out of a member’s navigation, Account and direct link, and renders no account data', async () => {
    const fake = installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, memberHousehold),
      ...itemRoutes(base, []),
      [accountsPath]: forbidden(),
    });
    app('/');
    // Let the household read settle first: only then is the rail's rendered state meaningful.
    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toBeVisible();
    expect(fake.requests.filter((request) => request.method === 'GET' && request.path === base)).toHaveLength(1);
    await waitFor(() => expect(within(nav()).getAllByRole('link')).toHaveLength(6));
    expect(within(nav()).getByRole('link', { name: 'Archive' })).toHaveAttribute('href', '/items?status=archived');
    expect(within(nav()).queryByRole('link', { name: 'Admin center' })).not.toBeInTheDocument();

    await userEvent.click(within(nav()).getByRole('link', { name: 'Your account' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Your account' })).toBeVisible();
    const main = screen.getByRole('main');
    expect(within(main).queryByRole('link', { name: 'Open Admin center' })).not.toBeInTheDocument();
    expect(within(main).queryByRole('heading', { name: 'Admin center' })).not.toBeInTheDocument();

    window.history.replaceState(null, '', '/admin');
    window.dispatchEvent(new PopStateEvent('popstate'));
    expect(await screen.findByRole('heading', { level: 1, name: 'Admin center' })).toBeVisible();
    expect(await screen.findByRole('heading', { name: 'Only administrators can open the Admin center.' })).toBeVisible();
    expect(screen.getByRole('main')).toHaveTextContent('Ask a household administrator if you need someone’s role changed.');
    expect(screen.getByRole('main').querySelectorAll('li[data-account-id]')).toHaveLength(0);
    expect(screen.getByRole('main')).not.toHaveTextContent('petr');
    expect(fake.requests.filter((request) => request.method === 'GET' && request.path === `${base}/accounts`)).toHaveLength(1);
  });

  it('promotes a member by reading a fresh version and quoting it exactly in the change, then updates the row', async () => {
    const promoted: Account = { ...petr, role: 'admin' };
    const fake = installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [owner, petr], nextCursor: null }),
      [accountPath(petr.userId)]: json(200, petr, { ETag: '"7"' }),
      [changePath(petr.userId)]: json(200, promoted, { ETag: '"8"' }),
    });
    app('/admin');
    await waitFor(() => expect(rowOf('petr')).toBeVisible());

    await userEvent.click(screen.getByRole('button', { name: 'Make administrator: petr' }));
    const dialog = await screen.findByRole('dialog');
    expect(dialog).toHaveTextContent('Make petr an administrator?');
    expect(dialog).toHaveTextContent('Administrators can invite people, manage invitations, change roles and help with passwords.');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Make administrator' }));

    const notice = await screen.findByText('petr is now an administrator.');
    expect(notice).toBeVisible();
    // Closing the dialog returns focus to the control that opened it; the live region speaks the result.
    expect(screen.getByRole('button', { name: 'Make member: petr' })).toHaveFocus();
    expect(screen.queryByRole('dialog', { name: 'Make petr an administrator?' })).not.toBeInTheDocument();
    await waitFor(() => expect(rowOf('petr')).toHaveTextContent('Administrator'));

    const read = fake.requests.find((request) => request.method === 'GET' && request.path === `${base}/accounts/${petr.userId}`);
    const change = fake.requests.find((request) => request.method === 'PATCH' && request.path === `${base}/accounts/${petr.userId}`);
    expect(read).toBeDefined();
    expect(change?.headers.get('If-Match')).toBe('"7"');
    expect(JSON.parse(change!.body)).toEqual({ role: 'admin' });
    // The fresh read must precede the change it authorizes.
    expect(fake.requests.indexOf(read!)).toBeLessThan(fake.requests.indexOf(change!));
  });

  it('explains that a household needs an administrator and leaves the role untouched', async () => {
    const fake = installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [owner, petr], nextCursor: null }),
      [accountPath(owner.userId)]: json(200, owner, { ETag: '"2"' }),
      [changePath(owner.userId)]: json(409, { type: 'about:blank', title: 'Conflict', status: 409, code: 'last_admin' }),
    });
    app('/admin');
    await waitFor(() => expect(rowOf('anna')).toBeVisible());

    await userEvent.click(screen.getByRole('button', { name: 'Make member: anna' }));
    const dialog = await screen.findByRole('dialog');
    expect(dialog).toHaveTextContent('You’ll lose access to the Admin center. A household always needs at least one administrator.');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Make member' }));

    expect(await screen.findByText('A household needs at least one administrator. Make someone else an administrator first.')).toBeVisible();
    expect(rowOf('anna')).toHaveTextContent('Administrator');
    expect(fake.requests.filter((request) => request.method === 'GET' && request.path === `${base}/accounts`)).toHaveLength(1);
  });

  it('reloads the page and says the account changed elsewhere when the version is stale', async () => {
    let listCalls = 0;
    const renamed: Account = { ...petr, login: 'petr_novak' };
    const fake = installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: () => json(200, { items: ++listCalls === 1 ? [owner, petr] : [owner, renamed], nextCursor: null }),
      [accountPath(petr.userId)]: json(200, petr, { ETag: '"3"' }),
      [changePath(petr.userId)]: json(412, { type: 'about:blank', title: 'Precondition Failed', status: 412, code: 'precondition_failed' }),
    });
    app('/admin');
    await waitFor(() => expect(rowOf('petr')).toBeVisible());
    await userEvent.click(screen.getByRole('button', { name: 'Make administrator: petr' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Make administrator' }));

    expect(await screen.findByText('This account changed elsewhere. The list has been refreshed; check the current role and try again.')).toBeVisible();
    await waitFor(() => expect(listCalls).toBe(2));
    expect(rowOf('petr_novak')).toHaveTextContent('Member');
    expect(fake.requests.filter((request) => request.method === 'GET' && request.path === `${base}/accounts`)).toHaveLength(2);
  });

  it('drops the Admin center and its data after demoting yourself', async () => {
    let viewer = household;
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: () => json(200, viewer),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [owner, { ...petr, role: 'admin' }], nextCursor: null }),
      [accountPath(owner.userId)]: json(200, owner, { ETag: '"4"' }),
      [changePath(owner.userId)]: () => {
        viewer = memberHousehold;
        return json(200, { ...owner, role: 'member' }, { ETag: '"5"' });
      },
    });
    app('/admin');
    await waitFor(() => expect(within(nav()).getByRole('link', { name: 'Admin center' })).toBeInTheDocument());

    await userEvent.click(screen.getByRole('button', { name: 'Make member: anna' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Make member' }));

    expect(await screen.findByText('You’re now a member of this household. The Admin center is no longer available to you.')).toBeVisible();
    expect(await screen.findByRole('heading', { name: 'Only administrators can open the Admin center.' })).toBeVisible();
    await waitFor(() => expect(within(nav()).queryByRole('link', { name: 'Admin center' })).not.toBeInTheDocument());
    expect(screen.getByRole('main').querySelectorAll('li[data-account-id]')).toHaveLength(0);
  });

  it('disables the role control and the dialog while a change is in flight', async () => {
    let release!: (response: Response) => void;
    const held = new Promise<Response>((resolve) => { release = resolve; });
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [owner, petr], nextCursor: null }),
      [accountPath(petr.userId)]: json(200, petr, { ETag: '"9"' }),
      [changePath(petr.userId)]: () => held,
    });
    app('/admin');
    await waitFor(() => expect(rowOf('petr')).toBeVisible());
    await userEvent.click(screen.getByRole('button', { name: 'Make administrator: petr' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Make administrator' }));

    const saving = await within(dialog).findByRole('button', { name: 'Saving…' });
    expect(saving).toBeDisabled();
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Make member: anna' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Make administrator: petr' })).toBeDisabled();

    release(json(200, { ...petr, role: 'admin' }, { ETag: '"10"' }));
    expect(await screen.findByText('petr is now an administrator.')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Make member: petr' })).toBeEnabled();
  });

  it('returns to sign-in when the account list reports an ended session', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: json(401, { type: 'about:blank', title: 'Unauthorized', status: 401 }),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
    });
    app('/admin');
    expect(await screen.findByLabelText('Password')).toBeVisible();
    expect(screen.getByRole('status')).toHaveTextContent('You were signed out. Sign in again to continue. Saved changes are kept.');
  });

  it('re-reads the viewer role when the server refuses the list, dropping the rail entry and the Account card', async () => {
    let releaseList!: (response: Response) => void;
    const held = new Promise<Response>((resolve) => { releaseList = resolve; });
    let householdCalls = 0;
    const fake = installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: () => json(200, ++householdCalls === 1 ? household : memberHousehold),
      ...itemRoutes(base, []),
      [accountsPath]: () => held,
    });
    app('/admin');
    // Another admin demoted the viewer just now: the rail still trusts the admin hint of the first read.
    await waitFor(() => expect(within(nav()).getByRole('link', { name: 'Admin center' })).toBeInTheDocument());
    releaseList(forbidden());

    expect(await screen.findByRole('heading', { name: 'Only administrators can open the Admin center.' })).toBeVisible();
    // The authoritative refusal rewrites the hint: the rail entry and the Account card go with it.
    await waitFor(() => expect(within(nav()).queryByRole('link', { name: 'Admin center' })).not.toBeInTheDocument());
    expect(fake.requests.filter((request) => request.method === 'GET' && request.path === base)).toHaveLength(2);

    window.history.replaceState(null, '', '/account');
    window.dispatchEvent(new PopStateEvent('popstate'));
    expect(await screen.findByRole('heading', { level: 1, name: 'Your account' })).toBeVisible();
    expect(within(screen.getByRole('main')).queryByRole('link', { name: 'Open Admin center' })).not.toBeInTheDocument();
  });

  it('says the person is gone and reloads the page when the fresh account read returns 404', async () => {
    let listCalls = 0;
    const fake = installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: () => json(200, { items: ++listCalls === 1 ? [owner, petr] : [owner], nextCursor: null }),
      [accountPath(petr.userId)]: json(404, { type: 'about:blank', title: 'Not Found', status: 404, code: 'not_found' }),
    });
    app('/admin');
    await waitFor(() => expect(rowOf('petr')).toBeVisible());
    await userEvent.click(screen.getByRole('button', { name: 'Make administrator: petr' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Make administrator' }));

    expect(await screen.findByText('That person is no longer a member of this household. The list has been refreshed.')).toBeVisible();
    await waitFor(() => expect(listCalls).toBe(2));
    await waitFor(() => expect(screen.getByRole('main').querySelectorAll('li[data-account-id]')).toHaveLength(1));
    expect(fake.requests.filter((request) => request.method === 'PATCH' && request.path === `${base}/accounts/${petr.userId}`)).toHaveLength(0);
  });

  it('settles the only-administrators state when the fresh account read is refused', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [owner, petr], nextCursor: null }),
      [accountPath(petr.userId)]: forbidden(),
    });
    app('/admin');
    await waitFor(() => expect(rowOf('petr')).toBeVisible());
    await userEvent.click(screen.getByRole('button', { name: 'Make administrator: petr' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Make administrator' }));

    expect(await screen.findByRole('heading', { name: 'Only administrators can open the Admin center.' })).toBeVisible();
    expect(screen.getByRole('main').querySelectorAll('li[data-account-id]')).toHaveLength(0);
  });

  it('keeps the list and speaks the common failure when the fresh account read fails', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [owner, petr], nextCursor: null }),
      [accountPath(petr.userId)]: json(500, { type: 'about:blank', title: 'Unavailable', status: 500 }),
    });
    app('/admin');
    await waitFor(() => expect(rowOf('petr')).toBeVisible());
    await userEvent.click(screen.getByRole('button', { name: 'Make administrator: petr' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Make administrator' }));

    expect(await screen.findByText('That didn’t work. Check your connection and try again.')).toBeVisible();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(rowOf('petr')).toHaveTextContent('Member');
  });

  it('says the person is gone and reloads the page when the change returns 404', async () => {
    let listCalls = 0;
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: () => json(200, { items: ++listCalls === 1 ? [owner, petr] : [owner], nextCursor: null }),
      [accountPath(petr.userId)]: json(200, petr, { ETag: '"6"' }),
      [changePath(petr.userId)]: json(404, { type: 'about:blank', title: 'Not Found', status: 404, code: 'not_found' }),
    });
    app('/admin');
    await waitFor(() => expect(rowOf('petr')).toBeVisible());
    await userEvent.click(screen.getByRole('button', { name: 'Make administrator: petr' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Make administrator' }));

    expect(await screen.findByText('That person is no longer a member of this household. The list has been refreshed.')).toBeVisible();
    await waitFor(() => expect(listCalls).toBe(2));
    await waitFor(() => expect(screen.getByRole('main').querySelectorAll('li[data-account-id]')).toHaveLength(1));
  });

  it('keeps the list and speaks the common failure when the change loses the network', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [owner, petr], nextCursor: null }),
      [accountPath(petr.userId)]: json(200, petr, { ETag: '"6"' }),
      [changePath(petr.userId)]: () => Promise.reject(new Error('offline')),
    });
    app('/admin');
    await waitFor(() => expect(rowOf('petr')).toBeVisible());
    await userEvent.click(screen.getByRole('button', { name: 'Make administrator: petr' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Make administrator' }));

    expect(await screen.findByText('That didn’t work. Check your connection and try again.')).toBeVisible();
    expect(rowOf('petr')).toHaveTextContent('Member');
    expect(screen.getByRole('button', { name: 'Make administrator: petr' })).toBeEnabled();
  });

  it('keeps the current page and offers an inline retry when a pager load fails', async () => {
    let failNextPage = true;
    const fake = installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: (request) => {
        if (!request.query.get('cursor')) return json(200, { items: [owner, petr], nextCursor: 'page-2' });
        if (failNextPage) return json(503, { type: 'about:blank', title: 'Unavailable', status: 503, code: 'unavailable' });
        return json(200, { items: [both], nextCursor: null });
      },
    });
    app('/admin');
    await waitFor(() => expect(rowOf('petr')).toBeVisible());
    await userEvent.click(within(screen.getByRole('navigation', { name: 'Accounts' })).getByRole('button', { name: 'Next page' }));

    // The failed page keeps the rows already on screen and offers a retry instead of jumping back to page one.
    expect(await screen.findByText('Accounts couldn’t be loaded. Check your connection and try again.')).toBeVisible();
    expect(rowOf('anna')).toBeVisible();
    expect(rowOf('petr')).toBeVisible();
    expect(within(screen.getByRole('navigation', { name: 'Accounts' })).getByText('Page 1')).toBeVisible();

    failNextPage = false;
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));

    await waitFor(() => expect(rowOf('jana')).toBeVisible());
    expect(screen.queryByText('Accounts couldn’t be loaded. Check your connection and try again.')).not.toBeInTheDocument();
    expect(within(screen.getByRole('navigation', { name: 'Accounts' })).getByText('Page 2')).toBeVisible();
    expect(fake.requests.filter((request) => request.method === 'GET' && request.path === `${base}/accounts` && request.query.get('cursor') === 'page-2')).toHaveLength(2);
  });

  it('offers password reset only where a local password exists and the account is someone else', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [owner, petr, both, localMember], nextCursor: null }),
    });
    app('/admin');
    await waitFor(() => expect(rowOf('petr')).toBeVisible());

    // The viewer's own row never offers it, even with a local password.
    expect(within(rowOf('anna')).queryByRole('button', { name: /Reset password/ })).not.toBeInTheDocument();
    // Single sign-on only: a calm explanation instead of an action that could never work.
    expect(within(rowOf('petr')).queryByRole('button', { name: /Reset password/ })).not.toBeInTheDocument();
    expect(rowOf('petr')).toHaveTextContent('This account signs in with single sign-on, so there’s no password to reset.');
    // Password only and password plus single sign-on both qualify.
    expect(within(rowOf('marta')).getByRole('button', { name: 'Reset password: marta' })).toBeVisible();
    expect(within(rowOf('jana')).getByRole('button', { name: 'Reset password: jana' })).toBeVisible();
  });

  it('issues a one-time reset link, copies it, and drops it from memory when the dialog closes', async () => {
    const token = 'A'.repeat(43);
    const expiresAt = '2030-01-02T10:30:00Z';
    const writeText = vi.fn(async () => undefined);
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } as unknown as Clipboard });
    const fake = installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [owner, both], nextCursor: null }),
      [`POST ${base}/accounts/${both.userId}/password-reset`]: json(201, { token, createdAt: '2030-01-01T10:30:00Z', expiresAt }),
    });
    app('/admin');
    await waitFor(() => expect(rowOf('jana')).toBeVisible());

    await userEvent.click(screen.getByRole('button', { name: 'Reset password: jana' }));
    const confirm = await screen.findByRole('dialog');
    expect(confirm).toHaveTextContent('Reset the password for jana?');
    expect(confirm).toHaveTextContent('The current password keeps working until the link is used.');
    expect(confirm).toHaveTextContent('Using the link signs them out on every device.');
    expect(confirm).toHaveTextContent('The link works once and expires after 24 hours.');
    expect(confirm).toHaveTextContent('You won’t see or choose their new password.');
    await userEvent.click(within(confirm).getByRole('button', { name: 'Create link' }));

    const linkDialog = await screen.findByRole('dialog', { name: 'Reset link for jana' });
    const link = `${window.location.origin}/reset-password#${token}`;
    const linkField = within(linkDialog).getByRole('textbox', { name: 'Reset link' }) as HTMLInputElement;
    expect(linkField).toHaveValue(link);
    expect(linkField).toHaveAttribute('readonly');
    // Focus lands on the one-time link after the dialog opens, and the field selects itself for copying.
    await waitFor(() => expect(linkField).toHaveFocus());
    expect(linkField.selectionStart).toBe(0);
    expect(linkField.selectionEnd).toBe(link.length);
    expect(linkDialog).toHaveTextContent('This link works once and expires');
    const issue = fake.requests.find((request) => request.method === 'POST' && request.path === `${base}/accounts/${both.userId}/password-reset`);
    expect(issue?.body).toBe('{}');
    expect(rowOf('jana')).toHaveTextContent('Reset link active until');
    expect(within(rowOf('jana')).getByRole('button', { name: 'Create a new reset link: jana' })).toBeVisible();

    await userEvent.click(within(linkDialog).getByRole('button', { name: 'Copy' }));
    expect(writeText).toHaveBeenCalledWith(link);
    await waitFor(() => expect(within(linkDialog).getByRole('button', { name: 'Copied' })).toBeVisible());
    expect(linkDialog).toHaveTextContent('Reset link copied.');

    await userEvent.click(within(linkDialog).getByRole('button', { name: 'Done' }));
    expect(screen.queryByRole('dialog', { name: 'Reset link for jana' })).not.toBeInTheDocument();
    expect(screen.queryByDisplayValue(link)).not.toBeInTheDocument();
    expect(window.location.href).not.toContain(token);
    expect(JSON.stringify({ ...localStorage, ...sessionStorage })).not.toContain(token);
    expect(document.body.textContent).not.toContain(token);
  });

  it('selects the link and explains the manual copy when the clipboard is unavailable', async () => {
    const token = 'C'.repeat(43);
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: vi.fn(async () => { throw new Error('blocked'); }) } as unknown as Clipboard,
    });
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [owner, both], nextCursor: null }),
      [`POST ${base}/accounts/${both.userId}/password-reset`]: json(201, {
        token, createdAt: '2030-01-01T10:30:00Z', expiresAt: '2030-01-02T10:30:00Z',
      }),
    });
    app('/admin');
    await waitFor(() => expect(rowOf('jana')).toBeVisible());
    await userEvent.click(screen.getByRole('button', { name: 'Reset password: jana' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Create link' }));

    const linkDialog = await screen.findByRole('dialog', { name: 'Reset link for jana' });
    const field = within(linkDialog).getByRole('textbox', { name: 'Reset link' }) as HTMLInputElement;
    await userEvent.click(within(linkDialog).getByRole('button', { name: 'Copy' }));

    expect(await within(linkDialog).findByText('The link could not be copied. It is selected so you can copy it manually.')).toBeVisible();
    expect(field).toHaveFocus();
    expect(field.selectionStart).toBe(0);
    expect(field.selectionEnd).toBe(field.value.length);
    expect(within(linkDialog).getByRole('button', { name: 'Copy' })).toBeVisible();
  });

  it('keeps the active-link note on the viewer’s own row but offers no cancel the server would refuse', async () => {
    const viewerReset: Account = {
      ...owner,
      passwordReset: { createdAt: '2030-01-01T10:30:00Z', expiresAt: '2030-01-02T10:30:00Z' },
    };
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [viewerReset, both], nextCursor: null }),
    });
    app('/admin');
    await waitFor(() => expect(rowOf('anna')).toBeVisible());

    expect(rowOf('anna')).toHaveTextContent('Reset link active until');
    expect(within(rowOf('anna')).queryByRole('button', { name: 'Cancel the reset link: anna' })).not.toBeInTheDocument();
    expect(within(rowOf('anna')).queryByRole('button', { name: 'Reset password: anna' })).not.toBeInTheDocument();
    expect(within(rowOf('anna')).queryByRole('button', { name: 'Create a new reset link: anna' })).not.toBeInTheDocument();
    expect(within(rowOf('anna')).getByRole('button', { name: 'Make member: anna' })).toBeVisible();
    // Someone else's outstanding link still has both actions.
    expect(within(rowOf('jana')).getByRole('button', { name: 'Reset password: jana' })).toBeVisible();
  });

  it('shows an outstanding link, replaces it, and cancels it without touching the account', async () => {
    const outstanding: Account = { ...both, passwordReset: { createdAt: '2030-01-01T10:30:00Z', expiresAt: '2030-01-02T10:30:00Z' } };
    const fake = installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [owner, outstanding], nextCursor: null }),
      [`DELETE ${base}/accounts/${both.userId}/password-reset`]: json(204),
    });
    app('/admin');
    await waitFor(() => expect(rowOf('jana')).toBeVisible());
    expect(rowOf('jana')).toHaveTextContent('Reset link active until');
    expect(within(rowOf('jana')).getByRole('button', { name: 'Create a new reset link: jana' })).toBeVisible();

    await userEvent.click(screen.getByRole('button', { name: 'Create a new reset link: jana' }));
    const renew = await screen.findByRole('dialog');
    expect(renew).toHaveTextContent('Replace the reset link for jana?');
    expect(renew).toHaveTextContent('The existing link stops working as soon as you create the new one.');
    await userEvent.click(within(renew).getByRole('button', { name: 'Cancel' }));

    await userEvent.click(screen.getByRole('button', { name: 'Cancel the reset link: jana' }));
    expect(await screen.findByText('The reset link for jana was cancelled.')).toBeVisible();
    await waitFor(() => expect(rowOf('jana')).not.toHaveTextContent('Reset link active until'));
    expect(within(rowOf('jana')).getByRole('button', { name: 'Reset password: jana' })).toBeVisible();
    expect(fake.requests.filter((request) => request.method === 'DELETE' && request.path === `${base}/accounts/${both.userId}/password-reset`)).toHaveLength(1);
    expect(fake.requests.some((request) => request.method === 'PATCH')).toBe(false);
  });

  it.each([
    [409, 'cannot_reset_self', 'You can’t reset your own password here. Use Account to change it.'],
    [404, 'not_found', 'That person is no longer a member of this household. The list has been refreshed.'],
    [429, 'rate_limited', 'Too many reset links were created just now. Wait a little, then try again.'],
    [503, 'unavailable', 'The reset link couldn’t be created. Check your connection and try again.'],
  ])('speaks the calm outcome for a %i reset issue refusal', async (status, code, message) => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [owner, both], nextCursor: null }),
      [`POST ${base}/accounts/${both.userId}/password-reset`]: json(status, { type: 'about:blank', title: 'x', status, code }),
    });
    app('/admin');
    await waitFor(() => expect(rowOf('jana')).toBeVisible());
    await userEvent.click(screen.getByRole('button', { name: 'Reset password: jana' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Create link' }));

    expect(await screen.findByText(message)).toBeVisible();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(rowOf('jana')).not.toHaveTextContent('Reset link active until');
    expect(within(rowOf('jana')).getByRole('button', { name: 'Reset password: jana' })).toBeEnabled();
  });

  it('reloads the list when the account turns out to have no local password after all', async () => {
    let listCalls = 0;
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: () => json(200, { items: ++listCalls === 1 ? [owner, both] : [owner, petr], nextCursor: null }),
      [`POST ${base}/accounts/${both.userId}/password-reset`]: json(409, { type: 'about:blank', title: 'Conflict', status: 409, code: 'no_local_password' }),
    });
    app('/admin');
    await waitFor(() => expect(rowOf('jana')).toBeVisible());
    await userEvent.click(screen.getByRole('button', { name: 'Reset password: jana' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Create link' }));

    expect(await screen.findByText('jana signs in with single sign-on, so there’s no password to reset.')).toBeVisible();
    await waitFor(() => expect(listCalls).toBe(2));
    expect(rowOf('petr')).toHaveTextContent('This account signs in with single sign-on');
  });

  it('re-reads the viewer role and drops the Admin center when a reset is refused as a member', async () => {
    let householdCalls = 0;
    const fake = installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: () => json(200, ++householdCalls === 1 ? household : memberHousehold),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [owner, both], nextCursor: null }),
      [`POST ${base}/accounts/${both.userId}/password-reset`]: forbidden(),
    });
    app('/admin');
    await waitFor(() => expect(within(nav()).getByRole('link', { name: 'Admin center' })).toBeInTheDocument());
    await userEvent.click(screen.getByRole('button', { name: 'Reset password: jana' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Create link' }));

    expect(await screen.findByRole('heading', { name: 'Only administrators can open the Admin center.' })).toBeVisible();
    expect(screen.getByRole('main').querySelectorAll('li[data-account-id]')).toHaveLength(0);
    await waitFor(() => expect(within(nav()).queryByRole('link', { name: 'Admin center' })).not.toBeInTheDocument());
    expect(fake.requests.filter((request) => request.method === 'GET' && request.path === base)).toHaveLength(2);
  });

  it('settles the only-administrators state when cancelling a link is refused', async () => {
    const outstanding: Account = { ...both, passwordReset: { createdAt: '2030-01-01T10:30:00Z', expiresAt: '2030-01-02T10:30:00Z' } };
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [owner, outstanding], nextCursor: null }),
      [`DELETE ${base}/accounts/${both.userId}/password-reset`]: forbidden(),
    });
    app('/admin');
    await waitFor(() => expect(rowOf('jana')).toBeVisible());
    await userEvent.click(screen.getByRole('button', { name: 'Cancel the reset link: jana' }));

    expect(await screen.findByRole('heading', { name: 'Only administrators can open the Admin center.' })).toBeVisible();
    expect(screen.getByRole('main').querySelectorAll('li[data-account-id]')).toHaveLength(0);
  });

  it('renders the Czech Admin center', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [owner, petr], nextCursor: null }),
    });
    app('/admin');
    expect(await screen.findByRole('heading', { level: 1, name: 'Správa domácnosti' })).toBeVisible();
    const navCs = screen.getByRole('navigation', { name: 'Navigace domácnosti' });
    await waitFor(() => expect(within(navCs).getByRole('link', { name: 'Správa domácnosti' })).toHaveAttribute('href', '/admin'));
    await waitFor(() => expect(rowOf('petr')).toHaveTextContent('Člen'));
    expect(screen.getByRole('button', { name: 'Jmenovat správcem: petr' })).toHaveTextContent('Jmenovat správcem');
    expect(screen.getByRole('main')).toHaveTextContent('Heslo');
    expect(screen.getByRole('main')).toHaveTextContent('Jednotné přihlášení');
  });
});
