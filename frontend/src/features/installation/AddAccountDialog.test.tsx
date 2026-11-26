import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, household, instanceAdminSession, installFakeServer, json, memberHousehold, session } from '../../test/fakeServer';
import { itemRoutes } from '../../test/fakeItems';
import type { components } from '../../../../api/generated/api';

type AdminAccount = components['schemas']['AdminAccount'];
type AdminHousehold = components['schemas']['AdminHousehold'];
type DirectoryAccount = components['schemas']['DirectoryAccount'];

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const target = '0198a2f0-0000-7000-8000-000000000011';
const directoryPath = 'GET /api/v1/admin/accounts';
const accountsLink = `/api/v1/admin/households/${target}/accounts`;
const accountsPath = `GET ${accountsLink}`;
const assignmentPath = (userId: string) => `PUT ${accountsLink}/${userId}`;
const detailPath = `/admin/households/${target}`;
const problem = (status: number, code?: string) => json(status, { type: 'about:blank', title: 'x', status, ...(code ? { code } : {}) });

const anna: DirectoryAccount = { userId: session().userId, login: 'anna' };
const petr: DirectoryAccount = { userId: '0198a2f0-0000-7000-8000-000000000002', login: 'petr' };
const jana: DirectoryAccount = { userId: '0198a2f0-0000-7000-8000-000000000003', login: 'jana' };
const annaAsAdmin: AdminAccount = { ...anna, role: 'admin', signInMethods: { password: true, oidc: false } };
const annaAsMember: AdminAccount = { ...anna, role: 'member', signInMethods: { password: true, oidc: false } };
const petrAsMember: AdminAccount = { ...petr, role: 'member', signInMethods: { password: false, oidc: true } };
const petrAsAdmin: AdminAccount = { ...petrAsMember, role: 'admin' };

/** The household page with its own list ready, and the add dialog's routes layered over it by each test. */
function renderAt(routes: Parameters<typeof installFakeServer>[0], path = detailPath) {
  const fake = installFakeServer({
    'GET /api/v1/session': json(200, instanceAdminSession()),
    [`GET ${base}`]: json(200, household),
    ...itemRoutes(base, []),
    [accountsPath]: json(200, { items: [annaAsAdmin], nextCursor: null }),
    ...routes,
  });
  window.history.replaceState(null, '', path);
  render(<I18nProvider><App /></I18nProvider>);
  return fake;
}

const addButton = () => screen.getByRole('button', { name: 'Add an account' });
const loginField = () => screen.getByLabelText('Login');

/** The screen's one primary action is only there once the list itself is: open the dialog through it. */
async function openDialog() {
  await waitFor(() => expect(addButton()).toBeEnabled());
  await userEvent.click(addButton());
  return screen.findByRole('dialog');
}

const directoryAsks = (fake: ReturnType<typeof installFakeServer>) =>
  fake.requests.filter((request) => request.method === 'GET' && request.path === '/api/v1/admin/accounts');
const listReads = (fake: ReturnType<typeof installFakeServer>) =>
  fake.requests.filter((request) => request.method === 'GET' && request.path === accountsLink).length;
const sessionReads = (fake: ReturnType<typeof installFakeServer>) =>
  fake.requests.filter((request) => request.method === 'GET' && request.path === '/api/v1/session').length;

/** The installation grant is the only authority here: nothing may ever be read from the household on screen. */
function expectNoHouseholdReads(fake: ReturnType<typeof installFakeServer>) {
  expect(fake.requests.filter((request) => request.path.startsWith(`/api/v1/households/${target}`))).toHaveLength(0);
}

async function choose(dialog: HTMLElement, login: string) {
  const radio = await within(dialog).findByRole('radio', { name: login });
  await userEvent.click(radio);
}

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

