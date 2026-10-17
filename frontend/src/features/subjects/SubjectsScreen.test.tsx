import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import {
  HOUSEHOLD_ID,
  household,
  emptyHome,
  installFakeServer,
  json,
  session,
  type RecordedRequest,
} from '../../test/fakeServer';
import type { Subject } from './subjectsApi';
import { clearPendingSubjectCreates } from './pendingSubjectCreate';

function renderApp() {
  return render(
    <I18nProvider>
      <App />
    </I18nProvider>,
  );
}

const problem = (status: number, extra: Record<string, unknown> = {}) =>
  json(status, { type: 'about:blank', title: 'x', status, ...extra });

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const listKey = `GET ${base}/subjects`;
const createKey = `POST ${base}/subjects`;
const membersKey = `GET ${base}/members`;
const memberId = '0198a2f0-0000-7000-8000-000000000099';
const membersResponse = () => json(200, { items: [{ userId: memberId, login: 'member_one', role: 'member' }], nextCursor: null });
const itemKey = (id: string, method = 'GET') => `${method} ${base}/subjects/${id}`;

const subject = (id: string, name: string, type: Subject['type'] = 'person', archived = false): Subject => ({
  id,
  name,
  type,
  linkedUserId: null,
  archived,
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
});

const anna = subject('s-1', 'Anna', 'person');
const octavia = subject('s-2', 'Octavia', 'vehicle');

const page = (items: Subject[], nextCursor: string | null = null) => json(200, { items, nextCursor });
const etagged = (s: Subject, etag: string) => json(200, s, { ETag: etag });

const signedIn = {
  'GET /api/v1/session': json(200, session()),
  [`GET ${base}`]: json(200, household),
  ...emptyHome,
};

beforeEach(() => {
  window.history.replaceState(null, '', '/');
  clearPendingSubjectCreates();
  sessionStorage.clear();
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

async function openPeople() {
  await userEvent.click(await screen.findByRole('link', { name: 'People and things' }));
  return screen.findByRole('heading', { level: 1, name: 'People and things' });
}

async function openAddForm() {
  await openPeople();
  await userEvent.click(await screen.findByRole('button', { name: 'Add' }));
  return screen.getByRole('form', { name: 'Add a person or thing' });
}

const sent = (requests: RecordedRequest[], method: string) => requests.filter((r) => r.method === method);

describe('navigation', () => {
  it('opens from home, focuses the heading, and goes back home with the Back button too', async () => {
    const fake = installFakeServer({ ...signedIn, [listKey]: page([anna]) });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible();
    expect(screen.getByRole('link', { name: 'People and things' })).toHaveAttribute('href', '/people');

    const heading = await openPeople();
    expect(heading).toHaveFocus();
    expect(window.location.pathname).toBe('/people');
    expect(await screen.findByText('Anna')).toBeVisible();
    expect(screen.queryByText(HOUSEHOLD_ID)).not.toBeInTheDocument();
    expect(fake.requests.find((r) => r.path === `${base}/subjects`)!.query.get('archived')).toBe('false');

    await userEvent.click(screen.getByRole('link', { name: 'Back to home' }));
    const home = await screen.findByRole('heading', { level: 1, name: 'Veselí' });
    expect(home).toHaveFocus();
    expect(window.location.pathname).toBe('/');

    window.history.back();
    expect(await screen.findByRole('heading', { level: 1, name: 'People and things' })).toBeVisible();
    window.history.back();
    expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible();
  });

  it('opens straight on /people', async () => {
    window.history.replaceState(null, '', '/people');
    installFakeServer({ ...signedIn, [listKey]: page([anna]) });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'People and things' })).toBeVisible();
    expect(await screen.findByText('Anna')).toBeVisible();
  });

  it('keeps Home calm with one obvious Add item action and one People and things link', async () => {
    installFakeServer(signedIn);
    renderApp();
    await screen.findByRole('heading', { level: 1, name: 'Veselí' });
    await waitFor(() => expect(screen.getAllByRole('button', { name: 'Add item' })).toHaveLength(1));
    expect(screen.getByRole('link', { name: 'People and things' })).toBeVisible();
    expect(screen.queryByRole('button', { name: /^add$/i })).not.toBeInTheDocument();
  });
});

