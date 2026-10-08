import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, emptyHome, household, installFakeServer, json, session } from '../../test/fakeServer';

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const page = (items: unknown[]) => json(200, { items, nextCursor: null });
const members = (role = 'owner') => [{ userId: session().userId, login: 'anna', role }, { userId: 'other', login: 'petr', role: 'member' }];
const invitation = (id: string, status = 'pending') => ({ id, status, createdAt: '2026-01-01T00:00:00Z', expiresAt: '2026-01-08T00:00:00Z' });
function renderApp() { return render(<I18nProvider><App /></I18nProvider>); }
function defaults() { return { 'GET /api/v1/session': json(200, session()), [`GET ${base}`]: json(200, household), ...emptyHome, [`GET ${base}/members`]: page(members()), [`GET ${base}/invitations`]: page([]) }; }
const problem = (status: number, extra: Record<string, unknown> = {}) => json(status, { type: 'about:blank', title: 'x', status, ...extra });
beforeEach(() => { vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']); });
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe('members and invitations', () => {
  it('home links to household members and displays member roles and who is you', async () => {
    const fake = installFakeServer(defaults()); renderApp();
    expect(await screen.findByRole('link', { name: 'Household members' })).toHaveAttribute('href', '/members');
    await userEvent.click(screen.getByRole('link', { name: 'Household members' }));
    const rows = await screen.findAllByRole('listitem');
    expect(rows.map((row) => row.textContent)).toEqual(['anna (you)Owner', 'petrMember']);
    expect(fake.requests.some((r) => r.method === 'GET' && r.path === `${base}/members`)).toBe(true);
  });

  it('creates links with unique idempotency keys, empty body, and copies the exact URL', async () => {
    const token = 'a'.repeat(43); const fake = installFakeServer({ ...defaults(), [`POST ${base}/invitations`]: json(201, { token }) });
    const writeText = vi.fn().mockResolvedValue(undefined); Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    const create = await screen.findByRole('button', { name: 'Create invite link' });
    await userEvent.click(create); const link = `${location.origin}/invite#${token}`;
    expect(await screen.findByLabelText('Invitation link')).toHaveValue(link);
    await userEvent.click(screen.getByRole('button', { name: 'Copy' })); expect(writeText).toHaveBeenCalledWith(link);
    await userEvent.click(screen.getByRole('button', { name: 'Create invite link' }));
    const posts = fake.requests.filter((r) => r.method === 'POST' && r.path === `${base}/invitations`);
    expect(posts).toHaveLength(2); expect(posts.map((r) => r.body)).toEqual(['{}', '{}']);
    const keys = posts.map((r) => r.headers.get('Idempotency-Key'));
    expect(keys[0]).toMatch(/^[0-9a-f]{32}$/); expect(keys[1]).toMatch(/^[0-9a-f]{32}$/); expect(keys[0]).not.toBe(keys[1]);
    expect(posts.every((r) => !r.headers.has('Origin'))).toBe(true);
  });

  it('selects the invite link and announces manual copy when clipboard fails', async () => {
    const token = 'b'.repeat(43); installFakeServer({ ...defaults(), [`POST ${base}/invitations`]: json(201, { token }) });
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn().mockRejectedValue(new Error()) } });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' })); await userEvent.click(await screen.findByRole('button', { name: 'Create invite link' }));
    await userEvent.click(screen.getByRole('button', { name: 'Copy' }));
    expect(await screen.findByText('The link could not be copied. It is selected so you can copy it manually.')).toBeVisible();
    await waitFor(() => expect(screen.getByLabelText('Invitation link')).toHaveFocus());
    const input = screen.getByLabelText('Invitation link') as HTMLInputElement;
    expect(input).toHaveValue(`${location.origin}/invite#${token}`);
    expect(input.selectionStart).toBe(0);
    expect(input.selectionEnd).toBe(input.value.length);
  });

  it('200 retry response explains that the link was not shown and refreshes invitations', async () => {
    let posts = 0; let lists = 0; const fake = installFakeServer({ ...defaults(), [`GET ${base}/invitations`]: () => (++lists === 1 ? page([]) : page([invitation('one')])), [`POST ${base}/invitations`]: () => (++posts === 1 ? json(201, { id: 'first', token: 'a'.repeat(43) }) : json(200, invitation('one'))) });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' })); await userEvent.click(await screen.findByRole('button', { name: 'Create invite link' }));
    await screen.findByLabelText('Invitation link');
    await userEvent.click(await screen.findByRole('button', { name: 'Create invite link' }));
    expect(await screen.findByText('That invitation was created, but its link can’t be shown again. Revoke it and create a new one.')).toBeVisible();
    expect(await screen.findByText('Pending')).toBeVisible(); expect(screen.queryByLabelText('Invitation link')).not.toBeInTheDocument();
    await waitFor(() => expect(fake.requests.filter((r) => r.method === 'GET' && r.path === `${base}/invitations`)).toHaveLength(3));
  });

  it('renders statuses, focuses revoke confirmation, clears the matching link, refreshes, and restores focus on cancel', async () => {
    const pending = invitation('one');
    let lists = 0;
    const fake = installFakeServer({ ...defaults(), [`GET ${base}/invitations`]: () => (++lists < 3 ? page([pending, invitation('two', 'accepted'), invitation('three', 'revoked')]) : page([invitation('one', 'revoked'), invitation('two', 'accepted'), invitation('three', 'revoked')])), [`POST ${base}/invitations`]: json(201, { id: 'one', token: 'c'.repeat(43) }), [`DELETE ${base}/invitations/one`]: json(204) });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    expect(await screen.findByText('Pending')).toBeVisible(); expect(screen.getByText('Accepted')).toBeVisible(); expect(screen.getByText('Revoked')).toBeVisible();
    const invite = await screen.findByRole('button', { name: 'Create invite link' });
    await userEvent.click(invite); expect(await screen.findByLabelText('Invitation link')).toHaveValue(`${location.origin}/invite#${'c'.repeat(43)}`);
    const invitations = screen.getByRole('heading', { name: 'Invitations' }).parentElement!;
    const rows = within(invitations).getAllByRole('listitem');
    expect(within(rows[1]!).queryByRole('button', { name: 'Revoke' })).not.toBeInTheDocument(); expect(within(rows[2]!).queryByRole('button', { name: 'Revoke' })).not.toBeInTheDocument();
    const originalRow = rows[0]!;
    const originalRevokeButton = within(originalRow).getByRole('button', { name: 'Revoke' });
    await userEvent.click(originalRevokeButton); expect(within(rows[0]!).getByRole('button', { name: 'Yes' })).toHaveFocus();
    await userEvent.click(within(originalRow).getByRole('button', { name: 'Cancel' })); await waitFor(() => expect(within(originalRow).getByRole('button', { name: 'Revoke' })).toHaveFocus());
    const currentRow = screen.getByRole('heading', { name: 'Invitations' }).parentElement!.querySelector('li')!;
    await userEvent.click(within(currentRow).getByRole('button', { name: 'Revoke' }));
    await waitFor(() => expect(within(currentRow).getByRole('button', { name: 'Yes' })).toHaveFocus());
    await userEvent.click(within(currentRow).getByRole('button', { name: 'Yes' }));
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('The invitation was revoked.'));
    expect(screen.getByRole('status')).toHaveFocus();
    await waitFor(() => expect(fake.requests.filter((r) => r.method === 'GET' && r.path === `${base}/invitations`)).toHaveLength(3));
    expect(screen.queryByLabelText('Invitation link')).not.toBeInTheDocument();
    const updatedRow = screen.getByRole('heading', { name: 'Invitations' }).parentElement!.querySelector('li')!;
    expect(updatedRow).toHaveTextContent('Revoked'); expect(within(updatedRow).queryByRole('button', { name: 'Revoke' })).not.toBeInTheDocument();
    expect(fake.requests.find((r) => r.method === 'DELETE')!.path).toBe(`${base}/invitations/one`);
  });

  it('Origin rejection keeps owner controls and explains the refusal', async () => {
    installFakeServer({ ...defaults(), [`POST ${base}/invitations`]: problem(403) });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Create invite link' }));
    expect(await screen.findByText('Tendo refused this request. If this keeps happening, ask whoever runs Tendo to check its address settings.')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Create invite link' })).toBeVisible();
  });

  it('only owner_required 403 hides owner controls', async () => {
    installFakeServer({ ...defaults(), [`POST ${base}/invitations`]: problem(403, { code: 'owner_required' }) });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' })); await userEvent.click(await screen.findByRole('button', { name: 'Create invite link' }));
    expect(await screen.findByText('Only household owners can manage invitations.')).toBeVisible(); expect(screen.queryByRole('button', { name: 'Create invite link' })).not.toBeInTheDocument();
  });

  it('member never requests invitations or sees invite controls', async () => {
    const routes = defaults(); routes[`GET ${base}/members`] = page(members('member')); const fake = installFakeServer(routes);
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    expect(await screen.findByText('Only household owners can invite someone.')).toBeVisible(); expect(screen.queryByRole('button', { name: 'Create invite link' })).not.toBeInTheDocument(); expect(fake.requests.some((r) => r.path === `${base}/invitations`)).toBe(false);
  });

  it('owner is discovered on a later members page', async () => {
    const ownerPage = json(200, { items: [{ userId: 'other', login: 'petr', role: 'member' }], nextCursor: 'next' });
    const routes = { ...defaults(), [`GET ${base}/members`]: (req: { query: URLSearchParams }) => req.query.get('cursor') === 'next' ? page([{ userId: session().userId, login: 'anna', role: 'owner' }]) : ownerPage };
    const fake = installFakeServer(routes);
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    expect(await screen.findByRole('button', { name: 'Create invite link' })).toBeVisible();
    expect(fake.requests.filter((r) => r.path === `${base}/members`).map((r) => r.query.get('cursor'))).toEqual([null, 'next']);
    expect(fake.requests.filter((r) => r.path === `${base}/invitations`)).toHaveLength(1);
  });

  it('a list 404 shows the household-specific not-found message', async () => {
    installFakeServer({ ...defaults(), [`GET ${base}/members`]: problem(404) });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    expect(await screen.findByText('This household isn’t available to your account.')).toBeVisible();
    expect(screen.queryByText('Members could not be loaded. Check your connection and try again.')).not.toBeInTheDocument();
  });

  it('invitation list error is retryable, not empty', async () => {
    let count = 0; installFakeServer({ ...defaults(), [`GET ${base}/invitations`]: () => (++count === 1 ? problem(503) : page([])) });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    expect(await screen.findByText('Invitations couldn’t be loaded. Try again.')).toBeVisible(); expect(screen.queryByText('No invitations yet.')).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Try again' })); expect(await screen.findByText('No invitations yet.')).toBeVisible();
  });

  it('replays unresolved creation with the same key, and a deliberate create gets a new key', async () => {
    let posts = 0;
    const fake = installFakeServer({ ...defaults(), [`POST ${base}/invitations`]: () => { posts += 1; return posts === 1 ? Promise.reject(new Error('network')) : posts === 2 ? json(201, { id: 'recovered', token: 'd'.repeat(43) }) : json(201, { id: 'fresh', token: 'e'.repeat(43) }); } });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Create invite link' }));
    expect(await screen.findByRole('button', { name: 'Check again' })).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Check again' }));
    expect(await screen.findByLabelText('Invitation link')).toHaveValue(`${location.origin}/invite#${'d'.repeat(43)}`);
    await userEvent.click(screen.getByRole('button', { name: 'Create invite link' }));
    const postsSent = fake.requests.filter((r) => r.method === 'POST' && r.path === `${base}/invitations`);
    expect(postsSent).toHaveLength(3);
    expect(postsSent[0]!.headers.get('Idempotency-Key')).toBe(postsSent[1]!.headers.get('Idempotency-Key'));
    expect(postsSent[2]!.headers.get('Idempotency-Key')).not.toBe(postsSent[1]!.headers.get('Idempotency-Key'));
  });

  it('replay 200 identifies the invitation and revokes that exact id', async () => {
    let posts = 0;
    const fake = installFakeServer({ ...defaults(), [`POST ${base}/invitations`]: () => (++posts === 1 ? Promise.reject(new Error('network')) : json(200, invitation('known'))), [`DELETE ${base}/invitations/known`]: json(204) });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' })); await userEvent.click(await screen.findByRole('button', { name: 'Create invite link' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Check again' }));
    expect(await screen.findByText('That invitation was created, but its link can’t be shown again. Revoke it and create a new one.')).toBeVisible();
    expect(screen.queryByLabelText('Invitation link')).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Revoke that invitation' }));
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('The invitation was revoked.'));
    expect(fake.requests.filter((r) => r.method === 'DELETE').map((r) => r.path)).toEqual([`${base}/invitations/known`]);
    expect(screen.queryByRole('button', { name: 'Revoke that invitation' })).not.toBeInTheDocument();
    const postKeys = fake.requests.filter((r) => r.method === 'POST').map((r) => r.headers.get('Idempotency-Key'));
    expect(postKeys).toHaveLength(2); expect(postKeys[0]).toBe(postKeys[1]);
  });

  it('owner controls stay disabled until the first invitations load resolves, then show that list', async () => {
    let release!: (response: Response) => void;
    const initial = new Promise<Response>((resolve) => { release = resolve; });
    installFakeServer({ ...defaults(), [`GET ${base}/invitations`]: () => initial });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    expect(await screen.findByRole('button', { name: 'Create invite link' })).toBeDisabled();
    release(page([invitation('first')]));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Create invite link' })).toBeEnabled());
    expect(screen.getByText('Pending')).toBeVisible();
  });

  it('a refresh after revoke wins over an older Show more response', async () => {
    let releaseMore!: (response: Response) => void;
    const more = new Promise<Response>((resolve) => { releaseMore = resolve; });
    let firstPages = 0;
    installFakeServer({
      ...defaults(),
      [`GET ${base}/invitations`]: (req: { query: URLSearchParams }) => req.query.get('cursor') === 'p2' ? more : ++firstPages === 1 ? json(200, { items: [invitation('one')], nextCursor: 'p2' }) : page([invitation('one', 'revoked')]),
      [`DELETE ${base}/invitations/one`]: json(204),
    });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Show more' }));
    await userEvent.click(screen.getByRole('button', { name: 'Revoke' }));
    await userEvent.click(screen.getByRole('button', { name: 'Yes' }));
    await waitFor(() => expect(screen.getByText('Revoked')).toBeVisible());
    await act(async () => {
      releaseMore(json(200, { items: [invitation('stale')], nextCursor: null }));
      await more;
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    const rows = within(screen.getByRole('heading', { name: 'Invitations' }).parentElement!).getAllByRole('listitem');
    expect(rows.map((row) => row.textContent?.startsWith('Revoked'))).toEqual([true]);
  });

  it('revoking an unrelated invitation keeps the unresolved create key for Check again', async () => {
    let posts = 0;
    const fake = installFakeServer({
      ...defaults(),
      [`GET ${base}/invitations`]: page([invitation('older')]),
      [`POST ${base}/invitations`]: () => (++posts === 1 ? Promise.reject(new Error('network')) : json(201, { id: 'recovered', token: 'g'.repeat(43) })),
      [`DELETE ${base}/invitations/older`]: json(204),
    });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Create invite link' }));
    await screen.findByRole('button', { name: 'Check again' });
    await userEvent.click(screen.getByRole('button', { name: 'Revoke' }));
    await userEvent.click(screen.getByRole('button', { name: 'Yes' }));
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('The invitation was revoked.'));
    await userEvent.click(screen.getByRole('button', { name: 'Check again' }));
    expect(await screen.findByLabelText('Invitation link')).toHaveValue(`${location.origin}/invite#${'g'.repeat(43)}`);
    const keys = fake.requests.filter((r) => r.method === 'POST').map((r) => r.headers.get('Idempotency-Key'));
    expect(keys).toHaveLength(2); expect(keys[1]).toBe(keys[0]);
  });

  it('a household 404 on create is definitive: household message and no Check again', async () => {
    installFakeServer({ ...defaults(), [`POST ${base}/invitations`]: problem(404, { code: 'not_found' }) });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Create invite link' }));
    expect(await screen.findByText('This household isn’t available to your account.')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Check again' })).not.toBeInTheDocument();
  });

  it('401 on members list signs out', async () => {
    const routes: Record<string, Response> = defaults(); routes[`GET ${base}/members`] = problem(401); routes['GET /api/v1/auth/setup'] = json(200, { required: false }); installFakeServer(routes);
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' })); expect(await screen.findByLabelText('Password')).toBeVisible();
  });
});
