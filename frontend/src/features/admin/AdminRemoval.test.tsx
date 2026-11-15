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
const removalPath = (userId: string) => `DELETE ${base}/accounts/${userId}`;
const anna: Account = { userId: session().userId, login: 'anna', role: 'admin', signInMethods: { password: true, oidc: false }, passwordReset: null };
const petr: Account = { userId: '0198a2f0-0000-7000-8000-000000000002', login: 'petr', role: 'member', signInMethods: { password: false, oidc: true }, passwordReset: null };
const problem = (status: number, code: string) => json(status, { type: 'about:blank', title: 'x', status, code });

function app(path = '/admin') {
  window.history.replaceState(null, '', path);
  return render(<I18nProvider><App /></I18nProvider>);
}
const nav = () => screen.getByRole('navigation', { name: 'Household navigation' });
function rowOf(login: string) {
  const rows = screen.getByRole('main').querySelectorAll('li[data-account-id]');
  return [...rows].find((row) => row.querySelector('[data-account-login]')?.textContent === login) as HTMLElement;
}
const rowsOnScreen = () => screen.getByRole('main').querySelectorAll('li[data-account-id]').length;

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

describe('Admin center membership removal', () => {
  it('asks before removing a member, quotes the fresh version, and only then drops the row', async () => {
    let release!: (response: Response) => void;
    const held = new Promise<Response>((resolve) => { release = resolve; });
    let listCalls = 0;
    const fake = installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: () => json(200, { items: ++listCalls === 1 ? [anna, petr] : [anna], nextCursor: null }),
      [accountPath(petr.userId)]: json(200, petr, { ETag: '"7"' }),
      [removalPath(petr.userId)]: () => held,
    });
    app();
    await waitFor(() => expect(rowOf('petr')).toBeVisible());

    await userEvent.click(screen.getByRole('button', { name: 'Remove from household: petr' }));
    const dialog = await screen.findByRole('dialog');
    expect(dialog).toHaveTextContent('Remove petr from Veselí?');
    expect(dialog).toHaveTextContent('petr will lose access to this household.');
    expect(dialog).toHaveTextContent('Their items stay here, but no one will be responsible for them.');
    expect(dialog).toHaveTextContent('Completed items stay in the history.');
    expect(dialog).toHaveTextContent('Invitations and password reset links for this household stop working.');
    expect(dialog).toHaveTextContent('They keep their other households.');

    await userEvent.click(within(dialog).getByRole('button', { name: 'Remove from household' }));
    // Nothing leaves the list before the server answers 204: the row stays, every control waits.
    expect(await within(dialog).findByRole('button', { name: 'Removing…' })).toBeDisabled();
    expect(within(dialog).getByRole('button', { name: 'Cancel' })).toBeDisabled();
    expect(rowOf('petr')).toBeVisible();
    expect(within(rowOf('petr')).getByRole('button', { name: 'Remove from household: petr' })).toBeDisabled();
    expect(fake.requests.filter((request) => request.method === 'DELETE')).toHaveLength(1);

    release(json(204));
    expect(await screen.findByText('petr was removed from this household.')).toBeVisible();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await waitFor(() => expect(rowsOnScreen()).toBe(1));

    const read = fake.requests.find((request) => request.method === 'GET' && request.path === `${base}/accounts/${petr.userId}`);
    const removal = fake.requests.find((request) => request.method === 'DELETE' && request.path === `${base}/accounts/${petr.userId}`);
    expect(removal?.headers.get('If-Match')).toBe('"7"');
    // The fresh read must precede the removal it authorizes.
    expect(fake.requests.indexOf(read!)).toBeLessThan(fake.requests.indexOf(removal!));
  });

  it('asks the server nothing when the confirmation is dismissed', async () => {
    const fake = installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
    });
    app();
    await waitFor(() => expect(rowOf('petr')).toBeVisible());

    const trigger = screen.getByRole('button', { name: 'Remove from household: petr' });
    await userEvent.click(trigger);
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Cancel' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(trigger).toHaveFocus();
    expect(rowsOnScreen()).toBe(2);
    expect(fake.requests.some((request) => request.method === 'DELETE')).toBe(false);
    expect(fake.requests.some((request) => request.method === 'GET' && request.path === `${base}/accounts/${petr.userId}`)).toBe(false);
  });

  it('lets the viewer leave from their own row and lands on Home without a household', async () => {
    let current = session();
    const fake = installFakeServer({
      'GET /api/v1/session': () => json(200, current),
      // The viewer's own leave quotes the household's viewerMembershipVersion,
      // read again right before the DELETE; the account route is not needed.
      [`GET ${base}`]: json(200, { ...household, viewerMembershipVersion: 5 }),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [accountPath(anna.userId)]: json(403, { type: 'about:blank', title: 'Forbidden', status: 403, code: 'admin_required' }),
      [removalPath(anna.userId)]: () => { current = session(false); return json(204); },
    });
    app();
    await waitFor(() => expect(rowOf('anna')).toBeVisible());

    await userEvent.click(screen.getByRole('button', { name: 'Leave household: anna' }));
    const dialog = await screen.findByRole('dialog');
    expect(dialog).toHaveTextContent('Leave Veselí?');
    expect(dialog).toHaveTextContent('You’ll lose access to this household.');
    expect(dialog).toHaveTextContent('Your items stay here, but no one will be responsible for them.');
    expect(dialog).toHaveTextContent('You keep your other households.');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Leave household' }));

    expect(await screen.findByText('You left Veselí. You’re not in a household now — join one with an invitation.')).toBeVisible();
    expect(window.location.pathname).toBe('/');
    expect(await screen.findByRole('heading', { level: 1, name: 'Join your household by invitation' })).toBeVisible();
    // The shell decides where Home opens from a fresh session read, not from the household this screen booted with.
    expect(fake.requests.filter((request) => request.method === 'GET' && request.path === '/api/v1/session')).toHaveLength(2);
    const removal = fake.requests.find((request) => request.method === 'DELETE' && request.path === `${base}/accounts/${anna.userId}`);
    expect(removal?.headers.get('If-Match')).toBe('"5"');
    expect(fake.requests.some((request) => request.method === 'GET' && request.path === accountPath(anna.userId))).toBe(false);
  });

  it('opens the next household when one remains, and says where the menu is', async () => {
    const otherId = '0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62';
    const otherBase = `/api/v1/households/${otherId}`;
    const declarationHousehold = { ...household, viewerMembershipVersion: 6 };
    let current = session();
    const fake = installFakeServer({
      'GET /api/v1/session': () => json(200, current),
      [`GET ${base}`]: json(200, declarationHousehold),
      [`GET ${otherBase}`]: json(200, { ...declarationHousehold, id: otherId, name: 'Praha' }),
      ...itemRoutes(base, []),
      ...itemRoutes(otherBase, []),
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [accountPath(anna.userId)]: json(403, { type: 'about:blank', title: 'Forbidden', status: 403, code: 'admin_required' }),
      [removalPath(anna.userId)]: () => { current = { ...session(), defaultHouseholdId: otherId }; return json(204); },
    });
    app();
    await waitFor(() => expect(rowOf('anna')).toBeVisible());
    await userEvent.click(screen.getByRole('button', { name: 'Leave household: anna' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Leave household' }));

    expect(await screen.findByText('You left Veselí. Choose another household in the menu above.')).toBeVisible();
    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toBeVisible();
    // Home opens in the household the fresh session names, and that household is what the shell reads.
    await waitFor(() => expect(fake.requests.some((request) => request.path === otherBase)).toBe(true));
    expect(window.location.pathname).toBe('/');
    const removal = fake.requests.find((request) => request.method === 'DELETE' && request.path === `${base}/accounts/${anna.userId}`);
    expect(removal?.headers.get('If-Match')).toBe('"6"');
  });

  it.each([
    [404, 'not_found', 'That person is no longer a member of this household. The list has been refreshed.', true],
    [412, 'precondition_failed', 'This account changed elsewhere. The list has been refreshed; check the current role and try again.', true],
    [409, 'last_admin', 'A household needs at least one administrator. Make someone else an administrator first.', false],
    [503, 'unavailable', 'That didn’t work. Check your connection and try again.', false],
  ])('speaks the calm outcome for a %i removal refusal', async (status, code, message, reloads) => {
    let listCalls = 0;
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: () => json(200, { items: ++listCalls === 1 ? [anna, petr] : [anna], nextCursor: null }),
      [accountPath(petr.userId)]: json(200, petr, { ETag: '"7"' }),
      [removalPath(petr.userId)]: problem(status, code),
    });
    app();
    await waitFor(() => expect(rowOf('petr')).toBeVisible());
    await userEvent.click(screen.getByRole('button', { name: 'Remove from household: petr' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Remove from household' }));

    expect(await screen.findByText(message)).toBeVisible();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    // A gone or changed membership asks the list again; a refusal leaves the rows exactly as they were.
    await waitFor(() => expect(listCalls).toBe(reloads ? 2 : 1));
    await waitFor(() => expect(rowsOnScreen()).toBe(reloads ? 1 : 2));
  });

  it('tells the last administrator to promote someone before leaving', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [anna], nextCursor: null }),
      [accountPath(anna.userId)]: json(403, { type: 'about:blank', title: 'Forbidden', status: 403, code: 'admin_required' }),
      [removalPath(anna.userId)]: problem(409, 'last_admin'),
    });
    app();
    await waitFor(() => expect(rowOf('anna')).toBeVisible());
    await userEvent.click(screen.getByRole('button', { name: 'Leave household: anna' }));
    const dialog = await screen.findByRole('dialog');
    expect(dialog).toHaveTextContent('A household always needs an administrator, so the last one can’t be removed.');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Leave household' }));

    expect(await screen.findByText('A household always needs at least one administrator. Make someone else an administrator before you leave.')).toBeVisible();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(window.location.pathname).toBe('/admin');
    expect(rowOf('anna')).toBeVisible();
  });

  it('settles the only-administrators state when the removal is refused as a member', async () => {
    let householdCalls = 0;
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: () => json(200, ++householdCalls === 1 ? household : memberHousehold),
      ...itemRoutes(base, []),
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [accountPath(petr.userId)]: json(200, petr, { ETag: '"7"' }),
      [removalPath(petr.userId)]: problem(403, 'admin_required'),
    });
    app();
    await waitFor(() => expect(rowOf('petr')).toBeVisible());
    await userEvent.click(screen.getByRole('button', { name: 'Remove from household: petr' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Remove from household' }));

    expect(await screen.findByRole('heading', { name: 'Only administrators can open the Admin center.' })).toBeVisible();
    expect(rowsOnScreen()).toBe(0);
    await waitFor(() => expect(within(nav()).queryByRole('link', { name: 'Admin center' })).not.toBeInTheDocument());
  });

  it('says the person is already gone when the fresh read returns 404', async () => {
    let listCalls = 0;
    const fake = installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, household),
      ...itemRoutes(base, []),
      [accountsPath]: () => json(200, { items: ++listCalls === 1 ? [anna, petr] : [anna], nextCursor: null }),
      [accountPath(petr.userId)]: problem(404, 'not_found'),
    });
    app();
    await waitFor(() => expect(rowOf('petr')).toBeVisible());
    await userEvent.click(screen.getByRole('button', { name: 'Remove from household: petr' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Remove from household' }));

    expect(await screen.findByText('That person is no longer a member of this household. The list has been refreshed.')).toBeVisible();
    await waitFor(() => expect(listCalls).toBe(2));
    await waitFor(() => expect(rowsOnScreen()).toBe(1));
    // Without a fresh version there is nothing to quote, so nothing is sent.
    expect(fake.requests.some((request) => request.method === 'DELETE')).toBe(false);
  });
});
