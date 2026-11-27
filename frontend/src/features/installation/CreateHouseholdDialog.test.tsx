import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, household, instanceAdminSession, installFakeServer, json, session } from '../../test/fakeServer';
import { itemRoutes } from '../../test/fakeItems';
import { countDateTimeFormats } from '../../test/countDateTimeFormats';
import type { components } from '../../../../api/generated/api';

type AdminHousehold = components['schemas']['AdminHousehold'];
type DirectoryAccount = components['schemas']['DirectoryAccount'];

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const listPath = 'GET /api/v1/admin/households';
const createPath = 'POST /api/v1/admin/households';
const directoryPath = 'GET /api/v1/admin/accounts';
const accountsPath = (id: string) => `GET /api/v1/admin/households/${id}/accounts`;
const problem = (status: number, code?: string, field?: string) => json(status, {
  type: 'about:blank', title: 'x', status, ...(code ? { code } : {}), ...(field ? { field } : {}),
});
/** The calm failure with nothing to point at: the dialog stays and everything typed in it stays too. */
const CALM_ERROR = 'The household couldn’t be created. Try again; if it keeps failing, choose another account.';

const anna: DirectoryAccount = { userId: session().userId, login: 'anna' };
const petr: DirectoryAccount = { userId: '0198a2f0-0000-7000-8000-000000000002', login: 'petr' };
const jana: DirectoryAccount = { userId: '0198a2f0-0000-7000-8000-000000000003', login: 'jana' };
const novo: AdminHousehold = {
  id: '0198a2f0-0000-7000-8000-0000000000aa', name: 'Chata', timezone: 'UTC', createdAt: '2026-11-15T10:00:00Z',
};

/** The household list with its own read ready, and each test's create and directory answers layered over it. */
function renderAt(routes: Parameters<typeof installFakeServer>[0], viewer = instanceAdminSession()) {
  const fake = installFakeServer({
    'GET /api/v1/session': json(200, viewer),
    [`GET ${base}`]: json(200, household),
    ...itemRoutes(base, []),
    [listPath]: json(200, { items: [], nextCursor: null }),
    [directoryPath]: json(200, { items: [petr], nextCursor: null }),
    ...routes,
  });
  window.history.replaceState(null, '', '/admin/households');
  render(<I18nProvider><App /></I18nProvider>);
  return fake;
}

const posts = (fake: ReturnType<typeof installFakeServer>) =>
  fake.requests.filter((request) => request.method === 'POST' && request.path === '/api/v1/admin/households');
const keysOf = (fake: ReturnType<typeof installFakeServer>) => posts(fake).map((request) => request.headers.get('Idempotency-Key'));
const directoryAsks = (fake: ReturnType<typeof installFakeServer>) =>
  fake.requests.filter((request) => request.method === 'GET' && request.path === '/api/v1/admin/accounts');
const listReads = (fake: ReturnType<typeof installFakeServer>) =>
  fake.requests.filter((request) => request.method === 'GET' && request.path === '/api/v1/admin/households').length;
const sessionReads = (fake: ReturnType<typeof installFakeServer>) =>
  fake.requests.filter((request) => request.method === 'GET' && request.path === '/api/v1/session').length;

/** The installation grant is the only authority here: nothing may ever be read from the household on screen. */
function expectNoHouseholdReads(fake: ReturnType<typeof installFakeServer>, householdId: string) {
  expect(fake.requests.filter((request) => request.path.startsWith(`/api/v1/households/${householdId}`))).toHaveLength(0);
}

/** The list's one primary action is only there once the list itself is: open the dialog through it. */
async function openDialog() {
  const trigger = await screen.findByRole('button', { name: 'Create household' });
  await waitFor(() => expect(trigger).toBeEnabled());
  await userEvent.click(trigger);
  return { dialog: await screen.findByRole('dialog'), trigger };
}

const nameField = (dialog: HTMLElement) => within(dialog).getByLabelText('Household name');
const zoneField = (dialog: HTMLElement) => within(dialog).getByLabelText('Time zone') as HTMLSelectElement;
const loginField = (dialog: HTMLElement) => within(dialog).getByLabelText('Login');
const submitButton = (dialog: HTMLElement) => within(dialog).getByRole('button', { name: 'Create household' });
const submit = async (dialog: HTMLElement) => { await userEvent.click(submitButton(dialog)); };

async function search(dialog: HTMLElement, text: string) {
  const login = loginField(dialog);
  await userEvent.clear(login);
  await userEvent.type(login, `${text}{Enter}`);
}