describe('installation household adding an account', () => {
  it('opens on everyone in the directory, with the login field ready and nothing chosen', async () => {
    const fake = renderAt({ [directoryPath]: json(200, { items: [anna, petr], nextCursor: null }) });
    const dialog = await openDialog();

    expect(dialog).toHaveTextContent('Add an account');
    expect(dialog).toHaveTextContent('Choose an account that already exists in Tendo.');
    // The screen's one primary action heads the Accounts section, and nothing is added until it is used.
    const section = screen.getByRole('region', { name: 'Accounts' });
    expect(within(section).getByRole('button', { name: 'Add an account' })).toBeVisible();
    await waitFor(() => expect(loginField()).toHaveFocus());
    // Opening runs the empty search, so the choices are there without guessing a login first.
    await waitFor(() => expect(within(dialog).getAllByRole('radio')).toHaveLength(4));
    expect(within(dialog).getByRole('radio', { name: 'petr' })).toBeInTheDocument();
    expect(within(dialog).getByRole('radio', { name: 'anna' })).toBeInTheDocument();
    expect(within(dialog).getByText('2 accounts on this page')).toBeInTheDocument();
    // The member role is the default; nothing can be added until an account is chosen.
    expect(within(dialog).getByRole('radio', { name: 'Member' })).toBeChecked();
    expect(within(dialog).getByRole('button', { name: 'Add to household' })).toBeDisabled();

    const [ask] = directoryAsks(fake);
    expect(ask.query.has('q')).toBe(false);
    expect(ask.query.get('limit')).toBe('20');
    expect(directoryAsks(fake)).toHaveLength(1);
    expectNoHouseholdReads(fake);
  });

  it('names the household in its title when the visit carried the name', async () => {
    const veseli: AdminHousehold = { id: target, name: 'Veselí', timezone: 'Europe/Prague', createdAt: '2026-10-06T10:00:00Z' };
    renderAt({
      'GET /api/v1/admin/households': json(200, { items: [veseli], nextCursor: null }),
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
    }, '/admin/households');
    await userEvent.click(await screen.findByRole('link', { name: 'Veselí' }));
    await waitFor(() => expect(screen.getByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible());

    const dialog = await openDialog();
    expect(dialog).toHaveTextContent('Add an account to Veselí');
  });

  it('adds the account the reader chose as a member, then says so and reads the list again', async () => {
    const fake = renderAt({
      [directoryPath]: (request) => json(200, {
        items: request.query.get('q') ? [petr] : [anna, petr],
        nextCursor: null,
      }),
      [assignmentPath(petr.userId)]: json(201, petrAsMember),
    });
    const dialog = await openDialog();
    await waitFor(() => expect(within(dialog).getByRole('radio', { name: 'petr' })).toBeInTheDocument());

    // The search only runs on the button or Enter, never while typing.
    await userEvent.type(loginField(), 'pet');
    expect(directoryAsks(fake)).toHaveLength(1);
    await userEvent.type(loginField(), '{Enter}');

    await waitFor(() => expect(within(dialog).getByText('1 account on this page')).toBeInTheDocument());
    expect(within(dialog).queryByRole('radio', { name: 'anna' })).not.toBeInTheDocument();
    // Enter searched without taking the reader out of the field.
    expect(loginField()).toHaveFocus();
    expect(directoryAsks(fake).at(-1)!.query.get('q')).toBe('pet');

    await choose(dialog, 'petr');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add to household' }));

    expect(await screen.findByText('petr was added to this household.')).toBeVisible();
    const put = fake.requests.find((request) => request.method === 'PUT');
    expect(put?.path).toBe(accountsLink + `/${petr.userId}`);
    expect(JSON.parse(put!.body)).toEqual({ role: 'member' });
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await waitFor(() => expect(listReads(fake)).toBe(2));
    await waitFor(() => expect(addButton()).toHaveFocus());
    // Someone else's new membership says nothing about the viewer's own session.
    expect(sessionReads(fake)).toBe(1);
    expectNoHouseholdReads(fake);
  });

  it('hands the reader back to the Add button while the list is still being read again', async () => {
    let release!: (response: Response) => void;
    const held = new Promise<Response>((resolve) => { release = resolve; });
    let listCalls = 0;
    const fake = renderAt({
      [accountsPath]: () => {
        listCalls += 1;
        return listCalls === 1 ? json(200, { items: [annaAsAdmin], nextCursor: null }) : held;
      },
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [assignmentPath(petr.userId)]: json(201, petrAsMember),
    });
    const dialog = await openDialog();

    await choose(dialog, 'petr');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add to household' }));

    expect(await screen.findByText('petr was added to this household.')).toBeVisible();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    // The list behind the dialog is reading itself again; the add trigger is still the place the dialog left the
    // reader, so the focus is back on a control they can use instead of on the document.
    await waitFor(() => expect(listCalls).toBe(2));
    expect(addButton()).toBeEnabled();
    await waitFor(() => expect(addButton()).toHaveFocus());
    expect(window.location.pathname).toBe(detailPath);

    release(json(200, { items: [annaAsAdmin, petrAsMember], nextCursor: null }));
    await waitFor(() => expect(screen.getByRole('main').querySelectorAll('li[data-install-account-id]')).toHaveLength(2));
    expect([...screen.getByRole('main').querySelectorAll('[data-install-login]')].map((node) => node.textContent))
      .toEqual(['anna', 'petr']);
    expect(addButton()).toHaveFocus();
    expect(fake.requests.filter((request) => request.method === 'PUT')).toHaveLength(1);
  });

  it('adds the account as an administrator when that role is chosen', async () => {
    const fake = renderAt({
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [assignmentPath(petr.userId)]: json(201, petrAsAdmin),
    });
    const dialog = await openDialog();

    await choose(dialog, 'petr');
    await userEvent.click(within(dialog).getByRole('radio', { name: 'Administrator' }));
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add to household' }));

    expect(await screen.findByText('petr was added to this household.')).toBeVisible();
    expect(JSON.parse(fake.requests.find((request) => request.method === 'PUT')!.body)).toEqual({ role: 'admin' });
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });

  it('pages the directory without leaving the dialog', async () => {
    const fake = renderAt({
      [directoryPath]: (request) => json(200, (request.query.get('cursor') === 'd2'
        ? { items: [jana], nextCursor: null }
        : { items: [petr], nextCursor: 'd2' })),
    });
    const dialog = await openDialog();
    const pager = await within(dialog).findByRole('navigation', { name: 'Matching accounts' });
    expect(within(pager).getByText('Page 1')).toBeInTheDocument();

    await userEvent.click(within(pager).getByRole('button', { name: 'Next page' }));
    expect(await within(dialog).findByRole('radio', { name: 'jana' })).toBeInTheDocument();
    expect(directoryAsks(fake).at(-1)!.query.get('cursor')).toBe('d2');
    expect(directoryAsks(fake).at(-1)!.query.get('limit')).toBe('20');

    await userEvent.click(within(dialog).getByRole('button', { name: 'First page' }));
    expect(await within(dialog).findByRole('radio', { name: 'petr' })).toBeInTheDocument();
    expect(directoryAsks(fake)).toHaveLength(3);
  });

  it('drops the choice and waits for the rows while another page is on its way', async () => {
    let release!: (response: Response) => void;
    const held = new Promise<Response>((resolve) => { release = resolve; });
    const fake = renderAt({
      [directoryPath]: (request) => (request.query.get('cursor') ? held : json(200, { items: [petr], nextCursor: 'd2' })),
    });
    const dialog = await openDialog();
    await choose(dialog, 'petr');
    const submit = within(dialog).getByRole('button', { name: 'Add to household' });
    expect(submit).toBeEnabled();

    await userEvent.click(within(dialog).getByRole('button', { name: 'Next page' }));

    // The rows of the page in hand stay readable, but nothing can be chosen from them while the next page decides;
    // and the choice made on them is gone, because the rows it was made from are being replaced.
    await waitFor(() => expect(within(dialog).getByRole('radio', { name: 'petr' })).toBeDisabled());
    expect(submit).toBeDisabled();

    release(json(200, { items: [jana], nextCursor: null }));
    expect(await within(dialog).findByRole('radio', { name: 'jana' })).toBeInTheDocument();
    expect(within(dialog).getByRole('radio', { name: 'jana' })).not.toBeChecked();
    expect(submit).toBeDisabled();
    expect(fake.requests.filter((request) => request.method === 'PUT')).toHaveLength(0);
  });

  it('drops the choice when the reader searches again', async () => {
    const fake = renderAt({
      [directoryPath]: (request) => json(200, { items: request.query.get('q') ? [jana] : [anna, petr], nextCursor: null }),
    });
    const dialog = await openDialog();
    await choose(dialog, 'petr');
    const submit = within(dialog).getByRole('button', { name: 'Add to household' });
    expect(submit).toBeEnabled();

    await userEvent.type(loginField(), 'jan');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Search' }));

    expect(await within(dialog).findByRole('radio', { name: 'jana' })).toBeInTheDocument();
    // The chosen account is no longer on screen, so it is no longer chosen: nothing can be added by a stray click.
    expect(within(dialog).getByRole('radio', { name: 'jana' })).not.toBeChecked();
    expect(submit).toBeDisabled();
    expect(fake.requests.filter((request) => request.method === 'PUT')).toHaveLength(0);
  });

  it('retries the page that failed, not the one that is on screen', async () => {
    let secondPageAsks = 0;
    const fake = renderAt({
      [directoryPath]: (request) => {
        if (request.query.get('cursor') !== 'd2') return json(200, { items: [petr], nextCursor: 'd2' });
        secondPageAsks += 1;
        return secondPageAsks === 1 ? problem(503) : json(200, { items: [jana], nextCursor: null });
      },
    });
    const dialog = await openDialog();

    await userEvent.click(within(dialog).getByRole('button', { name: 'Next page' }));
    expect(await within(dialog).findByText('Accounts couldn’t be loaded. Try again in a moment.')).toBeVisible();
    // The page in hand is still there, and the page that could not be read is the one asked for again.
    expect(within(dialog).getByRole('radio', { name: 'petr' })).toBeInTheDocument();

    await userEvent.click(within(dialog).getByRole('button', { name: 'Try again' }));
    expect(await within(dialog).findByRole('radio', { name: 'jana' })).toBeInTheDocument();
    expect(within(dialog).queryByRole('radio', { name: 'petr' })).not.toBeInTheDocument();
    expect(directoryAsks(fake).filter((request) => request.query.get('cursor') === 'd2')).toHaveLength(2);
  });

  it('never lets a slow search land over a newer one', async () => {
    let releaseFirst!: (response: Response) => void;
    const firstHeld = new Promise<Response>((resolve) => { releaseFirst = resolve; });
    let asks = 0;
    renderAt({
      [directoryPath]: () => {
        asks += 1;
        return asks === 1 ? firstHeld : json(200, { items: [jana], nextCursor: null });
      },
    });
    // Opening the dialog runs the empty search, which this test keeps in flight.
    const dialog = await openDialog();
    await userEvent.type(loginField(), 'jan{Enter}');
    expect(await within(dialog).findByRole('radio', { name: 'jana' })).toBeInTheDocument();

    await act(async () => {
      releaseFirst(json(200, { items: [anna, petr], nextCursor: null }));
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    // The answer to the abandoned search is stale, so it never replaces the rows the reader is looking at.
    expect(within(dialog).queryByRole('radio', { name: 'anna' })).not.toBeInTheDocument();
    expect(within(dialog).getByRole('radio', { name: 'jana' })).toBeInTheDocument();
  });

  it('says so when the search matches no account', async () => {
    renderAt({
      [directoryPath]: (request) => json(200, { items: request.query.get('q') ? [] : [anna], nextCursor: null }),
    });
    const dialog = await openDialog();
    await userEvent.type(loginField(), 'zzz');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Search' }));

    expect(await within(dialog).findByText('No account matches “zzz”.')).toBeVisible();
  });

  it('answers a refused search on the login field instead of offering a retry', async () => {
    renderAt({
      [directoryPath]: (request) => (request.query.get('q') ? problem(400, 'invalid_query') : json(200, { items: [petr], nextCursor: null })),
    });
    const dialog = await openDialog();
    await userEvent.type(loginField(), 'pet');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Search' }));

    const login = loginField();
    await waitFor(() => expect(login).toHaveAttribute('aria-invalid', 'true'));
    const describedBy = login.getAttribute('aria-describedby');
    expect(document.getElementById(describedBy!)).toHaveTextContent('That search can’t be used. Use plain text and 64 characters or fewer.');
    // The field the reader has to fix takes the focus, and nothing offers a retry that could never work.
    await waitFor(() => expect(login).toHaveFocus());
    expect(within(dialog).queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument();
  });

  it('puts the reader back on the field for every refused search, not only the first', async () => {
    const fake = renderAt({ [directoryPath]: json(200, { items: [petr], nextCursor: null }) });
    const dialog = await openDialog();
    const search = within(dialog).getByRole('button', { name: 'Search' });
    await userEvent.type(loginField(), 'a'.repeat(65));

    await userEvent.click(search);
    await waitFor(() => expect(loginField()).toHaveFocus());

    // Nothing about the text changed, so the second refusal repeats the first one word for word: the focus is the
    // whole answer, and it has to come back from wherever the reader stands when they try the search again.
    search.focus();
    expect(search).toHaveFocus();
    await userEvent.click(search);
    await waitFor(() => expect(loginField()).toHaveFocus());
    expect(directoryAsks(fake)).toHaveLength(1);
  });

  it('counts the search in code points, as the server does', async () => {
    const fake = renderAt({ [directoryPath]: json(200, { items: [petr], nextCursor: null }) });
    const dialog = await openDialog();
    const login = loginField();
    await userEvent.click(login);
    // 64 emoji are 128 UTF-16 units but 64 code points, which the server accepts as a whole search.
    await userEvent.paste('😀'.repeat(64));
    await userEvent.click(within(dialog).getByRole('button', { name: 'Search' }));
    await waitFor(() => expect(directoryAsks(fake)).toHaveLength(2));
    expect(directoryAsks(fake).at(-1)!.query.get('q')).toBe('😀'.repeat(64));

    // One more is one too many: the field says so instead of asking for a search the server would refuse.
    await userEvent.click(login);
    await userEvent.paste('😀');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Search' }));
    await waitFor(() => expect(login).toHaveAttribute('aria-invalid', 'true'));
    expect(directoryAsks(fake)).toHaveLength(2);
  });

  it('never asks the server for a search text longer than it accepts', async () => {
    const fake = renderAt({ [directoryPath]: json(200, { items: [petr], nextCursor: null }) });
    const dialog = await openDialog();
    // The choices the empty search brought stay gone once the field refuses what was typed.
    await userEvent.type(loginField(), `${'a'.repeat(65)}{Enter}`);

    await waitFor(() => expect(loginField()).toHaveAttribute('aria-invalid', 'true'));
    expect(within(dialog).queryByRole('radio', { name: 'petr' })).not.toBeInTheDocument();
    expect(directoryAsks(fake)).toHaveLength(1);
  });

  it('keeps a failed search recoverable', async () => {
    let asks = 0;
    renderAt({
      [directoryPath]: () => {
        asks += 1;
        return asks === 1 ? json(200, { items: [anna, petr], nextCursor: null }) : asks === 2 ? problem(503) : json(200, { items: [petr], nextCursor: null });
      },
    });
    const dialog = await openDialog();
    await waitFor(() => expect(within(dialog).getByRole('radio', { name: 'petr' })).toBeInTheDocument());

    await userEvent.type(loginField(), 'pet{Enter}');
    expect(await within(dialog).findByText('Accounts couldn’t be loaded. Try again in a moment.')).toBeVisible();

    await userEvent.click(within(dialog).getByRole('button', { name: 'Try again' }));
    expect(await within(dialog).findByRole('radio', { name: 'petr' })).toBeInTheDocument();
    expect(asks).toBe(3);
  });

  it('closes and says the account already belongs here when the server repeats the same role', async () => {
    const fake = renderAt({
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [assignmentPath(petr.userId)]: json(200, petrAsMember),
    });
    const dialog = await openDialog();

    await choose(dialog, 'petr');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add to household' }));

    expect(await screen.findByText('petr already belongs to this household.')).toBeVisible();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await waitFor(() => expect(listReads(fake)).toBe(2));
  });

  it('says the viewer already belongs here in the second person when nothing changed', async () => {
    const fake = renderAt({
      [directoryPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [assignmentPath(anna.userId)]: json(200, annaAsMember),
    });
    const dialog = await openDialog();

    await choose(dialog, 'anna');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add to household' }));

    expect(await screen.findByText('You already belong to this household.')).toBeVisible();
    // The viewer's own membership is never reported back to them by their own login.
    expect(screen.queryByText(/anna already belongs/)).not.toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await waitFor(() => expect(listReads(fake)).toBe(2));
    // Nothing moved, so the shell keeps the session it has.
    expect(sessionReads(fake)).toBe(1);
  });

  it('keeps the dialog open when the account already belongs here with another role', async () => {
    const fake = renderAt({
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [assignmentPath(petr.userId)]: problem(409, 'already_member'),
    });
    const dialog = await openDialog();

    await choose(dialog, 'petr');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add to household' }));

    expect(await within(dialog).findByText('petr already belongs to this household with another role. Change the role in the list instead.')).toBeVisible();
    expect(screen.getByRole('dialog')).toBeVisible();
    expect(within(dialog).getByRole('radio', { name: 'petr' })).toBeChecked();
    // Nothing was added, so the list is not read again.
    expect(listReads(fake)).toBe(1);
  });

  it.each([
    { what: 'the server cannot answer', answer: () => problem(503) },
    { what: 'the request is refused as a whole', answer: () => problem(403, 'forbidden_origin') },
  ])('keeps the dialog and the choice when $what', async ({ answer }) => {
    const fake = renderAt({
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [assignmentPath(petr.userId)]: answer,
    });
    const dialog = await openDialog();

    await choose(dialog, 'petr');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add to household' }));

    expect(await within(dialog).findByText('That didn’t work. Try again in a moment.')).toBeVisible();
    // The reader keeps the account and the role they picked, so trying again is one press away.
    expect(within(dialog).getByRole('radio', { name: 'petr' })).toBeChecked();
    expect(within(dialog).getByRole('radio', { name: 'Member' })).toBeChecked();
    expect(within(dialog).getByRole('button', { name: 'Add to household' })).toBeEnabled();
    // Nothing was added, so the list behind the dialog is not read again.
    expect(listReads(fake)).toBe(1);
  });

  it('adds once even when the press is delivered twice', async () => {
    let release!: (response: Response) => void;
    const held = new Promise<Response>((resolve) => { release = resolve; });
    const fake = renderAt({
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [assignmentPath(petr.userId)]: () => held,
    });
    const dialog = await openDialog();
    await choose(dialog, 'petr');
    const submit = within(dialog).getByRole('button', { name: 'Add to household' });
    const press = () => { submit.dispatchEvent(new MouseEvent('click', { bubbles: true })); };

    // Two presses that reach the handler in the same batch, before the disabled state can be rendered between them:
    // the working add itself is what has to refuse the second one.
    await act(async () => { press(); press(); });
    expect(fake.requests.filter((request) => request.method === 'PUT')).toHaveLength(1);

    release(json(201, petrAsMember));
    expect(await screen.findByText('petr was added to this household.')).toBeVisible();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(fake.requests.filter((request) => request.method === 'PUT')).toHaveLength(1);
  });

  it('says on the role field that a household without an administrator needs one first', async () => {
    renderAt({
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [assignmentPath(petr.userId)]: problem(409, 'household_requires_admin'),
    });
    const dialog = await openDialog();

    await choose(dialog, 'petr');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add to household' }));

    const role = within(dialog).getByRole('radiogroup', { name: 'Role' });
    await waitFor(() => expect(role).toHaveAttribute('aria-invalid', 'true'));
    const describedBy = role.getAttribute('aria-describedby');
    expect(document.getElementById(describedBy!)).toHaveTextContent(
      'This household has no administrator yet, so the first account added must be an administrator.',
    );
    expect(screen.getByRole('dialog')).toBeVisible();
    // Changing the role answers the refusal, so the field stops saying it.
    await userEvent.click(within(dialog).getByRole('radio', { name: 'Administrator' }));
    expect(within(dialog).getByRole('radiogroup', { name: 'Role' })).toHaveAttribute('aria-invalid', 'false');
  });

  it('refreshes the list and says so when the account or the household is gone', async () => {
    const fake = renderAt({
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [assignmentPath(petr.userId)]: problem(404),
    });
    const dialog = await openDialog();

    await choose(dialog, 'petr');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add to household' }));

    expect(await screen.findByText('That account or household no longer exists. The list has been refreshed.')).toBeVisible();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await waitFor(() => expect(listReads(fake)).toBe(2));
  });

  it('ends the search and the dialog when the installation grant is refused', async () => {
    let reads = 0;
    renderAt({
      'GET /api/v1/session': () => {
        reads += 1;
        return json(200, reads === 1 ? instanceAdminSession() : session());
      },
      [`GET ${base}`]: json(200, memberHousehold),
      [directoryPath]: problem(403, 'instance_admin_required'),
    });
    await waitFor(() => expect(addButton()).toBeEnabled());
    await userEvent.click(addButton());

    expect(await screen.findByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' })).toBeVisible();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(screen.getByRole('main').querySelectorAll('li[data-install-account-id]')).toHaveLength(0);
    await waitFor(() => expect(reads).toBe(2));
  });

  it('ends the dialog and the screen when the grant is refused while adding', async () => {
    let reads = 0;
    renderAt({
      'GET /api/v1/session': () => {
        reads += 1;
        return json(200, reads === 1 ? instanceAdminSession() : session());
      },
      [`GET ${base}`]: json(200, memberHousehold),
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [assignmentPath(petr.userId)]: problem(403, 'instance_admin_required'),
    });
    const dialog = await openDialog();

    await choose(dialog, 'petr');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add to household' }));

    expect(await screen.findByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' })).toBeVisible();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    await waitFor(() => expect(reads).toBe(2));
  });

  it('returns to sign-in when the add reports an ended session', async () => {
    renderAt({
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [assignmentPath(petr.userId)]: problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
    });
    const dialog = await openDialog();

    await choose(dialog, 'petr');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add to household' }));

    expect(await screen.findByLabelText('Password')).toBeVisible();
    expect(screen.getByRole('status')).toHaveTextContent('You were signed out. Sign in again to continue. Saved changes are kept.');
  });

  it('adds the viewer themselves and adopts this household when it becomes their first one', async () => {
    let reads = 0;
    renderAt({
      'GET /api/v1/session': () => {
        reads += 1;
        // The viewer holds no household until this add: the membership it creates is their first one.
        return json(200, reads === 1
          ? instanceAdminSession(false)
          : { ...instanceAdminSession(false), defaultHouseholdId: target });
      },
      [`GET /api/v1/households/${target}`]: json(200, { ...household, id: target, name: 'Veselí' }),
      [directoryPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [assignmentPath(anna.userId)]: json(201, annaAsMember),
    });
    const dialog = await openDialog();

    await choose(dialog, 'anna');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add to household' }));

    // The viewer's own new membership is said in the second person, never as their own login.
    expect(await screen.findByText('You’re now a member of this household.')).toBeVisible();
    await waitFor(() => expect(reads).toBe(2));
    // The household the server made theirs is adopted, and the screen mounts again carrying the notice with it.
    expect(await screen.findByRole('button', { name: 'Household: Veselí' })).toBeVisible();
    expect(screen.getByText('You’re now a member of this household.')).toBeVisible();
    await waitFor(() => expect(screen.getByRole('heading', { level: 1, name: 'Household accounts' })).toHaveFocus());
  });

  it('adds the viewer themselves without mounting the screen again when their household stays put', async () => {
    let reads = 0;
    const fake = renderAt({
      'GET /api/v1/session': () => {
        reads += 1;
        // They already call another household home, and an assignment never moves an existing default (ADR 0020).
        return json(200, instanceAdminSession());
      },
      [directoryPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [assignmentPath(anna.userId)]: json(201, annaAsMember),
    });
    const dialog = await openDialog();

    await choose(dialog, 'anna');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add to household' }));

    expect(await screen.findByText('You’re now a member of this household.')).toBeVisible();
    await waitFor(() => expect(reads).toBe(2));
    await waitFor(() => expect(listReads(fake)).toBe(2));
    // Nothing moved, so nothing remounts: the notice stays on the screen the reader is already on.
    expect(screen.getByRole('heading', { level: 1, name: 'Household accounts' })).not.toHaveFocus();
    expect(screen.getByText('You’re now a member of this household.')).toBeVisible();
    expect(window.location.pathname).toBe(detailPath);
  });

  it('re-boots when the session cannot be read after the viewer adds themselves', async () => {
    let reads = 0;
    const fake = renderAt({
      'GET /api/v1/session': () => {
        reads += 1;
        return reads === 2 ? problem(503) : json(200, instanceAdminSession());
      },
      [directoryPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [assignmentPath(anna.userId)]: json(201, annaAsMember),
    });
    const dialog = await openDialog();

    await choose(dialog, 'anna');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Add to household' }));

    // An add can give a household-less shell its first household, so a session that cannot be read is not trusted:
    // the app boots again from the server instead.
    await waitFor(() => expect(reads).toBe(3));
    expect(sessionReads(fake)).toBe(3);
    expect(await screen.findByRole('heading', { level: 2, name: 'Accounts' })).toBeVisible();
    expect(window.location.pathname).toBe(detailPath);
  });

  it('closes on Cancel without asking the server for anything', async () => {
    const fake = renderAt({ [directoryPath]: json(200, { items: [petr], nextCursor: null }) });
    const dialog = await openDialog();

    await choose(dialog, 'petr');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(addButton()).toHaveFocus();
    expect(fake.requests.filter((request) => request.method === 'PUT')).toHaveLength(0);
  });
});
