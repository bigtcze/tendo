import { act, render, screen, waitFor, within } from '@testing-library/react';
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
const elsewhere = '0198a2f0-0000-7000-8000-000000000021';
const listPath = 'GET /api/v1/admin/households';
const accountsLink = `/api/v1/admin/households/${target}/accounts`;
const accountsPath = `GET ${accountsLink}`;
const accountLink = (userId: string) => `${accountsLink}/${userId}`;
const readPath = (userId: string) => `GET ${accountLink(userId)}`;
const changePath = (userId: string) => `PATCH ${accountLink(userId)}`;
const removalPath = (userId: string) => `DELETE ${accountLink(userId)}`;
const detailPath = `/admin/households/${target}`;
const problem = (status: number, code?: string) => json(status, { type: 'about:blank', title: 'x', status, ...(code ? { code } : {}) });

const veseli: AdminHousehold = { id: target, name: 'Veselí', timezone: 'Europe/Prague', createdAt: '2026-10-06T10:00:00Z' };
const anna: AdminAccount = { userId: session().userId, login: 'anna', role: 'admin', signInMethods: { password: true, oidc: false } };
const annaAsMember: AdminAccount = { ...anna, role: 'member' };
const petr: AdminAccount = { userId: '0198a2f0-0000-7000-8000-000000000002', login: 'petr', role: 'member', signInMethods: { password: false, oidc: true } };
const petrAsAdmin: AdminAccount = { ...petr, role: 'admin' };

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

function rowOf(login: string) {
  return accountRows().find((row) => row.querySelector('[data-install-login]')?.textContent === login) as HTMLElement;
}

/** The installation grant is the only authority here: nothing may ever be read from the household on screen. */
function expectNoHouseholdReads(fake: ReturnType<typeof installFakeServer>) {
  expect(fake.requests.filter((request) => request.path.startsWith(`/api/v1/households/${target}`))).toHaveLength(0);
}

function sessionReads(fake: ReturnType<typeof installFakeServer>) {
  return fake.requests.filter((request) => request.method === 'GET' && request.path === '/api/v1/session').length;
}

function nav() {
  return screen.getByRole('navigation', { name: 'Household navigation' });
}

/** The one Admin center entry the rail offers; its target says which center the viewer's own role really opens. */
function adminEntry() {
  return within(nav()).getByRole('link', { name: 'Admin center' });
}

async function confirm(trigger: string, label: string) {
  await userEvent.click(screen.getByRole('button', { name: trigger }));
  const dialog = await screen.findByRole('dialog');
  await userEvent.click(within(dialog).getByRole('button', { name: label }));
  return dialog;
}

/**
 * The session read that follows a self change, held open until the test lets it through. The boot is answered at
 * once; every later read waits for the gate and then answers what the test settled on, so a test can watch the
 * screen while the shell is still holding the session it booted with.
 */