async function choose(dialog: HTMLElement, login: string) {
  await userEvent.click(await within(dialog).findByRole('radio', { name: login }));
}

/** A name, a zone and a chosen account: the three things Create waits for. */
async function fillForm(dialog: HTMLElement, account = 'petr') {
  await userEvent.type(nameField(dialog), 'Chata');
  await userEvent.selectOptions(zoneField(dialog), 'UTC');
  await search(dialog, account.slice(0, 3));
  await choose(dialog, account);
}

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

describe('installation household creation', () => {
  it('opens on a clean form without searching the directory, and waits for a name and an account', async () => {
    const fake = renderAt({});
    const { dialog } = await openDialog();

    expect(within(dialog).getByRole('heading', { name: 'Create a household' })).toBeVisible();
    expect(dialog).toHaveTextContent('Give the household a name');
    const detected = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
    await waitFor(() => expect(nameField(dialog)).toHaveFocus());
    expect(nameField(dialog)).toHaveValue('');
    expect(zoneField(dialog)).toHaveValue(detected);
    expect(submitButton(dialog)).toBeDisabled();
    // The reader searches when they are ready to: opening the dialog asks the directory nothing.
    expect(directoryAsks(fake)).toHaveLength(0);
    expect(within(dialog).queryByRole('radio')).not.toBeInTheDocument();
  });

  it('typing the name does not relabel every time zone', async () => {
    renderAt({});
    const { dialog } = await openDialog();
    await waitFor(() => expect(nameField(dialog)).toHaveFocus());
    expect(zoneField(dialog).options.length).toBeGreaterThan(1);
    // Page-wide: no date formatter of any kind may be built per keystroke, the zone labels being the one there was.
    const counter = countDateTimeFormats();
    try {
      await userEvent.type(nameField(dialog), 'Chata');
      expect(nameField(dialog)).toHaveValue('Chata');
      expect(counter.count).toBe(0);
    } finally {
      counter.restore();
    }
  });

  it('creates the household with someone else as first admin, says so and opens it', async () => {
    const fake = renderAt({
      [createPath]: json(201, novo),
      [accountsPath(novo.id)]: json(200, { items: [], nextCursor: null }),
    });
    const { dialog } = await openDialog();
    await userEvent.type(nameField(dialog), '  Chata  ');
    await userEvent.selectOptions(zoneField(dialog), 'UTC');
    await search(dialog, 'pet');
    await choose(dialog, 'petr');
    await submit(dialog);

    expect(await screen.findByText('Household Chata was created.')).toBeVisible();
    await waitFor(() => expect(window.location.pathname).toBe(`/admin/households/${novo.id}`));
    const heading = await screen.findByRole('heading', { level: 1, name: 'Chata' });
    expect(heading).toBeVisible();
    // The change took the focused button with the list; the heading says where the reader landed.
    await waitFor(() => expect(heading).toHaveFocus());

    const [post] = posts(fake);
    expect(JSON.parse(post.body)).toEqual({ name: 'Chata', timezone: 'UTC', adminUserId: petr.userId });
    expect(post.headers.get('Idempotency-Key')).toMatch(/^[0-9a-f]{32}$/);
    // The list behind the dialog is on its way out with it, so it is not read a second time; someone else's first
    // membership says nothing about the session either.
    expect(listReads(fake)).toBe(1);
    expect(sessionReads(fake)).toBe(1);
    expectNoHouseholdReads(fake, novo.id);
  });

  it('focuses the name field the server refused and keeps the dialog open', async () => {
    const fake = renderAt({ [createPath]: problem(422, 'invalid_length', 'name') });
    const { dialog } = await openDialog();
    await fillForm(dialog);
    await submit(dialog);

    const name = nameField(dialog);
    await waitFor(() => expect(name).toHaveAttribute('aria-invalid', 'true'));
    expect(name).toHaveAccessibleDescription(/wrong length/);
    await waitFor(() => expect(name).toHaveFocus());
    expect(screen.getByRole('dialog')).toBeVisible();
    expect(posts(fake)).toHaveLength(1);
  });

  it('focuses the timezone field the server refused', async () => {
    renderAt({ [createPath]: problem(422, 'invalid_iana_timezone', 'timezone') });
    const { dialog } = await openDialog();
    await fillForm(dialog);
    await submit(dialog);

    const zone = zoneField(dialog);
    await waitFor(() => expect(zone).toHaveAttribute('aria-invalid', 'true'));
    expect(zone).toHaveAccessibleDescription(/Tendo recognizes/);
    await waitFor(() => expect(zone).toHaveFocus());
    expect(screen.getByRole('dialog')).toBeVisible();
  });

  it('focuses the account field the server refused', async () => {
    renderAt({ [createPath]: problem(422, 'invalid_reference', 'adminUserId') });
    const { dialog } = await openDialog();
    await fillForm(dialog);
    await submit(dialog);

    const login = loginField(dialog);
    await waitFor(() => expect(login).toHaveAttribute('aria-invalid', 'true'));
    expect(login).toHaveAccessibleDescription(/can’t be the first administrator/);
    await waitFor(() => expect(login).toHaveFocus());
    expect(screen.getByRole('dialog')).toBeVisible();
  });

  it('keeps what the server said about the name when the reader searches the directory', async () => {
    renderAt({ [createPath]: problem(422, 'invalid_length', 'name') });
    const { dialog } = await openDialog();
    await fillForm(dialog);
    await submit(dialog);

    const name = nameField(dialog);
    await waitFor(() => expect(name).toHaveAttribute('aria-invalid', 'true'));

    // A search replaces the choices; it never answers for a field the server refused.
    await search(dialog, 'pet');

    expect(name).toHaveAttribute('aria-invalid', 'true');
    expect(name).toHaveAccessibleDescription(/wrong length/);
    expect(within(dialog).getByRole('radio', { name: 'petr' })).toBeInTheDocument();
  });

  it('keeps the focus on a chosen account after a refused search and a refused account', async () => {
    const fake = renderAt({
      [directoryPath]: json(200, { items: [petr, jana], nextCursor: null }),
      [createPath]: problem(422, 'invalid_reference', 'adminUserId'),
    });
    const { dialog } = await openDialog();
    await userEvent.type(nameField(dialog), 'Chata');
    await userEvent.selectOptions(zoneField(dialog), 'UTC');
    // One refused search first, so the field is already the answer to it...
    await search(dialog, 'a'.repeat(65));
    const login = loginField(dialog);
    await waitFor(() => expect(login).toHaveFocus());

    await search(dialog, 'a');
    await choose(dialog, 'petr');
    await submit(dialog);
    // ...then an account the server refused, which the dialog points the same field at.
    await waitFor(() => expect(login).toHaveAttribute('aria-invalid', 'true'));
    await waitFor(() => expect(login).toHaveFocus());

    // Choosing again answers the account refusal; the search refusal behind it must not take the focus back.
    const janaRadio = within(dialog).getByRole('radio', { name: 'jana' });
    await userEvent.click(janaRadio);
    await waitFor(() => expect(janaRadio).toHaveFocus());
    expect(login).not.toHaveAttribute('aria-invalid', 'true');
    expect(posts(fake)).toHaveLength(1);
  });

  it('answers a too-long name locally without asking the server', async () => {
    const fake = renderAt({});
    const { dialog } = await openDialog();
    await fillForm(dialog);
    const name = nameField(dialog);
    await userEvent.clear(name);
    await userEvent.click(name);
    await userEvent.paste('a'.repeat(101));
    await submit(dialog);

    await waitFor(() => expect(name).toHaveAttribute('aria-invalid', 'true'));
    expect(name).toHaveAccessibleDescription(/up to 100 characters/);
    await waitFor(() => expect(name).toHaveFocus());
    expect(posts(fake)).toHaveLength(0);
  });

  it('keeps everything typed when the server cannot be reached, and retries with the same key', async () => {
    let attempt = 0;
    const fake = renderAt({
      [createPath]: () => (++attempt === 1 ? problem(503) : json(201, novo)),
      [accountsPath(novo.id)]: json(200, { items: [], nextCursor: null }),
    });
    const { dialog } = await openDialog();
    await fillForm(dialog);
    await submit(dialog);

    expect(await within(dialog).findByText(CALM_ERROR)).toBeVisible();
    // The reader keeps the name, the zone and the account they picked, so trying again is one press away.
    expect(nameField(dialog)).toHaveValue('Chata');
    expect(zoneField(dialog)).toHaveValue('UTC');
    expect(within(dialog).getByRole('radio', { name: 'petr' })).toBeChecked();
    expect(submitButton(dialog)).toBeEnabled();

    await submit(dialog);
    await waitFor(() => expect(posts(fake)).toHaveLength(2));
    expect(keysOf(fake)[1]).toBe(keysOf(fake)[0]);
    expect(await screen.findByText('Household Chata was created.')).toBeVisible();
  });

  it('retries with the same key after the dialog is closed and opened again', async () => {
    const fake = renderAt({ [createPath]: problem(503) });
    const { dialog } = await openDialog();
    await fillForm(dialog);
    await submit(dialog);
    expect(await within(dialog).findByText(CALM_ERROR)).toBeVisible();

    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    const reopened = (await openDialog()).dialog;
    await fillForm(reopened);
    await submit(reopened);

    await waitFor(() => expect(posts(fake)).toHaveLength(2));
    // Reopening the dialog and pressing Create again can still be the household the server may already hold.
    expect(keysOf(fake)[1]).toBe(keysOf(fake)[0]);
  });

  it('starts a new intent with a new key when the timezone changes', async () => {
    const fake = renderAt({ [createPath]: problem(503) });
    const { dialog } = await openDialog();
    await fillForm(dialog);
    await submit(dialog);
    expect(await within(dialog).findByText(CALM_ERROR)).toBeVisible();

    const zone = zoneField(dialog);
    const other = [...zone.options].map((option) => option.value).find((value) => value !== zone.value)!;
    await userEvent.selectOptions(zone, other);
    await submit(dialog);

    await waitFor(() => expect(posts(fake)).toHaveLength(2));
    expect(keysOf(fake)[1]).not.toBe(keysOf(fake)[0]);
  });

  it('starts a new intent with a new key when only the name changes', async () => {
    const fake = renderAt({ [createPath]: problem(503) });
    const { dialog } = await openDialog();
    await fillForm(dialog);
    await submit(dialog);
    expect(await within(dialog).findByText(CALM_ERROR)).toBeVisible();

    await userEvent.type(nameField(dialog), ' 2');
    await submit(dialog);

    await waitFor(() => expect(posts(fake)).toHaveLength(2));
    expect(JSON.parse(posts(fake)[1].body).name).toBe('Chata 2');
    expect(keysOf(fake)[1]).not.toBe(keysOf(fake)[0]);
  });

  it('starts a new intent with a new key when only the chosen account changes', async () => {
    const fake = renderAt({
      [directoryPath]: json(200, { items: [petr, jana], nextCursor: null }),
      [createPath]: problem(503),
    });
    const { dialog } = await openDialog();
    await fillForm(dialog);
    await submit(dialog);
    expect(await within(dialog).findByText(CALM_ERROR)).toBeVisible();

    await choose(dialog, 'jana');
    await submit(dialog);

    await waitFor(() => expect(posts(fake)).toHaveLength(2));
    expect(JSON.parse(posts(fake)[1].body).adminUserId).toBe(jana.userId);
    expect(keysOf(fake)[1]).not.toBe(keysOf(fake)[0]);
  });

  it('starts over with a new key when the server says the key was reused', async () => {
    const fake = renderAt({ [createPath]: problem(422, 'idempotency_key_reused') });
    const { dialog } = await openDialog();
    await fillForm(dialog);
    await submit(dialog);

    expect(await within(dialog).findByText(CALM_ERROR)).toBeVisible();
    expect(screen.getByRole('dialog')).toBeVisible();

    await submit(dialog);
    await waitFor(() => expect(posts(fake)).toHaveLength(2));
    expect(keysOf(fake)[1]).not.toBe(keysOf(fake)[0]);
  });

  it('creates once even when the press is delivered twice', async () => {
    let release!: (response: Response) => void;
    const held = new Promise<Response>((resolve) => { release = resolve; });
    const fake = renderAt({
      [createPath]: () => held,
      [accountsPath(novo.id)]: json(200, { items: [], nextCursor: null }),
    });
    const { dialog } = await openDialog();
    await fillForm(dialog);
    const button = submitButton(dialog);
    const press = () => { button.dispatchEvent(new MouseEvent('click', { bubbles: true })); };

    // Two presses that reach the handler in the same batch, before the disabled state can be rendered between them:
    // the working create itself is what has to refuse the second one.
    await act(async () => { press(); press(); });
    expect(posts(fake)).toHaveLength(1);

    release(json(201, novo));
    expect(await screen.findByText('Household Chata was created.')).toBeVisible();
    expect(posts(fake)).toHaveLength(1);
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
    const { dialog } = await openDialog();
    await search(dialog, 'pet');
    await search(dialog, 'jan');
    expect(await within(dialog).findByRole('radio', { name: 'jana' })).toBeInTheDocument();

    await act(async () => {
      releaseFirst(json(200, { items: [petr], nextCursor: null }));
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    // The answer to the abandoned search is stale, so it never replaces the rows the reader is looking at.
    expect(within(dialog).queryByRole('radio', { name: 'petr' })).not.toBeInTheDocument();
    expect(within(dialog).getByRole('radio', { name: 'jana' })).toBeInTheDocument();
    expect(submitButton(dialog)).toBeDisabled();
  });

  it('re-reads the session and carries the notice when the viewer is the first admin', async () => {
    let reads = 0;
    renderAt({
      'GET /api/v1/session': () => {
        reads += 1;
        // The viewer holds no household until this create: the membership it makes is their first one.
        return json(200, reads === 1
          ? instanceAdminSession(false)
          : { ...instanceAdminSession(false), defaultHouseholdId: novo.id });
      },
      [`GET /api/v1/households/${novo.id}`]: json(200, {
        id: novo.id, name: 'Chata', timezone: 'UTC', createdAt: novo.createdAt, viewerRole: 'admin', viewerMembershipVersion: 1,
      }),
      [directoryPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [createPath]: json(201, novo),
      [accountsPath(novo.id)]: json(200, { items: [], nextCursor: null }),
    });
    const { dialog } = await openDialog();
    await userEvent.type(nameField(dialog), 'Chata');
    await userEvent.selectOptions(zoneField(dialog), 'UTC');
    await search(dialog, 'ann');
    await choose(dialog, 'anna');
    await submit(dialog);

    expect(await screen.findByText('Household Chata was created.')).toBeVisible();
    await waitFor(() => expect(reads).toBe(2));
    // The household the server gave the viewer is adopted, and the screen mounts again carrying the notice with it.
    expect(await screen.findByRole('button', { name: 'Household: Chata' })).toBeVisible();
    expect(screen.getByText('Household Chata was created.')).toBeVisible();
    await waitFor(() => expect(screen.getByRole('heading', { level: 1, name: 'Chata' })).toHaveFocus());
  });

  it('leaves the viewer’s own default household where it is when they become the new one’s first admin', async () => {
    // The viewer already calls a household home; administering another one does not move it.
    const fake = renderAt({
      [`GET /api/v1/households/${novo.id}`]: json(200, {
        id: novo.id, name: 'Chata', timezone: 'UTC', createdAt: novo.createdAt, viewerRole: 'admin', viewerMembershipVersion: 1,
      }),
      [directoryPath]: json(200, { items: [anna, petr], nextCursor: null }),
      [createPath]: json(201, novo),
      [accountsPath(novo.id)]: json(200, { items: [], nextCursor: null }),
    });
    const { dialog } = await openDialog();
    await userEvent.type(nameField(dialog), 'Chata');
    await userEvent.selectOptions(zoneField(dialog), 'UTC');
    await search(dialog, 'ann');
    await choose(dialog, 'anna');
    await submit(dialog);

    expect(await screen.findByText('Household Chata was created.')).toBeVisible();
    await waitFor(() => expect(sessionReads(fake)).toBe(2));
    expect(screen.getByRole('button', { name: 'Household: Veselí' })).toBeVisible();
    await waitFor(() => expect(screen.getByRole('heading', { level: 1, name: 'Chata' })).toHaveFocus());
  });

  it('ends the dialog and the list when the installation grant is refused', async () => {
    let reads = 0;
    renderAt({
      'GET /api/v1/session': () => {
        reads += 1;
        return json(200, reads === 1 ? instanceAdminSession() : session());
      },
      [createPath]: problem(403, 'instance_admin_required'),
    });
    const { dialog } = await openDialog();
    await fillForm(dialog);
    await submit(dialog);

    expect(await screen.findByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' })).toBeVisible();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    await waitFor(() => expect(reads).toBe(2));
  });

  it('returns to sign-in when the create reports an ended session', async () => {
    renderAt({
      [createPath]: problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
    });
    const { dialog } = await openDialog();
    await fillForm(dialog);
    await submit(dialog);

    expect(await screen.findByLabelText('Password')).toBeVisible();
    expect(screen.getByRole('status')).toHaveTextContent('You were signed out. Sign in again to continue.');
  });

  it('closes on Cancel and gives the focus back to the household list trigger', async () => {
    const fake = renderAt({});
    const { dialog, trigger } = await openDialog();
    await userEvent.type(nameField(dialog), 'Chata');

    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await waitFor(() => expect(trigger).toHaveFocus());
    expect(posts(fake)).toHaveLength(0);
  });
});