describe('list', () => {
  it('shows linked account login on a fresh visit without opening an editor', async () => {
    const linked = { ...anna, linkedUserId: memberId };
    installFakeServer({
      ...signedIn,
      [listKey]: page([linked]),
      [membersKey]: membersResponse(),
    });
    renderApp();
    await openPeople();
    expect(await screen.findByText('Account: member_one')).toBeVisible();
    expect(screen.queryByRole('form')).not.toBeInTheDocument();
  });

  it('shows a friendly empty state with an obvious add action', async () => {
    installFakeServer({ ...signedIn, [listKey]: page([]) });
    renderApp();
    await openPeople();
    expect(await screen.findByText('Nothing here yet.')).toBeVisible();
    expect(screen.getByText(/a child, your home, the car or a pet/)).toBeVisible();
    expect(screen.getAllByRole('button', { name: 'Add' })).toHaveLength(1);
  });

  it('shows name and a human type label for every type', async () => {
    installFakeServer({
      ...signedIn,
      [listKey]: page([
        anna,
        subject('a', 'Flat', 'home'),
        octavia,
        subject('b', 'Rex', 'pet'),
        subject('c', 'Piano', 'custom'),
      ]),
    });
    renderApp();
    await openPeople();
    const rows = within(await screen.findByRole('list')).getAllByRole('listitem');
    expect(rows.map((r) => r.querySelector('[data-subject-name]')!.textContent)).toEqual([
      'Anna',
      'Flat',
      'Octavia',
      'Rex',
      'Piano',
    ]);
    expect(rows.map((r) => r.querySelector('[data-subject-type]')!.textContent)).toEqual([
      'Person',
      'Home',
      'Vehicle',
      'Pet',
      'Other',
    ]);
    expect(screen.queryByText(/subject|custom/i)).not.toBeInTheDocument();
  });

  it('"Show more" passes the cursor and appends the next page', async () => {
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: (req) =>
        req.query.get('cursor') === 'abc' ? page([octavia]) : page([anna], 'abc'),
    });
    renderApp();
    await openPeople();
    await screen.findByText('Anna');
    await userEvent.click(screen.getByRole('button', { name: 'Show more' }));
    expect(await screen.findByText('Octavia')).toBeVisible();
    expect(screen.getByText('Anna')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Show more' })).not.toBeInTheDocument();
    const lists = fake.requests.filter((r) => r.method === 'GET' && r.path === `${base}/subjects` && r.query.get('limit') === '50');
    expect(lists.map((r) => r.query.get('cursor'))).toEqual([null, 'abc']);
  });

  it('a failed load is retryable', async () => {
    let calls = 0;
    installFakeServer({
      ...signedIn,
      [listKey]: (req) => req.query.get('limit') === '100' ? page([]) : (++calls === 1 ? problem(503) : page([anna])),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Try again' }));
    expect(await screen.findByText('Anna')).toBeVisible();
  });

  it('401 returns to sign-in', async () => {
    installFakeServer({
      ...signedIn,
      [listKey]: problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('link', { name: 'People and things' }));
    expect(await screen.findByLabelText('Password')).toBeVisible();
    expect(screen.getByRole('heading', { level: 1, name: 'Sign in to Tendo' })).toHaveFocus();
    expect(window.location.pathname).toBe('/');
  });
});

describe('add', () => {
  it('creates a person with the exact body and shows it in the list', async () => {
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([]),
      [membersKey]: membersResponse(),
      [createKey]: json(201, subject('s-9', 'Zuzka'), { ETag: '"1"' }),
    });
    renderApp();
    const form = await openAddForm();
    expect(within(form).getByRole('radio', { name: 'Person' })).toBeChecked();
    for (const label of ['Person', 'Home', 'Vehicle', 'Pet', 'Other']) {
      expect(within(form).getByRole('radio', { name: label })).toBeVisible();
    }
    expect(within(form).getAllByRole('textbox')).toHaveLength(1);
    await screen.findByRole('combobox', { name: 'Their account in Tendo' });
    await userEvent.type(within(form).getByLabelText('Name'), '  Zuzka ');
    await userEvent.click(within(form).getByRole('button', { name: 'Add' }));

    expect(await screen.findByText('Zuzka was added.')).toBeVisible();
    const post = sent(fake.requests, 'POST')[0]!;
    expect(post.path).toBe(`${base}/subjects`);
    expect(JSON.parse(post.body)).toEqual({ name: '  Zuzka ', type: 'person' });
    expect(post.headers.get('Idempotency-Key')).toMatch(/^[0-9a-f]{32}$/);
    await screen.findByText('Zuzka');
    const row = within(screen.getByRole('list')).getByText('Zuzka').closest('li')!;
    expect(row).toHaveTextContent('Person');
    expect(screen.queryByRole('form')).not.toBeInTheDocument();
    expect(screen.queryByText('Nothing here yet.')).not.toBeInTheDocument();
  });

  it('recovers a stored linked create after remount when member loading is unavailable', async () => {
    const key = 'a'.repeat(32);
    const pending = { version: 1, key, body: { name: 'Stored linked', type: 'person', linkedUserId: memberId } };
    sessionStorage.setItem(`tendo.pendingSubjectCreate.v1:${session().userId}:${HOUSEHOLD_ID}`, JSON.stringify(pending));
    let postCalls = 0;
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([]),
      [membersKey]: problem(503),
      [createKey]: () => {
        postCalls++;
        expect(JSON.parse(sessionStorage.getItem(`tendo.pendingSubjectCreate.v1:${session().userId}:${HOUSEHOLD_ID}`)!)).toEqual(pending);
        return postCalls === 1 ? Promise.reject(new Error('offline')) : json(201, { ...subject('from-storage', 'Stored linked'), linkedUserId: memberId });
      },
    });
    cleanup();
    const firstMount = renderApp();
    await userEvent.click(await screen.findByRole('link', { name: 'People and things' }));
    const firstForm = await screen.findByRole('form', { name: 'Add a person or thing' });
    await screen.findByRole('button', { name: 'Check again' });
    await userEvent.click(within(firstForm).getByRole('button', { name: 'Check again' }));
    await waitFor(() => expect(sent(fake.requests, 'POST')).toHaveLength(1));
    const first = sent(fake.requests, 'POST')[0]!;
    expect(JSON.parse(first.body)).toEqual(pending.body);
    expect(first.headers.get('Idempotency-Key')).toBe(key);
    await screen.findByRole('button', { name: 'Check again' });
    firstMount.unmount();
    clearPendingSubjectCreates();
    sessionStorage.setItem(`tendo.pendingSubjectCreate.v1:${session().userId}:${HOUSEHOLD_ID}`, JSON.stringify(pending));
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Check again' }));
    await screen.findByText('Stored linked was added.');
    const posts = sent(fake.requests, 'POST');
    expect(posts).toHaveLength(2);
    expect(posts[1]!.headers.get('Idempotency-Key')).toBe(key);
    expect(JSON.parse(posts[1]!.body)).toEqual(pending.body);
  });

  it('creates with an optional linked account and persists the exact attempt before the first POST', async () => {
    let posts = 0;
    const key = 'b'.repeat(32);
    // Captured inside the handler and asserted outside: a throw in the handler would surface as a network error.
    const storedAtPost: Array<{ raw: string | null; requestKey: string | null }> = [];
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([]),
      [membersKey]: membersResponse(),
      [createKey]: (req) => {
        posts++;
        storedAtPost.push({
          raw: sessionStorage.getItem(`tendo.pendingSubjectCreate.v1:${session().userId}:${HOUSEHOLD_ID}`),
          requestKey: req.headers.get('Idempotency-Key'),
        });
        return posts === 1 ? Promise.reject(new Error('offline')) : json(201, { ...subject('linked', 'Lena'), linkedUserId: memberId });
      },
    });
    vi.spyOn(crypto, 'getRandomValues').mockImplementation((array) => { (array as Uint8Array).fill(0xbb); return array; });
    renderApp();
    const form = await openAddForm();
    await userEvent.type(within(form).getByLabelText('Name'), 'Lena');
    const account = await screen.findByRole('combobox', { name: 'Their account in Tendo' });
    await userEvent.selectOptions(account, memberId);
    await userEvent.click(within(form).getByRole('button', { name: 'Add' }));
    await waitFor(() => expect(storedAtPost).toHaveLength(1));
    expect(storedAtPost[0]!.raw).not.toBeNull();
    const storedFirst = JSON.parse(storedAtPost[0]!.raw!);
    expect(storedFirst.body).toEqual({ name: 'Lena', type: 'person', linkedUserId: memberId });
    expect(storedFirst.key).toBe(key);
    expect(storedAtPost[0]!.requestKey).toBe(key);
    const first = sent(fake.requests, 'POST')[0]!;
    expect(JSON.parse(first.body)).toEqual({ name: 'Lena', type: 'person', linkedUserId: memberId });
    expect(JSON.parse(sessionStorage.getItem(`tendo.pendingSubjectCreate.v1:${session().userId}:${HOUSEHOLD_ID}`)!).body).toEqual({ name: 'Lena', type: 'person', linkedUserId: memberId });
    await userEvent.click(await screen.findByRole('button', { name: 'Check again' }));
    await screen.findByText('Lena was added.');
    expect(JSON.parse(sent(fake.requests, 'POST')[1]!.body)).toEqual({ name: 'Lena', type: 'person', linkedUserId: memberId });
    expect(sessionStorage.getItem(`tendo.pendingSubjectCreate.v1:${session().userId}:${HOUSEHOLD_ID}`)).toBeNull();
  });

  it('sends the chosen type; "Other" is custom', async () => {
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([]),
      [membersKey]: membersResponse(),
      [createKey]: json(201, subject('s-9', 'Piano', 'custom')), 
    });
    renderApp();
    const form = await openAddForm();
    await userEvent.type(within(form).getByLabelText('Name'), 'Piano');
    await userEvent.click(within(form).getByRole('radio', { name: 'Other' }));
    await userEvent.click(within(form).getByRole('button', { name: 'Add' }));
    await screen.findByText('Piano was added.');
    expect(JSON.parse(sent(fake.requests, 'POST')[0]!.body)).toEqual({ name: 'Piano', type: 'custom' });
  });

  it('asks for an empty name without calling the server', async () => {
    const fake = installFakeServer({ ...signedIn, [listKey]: page([]) });
    renderApp();
    const form = await openAddForm();
    await userEvent.click(within(form).getByRole('button', { name: 'Add' }));
    const alert = await within(form).findByRole('alert');
    expect(alert).toHaveTextContent('Enter a name.');
    const input = within(form).getByLabelText('Name');
    expect(input).toHaveFocus();
    expect(input).toHaveAttribute('aria-invalid', 'true');
    expect(input.getAttribute('aria-describedby')).toBe(alert.id);
    expect(sent(fake.requests, 'POST')).toHaveLength(0);
  });

  it.each([
    ['invalid_length', 'The name needs to be 1 to 100 characters long.'],
    ['invalid_characters', 'The name has a character that isn’t allowed, like a line break. Try plain text.'],
  ])('422 %s on name shows a field error, focus and aria', async (code, message) => {
    installFakeServer({
      ...signedIn,
      [listKey]: page([]),
      [createKey]: problem(422, { field: 'name', code }),
    });
    renderApp();
    const form = await openAddForm();
    await userEvent.type(within(form).getByLabelText('Name'), 'x');
    await userEvent.click(within(form).getByRole('button', { name: 'Add' }));
    const alert = await within(form).findByRole('alert');
    await waitFor(() => expect(alert).toHaveTextContent(message));
    const input = within(form).getByLabelText('Name');
    expect(input).toHaveFocus();
    expect(input).toHaveAttribute('aria-invalid', 'true');
    expect(input.getAttribute('aria-describedby')).toBe(alert.id);
    expect(input).toHaveValue('x');
    expect(within(form).getByRole('button', { name: 'Add' })).toBeEnabled();
  });

  it('422 invalid_type focuses the type choice', async () => {
    installFakeServer({
      ...signedIn,
      [listKey]: page([]),
      [createKey]: problem(422, { field: 'type', code: 'invalid_type' }),
    });
    renderApp();
    const form = await openAddForm();
    await userEvent.type(within(form).getByLabelText('Name'), 'x');
    await userEvent.click(within(form).getByRole('button', { name: 'Add' }));
    const alert = await within(form).findByRole('alert');
    await waitFor(() => expect(alert).toHaveTextContent('Pick one of the options.'));
    expect(within(form).getByRole('radio', { name: 'Person' })).toHaveFocus();
    expect(within(form).getByRole('radiogroup')).toHaveAttribute('aria-invalid', 'true');
    expect(within(form).getByRole('radiogroup').getAttribute('aria-describedby')).toBe(alert.id);
  });

  it('5xx keeps the form and says to try again', async () => {
    installFakeServer({ ...signedIn, [listKey]: page([]), [createKey]: problem(503) });
    renderApp();
    const form = await openAddForm();
    await userEvent.type(within(form).getByLabelText('Name'), 'Zuzka');
    await userEvent.click(within(form).getByRole('button', { name: 'Add' }));
    expect(await within(form).findByText('We couldn’t confirm this was saved.')).toBeVisible();
    expect(within(form).getByRole('button', { name: 'Check again' })).toBeVisible();
    expect(within(form).getByLabelText('Name')).toHaveValue('Zuzka');
    expect(within(form).getByLabelText('Name')).toBeDisabled();
  });

  it('retries an uncertain create with the stored key and body regardless of later form edits', async () => {
    let posts = 0;
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([]),
      [createKey]: (req) => ++posts === 1 ? Promise.reject(new Error('offline')) : json(201, subject('s-retry', JSON.parse(req.body).name)),
    });
    renderApp();
    const form = await openAddForm();
    await userEvent.type(within(form).getByLabelText('Name'), 'Original');
    await userEvent.click(within(form).getByRole('button', { name: 'Add' }));
    const first = sent(fake.requests, 'POST')[0]!;
    const storedBody = JSON.parse(first.body);
    const retry = within(screen.getByRole('form')).getByRole('button', { name: 'Check again' });
    await waitFor(() => expect(within(screen.getByRole('form')).getByText('We couldn’t confirm this was saved.').closest('[role="status"]')).toHaveFocus());
    await userEvent.click(retry);
    expect(await screen.findByText('Original was added.')).toBeVisible();
    const postsSent = sent(fake.requests, 'POST');
    expect(postsSent).toHaveLength(2);
    expect(postsSent[1]!.headers.get('Idempotency-Key')).toBe(first.headers.get('Idempotency-Key'));
    expect(JSON.parse(postsSent[1]!.body)).toEqual(storedBody);
  });

  it('422 field validation clears the attempt and a corrected submit gets a new key', async () => {
    let calls = 0;
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([]),
      [createKey]: (req) => ++calls === 1 ? problem(422, { field: 'name', code: 'invalid_length' }) : json(201, subject('s-fixed', JSON.parse(req.body).name)),
    });
    renderApp();
    const form = await openAddForm();
    await userEvent.type(within(form).getByLabelText('Name'), 'x');
    await userEvent.click(within(form).getByRole('button', { name: 'Add' }));
    await within(form).findByRole('alert');
    const first = sent(fake.requests, 'POST')[0]!;
    await userEvent.click(within(form).getByRole('button', { name: 'Add' }));
    expect(await screen.findByText('x was added.')).toBeVisible();
    const posts = sent(fake.requests, 'POST');
    expect(posts[1]!.headers.get('Idempotency-Key')).not.toBe(first.headers.get('Idempotency-Key'));
  });

  it('401 on create returns to sign-in', async () => {
    installFakeServer({
      ...signedIn,
      [listKey]: page([]),
      [createKey]: problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
    });
    renderApp();
    const form = await openAddForm();
    await userEvent.type(within(form).getByLabelText('Name'), 'Zuzka');
    await userEvent.click(within(form).getByRole('button', { name: 'Add' }));
    expect(await screen.findByLabelText('Password')).toBeVisible();
  });

  it('cannot be submitted twice while pending', async () => {
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([]),
      [createKey]: async () => {
        await gate;
        return json(201, subject('s-9', 'Zuzka'));
      },
    });
    renderApp();
    const form = await openAddForm();
    await userEvent.type(within(form).getByLabelText('Name'), 'Zuzka{Enter}');
    expect(await within(form).findByRole('button', { name: 'Adding…' })).toBeDisabled();
    await userEvent.type(within(form).getByLabelText('Name'), '{Enter}');
    expect(sent(fake.requests, 'POST')).toHaveLength(1);
    release();
    expect(await screen.findByText('Zuzka was added.')).toBeVisible();
    expect(sent(fake.requests, 'POST')).toHaveLength(1);
  });
});