function heldSession(after: () => Response = () => json(200, instanceAdminSession())) {
  let open!: () => void;
  const gate = new Promise<void>((resolve) => { open = resolve; });
  let calls = 0;
  return {
    route: async () => {
      calls += 1;
      if (calls === 1) return json(200, instanceAdminSession());
      await gate;
      return after();
    },
    release: () => open(),
    calls: () => calls,
  };
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
  it('drills down from the list under the household’s own name, with each account’s two actions', async () => {
    const fake = renderAt({
      [listPath]: json(200, { items: [veseli], nextCursor: null }),
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
    }, '/admin/households');
    await userEvent.click(await screen.findByRole('link', { name: 'Veselí' }));
    await waitFor(() => expect(screen.getByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible());
    expect(window.location.pathname).toBe(detailPath);

    await waitFor(() => expect(accountRows()).toHaveLength(2));
    const [first, second] = accountRows();
    expect(first).toHaveTextContent('anna (you)');
    expect(first).toHaveTextContent('Administrator');
    expect(first).toHaveTextContent('Password');
    expect(second).toHaveTextContent('petr');
    expect(second).toHaveTextContent('Member');
    expect(second).toHaveTextContent('Single sign-on');
    expect(screen.getByRole('heading', { level: 2, name: 'Accounts' })).toBeVisible();
    const main = screen.getByRole('main');
    expect(main).toHaveTextContent('Everyone who belongs to this household, their role and how they sign in.');
    expect(within(main).getByRole('link', { name: 'Back to households' })).toHaveAttribute('href', '/admin/households');
    // The viewer's own row offers leaving instead of removal; the other row offers both membership actions.
    expect(within(first!).getByRole('button', { name: 'Make member: anna' })).toBeVisible();
    expect(within(first!).getByRole('button', { name: 'Leave household: anna' })).toBeVisible();
    expect(within(first!).queryByRole('button', { name: 'Remove from household: anna' })).not.toBeInTheDocument();
    expect(within(second!).getByRole('button', { name: 'Make administrator: petr' })).toBeVisible();
    expect(within(second!).getByRole('button', { name: 'Remove from household: petr' })).toBeVisible();
    // The screen's one primary action, then the two row actions per account: no password help, no row links.
    expect(within(main).getByRole('button', { name: 'Add an account' })).toBeVisible();
    expect(within(main).queryAllByRole('button')).toHaveLength(5);
    expect(within(first!).queryAllByRole('link')).toHaveLength(0);
    expect(within(second!).queryAllByRole('link')).toHaveLength(0);
    expectNoHouseholdReads(fake);
    expect(fake.requests.map((request) => request.path).filter((path) => path.startsWith('/api/v1/admin/')))
      .toEqual(['/api/v1/admin/households', accountsLink]);
  });

  it('falls back to a generic heading on a direct load instead of reading the list again', async () => {
    const fake = renderAt({ [accountsPath]: json(200, { items: [anna], nextCursor: null }) });
    expect(await screen.findByRole('heading', { level: 1, name: 'Household accounts' })).toBeVisible();
    await waitFor(() => expect(accountRows()).toHaveLength(1));
    const paths = fake.requests.map((request) => request.path);
    expect(paths.filter((path) => path === '/api/v1/admin/households')).toHaveLength(0);
    expect(paths.filter((path) => path.startsWith('/api/v1/admin/'))).toEqual([accountsLink]);
  });

  it('shows the generic heading again when history comes back to the detail', async () => {
    renderAt({
      [listPath]: json(200, { items: [veseli], nextCursor: null }),
      [accountsPath]: json(200, { items: [anna], nextCursor: null }),
    }, '/admin/households');
    await userEvent.click(await screen.findByRole('link', { name: 'Veselí' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible();

    await act(async () => {
      window.history.back();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    expect(await screen.findByRole('heading', { level: 2, name: 'Households' })).toBeVisible();

    // A history entry never carried the name, so returning Forward shows the generic heading instead of a name
    // that may not belong to the household any more.
    await act(async () => {
      window.history.forward();
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    expect(await screen.findByRole('heading', { level: 1, name: 'Household accounts' })).toBeVisible();
    expect(window.location.pathname).toBe(detailPath);
  });

  it('makes another member an administrator only after reading the version it quotes', async () => {
    const fake = renderAt({
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [readPath(petr.userId)]: json(200, petr, { ETag: '"3"' }),
      [changePath(petr.userId)]: json(200, petrAsAdmin),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));

    await userEvent.click(screen.getByRole('button', { name: 'Make administrator: petr' }));
    const dialog = await screen.findByRole('dialog');
    expect(dialog).toHaveTextContent('Make petr an administrator?');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Make administrator' }));

    expect(await screen.findByText('petr is now an administrator.')).toBeVisible();
    expect(within(rowOf('petr')).getByText('Administrator')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Make member: petr' })).toBeEnabled();

    const read = fake.requests.find((request) => request.method === 'GET' && request.path === accountLink(petr.userId));
    const patch = fake.requests.find((request) => request.method === 'PATCH' && request.path === accountLink(petr.userId));
    expect(patch?.headers.get('If-Match')).toBe('"3"');
    expect(JSON.parse(patch!.body)).toEqual({ role: 'admin' });
    expect(fake.requests.indexOf(read!)).toBeLessThan(fake.requests.indexOf(patch!));
    // Someone else's role is not the viewer's session: the shell is asked nothing.
    expect(sessionReads(fake)).toBe(1);
    expectNoHouseholdReads(fake);
  });

  it('makes another administrator a member', async () => {
    const fake = renderAt({
      [accountsPath]: json(200, { items: [anna, petrAsAdmin], nextCursor: null }),
      [readPath(petr.userId)]: json(200, petrAsAdmin, { ETag: '"4"' }),
      [changePath(petr.userId)]: json(200, petr),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));

    await userEvent.click(screen.getByRole('button', { name: 'Make member: petr' }));
    const dialog = await screen.findByRole('dialog');
    expect(dialog).toHaveTextContent('Make petr a member?');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Make member' }));

    expect(await screen.findByText('petr is now a member.')).toBeVisible();
    expect(within(rowOf('petr')).getByText('Member')).toBeVisible();
    expect(fake.requests.find((request) => request.method === 'PATCH')?.headers.get('If-Match')).toBe('"4"');
    expectNoHouseholdReads(fake);
  });

  it('removes another member from this household after quoting the version it read', async () => {
    let release!: (response: Response) => void;
    const held = new Promise<Response>((resolve) => { release = resolve; });
    let listCalls = 0;
    const fake = renderAt({
      [accountsPath]: () => json(200, { items: ++listCalls === 1 ? [anna, petr] : [anna], nextCursor: null }),
      [readPath(petr.userId)]: json(200, petr, { ETag: '"5"' }),
      [removalPath(petr.userId)]: () => held,
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));

    await userEvent.click(screen.getByRole('button', { name: 'Remove from household: petr' }));
    const dialog = await screen.findByRole('dialog');
    // The name only travelled in from the list; a direct load says “this household” and keeps the action.
    expect(dialog).toHaveTextContent('Remove petr from this household?');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Remove from household' }));

    // Nothing leaves the list before the server answers, and every row waits while it thinks.
    expect(await within(dialog).findByRole('button', { name: 'Removing…' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Make member: anna' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Leave household: anna' })).toBeDisabled();
    expect(rowOf('petr')).toBeVisible();

    release(json(204));
    expect(await screen.findByText('petr was removed from this household.')).toBeVisible();
    await waitFor(() => expect(accountRows()).toHaveLength(1));

    const read = fake.requests.find((request) => request.method === 'GET' && request.path === accountLink(petr.userId));
    const removal = fake.requests.find((request) => request.method === 'DELETE' && request.path === accountLink(petr.userId));
    expect(removal?.headers.get('If-Match')).toBe('"5"');
    expect(fake.requests.indexOf(read!)).toBeLessThan(fake.requests.indexOf(removal!));
    expect(sessionReads(fake)).toBe(1);
    expectNoHouseholdReads(fake);
  });

  it('demotes the viewer in this household without leaving the screen or the Admin center', async () => {
    let reads = 0;
    const fake = renderAt({
      'GET /api/v1/session': () => { reads += 1; return json(200, instanceAdminSession()); },
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [readPath(anna.userId)]: json(200, anna, { ETag: '"8"' }),
      [changePath(anna.userId)]: json(200, annaAsMember),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));

    await userEvent.click(screen.getByRole('button', { name: 'Make member: anna' }));
    const dialog = await screen.findByRole('dialog');
    // The installation grant, not the household role, opens this screen, so demoting yourself here costs nothing.
    expect(dialog).toHaveTextContent('You’ll become a member of this household. You can still manage its accounts here, under Households.');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Make member' }));

    expect(await screen.findByText('You’re now a member of this household.')).toBeVisible();
    await waitFor(() => expect(within(rowOf('anna')).getByText('Member')).toBeVisible());
    // The screen stays exactly where it was, and the shell reads the session again for the real household state.
    expect(window.location.pathname).toBe(detailPath);
    await waitFor(() => expect(reads).toBe(2));
    expect(screen.getByRole('heading', { level: 2, name: 'Accounts' })).toBeVisible();
    expectNoHouseholdReads(fake);
  });

  it('refreshes the role behind the rail when this household is the one the viewer calls home', async () => {
    // The target is the viewer's own default household here, so the shell's read and the screen's household coincide.
    const ownAccounts = `/api/v1/admin/households/${HOUSEHOLD_ID}/accounts`;
    const ownAccount = (userId: string) => `${ownAccounts}/${userId}`;
    let householdReads = 0;
    const fake = renderAt({
      [`GET ${base}`]: () => {
        householdReads += 1;
        // The viewer is an administrator of the household they call home until this change lands.
        return json(200, householdReads === 1 ? household : memberHousehold);
      },
      [`GET ${ownAccounts}`]: json(200, { items: [anna, petr], nextCursor: null }),
      [`GET ${ownAccount(anna.userId)}`]: json(200, anna, { ETag: '"8"' }),
      [`PATCH ${ownAccount(anna.userId)}`]: json(200, annaAsMember),
    }, `/admin/households/${HOUSEHOLD_ID}`);
    await waitFor(() => expect(accountRows()).toHaveLength(2));
    await waitFor(() => expect(adminEntry()).toHaveAttribute('href', '/admin'));
    const readsBefore = householdReads;

    await confirm('Make member: anna', 'Make member');

    expect(await screen.findByText('You’re now a member of this household.')).toBeVisible();
    // The role the rail shows is the viewer's own membership, so their household is read again after the change…
    await waitFor(() => expect(householdReads).toBe(readsBefore + 1));
    const patch = fake.requests.findIndex((request) => request.method === 'PATCH');
    const refresh = fake.requests.findIndex((request, index) =>
      index > patch && request.method === 'GET' && request.path === `/api/v1/households/${HOUSEHOLD_ID}`);
    expect(patch).toBeGreaterThan(-1);
    expect(refresh).toBeGreaterThan(patch);
    // …and the one Admin center entry now opens the installation list instead of this household's own center.
    await waitFor(() => expect(adminEntry()).toHaveAttribute('href', '/admin/households'));
    // The screen itself does not move: the installation grant is what opened it, and the grant is untouched.
    expect(window.location.pathname).toBe(`/admin/households/${HOUSEHOLD_ID}`);
    expect(screen.getByRole('heading', { level: 2, name: 'Accounts' })).toBeVisible();
  });

  it('leaves the viewer’s own household unread when the change belongs to another household', async () => {
    let householdReads = 0;
    const fake = renderAt({
      [`GET ${base}`]: () => { householdReads += 1; return json(200, household); },
      [accountsPath]: json(200, { items: [annaAsMember, petr], nextCursor: null }),
      [readPath(anna.userId)]: json(200, annaAsMember, { ETag: '"7"' }),
      [changePath(anna.userId)]: json(200, anna),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));
    await waitFor(() => expect(adminEntry()).toHaveAttribute('href', '/admin'));
    const readsBefore = householdReads;

    await confirm('Make administrator: anna', 'Make administrator');

    expect(await screen.findByText('You’re now an administrator of this household.')).toBeVisible();
    // The shell still reads the session, because a self change can move the default household…
    await waitFor(() => expect(sessionReads(fake)).toBe(2));
    // …but a role in someone else's household says nothing about the rail, so nothing is read for it.
    expect(householdReads).toBe(readsBefore);
    expect(adminEntry()).toHaveAttribute('href', '/admin');
    expectNoHouseholdReads(fake);
  });

  it('announces a self promotion as a promotion, not as the demotion wording', async () => {
    let reads = 0;
    const fake = renderAt({
      'GET /api/v1/session': () => { reads += 1; return json(200, instanceAdminSession()); },
      [accountsPath]: json(200, { items: [annaAsMember, petr], nextCursor: null }),
      [readPath(anna.userId)]: json(200, annaAsMember, { ETag: '"2"' }),
      [changePath(anna.userId)]: json(200, anna),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));

    await userEvent.click(screen.getByRole('button', { name: 'Make administrator: anna' }));
    const dialog = await screen.findByRole('dialog');
    expect(dialog).toHaveTextContent('Make anna an administrator?');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Make administrator' }));

    expect(await screen.findByText('You’re now an administrator of this household.')).toBeVisible();
    expect(screen.queryByText('You’re now a member of this household.')).not.toBeInTheDocument();
    await waitFor(() => expect(within(rowOf('anna')).getByText('Administrator')).toBeVisible());
    expect(window.location.pathname).toBe(detailPath);
    await waitFor(() => expect(reads).toBe(2));
    expectNoHouseholdReads(fake);
  });

  it('lets the viewer leave this household and keeps them on the screen they were reading', async () => {
    let reads = 0;
    let listCalls = 0;
    const fake = renderAt({
      'GET /api/v1/session': () => { reads += 1; return json(200, instanceAdminSession()); },
      [accountsPath]: () => json(200, { items: ++listCalls === 1 ? [anna, petr] : [petr], nextCursor: null }),
      [readPath(anna.userId)]: json(200, anna, { ETag: '"9"' }),
      [removalPath(anna.userId)]: json(204),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));

    await userEvent.click(screen.getByRole('button', { name: 'Leave household: anna' }));
    const dialog = await screen.findByRole('dialog');
    expect(dialog).toHaveTextContent('Leave this household?');
    expect(dialog).toHaveTextContent('You’ll lose access to this household.');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Leave household' }));

    expect(await screen.findByText('You’re no longer a member of this household.')).toBeVisible();
    // Leaving this household is not leaving the Admin center: the same screen survives, with the server's list.
    await waitFor(() => expect(accountRows()).toHaveLength(1));
    expect(rowOf('petr')).toBeVisible();
    expect(window.location.pathname).toBe(detailPath);
    expect(screen.queryByRole('heading', { level: 1, name: 'Home' })).not.toBeInTheDocument();
    await waitFor(() => expect(reads).toBe(2));
    expectNoHouseholdReads(fake);
  });

  it('keeps the self-change notice and the reader on the heading when a moved default household remounts the screen', async () => {
    let reads = 0;
    renderAt({
      'GET /api/v1/session': () => {
        reads += 1;
        return json(200, reads === 1 ? instanceAdminSession() : { ...instanceAdminSession(), defaultHouseholdId: elsewhere });
      },
      [`GET /api/v1/households/${elsewhere}`]: json(200, { ...household, id: elsewhere, name: 'U Nováků' }),
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [readPath(anna.userId)]: json(200, anna, { ETag: '"6"' }),
      [changePath(anna.userId)]: json(200, annaAsMember),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));
    // Nothing has moved this reader anywhere yet, so the heading below is focused by the handoff and not by a
    // screen transition.
    expect(screen.getByRole('heading', { level: 1, name: 'Household accounts' })).not.toHaveFocus();

    await confirm('Make member: anna', 'Make member');
    expect(await screen.findByText('You’re now a member of this household.')).toBeVisible();
    await waitFor(() => expect(reads).toBe(2));
    // The moved default household is adopted and the screen mounted again under it.
    expect(await screen.findByRole('button', { name: 'Household: U Nováků' })).toBeVisible();
    // The notice is still on screen, and the reader is back on the heading of the remounted screen.
    expect(screen.getByText('You’re now a member of this household.')).toBeVisible();
    await waitFor(() => expect(screen.getByRole('heading', { level: 1, name: 'Household accounts' })).toHaveFocus());
    expect(window.location.pathname).toBe(detailPath);
  });

  it('keeps the notice and the reader’s place when the moved default household arrives late', async () => {
    // The session re-read that decides whether the default household moved is still in flight when the screen speaks.
    const held = heldSession(() => json(200, { ...instanceAdminSession(), defaultHouseholdId: elsewhere }));
    renderAt({
      'GET /api/v1/session': held.route,
      [`GET /api/v1/households/${elsewhere}`]: json(200, { ...household, id: elsewhere, name: 'U Nováků' }),
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [readPath(anna.userId)]: json(200, anna, { ETag: '"6"' }),
      [changePath(anna.userId)]: json(200, annaAsMember),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));
    const heading = screen.getByRole('heading', { level: 1, name: 'Household accounts' });
    expect(heading).not.toHaveFocus();

    await confirm('Make member: anna', 'Make member');

    // The screen has already spoken and the shell is still waiting for the session, so nothing has remounted yet.
    expect(await screen.findByText('You’re now a member of this household.')).toBeVisible();
    await waitFor(() => expect(held.calls()).toBe(2));
    expect(screen.getByRole('heading', { level: 1, name: 'Household accounts' })).not.toHaveFocus();
    expect(window.location.pathname).toBe(detailPath);

    held.release();

    // The late answer moves the default household: the screen comes back with the notice, and the reader with it.
    expect(await screen.findByRole('button', { name: 'Household: U Nováků' })).toBeVisible();
    await waitFor(() => expect(screen.getByRole('heading', { level: 1, name: 'Household accounts' })).toHaveFocus());
    expect(screen.getByText('You’re now a member of this household.')).toBeVisible();
    expect(window.location.pathname).toBe(detailPath);
  });

  it('does not carry a notice into a later visit when the answer arrives after the reader left', async () => {
    const held = heldSession(() => json(200, { ...instanceAdminSession(), defaultHouseholdId: elsewhere }));
    const fake = renderAt({
      'GET /api/v1/session': held.route,
      [listPath]: json(200, { items: [veseli], nextCursor: null }),
      [`GET /api/v1/households/${elsewhere}`]: json(200, { ...household, id: elsewhere, name: 'U Nováků' }),
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [readPath(anna.userId)]: json(200, anna, { ETag: '"6"' }),
      [changePath(anna.userId)]: json(200, annaAsMember),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));

    await confirm('Make member: anna', 'Make member');
    expect(await screen.findByText('You’re now a member of this household.')).toBeVisible();
    await waitFor(() => expect(held.calls()).toBe(2));

    // The reader walks away while the shell is still waiting, so the notice belongs to a visit that is over.
    await userEvent.click(screen.getByRole('link', { name: 'Back to households' }));
    expect(await screen.findByRole('heading', { level: 2, name: 'Households' })).toBeVisible();

    held.release();
    // The answer is adopted — the bar follows the moved household — but the notice it carried died with the visit.
    expect(await screen.findByRole('button', { name: 'Household: U Nováků' })).toBeVisible();

    await userEvent.click(await screen.findByRole('link', { name: 'Veselí' }));
    await waitFor(() => expect(accountRows()).toHaveLength(2));
    expect(screen.queryByText('You’re now a member of this household.')).not.toBeInTheDocument();
    expect(window.location.pathname).toBe(detailPath);
    expect(sessionReads(fake)).toBe(2);
  });

  it('boots again when the session that answers the re-read belongs to another account', async () => {
    const bea = { ...session(), userId: '0198a2f0-0000-7000-8000-0000000000ff', login: 'bea' };
    const held = heldSession(() => json(200, bea));
    const fake = renderAt({
      'GET /api/v1/session': held.route,
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [readPath(anna.userId)]: json(200, anna, { ETag: '"6"' }),
      [changePath(anna.userId)]: json(200, annaAsMember),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));

    await confirm('Make member: anna', 'Make member');
    await waitFor(() => expect(held.calls()).toBe(2));

    held.release();

    // Someone else's session is never folded into this shell: the app boots again from the server instead.
    await waitFor(() => expect(held.calls()).toBe(3));
    expect(sessionReads(fake)).toBe(3);
  });

  it('returns to sign-in when the session answered the re-read as ended', async () => {
    const held = heldSession(() => json(401));
    renderAt({
      'GET /api/v1/session': held.route,
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [readPath(anna.userId)]: json(200, anna, { ETag: '"6"' }),
      [changePath(anna.userId)]: json(200, annaAsMember),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));

    await confirm('Make member: anna', 'Make member');
    await waitFor(() => expect(held.calls()).toBe(2));

    held.release();

    expect(await screen.findByLabelText('Password')).toBeVisible();
    expect(screen.getByRole('status')).toHaveTextContent('You were signed out. Sign in again to continue. Saved changes are kept.');
  });

  it('keeps the screen and its notice when the session cannot be read after a role change', async () => {
    let reads = 0;
    renderAt({
      'GET /api/v1/session': () => { reads += 1; return reads === 1 ? json(200, instanceAdminSession()) : problem(503); },
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [readPath(anna.userId)]: json(200, anna, { ETag: '"6"' }),
      [changePath(anna.userId)]: json(200, annaAsMember),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));

    await confirm('Make member: anna', 'Make member');

    expect(await screen.findByText('You’re now a member of this household.')).toBeVisible();
    await waitFor(() => expect(reads).toBe(2));
    // A role change never moves the default household, so a read that failed leaves the shell exactly where it was.
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(reads).toBe(2);
    expect(screen.getByRole('heading', { level: 2, name: 'Accounts' })).toBeVisible();
    expect(window.location.pathname).toBe(detailPath);
  });

  it('boots again when the session cannot be read after the viewer leaves this household', async () => {
    let reads = 0;
    let listCalls = 0;
    const fake = renderAt({
      'GET /api/v1/session': () => {
        reads += 1;
        return reads === 2 ? problem(503) : json(200, instanceAdminSession());
      },
      [accountsPath]: () => json(200, { items: ++listCalls === 1 ? [anna, petr] : [petr], nextCursor: null }),
      [readPath(anna.userId)]: json(200, anna, { ETag: '"9"' }),
      [removalPath(anna.userId)]: json(204),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));

    await confirm('Leave household: anna', 'Leave household');

    // Leaving moves the default household, so a session that cannot be read is not trusted: the shell re-boots.
    await waitFor(() => expect(reads).toBe(3));
    expect(sessionReads(fake)).toBe(3);
    // The re-boot comes back to the same page, with the one row the server still holds.
    expect(await screen.findByRole('heading', { level: 2, name: 'Accounts' })).toBeVisible();
    expect(rowOf('petr')).toBeVisible();
    expect(window.location.pathname).toBe(detailPath);
  });

  it('says the viewer has already left when their own row is no longer there', async () => {
    const fake = renderAt({
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [readPath(anna.userId)]: problem(404),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));

    await confirm('Leave household: anna', 'Leave household');

    expect(await screen.findByText('You’re no longer a member of this household.')).toBeVisible();
    // Nothing was removed because there was nothing left to remove.
    expect(fake.requests.filter((request) => request.method === 'DELETE')).toHaveLength(0);
    expect(window.location.pathname).toBe(detailPath);
  });

  it('drops the rows and re-reads the session when reading an account is refused', async () => {
    let reads = 0;
    renderAt({
      'GET /api/v1/session': () => {
        reads += 1;
        return json(200, reads === 1 ? instanceAdminSession() : session());
      },
      [`GET ${base}`]: json(200, memberHousehold),
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
      // The grant hint was stale: the read refuses before a change is even attempted.
      [readPath(anna.userId)]: problem(403, 'instance_admin_required'),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));

    await confirm('Make member: anna', 'Make member');

    expect(await screen.findByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' })).toBeVisible();
    expect(accountRows()).toHaveLength(0);
    await waitFor(() => expect(reads).toBe(2));
  });

  it('says a household needs an administrator when the last one cannot be removed', async () => {
    renderAt({
      [accountsPath]: json(200, { items: [anna, petrAsAdmin], nextCursor: null }),
      [readPath(petr.userId)]: json(200, petrAsAdmin, { ETag: '"1"' }),
      [removalPath(petr.userId)]: problem(409, 'last_admin'),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));

    await confirm('Remove from household: petr', 'Remove from household');
    expect(await screen.findByText('A household needs at least one administrator. Make someone else an administrator first.')).toBeVisible();
    expect(rowOf('petr')).toBeVisible();
  });

  it('names the viewer’s own leave when the last administrator cannot leave', async () => {
    renderAt({
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [readPath(anna.userId)]: json(200, anna, { ETag: '"1"' }),
      [removalPath(anna.userId)]: problem(409, 'last_admin'),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));

    await confirm('Leave household: anna', 'Leave household');
    expect(await screen.findByText('A household always needs at least one administrator. Make someone else an administrator before you leave.')).toBeVisible();
    expect(rowOf('anna')).toBeVisible();
    expect(window.location.pathname).toBe(detailPath);
  });

  it('refreshes the list and says so when the account changed elsewhere', async () => {
    let listCalls = 0;
    const fake = renderAt({
      [accountsPath]: () => json(200, { items: ++listCalls === 1 ? [anna, petr] : [anna, petrAsAdmin], nextCursor: null }),
      [readPath(petr.userId)]: json(200, petr, { ETag: '"1"' }),
      [changePath(petr.userId)]: problem(412),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));

    await confirm('Make administrator: petr', 'Make administrator');
    expect(await screen.findByText('This account changed elsewhere. The list has been refreshed; check the current role and try again.')).toBeVisible();
    await waitFor(() => expect(within(rowOf('petr')).getByText('Administrator')).toBeVisible());
    expect(listCalls).toBe(2);
    expectNoHouseholdReads(fake);
  });

  it('drops the rows and re-reads the session when the grant is refused mid-action', async () => {
    let reads = 0;
    renderAt({
      'GET /api/v1/session': () => {
        reads += 1;
        return json(200, reads === 1 ? instanceAdminSession() : session());
      },
      [`GET ${base}`]: json(200, memberHousehold),
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [readPath(petr.userId)]: json(200, petr, { ETag: '"1"' }),
      [changePath(petr.userId)]: problem(403, 'instance_admin_required'),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));

    await confirm('Make administrator: petr', 'Make administrator');
    expect(await screen.findByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' })).toBeVisible();
    expect(accountRows()).toHaveLength(0);
    await waitFor(() => expect(reads).toBe(2));
  });

  it('returns to sign-in when an action reports an ended session', async () => {
    renderAt({
      [accountsPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [readPath(petr.userId)]: json(200, petr, { ETag: '"1"' }),
      [changePath(petr.userId)]: problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
    });
    await waitFor(() => expect(accountRows()).toHaveLength(2));

    await confirm('Make administrator: petr', 'Make administrator');
    expect(await screen.findByLabelText('Password')).toBeVisible();
    expect(screen.getByRole('status')).toHaveTextContent('You were signed out. Sign in again to continue. Saved changes are kept.');
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
    let reads = 0;
    renderAt({
      'GET /api/v1/session': () => {
        reads += 1;
        return json(200, reads === 1 ? instanceAdminSession() : session());
      },
      [`GET ${base}`]: json(200, memberHousehold),
      [accountsPath]: problem(403, 'instance_admin_required'),
    });
    expect(await screen.findByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' })).toBeVisible();
    expect(accountRows()).toHaveLength(0);
    await waitFor(() => expect(reads).toBe(2));
    await waitFor(() => expect(within(screen.getByRole('navigation', { name: 'Household navigation' }))
      .queryByRole('link', { name: 'Admin center' })).not.toBeInTheDocument());
  });

  it('keeps a failed read recoverable', async () => {
    let attempt = 0;
    renderAt({
      [accountsPath]: () => (++attempt === 1 ? problem(503) : json(200, { items: [anna], nextCursor: null })),
    });
    expect(await screen.findByText('Accounts couldn’t be loaded. Try again in a moment.')).toBeVisible();
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