describe('edit', () => {
  it('reads the subject first, then PATCHes with the ETag from that read and only the changed fields', async () => {
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([anna, octavia]),
      [itemKey('s-2')]: etagged(octavia, '"7"'),
      [itemKey('s-2', 'PATCH')]: json(200, subject('s-2', 'Škoda Octavia', 'vehicle'), { ETag: '"8"' }),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Octavia' }));
    const form = await screen.findByRole('form', { name: 'Edit Octavia' });
    expect(within(form).getByLabelText('Name')).toHaveValue('Octavia');
    expect(within(form).getByRole('radio', { name: 'Vehicle' })).toBeChecked();
    const name = within(form).getByLabelText('Name');
    await userEvent.clear(name);
    await userEvent.type(name, 'Škoda Octavia');
    await userEvent.click(within(form).getByRole('button', { name: 'Save' }));

    expect(await screen.findByText('Škoda Octavia')).toBeVisible();
    expect(screen.queryByRole('form')).not.toBeInTheDocument();
    const calls = fake.requests.filter((r) => r.path === `${base}/subjects/s-2`);
    expect(calls.map((r) => r.method)).toEqual(['GET', 'PATCH']);
    expect(calls[1]!.headers.get('If-Match')).toBe('"7"');
    expect(JSON.parse(calls[1]!.body)).toEqual({ name: 'Škoda Octavia' });
  });

  it('patches a changed linked account, clears it explicitly, and clears automatically when changing type', async () => {
    const linked = { ...anna, linkedUserId: memberId };
    let current = linked;
    let etag = '"1"';
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([linked]),
      [membersKey]: membersResponse(),
      [itemKey('s-1')]: () => etagged(current, etag),
      [itemKey('s-1', 'PATCH')]: (req) => {
        const changes = JSON.parse(req.body);
        current = { ...current, ...changes };
        etag = `"${Number(etag.replaceAll('"', '')) + 1}"`;
        return json(200, current, { ETag: etag });
      },
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Anna' }));
    let form = await screen.findByRole('form', { name: 'Edit Anna' });
    await within(form).findByRole('combobox', { name: 'Their account in Tendo' });
    await userEvent.selectOptions(within(form).getByRole('combobox', { name: 'Their account in Tendo' }), '');
    await userEvent.click(within(form).getByRole('button', { name: 'Save' }));
    expect(JSON.parse(sent(fake.requests, 'PATCH')[0]!.body)).toEqual({ linkedUserId: null });

    await userEvent.click(await screen.findByRole('button', { name: 'Edit Anna' }));
    form = await screen.findByRole('form', { name: 'Edit Anna' });
    await userEvent.selectOptions(within(form).getByRole('combobox', { name: 'Their account in Tendo' }), memberId);
    await userEvent.click(within(form).getByRole('button', { name: 'Save' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Anna' }));
    form = await screen.findByRole('form', { name: 'Edit Anna' });
    await userEvent.click(within(form).getByRole('radio', { name: 'Vehicle' }));
    expect(within(form).getByText('Changing this to something other than a person will clear the linked account.')).toBeVisible();
    await userEvent.click(within(form).getByRole('button', { name: 'Save' }));
    expect(JSON.parse(sent(fake.requests, 'PATCH')[2]!.body)).toEqual({ type: 'vehicle', linkedUserId: null });
  });

  it('a failed member lookup does not block a name-only edit of a linked person', async () => {
    const linked = { ...anna, linkedUserId: memberId };
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([linked]),
      [membersKey]: problem(503),
      [itemKey('s-1')]: etagged(linked, '"6"'),
      [itemKey('s-1', 'PATCH')]: json(200, { ...linked, name: 'Anna Marie' }),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Anna' }));
    const form = await screen.findByRole('form', { name: 'Edit Anna' });
    await waitFor(() => expect(fake.requests.find((request) => request.method === 'GET' && request.path === `${base}/members`)).toBeDefined());
    await waitFor(() => expect(within(form).queryByRole('combobox', { name: 'Their account in Tendo' })).not.toBeInTheDocument());
    await userEvent.type(within(form).getByLabelText('Name'), ' Marie');
    await userEvent.click(within(form).getByRole('button', { name: 'Save' }));
    expect(JSON.parse(sent(fake.requests, 'PATCH')[0]!.body)).toEqual({ name: 'Anna Marie' });
  });

  it('keeps an unknown linked member value while saving an unrelated field', async () => {
    const linked = { ...anna, linkedUserId: memberId };
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([linked]),
      [membersKey]: json(200, { items: [{ userId: 'other-member', login: 'other', role: 'member' }], nextCursor: null }),
      [itemKey('s-1')]: etagged(linked, '"9"'),
      [itemKey('s-1', 'PATCH')]: json(200, { ...linked, name: 'Anna Marie' }),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Anna' }));
    const form = await screen.findByRole('form', { name: 'Edit Anna' });
    const account = await within(form).findByRole('combobox', { name: 'Their account in Tendo' });
    expect(account).toHaveValue(memberId);
    expect(within(account).getByRole('option', { name: 'Account not listed' })).toHaveValue(memberId);
    await userEvent.type(within(form).getByLabelText('Name'), ' Marie');
    await userEvent.click(within(form).getByRole('button', { name: 'Save' }));
    expect(JSON.parse(sent(fake.requests, 'PATCH')[0]!.body)).toEqual({ name: 'Anna Marie' });
  });

  it('passes member cursors and stops paging at the page cap', async () => {
    let listed = 0;
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([]),
      [membersKey]: (req) => {
        listed++;
        return req.query.get('cursor') === 'next'
          ? json(200, { items: [{ userId: 'member-page-two', login: 'member_two', role: 'member' }], nextCursor: null })
          : json(200, { items: [{ userId: memberId, login: 'member_one', role: 'member' }], nextCursor: 'next' });
      },
    });
    renderApp();
    const form = await openAddForm();
    await waitFor(() => expect(listed).toBe(6));
    const memberRequests = fake.requests.filter((request) => request.method === 'GET' && request.path === `${base}/members`);
    expect(memberRequests.map((request) => request.query.get('cursor'))).toEqual([null, 'next', null, 'next', null, 'next']);
    await waitFor(() => expect(within(form).getByRole('option', { name: 'member_two' })).toHaveValue('member-page-two'));

    cleanup();
    const capped = installFakeServer({
      ...signedIn,
      [listKey]: page([]),
      [membersKey]: (req) => json(200, { items: [], nextCursor: `cursor-${req.query.get('cursor') ?? 'start'}` }),
    });
    window.history.replaceState(null, '', '/people');
    renderApp();
    await screen.findByRole('heading', { level: 1, name: 'People and things' });
    await userEvent.click(await screen.findByRole('button', { name: 'Add' }));
    const capForm = await screen.findByRole('form', { name: 'Add a person or thing' });
    await waitFor(() => expect(capped.requests.filter((request) => request.method === 'GET' && request.path === `${base}/members`).length).toBe(40));
    expect(within(capForm).queryByRole('combobox', { name: 'Their account in Tendo' })).not.toBeInTheDocument();
  });

  it('allows saving while member loading is unresolved', async () => {
    let release!: (response: Response) => void;
    const waiting = new Promise<Response>((resolve) => { release = resolve; });
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([]),
      [membersKey]: () => waiting,
      [createKey]: json(201, subject('s-save-first', 'Solo')),
    });
    renderApp();
    const form = await openAddForm();
    await userEvent.type(within(form).getByLabelText('Name'), 'Solo');
    await userEvent.click(within(form).getByRole('button', { name: 'Add' }));
    expect(await screen.findByText('Solo was added.')).toBeVisible();
    release(membersResponse());
    expect(sent(fake.requests, 'POST')).toHaveLength(1);
  });

  it('ignores an obsolete successful member result after switching editors', async () => {
    let releaseObsolete!: (response: Response) => void;
    const obsoleteMembers = new Promise<Response>((resolve) => { releaseObsolete = resolve; });
    const linked = { ...anna, linkedUserId: memberId };
    let defer = false;
    let memberCall = 0;
    const currentMembers = () => json(200, { items: [{ userId: 'fresh-user', login: 'fresh_member', role: 'member' }], nextCursor: null });
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([linked]),
      [membersKey]: () => {
        memberCall++;
        if (defer) return obsoleteMembers;
        return memberCall <= 2 ? membersResponse() : currentMembers();
      },
      [itemKey('s-1')]: etagged(linked, '"1"'),
    });
    renderApp();
    await openPeople();
    await screen.findByText('Account: member_one');
    defer = true;
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Anna' }));
    const obsoleteForm = await screen.findByRole('form', { name: 'Edit Anna' });
    await waitFor(() => expect(fake.requests.filter((request) => request.method === 'GET' && request.path === `${base}/members`).length).toBeGreaterThan(1));
    await userEvent.click(within(obsoleteForm).getByRole('button', { name: 'Cancel' }));
    defer = false;
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Anna' }));
    const form = await screen.findByRole('form', { name: 'Edit Anna' });
    await waitFor(() => expect(within(form).getByRole('option', { name: 'fresh_member' })).toBeInTheDocument());
    await act(async () => { releaseObsolete(json(200, { items: [{ userId: 'obsolete-user', login: 'obsolete_member', role: 'member' }], nextCursor: null })); });
    expect(within(form).getByRole('option', { name: 'fresh_member' })).toBeInTheDocument();
    expect(within(form).queryByRole('option', { name: 'obsolete_member' })).not.toBeInTheDocument();
    expect(fake.requests.filter((request) => request.method === 'GET' && request.path === `${base}/members`).length).toBeGreaterThan(2);
  });

  it('ignores an obsolete member 401 after switching editors', async () => {
    let releaseObsolete!: (response: Response) => void;
    const obsolete = new Promise<Response>((resolve) => { releaseObsolete = resolve; });
    let calls = 0;
    const linkedFirst = { ...anna, linkedUserId: memberId };
    const linkedSecond = { ...subject('s-3', 'Bea'), linkedUserId: memberId };
    const currentMembers = () => json(200, { items: [{ userId: 'current-user', login: 'current_member', role: 'member' }], nextCursor: null });
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([linkedFirst, linkedSecond]),
      [membersKey]: () => {
        const call = calls++;
        if (call === 0) return currentMembers();
        if (call === 1) return obsolete;
        return currentMembers();
      },
      [itemKey('s-1')]: etagged(linkedFirst, '"1"'),
      [itemKey('s-3')]: etagged(linkedSecond, '"1"'),
    });
    renderApp();
    await openPeople();
    const firstOpening = userEvent.click(await screen.findByRole('button', { name: 'Edit Anna' }));
    await firstOpening;
    const firstForm = await screen.findByRole('form', { name: 'Edit Anna' });
    await waitFor(() => expect(fake.requests.filter((request) => request.method === 'GET' && request.path === `${base}/members`)).toHaveLength(3));
    await userEvent.click(within(firstForm).getByRole('button', { name: 'Cancel' }));
    const secondOpening = userEvent.click(await screen.findByRole('button', { name: 'Edit Bea' }));
    await secondOpening;
    await waitFor(() => expect(fake.requests.filter((request) => request.method === 'GET' && request.path === `${base}/members`)).toHaveLength(5));
    const currentForm = await screen.findByRole('form', { name: 'Edit Bea' });
    await waitFor(() => expect(within(currentForm).getByRole('option', { name: 'current_member' })).toBeInTheDocument());
    await act(async () => { releaseObsolete(problem(401)); });
    expect(within(currentForm).getByRole('option', { name: 'current_member' })).toBeInTheDocument();
    expect(within(currentForm).queryByRole('option', { name: 'member_one' })).not.toBeInTheDocument();
    expect(fake.requests.filter((request) => request.method === 'GET' && request.path === `${base}/members`)).toHaveLength(5);
    expect(screen.queryByLabelText('Password')).not.toBeInTheDocument();
  });

  it('ignores a member 401 after navigating away while App remains mounted', async () => {
    let release!: (response: Response) => void;
    const delayed = new Promise<Response>((resolve) => { release = resolve; });
    let calls = 0;
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([]),
      [membersKey]: () => {
        const call = ++calls;
        return call === 2 ? delayed : membersResponse();
      },
    });
    const app = renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Add' }));
    await screen.findByRole('form', { name: 'Add a person or thing' });
    await waitFor(() => expect(calls).toBe(3));
    await waitFor(() => expect(fake.requests.some((request) => request.method === 'GET' && request.path === `${base}/members`)).toBe(true));
    await userEvent.click(await screen.findByRole('link', { name: 'Back to home' }));
    await screen.findByRole('heading', { level: 1, name: 'Veselí' });
    await act(async () => { release(problem(401)); });
    expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible();
    expect(screen.queryByLabelText('Password')).not.toBeInTheDocument();
    app.unmount();
  });

  it('hides the account chooser after member loading fails but still saves', async () => {
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([]),
      [membersKey]: problem(503),
      [createKey]: json(201, subject('s-9', 'Solo')),
    });
    renderApp();
    const form = await openAddForm();
    await userEvent.type(within(form).getByLabelText('Name'), 'Solo');
    await waitFor(() => expect(within(form).queryByRole('combobox', { name: 'Their account in Tendo' })).not.toBeInTheDocument());
    await userEvent.click(within(form).getByRole('button', { name: 'Add' }));
    expect(JSON.parse(sent(fake.requests, 'POST')[0]!.body)).toEqual({ name: 'Solo', type: 'person' });
  });

  it('can change the type', async () => {
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([octavia]),
      [itemKey('s-2')]: etagged(octavia, '"3"'),
      [itemKey('s-2', 'PATCH')]: json(200, subject('s-2', 'Octavia', 'custom'), { ETag: '"4"' }),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Octavia' }));
    await userEvent.click(await screen.findByRole('radio', { name: 'Other' }));
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    await waitFor(() => expect(screen.queryByRole('form')).not.toBeInTheDocument());
    expect(JSON.parse(sent(fake.requests, 'PATCH')[0]!.body)).toEqual({ type: 'custom' });
    expect(screen.getByText('Other')).toBeVisible();
  });

  it('412 shows a calm message, reloads the latest values, and a retry uses the new ETag', async () => {
    let patches = 0;
    let gets = 0;
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([octavia]),
      [itemKey('s-2')]: () =>
        ++gets === 1 ? etagged(octavia, '"7"') : etagged(subject('s-2', 'Octavia RS', 'vehicle'), '"8"'),
      [itemKey('s-2', 'PATCH')]: () =>
        ++patches === 1 ? problem(412, { code: 'precondition_failed' }) : json(200, subject('s-2', 'Octavia RS 2', 'vehicle')),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Octavia' }));
    const name = await screen.findByLabelText('Name');
    await userEvent.clear(name);
    await userEvent.type(name, 'Mine');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(await screen.findByText(/Someone else changed this just now/)).toBeVisible();
    expect(screen.getByLabelText('Name')).toHaveValue('Octavia RS');
    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled();

    const retry = screen.getByLabelText('Name');
    await userEvent.clear(retry);
    await userEvent.type(retry, 'Octavia RS 2');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('Octavia RS 2')).toBeVisible();
    const patchesSent = sent(fake.requests, 'PATCH');
    expect(patchesSent.map((r) => r.headers.get('If-Match'))).toEqual(['"7"', '"8"']);
    expect(JSON.parse(patchesSent[1]!.body)).toEqual({ name: 'Octavia RS 2' });
  });

  it('412 summarizes the previous account choice and retry uses latest values and ETag', async () => {
    const latestUserId = '0198a2f0-0000-7000-8000-000000000088';
    const latest = { ...anna, linkedUserId: latestUserId };
    let reads = 0;
    let patches = 0;
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([anna]),
      [membersKey]: json(200, { items: [
        { userId: memberId, login: 'member_one', role: 'member' },
        { userId: latestUserId, login: 'member_two', role: 'member' },
      ], nextCursor: null }),
      [itemKey('s-1')]: () => ++reads === 1 ? etagged(anna, '"1"') : etagged(latest, '"2"'),
      [itemKey('s-1', 'PATCH')]: () => ++patches === 1 ? problem(412) : json(200, { ...latest, linkedUserId: memberId }),
    });
    renderApp();
    await openPeople();
    const opening = userEvent.click(await screen.findByRole('button', { name: 'Edit Anna' }));
    await opening;
    let form = await screen.findByRole('form', { name: 'Edit Anna' });
    await userEvent.selectOptions(await within(form).findByRole('combobox', { name: 'Their account in Tendo' }), memberId);
    await userEvent.click(within(form).getByRole('button', { name: 'Save' }));
    form = await screen.findByRole('form', { name: 'Edit Anna' });
    expect(await screen.findByText('You had: Anna (Person, Account: member_one)')).toBeVisible();
    expect(within(form).getByRole('combobox', { name: 'Their account in Tendo' })).toHaveValue(latestUserId);
    await userEvent.selectOptions(within(form).getByRole('combobox', { name: 'Their account in Tendo' }), memberId);
    await userEvent.click(within(form).getByRole('button', { name: 'Save' }));
    const patchesSent = sent(fake.requests, 'PATCH');
    expect(JSON.parse(patchesSent[0]!.body)).toEqual({ linkedUserId: memberId });
    expect(JSON.parse(patchesSent[1]!.body)).toEqual({ linkedUserId: memberId });
    expect(patchesSent.map((request) => request.headers.get('If-Match'))).toEqual(['"1"', '"2"']);
  });

  it('422 linkedUserId focuses the account select and announces a field error', async () => {
    installFakeServer({
      ...signedIn,
      [listKey]: page([]),
      [membersKey]: membersResponse(),
      [createKey]: problem(422, { field: 'linkedUserId', code: 'invalid_reference' }),
    });
    renderApp();
    const form = await openAddForm();
    await userEvent.type(within(form).getByLabelText('Name'), 'Lena');
    const select = await screen.findByRole('combobox', { name: 'Their account in Tendo' });
    await userEvent.selectOptions(select, memberId);
    await userEvent.click(within(form).getByRole('button', { name: 'Add' }));
    const alert = await within(form).findByRole('alert');
    await waitFor(() => expect(alert).toHaveTextContent('Choose an account that belongs to this household.'));
    expect(select).toHaveFocus();
    expect(select).toHaveAttribute('aria-invalid', 'true');
    expect(select.getAttribute('aria-describedby')).toBe(alert.id);
  });

  it('404 on save says it no longer exists and refreshes the list', async () => {
    let lists = 0;
    installFakeServer({
      ...signedIn,
      [listKey]: (req) => req.query.get('limit') === '100' ? page([]) : (++lists === 1 ? page([anna, octavia]) : page([anna])),
      [itemKey('s-2')]: etagged(octavia, '"7"'),
      [itemKey('s-2', 'PATCH')]: problem(404),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Octavia' }));
    await userEvent.type(await screen.findByLabelText('Name'), 'x');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText(/no longer exists/)).toBeVisible();
    await waitFor(() => expect(screen.queryByText('Octavia')).not.toBeInTheDocument());
    expect(screen.getByText('Anna')).toBeVisible();
  });

  it('422 on rename marks the name field', async () => {
    installFakeServer({
      ...signedIn,
      [listKey]: page([anna]),
      [itemKey('s-1')]: etagged(anna, '"1"'),
      [itemKey('s-1', 'PATCH')]: problem(422, { field: 'name', code: 'invalid_length' }),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Anna' }));
    await userEvent.type(await screen.findByLabelText('Name'), '!');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    const input = screen.getByLabelText('Name');
    await waitFor(() => expect(input).toHaveAttribute('aria-invalid', 'true'));
    expect(input).toHaveFocus();
    expect(within(screen.getByRole('form', { name: 'Edit Anna' })).getByRole('alert')).toHaveTextContent('1 to 100 characters');
  });

  it('cancel closes the form without any PATCH', async () => {
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([anna]),
      [itemKey('s-1')]: etagged(anna, '"1"'),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Anna' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('form')).not.toBeInTheDocument();
    expect(sent(fake.requests, 'PATCH')).toHaveLength(0);
  });
});

describe('archive and restore', () => {
  it('archive sends {archived:true} with the ETag, removes the row, and Undo restores it with the new ETag and focus', async () => {
    let current = octavia;
    let etag = '"5"';
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([anna, octavia]),
      [itemKey('s-2')]: () => etagged(current, etag),
      [itemKey('s-2', 'PATCH')]: (req) => {
        current = { ...current, archived: JSON.parse(req.body).archived };
        etag = `"${Number(etag.replaceAll('"', '')) + 1}"`;
        return json(200, current, { ETag: etag });
      },
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Archive Octavia' }));

    expect(await screen.findByText('Octavia was archived.')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Archive Octavia' })).not.toBeInTheDocument();
    expect(within(screen.getByRole('list')).queryByText('Octavia')).not.toBeInTheDocument();
    expect(screen.getByText('Anna')).toBeVisible();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    let patch = sent(fake.requests, 'PATCH')[0]!;
    expect(patch.headers.get('If-Match')).toBe('"5"');
    expect(JSON.parse(patch.body)).toEqual({ archived: true });
    expect(fake.requests.indexOf(patch)).toBeGreaterThan(
      fake.requests.findIndex((r) => r.method === 'GET' && r.path === `${base}/subjects/s-2`),
    );

    await userEvent.click(screen.getByRole('button', { name: 'Undo' }));
    const edit = await screen.findByRole('button', { name: 'Edit Octavia' });
    expect(sent(fake.requests, 'PATCH')).toHaveLength(2);
    patch = sent(fake.requests, 'PATCH')[1]!;
    expect(JSON.parse(patch.body)).toEqual({ archived: false });
    expect(patch.headers.get('If-Match')).toBe('"6"');
    await waitFor(() => expect(edit).toHaveFocus());
    expect(screen.queryByText('Octavia was archived.')).not.toBeInTheDocument();
    const names = within(screen.getByRole('list'))
      .getAllByRole('listitem')
      .map((r) => r.querySelector('[data-subject-name]')!.textContent);
    expect(names).toEqual(['Anna', 'Octavia']);
  });

  it('skips the PATCH when the fresh read already shows the wanted archived state', async () => {
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([octavia]),
      [itemKey('s-2')]: etagged({ ...octavia, archived: true }, '"5"'),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Archive Octavia' }));
    expect(await screen.findByText('Octavia was archived.')).toBeVisible();
    expect(sent(fake.requests, 'PATCH')).toHaveLength(0);
  });

  it('archived view lists with archived=true and can bring one back', async () => {
    const archivedOcta = { ...octavia, archived: true };
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: (req) => (req.query.get('archived') === 'true' ? page([archivedOcta]) : page([anna])),
      [itemKey('s-2')]: etagged(archivedOcta, '"9"'),
      [itemKey('s-2', 'PATCH')]: json(200, octavia, { ETag: '"10"' }),
    });
    renderApp();
    await openPeople();
    await screen.findByText('Anna');
    await userEvent.click(screen.getByRole('button', { name: 'Show archived' }));

    expect(await screen.findByText('Octavia')).toBeVisible();
    expect(screen.queryByText('Anna')).not.toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 2, name: 'Archived' })).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Add' })).not.toBeInTheDocument();
    const archivedCall = fake.requests.filter((r) => r.method === 'GET' && r.path === `${base}/subjects`).at(-1)!;
    expect(archivedCall.query.get('archived')).toBe('true');

    await userEvent.click(screen.getByRole('button', { name: 'Restore Octavia' }));
    expect(await screen.findByText('Octavia was restored.')).toBeVisible();
    const patch = sent(fake.requests, 'PATCH')[0]!;
    expect(patch.headers.get('If-Match')).toBe('"9"');
    expect(JSON.parse(patch.body)).toEqual({ archived: false });
    expect(screen.queryByRole('button', { name: 'Restore Octavia' })).not.toBeInTheDocument();
    expect(screen.getByText('Nothing is archived.')).toBeVisible();

    await userEvent.click(screen.getByRole('button', { name: 'Back to the list' }));
    expect(await screen.findByText('Anna')).toBeVisible();
  });

  it('412 while archiving shows the changed notice, sends one PATCH, re-fetches and keeps the row', async () => {
    let lists = 0;
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: (req) => req.query.get('limit') === '100' ? page([]) : (++lists, page([octavia])),
      [itemKey('s-2')]: etagged(octavia, '"5"'),
      [itemKey('s-2', 'PATCH')]: problem(412),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Archive Octavia' }));
    const notice = await screen.findByText('That changed just now. The list is up to date.');
    expect(notice).toBeVisible();
    expect(screen.queryByText(/no longer exists/)).not.toBeInTheDocument();
    await waitFor(() => expect(lists).toBe(2));
    expect(sent(fake.requests, 'PATCH')).toHaveLength(1);
    expect(await screen.findByRole('button', { name: 'Archive Octavia' })).toBeVisible();
    expect(screen.getByText('Octavia')).toBeVisible();
  });

  it('a failed archive says so and keeps the row', async () => {
    installFakeServer({
      ...signedIn,
      [listKey]: page([octavia]),
      [itemKey('s-2')]: problem(503),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Archive Octavia' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('That didn’t work.');
    expect(screen.getByText('Octavia')).toBeVisible();
  });

  it('401 while archiving returns to sign-in', async () => {
    installFakeServer({
      ...signedIn,
      [listKey]: page([octavia]),
      [itemKey('s-2')]: problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Archive Octavia' }));
    expect(await screen.findByLabelText('Password')).toBeVisible();
  });
});

describe('Czech', () => {
  it('renders the linked account labels in Czech', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer({ ...signedIn, [listKey]: page([]), [membersKey]: membersResponse() });
    renderApp();
    await userEvent.click(await screen.findByRole('link', { name: 'Lidé a věci' }));
    await screen.findByRole('heading', { level: 1, name: 'Lidé a věci' });
    await userEvent.click(await screen.findByRole('button', { name: 'Přidat' }));
    const select = await screen.findByRole('combobox', { name: 'Jejich účet v Tendu' });
    expect(select).toHaveValue('');
    expect(within(screen.getByRole('form')).getByText('Pomáhá jen poznat, o koho jde. Nemění to, co kdo může vidět.')).toBeVisible();
  });

  it('shows the uncertain-create notice in Czech', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer({ ...signedIn, [listKey]: page([]), [createKey]: problem(503) });
    renderApp();
    await userEvent.click(await screen.findByRole('link', { name: 'Lidé a věci' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Přidat' }));
    await userEvent.type(screen.getByLabelText('Jméno nebo název'), 'Anna');
    await userEvent.click(screen.getByRole('button', { name: 'Přidat' }));
    expect(await screen.findByText('Nepodařilo se ověřit, že se to uložilo.')).toBeVisible();
  });

  it('renders the screen, types and actions in Czech', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer({ ...signedIn, [listKey]: page([anna, octavia]) });
    renderApp();
    await userEvent.click(await screen.findByRole('link', { name: 'Lidé a věci' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Lidé a věci' })).toBeVisible();
    expect(await screen.findByText('Osoba')).toBeVisible();
    expect(screen.getByText('Vozidlo')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Upravit: Anna' })).toBeVisible();
    expect(screen.getByRole('button', { name: 'Archivovat: Anna' })).toBeVisible();
    expect(screen.getByRole('link', { name: 'Zpět na úvod' })).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Přidat' }));
    const form = screen.getByRole('form', { name: 'Přidat osobu nebo věc' });
    for (const label of ['Osoba', 'Domov', 'Vozidlo', 'Zvíře', 'Jiné']) {
      expect(within(form).getByRole('radio', { name: label })).toBeVisible();
    }
  });
});

describe('stale responses', () => {
  /** A promise the test releases by hand, plus a flag that says the server finished answering. */
  function gate() {
    let release!: () => void;
    const open = new Promise<void>((r) => (release = r));
    const state = { answered: false };
    return { open, release, state };
  }
  const settle = () => act(async () => {});

  it('a slow "Show more" from one view never lands in another', async () => {
    const g = gate();
    const archivedOcta = { ...octavia, archived: true };
    installFakeServer({
      ...signedIn,
      [listKey]: async (req) => {
        if (req.query.get('cursor') === 'abc') {
          await g.open;
          g.state.answered = true;
          return page([subject('s-3', 'Late active', 'pet')]);
        }
        return req.query.get('archived') === 'true' ? page([archivedOcta]) : page([anna], 'abc');
      },
    });
    renderApp();
    await openPeople();
    await screen.findByText('Anna');
    await userEvent.click(screen.getByRole('button', { name: 'Show more' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Show archived' }));
    expect(await screen.findByText('Octavia')).toBeVisible();
    g.release();
    await waitFor(() => expect(g.state.answered).toBe(true));
    await settle();
    expect(screen.queryByText('Late active')).not.toBeInTheDocument();
    expect(screen.getByText('Octavia')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Show more' })).not.toBeInTheDocument();
  });

  it('an edit read that resolves after switching views and back does not open the form', async () => {
    const g = gate();
    installFakeServer({
      ...signedIn,
      [listKey]: (req) => (req.query.get('archived') === 'true' ? page([]) : page([anna])),
      [itemKey('s-1')]: async () => {
        await g.open;
        g.state.answered = true;
        return etagged(anna, '"1"');
      },
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Anna' }));
    await userEvent.click(screen.getByRole('button', { name: 'Show archived' }));
    expect(await screen.findByText('Nothing is archived.')).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Back to the list' }));
    expect(await screen.findByText('Anna')).toBeVisible();
    g.release();
    await waitFor(() => expect(g.state.answered).toBe(true));
    await settle();
    expect(screen.queryByRole('form')).not.toBeInTheDocument();
  });

  it('an add that finishes after a view switch still says it was added', async () => {
    const g = gate();
    installFakeServer({
      ...signedIn,
      [listKey]: (req) => (req.query.get('archived') === 'true' ? page([]) : page([anna])),
      [createKey]: async () => {
        await g.open;
        g.state.answered = true;
        return json(201, subject('s-9', 'Zuzka'), { ETag: '"1"' });
      },
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Add' }));
    await userEvent.type(screen.getByLabelText('Name'), 'Zuzka{Enter}');
    await userEvent.click(await screen.findByRole('button', { name: 'Show archived' }));
    expect(await screen.findByText('Nothing is archived.')).toBeVisible();
    g.release();
    await waitFor(() => expect(g.state.answered).toBe(true));
    expect(await screen.findByText('Zuzka was added.')).toBeVisible();
    // The archived list must not have taken the new active row.
    expect(screen.queryByRole('list')).not.toBeInTheDocument();
  });
});

describe('focus', () => {
  async function openEdit(name = 'Anna') {
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: `Edit ${name}` }));
    return screen.findByRole('form', { name: `Edit ${name}` });
  }
  const editable = {
    ...signedIn,
    [listKey]: page([anna]),
    [itemKey('s-1')]: etagged(anna, '"1"'),
  };

  it('a freshly opened edit form focuses its name field', async () => {
    installFakeServer(editable);
    renderApp();
    const form = await openEdit();
    expect(within(form).getByLabelText('Name')).toHaveFocus();
  });

  it('Save returns focus to that row’s Edit button', async () => {
    installFakeServer({ ...editable, [itemKey('s-1', 'PATCH')]: json(200, subject('s-1', 'Anna B'), { ETag: '"2"' }) });
    renderApp();
    const form = await openEdit();
    await userEvent.type(within(form).getByLabelText('Name'), ' B');
    await userEvent.click(within(form).getByRole('button', { name: 'Save' }));
    const edit = await screen.findByRole('button', { name: 'Edit Anna B' });
    await waitFor(() => expect(edit).toHaveFocus());
  });

  it('Cancel returns focus to that row’s Edit button', async () => {
    installFakeServer(editable);
    renderApp();
    const form = await openEdit();
    await userEvent.click(within(form).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Edit Anna' })).toHaveFocus());
  });

  it('Cancel of the add form returns focus to Add', async () => {
    installFakeServer({ ...signedIn, [listKey]: page([anna]) });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Add' }));
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Add' })).toHaveFocus());
  });

  it('the view toggle keeps focus on itself, in both directions', async () => {
    installFakeServer({ ...signedIn, [listKey]: page([anna]) });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Show archived' }));
    const back = await screen.findByRole('button', { name: 'Back to the list' });
    expect(back).toHaveFocus();
    await userEvent.click(back);
    expect(await screen.findByRole('button', { name: 'Show archived' })).toHaveFocus();
  });

  it('after the last "Show more" page, focus lands on the first new row', async () => {
    installFakeServer({
      ...signedIn,
      [listKey]: (req) => (req.query.get('cursor') ? page([octavia]) : page([anna], 'abc')),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Show more' }));
    const edit = await screen.findByRole('button', { name: 'Edit Octavia' });
    await waitFor(() => expect(edit).toHaveFocus());
  });
});

describe('conflict', () => {
  it('focuses the announcement and shows what the user had typed next to the latest values', async () => {
    let gets = 0;
    installFakeServer({
      ...signedIn,
      [listKey]: page([octavia]),
      [itemKey('s-2')]: () =>
        ++gets === 1 ? etagged(octavia, '"7"') : etagged(subject('s-2', 'Octavia RS', 'vehicle'), '"8"'),
      [itemKey('s-2', 'PATCH')]: problem(412),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Octavia' }));
    const name = await screen.findByLabelText('Name');
    await userEvent.clear(name);
    await userEvent.type(name, 'Mine');
    await userEvent.click(screen.getByRole('radio', { name: 'Pet' }));
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));

    const message = await screen.findByText(/Someone else changed this just now/);
    const notice = message.closest('[role="status"]')!;
    await waitFor(() => expect(notice).toHaveFocus());
    expect(screen.getByLabelText('Name')).toHaveValue('Octavia RS');
    expect(screen.getByRole('radio', { name: 'Vehicle' })).toBeChecked();
    expect(within(notice as HTMLElement).getByText('You had: Mine (Pet)')).toBeVisible();
  });
});

describe('other failures and edge cases', () => {
  it('404 on the read that opens the editor says it is gone and reloads', async () => {
    let lists = 0;
    installFakeServer({
      ...signedIn,
      [listKey]: (req) => req.query.get('limit') === '100' ? page([]) : (++lists === 1 ? page([anna, octavia]) : page([anna])),
      [itemKey('s-2')]: problem(404),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Octavia' }));
    expect(await screen.findByText('That one no longer exists. The list is up to date now.')).toBeVisible();
    await waitFor(() => expect(screen.queryByText('Octavia')).not.toBeInTheDocument());
    expect(lists).toBe(2);
    expect(screen.queryByRole('form')).not.toBeInTheDocument();
  });

  it('opening the editor writes the fresh values back into the row', async () => {
    installFakeServer({
      ...signedIn,
      [listKey]: page([anna]),
      [itemKey('s-1')]: etagged(subject('s-1', 'Anna Nová'), '"4"'),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Anna' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Cancel' }));
    expect(await screen.findByRole('button', { name: 'Edit Anna Nová' })).toBeVisible();
  });

  it('a failed "Show more" shows an alert and keeps the list and the button', async () => {
    installFakeServer({
      ...signedIn,
      [listKey]: (req) => (req.query.get('cursor') ? problem(503) : page([anna], 'abc')),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Show more' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('That didn’t work.');
    expect(screen.getByText('Anna')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Show more' })).toBeEnabled();
  });

  it('saving without changes sends no PATCH', async () => {
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([anna]),
      [itemKey('s-1')]: etagged(anna, '"1"'),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Anna' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Save' }));
    await waitFor(() => expect(screen.queryByRole('form')).not.toBeInTheDocument());
    expect(sent(fake.requests, 'PATCH')).toHaveLength(0);
  });

  it('the type choice is a radiogroup named "What is it?"', async () => {
    installFakeServer({ ...signedIn, [listKey]: page([]) });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Add' }));
    expect(screen.getByRole('radiogroup', { name: 'What is it?' })).toBeVisible();
  });

  it('an unrecognised 422 reads as a problem with the name, not a connection error', async () => {
    installFakeServer({
      ...signedIn,
      [listKey]: page([]),
      [createKey]: problem(422, { code: 'something_new' }),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Add' }));
    await userEvent.type(screen.getByLabelText('Name'), 'x');
    await userEvent.click(within(screen.getByRole('form')).getByRole('button', { name: 'Add' }));
    const alert = await screen.findByRole('alert');
    await waitFor(() => expect(alert).toHaveTextContent('That name won’t work.'));
    expect(screen.getByLabelText('Name')).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByLabelText('Name')).toHaveFocus();
  });

  it('a household 404 shows the not-part-of-a-household message, no retry loop', async () => {
    const fake = installFakeServer({ ...signedIn, [listKey]: problem(404) });
    renderApp();
    await openPeople();
    expect(await screen.findByText('Your account isn’t part of a household yet.')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument();
    expect(fake.requests.filter((r) => r.method === 'GET' && r.path === `${base}/subjects` && r.query.get('limit') === '50')).toHaveLength(1);
  });

  it('shows who is signed in and Sign out on the people screen, and signing out works', async () => {
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([anna]),
      'DELETE /api/v1/session': json(204),
      'GET /api/v1/auth/setup': json(200, { required: false }),
    });
    renderApp();
    await openPeople();
    expect(screen.getByText('Signed in as anna')).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Sign out' }));
    expect(await screen.findByLabelText('Password')).toBeVisible();
    expect(sent(fake.requests, 'DELETE')).toHaveLength(1);
    expect(window.location.pathname).toBe('/');
  });

  it('signing in from /people lands on home at /', async () => {
    window.history.replaceState(null, '', '/people');
    installFakeServer({
      'GET /api/v1/session': problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'POST /api/v1/session': json(201, session()),
      [`GET ${base}`]: json(200, household),
    });
    renderApp();
    await userEvent.type(await screen.findByLabelText('Login'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'correct horse battery{Enter}');
    expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible();
    expect(window.location.pathname).toBe('/');
  });
});

describe('changed meanwhile', () => {
  it('opening the editor on a subject archived by someone else shows the changed notice instead', async () => {
    let lists = 0;
    installFakeServer({
      ...signedIn,
      [listKey]: (req) => req.query.get('limit') === '100' ? page([]) : (++lists === 1 ? page([anna, octavia]) : page([anna])),
      [itemKey('s-2')]: etagged({ ...octavia, archived: true }, '"5"'),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Octavia' }));
    expect(await screen.findByText('That changed just now. The list is up to date.')).toBeVisible();
    expect(screen.queryByRole('form')).not.toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText('Octavia')).not.toBeInTheDocument());
    expect(lists).toBe(2);
  });

  it('a conflict re-read that finds the subject archived closes the editor with the changed notice', async () => {
    let gets = 0;
    let lists = 0;
    installFakeServer({
      ...signedIn,
      [listKey]: (req) => req.query.get('limit') === '100' ? page([]) : (++lists === 1 ? page([octavia]) : page([])),
      [itemKey('s-2')]: () => (++gets === 1 ? etagged(octavia, '"7"') : etagged({ ...octavia, archived: true }, '"8"')),
      [itemKey('s-2', 'PATCH')]: problem(412),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Edit Octavia' }));
    await userEvent.type(await screen.findByLabelText('Name'), 'x');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('That changed just now. The list is up to date.')).toBeVisible();
    expect(screen.queryByRole('form')).not.toBeInTheDocument();
    await waitFor(() => expect(lists).toBe(2));
  });
});

describe('failed undo', () => {
  it('keeps the notice and Undo, shows the alert, and focus stays on the notice', async () => {
    let patches = 0;
    const fake = installFakeServer({
      ...signedIn,
      [listKey]: page([anna, octavia]),
      // The server really holds the archived state after the first PATCH, so Undo has work to do.
      [itemKey('s-2')]: () => (patches === 0 ? etagged(octavia, '"5"') : etagged({ ...octavia, archived: true }, '"6"')),
      [itemKey('s-2', 'PATCH')]: () =>
        ++patches === 1 ? json(200, { ...octavia, archived: true }, { ETag: '"6"' }) : problem(503),
    });
    renderApp();
    await openPeople();
    await userEvent.click(await screen.findByRole('button', { name: 'Archive Octavia' }));
    await screen.findByText('Octavia was archived.');
    await userEvent.click(screen.getByRole('button', { name: 'Undo' }));

    expect(await screen.findByRole('alert')).toHaveTextContent('That didn’t work.');
    expect(screen.getByText('Octavia was archived.')).toBeVisible();
    const undo = screen.getByRole('button', { name: 'Undo' });
    expect(undo).toBeEnabled();
    expect(document.body).not.toHaveFocus();
    await waitFor(() => expect(screen.getByText('Octavia was archived.').closest('[role="status"]')).toHaveFocus());
    expect(sent(fake.requests, 'PATCH')).toHaveLength(2);

    // And it can simply be tried again.
    await userEvent.click(undo);
    await waitFor(() => expect(sent(fake.requests, 'PATCH')).toHaveLength(3));
  });
});

describe('Czech name errors', () => {
  it('say "Jméno nebo název" consistently', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer({ ...signedIn, [listKey]: page([]) });
    renderApp();
    await userEvent.click(await screen.findByRole('link', { name: 'Lidé a věci' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Přidat' }));
    await userEvent.click(within(screen.getByRole('form')).getByRole('button', { name: 'Přidat' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Zadejte jméno nebo název.');
  });
});

describe('no household on /people', () => {
  it('lands on home and puts / in the address bar', async () => {
    window.history.replaceState(null, '', '/people');
    installFakeServer({ 'GET /api/v1/session': json(200, session(false)) });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1 })).toHaveTextContent(
      'Your account isn’t part of a household yet.',
    );
    await waitFor(() => expect(window.location.pathname).toBe('/'));
  });
});
