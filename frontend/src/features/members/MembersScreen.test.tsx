import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { MembersScreen } from './MembersScreen';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, emptyHome, household, installFakeServer, json, session, type RecordedRequest } from '../../test/fakeServer';

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const page = (items: unknown[]) => json(200, { items, nextCursor: null });
const members = (role = 'admin') => [{ userId: session().userId, login: 'anna', role }, { userId: 'other', login: 'petr', role: 'member' }];
const invitation = (id: string, status = 'pending') => ({ id, status, createdAt: '2026-01-01T00:00:00Z', expiresAt: '2026-01-08T00:00:00Z' });
function renderApp() { return render(<I18nProvider><App /></I18nProvider>); }
function defaults() { return { 'GET /api/v1/session': json(200, session()), [`GET ${base}`]: json(200, household), ...emptyHome, [`GET ${base}/members`]: page(members()), [`GET ${base}/invitations`]: page([]) }; }
const problem = (status: number, extra: Record<string, unknown> = {}) => json(status, { type: 'about:blank', title: 'x', status, ...extra });
// Home reads members in a passive effect. If the Members click lands before that effect has run, React flushes it
// after openMembers' pushState, so Home's read is recorded with documentPath '/members' and miscounted as discovery.
// Wait until Home (path '/') has issued its read before navigating.
async function openMembersAfterHomeRead(fake: { requests: RecordedRequest[] }) {
  await waitFor(() => expect(fake.requests.some((r) => r.method === 'GET' && r.path === `${base}/members` && r.documentPath === '/')).toBe(true));
  await userEvent.click(screen.getByRole('link', { name: 'Household members' }));
}
beforeEach(() => { window.history.replaceState(null, '', '/'); vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']); });
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe('members and invitations', () => {
  it('renders owner/member explanation alongside the member role list', async () => {
    const fake = installFakeServer(defaults());
    renderApp();
    await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    expect(await screen.findByText('Administrators can invite people and manage invitations; members can use the household.')).toBeVisible();
    expect(screen.queryByRole('heading', { name: 'Need to invite someone?' })).not.toBeInTheDocument();
    expect(screen.queryByText('Ask a household administrator to send them an invite link.')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Create invite link' })).toBeVisible();
    const memberList = screen.getByRole('list', { name: 'Members' });
    expect(within(memberList).getAllByRole('listitem')).toHaveLength(2);
    expect(fake.requests.some((request) => request.path === `${base}/members`)).toBe(true);
  });

  it('owner discovery makes exactly 20 member requests then one invitations probe', async () => {
    const discoveryCursors: Array<string | null> = [];
    let routeRequests = 0;
    const fake = installFakeServer({
      ...defaults(),
      [`GET ${base}/members`]: (req: RecordedRequest) => {
        if (req.documentPath !== '/members') return page(members());
        routeRequests += 1;
        const cursor = req.query.get('cursor');
        discoveryCursors.push(cursor);
        const pageNumber = Number(cursor?.replace('p', '') ?? 1);
        return json(200, { items: [{ userId: `other-${pageNumber}`, login: `other_${pageNumber}`, role: 'member' }], nextCursor: pageNumber < 20 ? `p${pageNumber + 1}` : null });
      },
      [`GET ${base}/invitations`]: page([]),
    });
    renderApp();
    await openMembersAfterHomeRead(fake);
    expect(await screen.findByRole('button', { name: 'Create invite link' })).toBeVisible();
    expect(routeRequests).toBe(20);
    expect(discoveryCursors).toEqual([null, ...Array.from({ length: 19 }, (_, i) => `p${i + 2}`)]);
    expect(fake.requests.filter((request) => request.path === `${base}/invitations`)).toHaveLength(1);
  });

  it('unmounting during a deferred members initial load ignores late page results', async () => {
    let release!: (response: Response) => void;
    const pending = new Promise<Response>((resolve) => { release = resolve; });
    const onSignedOut = vi.fn();
    installFakeServer({ ...defaults(), [`GET ${base}/members`]: () => pending });
    const view = render(<I18nProvider><MembersScreen householdId={HOUSEHOLD_ID} userId={session().userId} login={session().login} onSignedOut={onSignedOut} /></I18nProvider>);
    view.unmount();
    await act(async () => { release(problem(401)); await pending; });
    expect(onSignedOut).not.toHaveBeenCalled();
  });

  it('home links to household members and displays member roles and who is you', async () => {
    const fake = installFakeServer(defaults()); renderApp();
    expect(await screen.findByRole('link', { name: 'Household members' })).toHaveAttribute('href', '/members');
    await userEvent.click(screen.getByRole('link', { name: 'Household members' }));
    const memberList = within(screen.getByRole('list', { name: 'Members' }));
    const rows = memberList.getAllByRole('listitem');
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent('anna (you)');
    expect(rows[0]).toHaveTextContent('Administrator');
    expect(rows[1]).toHaveTextContent('petr');
    expect(rows[1]).toHaveTextContent('Member');
    // No avatar tile: the login and its role are the whole row, so nothing decorative precedes the name.
    expect(rows[0].querySelector('[data-member-avatar]')).toBeNull();
    expect(rows[0].firstElementChild).toHaveAttribute('data-member-login');
    expect(fake.requests.some((r) => r.method === 'GET' && r.path === `${base}/members`)).toBe(true);
  });

  /**
   * V5-07 slice: the calm list on Members. The member list is flat and divided, the role is plain muted text,
   * the invite section is a plain section rather than a card inside a card, and invitation status is text.
   */
  it('lists members as a flat divided list with the role as plain text', async () => {
    installFakeServer(defaults());
    renderApp();
    await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    const list = await screen.findByRole('list', { name: 'Members' });
    expect(list).toHaveClass('divide-y', 'divide-line');
    expect(list.className).not.toMatch(/rounded-2xl|bg-raised/);
    const rows = within(list).getAllByRole('listitem');
    const role = within(rows[0]!).getByText('Administrator');
    expect(role).toHaveClass('text-meta', 'text-muted');
    expect(role.classList.contains('rounded-full')).toBe(false);
    expect(role.classList.contains('bg-sand')).toBe(false);
    expect(rows[0]!.querySelector('[data-member-avatar]')).toBeNull();
    expect(rows[0]!.querySelector('[data-member-login]')).toHaveTextContent('anna (you)');
  });

  it('opens the invite area as a plain section instead of a card inside a card', async () => {
    installFakeServer(defaults());
    renderApp();
    await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    const section = await screen.findByRole('region', { name: 'Invite someone' });
    expect(section.className).not.toMatch(/rounded-2xl|bg-raised/);
    expect(section.querySelector('.rounded-2xl')).toBeNull();
    expect(within(section).getByRole('heading', { name: 'Invite someone' })).toBeVisible();
    expect(within(section).getByText('Create a private link to invite someone to your household. The link works once and is shown only now.')).toBeVisible();
    expect(within(section).getByRole('button', { name: 'Create invite link' })).toBeVisible();
  });

  it('shows invitation status as text beside one created and expiry line', async () => {
    installFakeServer({ ...defaults(), [`GET ${base}/invitations`]: page([invitation('one')]) });
    renderApp();
    await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    const list = await screen.findByRole('list', { name: 'Invitations' });
    const row = within(list).getAllByRole('listitem')[0]!;
    const status = within(row).getByText('Pending', { exact: true });
    expect(status).toHaveClass('text-waiting');
    expect(status.classList.contains('rounded-full')).toBe(false);
    expect(status.classList.contains('bg-waiting-bg')).toBe(false);
    expect(row.querySelector('[data-invitation-details]')).toHaveTextContent(/^Created .+ · Expires .+$/);
    // The status and the dates are one line of text; no second paragraph repeats the state.
    expect(row.querySelectorAll('p')).toHaveLength(1);
    const line = row.querySelector('p')!;
    expect(line.firstElementChild).toBe(status);
    expect(line.children[1]).toBe(row.querySelector('[data-invitation-details]'));
    expect(within(row).getByRole('button', { name: 'Revoke' })).toBeVisible();
  });

  it('explains the member-only view without a tinted card', async () => {
    const routes = defaults();
    routes[`GET ${base}/members`] = page(members('member'));
    installFakeServer(routes);
    renderApp();
    await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    const help = await screen.findByRole('region', { name: 'Need to invite someone?' });
    expect(help.className).not.toMatch(/rounded-2xl|bg-sand|border/);
    expect(within(help).getByRole('heading', { name: 'Need to invite someone?' })).toBeVisible();
    expect(within(help).getByText('Ask a household administrator to send them an invite link.')).toBeVisible();
  });

  it('retryable list failure announces the error and keeps Retry adjacent', async () => {
    installFakeServer({ ...defaults(), [`GET ${base}/members`]: problem(503) });
    render(<I18nProvider><MembersScreen householdId={HOUSEHOLD_ID} userId={session().userId} login={session().login} onSignedOut={() => {}} /></I18nProvider>);
    await screen.findByRole('button', { name: 'Try again' });
    const visibleError = await screen.findByRole('alert');
    expect(visibleError).toHaveTextContent('Members could not be loaded. Check your connection and try again.');
    expect(visibleError).toBeVisible();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeVisible();
  });

  it('lets the long Czech invite action wrap on a narrow screen', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer(defaults());
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Členové domácnosti' }));
    const main = screen.getByRole('main');
    const action = await within(main).findByRole('button', { name: 'Vytvořit odkaz s pozvánkou' });
    expect(action).toHaveClass('whitespace-normal', 'w-full');
  });

  it('member paging does not cancel an in-flight invite', async () => {
    let release!: (response: Response) => void;
    const pending = new Promise<Response>((resolve) => { release = resolve; });
    installFakeServer({
      ...defaults(),
      [`GET ${base}/members`]: (req: { query: URLSearchParams }) => req.query.get('cursor') === 'p2'
        ? page([{ userId: 'next-member', login: 'next_member', role: 'member' }])
        : json(200, { items: members(), nextCursor: 'p2' }),
      [`POST ${base}/invitations`]: () => pending,
    });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    const invite = await screen.findByRole('button', { name: 'Create invite link' });
    await userEvent.click(invite);
    const membersSection = screen.getByRole('region', { name: 'Members' });
    await userEvent.click(within(membersSection).getByRole('button', { name: 'Next page' }));
    expect(await within(membersSection).findByText('next_member')).toBeVisible();
    await act(async () => { release(json(201, { id: 'created', token: 'x'.repeat(43) })); await pending; });
    expect(await screen.findByLabelText('Invitation link')).toHaveValue(`${location.origin}/invite#${'x'.repeat(43)}`);
    const pageStatuses = screen.getAllByRole('status').filter((element) => !screen.getByRole('region', { name: 'Invite someone' }).contains(element));
    expect(pageStatuses).toHaveLength(1);
    expect(pageStatuses[0]).toHaveTextContent('Your invite link is ready.');
    expect(screen.getByRole('button', { name: 'Create invite link' })).toBeEnabled();
  });

  it('creates links with unique idempotency keys, empty body, and copies the exact URL', async () => {
    const token = 'a'.repeat(43); const fake = installFakeServer({ ...defaults(), [`POST ${base}/invitations`]: json(201, { token }) });
    const writeText = vi.fn().mockResolvedValue(undefined); Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    const create = await screen.findByRole('button', { name: 'Create invite link' });
    await userEvent.click(create); const link = `${location.origin}/invite#${token}`;
    expect(await screen.findByLabelText('Invitation link')).toHaveValue(link);
    // 16 px like every field, so iOS Safari does not zoom the page when the selected link takes focus.
    expect(screen.getByLabelText('Invitation link')).toHaveClass('text-control');
    expect(screen.getByLabelText('Invitation link').className).not.toMatch(/\btext-(small|label|meta|micro|sm|xs)\b/);
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
    const invitePanel = screen.getByRole('region', { name: 'Invite someone' });
    const copyNotice = await within(invitePanel).findByText('The link could not be copied. It is selected so you can copy it manually.');
    expect(copyNotice).toBeVisible();
    expect(within(invitePanel).getByRole('status')).toContainElement(copyNotice);
    expect(within(invitePanel).getByLabelText('Invitation link').closest('div')?.parentElement).toContainElement(copyNotice);
    expect(screen.getByLabelText('Invitation link')).toHaveFocus();
    await waitFor(() => expect(screen.getByLabelText('Invitation link')).toHaveFocus());
    const input = screen.getByLabelText('Invitation link') as HTMLInputElement;
    expect(input).toHaveValue(`${location.origin}/invite#${token}`);
    expect(input.selectionStart).toBe(0);
    expect(input.selectionEnd).toBe(input.value.length);
  });

  it('200 retry response explains that the link was not shown and refreshes invitations', async () => {
    let posts = 0; let lists = 0; const fake = installFakeServer({ ...defaults(), [`GET ${base}/invitations`]: () => (++lists <= 2 ? page([]) : page([invitation('one')])), [`POST ${base}/invitations`]: () => (++posts === 1 ? json(201, { id: 'first', token: 'a'.repeat(43) }) : json(200, invitation('one'))) });
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
    const invitationsRegion = screen.getByRole('region', { name: 'Invite someone' });
    const invitations = within(invitationsRegion).getByRole('list', { name: 'Invitations' });
    const rows = within(invitations).getAllByRole('listitem');
    expect(within(rows[1]!).queryByRole('button', { name: 'Revoke' })).not.toBeInTheDocument(); expect(within(rows[2]!).queryByRole('button', { name: 'Revoke' })).not.toBeInTheDocument();
    const originalRow = rows[0]!;
    const originalRevokeButton = within(originalRow).getByRole('button', { name: 'Revoke' });
    await userEvent.click(originalRevokeButton); expect(within(rows[0]!).getByRole('button', { name: 'Yes' })).toHaveFocus();
    expect(within(rows[0]!).getByRole('button', { name: 'Yes' })).toHaveAccessibleDescription(`Revoke? ${within(rows[0]!).getByText(/^Created /).textContent}`);
    await userEvent.click(within(originalRow).getByRole('button', { name: 'Cancel' })); await waitFor(() => expect(within(originalRow).getByRole('button', { name: 'Revoke' })).toHaveFocus());
    const currentRow = invitations.querySelector('li')!;
    await userEvent.click(within(currentRow).getByRole('button', { name: 'Revoke' }));
    await waitFor(() => expect(within(currentRow).getByRole('button', { name: 'Yes' })).toHaveFocus());
    await userEvent.click(within(currentRow).getByRole('button', { name: 'Yes' }));
    await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('The invitation was revoked.'));
    expect(screen.getByRole('status')).toHaveFocus();
    await waitFor(() => expect(fake.requests.filter((r) => r.method === 'GET' && r.path === `${base}/invitations`)).toHaveLength(3));
    expect(screen.queryByLabelText('Invitation link')).not.toBeInTheDocument();
    const updatedRow = invitations.querySelector('li')!;
    expect(updatedRow).toHaveTextContent('Revoked'); expect(within(updatedRow).queryByRole('button', { name: 'Revoke' })).not.toBeInTheDocument();
    expect(fake.requests.find((r) => r.method === 'DELETE')!.path).toBe(`${base}/invitations/one`);
  });

  it('owner-required after a permission change calmly removes controls and explains why', async () => {
    const fake = installFakeServer({
      ...defaults(),
      [`GET ${base}/invitations`]: page([]),
      [`POST ${base}/invitations`]: problem(403, { code: 'owner_required' }),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Create invite link' }));
    expect(await screen.findByText('Only household administrators can manage invitations.')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Create invite link' })).not.toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Need to invite someone?' })).toBeVisible();
    expect(screen.getByText('Ask a household administrator to send them an invite link.')).toBeVisible();
    expect(fake.requests.some((request) => request.path === `${base}/invitations` && request.method === 'POST')).toBe(true);
  });

  it('Origin rejection keeps owner controls and explains the refusal', async () => {
    installFakeServer({ ...defaults(), [`POST ${base}/invitations`]: problem(403) });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Create invite link' }));
    const notice = await screen.findByText('Tendo refused this request. If this keeps happening, ask whoever runs Tendo to check its address settings.');
    expect(notice).toBeVisible();
    expect(notice.closest('[role="status"]')).not.toBeNull();
    expect(screen.getByRole('button', { name: 'Create invite link' })).toBeVisible();
  });

  it.each(['owner_required', 'admin_required'])('a %s 403 hides invite controls', async (code) => {
    installFakeServer({ ...defaults(), [`POST ${base}/invitations`]: problem(403, { code }) });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' })); await userEvent.click(await screen.findByRole('button', { name: 'Create invite link' }));
    expect(await screen.findByText('Only household administrators can manage invitations.')).toBeVisible(); expect(screen.queryByRole('button', { name: 'Create invite link' })).not.toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Need to invite someone?' })).toBeVisible();
    expect(screen.getByText('Ask a household administrator to send them an invite link.')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Create invite link' })).not.toBeInTheDocument();
  });

  it('member never requests invitations or sees invite controls', async () => {
    const routes = defaults(); routes[`GET ${base}/members`] = page(members('member')); const fake = installFakeServer(routes);
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    expect(await screen.findByText('Ask a household administrator to send them an invite link.')).toBeVisible();
    expect(screen.getByRole('heading', { name: 'Need to invite someone?' })).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Create invite link' })).not.toBeInTheDocument(); expect(fake.requests.some((r) => r.path === `${base}/invitations`)).toBe(false);
  });

  it('finds the viewer on page three but renders only first-page rows', async () => {
    const requests: Array<string | null> = [];
    const fake = installFakeServer({ ...defaults(), [`GET ${base}/members`]: (req: RecordedRequest) => {
      if (req.documentPath !== '/members') return page(members());
      const cursor = req.query.get('cursor'); requests.push(cursor);
      if (cursor === null) return json(200, { items: [{ userId: 'first', login: 'first_page', role: 'member' }], nextCursor: 'p2' });
      if (cursor === 'p2') return json(200, { items: [{ userId: 'second', login: 'second_page', role: 'member' }], nextCursor: 'p3' });
      return json(200, { items: [{ userId: session().userId, login: 'anna', role: 'admin' }], nextCursor: 'p4' });
    } });
    renderApp(); await openMembersAfterHomeRead(fake);
    expect(await screen.findByRole('button', { name: 'Create invite link' })).toBeVisible();
    const memberList = screen.getByRole('list', { name: 'Members' });
    expect(within(memberList).getByText('first_page')).toBeVisible();
    expect(within(memberList).queryByText('second_page')).not.toBeInTheDocument();
    expect(within(memberList).queryByText('anna')).not.toBeInTheDocument();
    expect(requests.filter((cursor) => cursor === null || cursor === 'p2' || cursor === 'p3').slice(0, 3)).toEqual([null, 'p2', 'p3']);
    expect(fake.requests.filter((r) => r.path === `${base}/invitations`)).toHaveLength(1);
  });

  it('caps viewer discovery then probes owner access through invitations', async () => {
    const routeCursors: Array<string | null> = [];
    const fake = installFakeServer({
      ...defaults(),
      [`GET ${base}/members`]: (req: RecordedRequest) => {
        if (req.documentPath !== '/members') return page(members());
        routeCursors.push(req.query.get('cursor'));
        const pageNumber = Number(req.query.get('cursor')?.replace('p', '') ?? 1);
        return json(200, { items: [{ userId: `user-${pageNumber}`, login: `page_${pageNumber}`, role: 'member' }], nextCursor: `p${pageNumber + 1}` });
      },
      [`GET ${base}/invitations`]: page([]),
    });
    renderApp();
    await openMembersAfterHomeRead(fake);
    expect(await screen.findByRole('button', { name: 'Create invite link' })).toBeVisible();
    expect(screen.queryByRole('heading', { name: 'Need to invite someone?' })).not.toBeInTheDocument();
    expect(screen.queryByText('Ask a household administrator to send them an invite link.')).not.toBeInTheDocument();
    expect(routeCursors).toHaveLength(20);
    expect(routeCursors.filter((cursor) => cursor !== null)).toHaveLength(19);
    expect(screen.getByRole('heading', { name: 'Members' })).toBeVisible();
    expect(screen.getByRole('heading', { name: 'Invitations' })).toBeVisible();
    expect(screen.queryByRole('heading', { name: 'Need to invite someone?' })).not.toBeInTheDocument();
    expect(screen.queryByText('Ask a household administrator to send them an invite link.')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Create invite link' })).toBeVisible();
    expect(fake.requests.filter((r) => r.path === `${base}/invitations`)).toHaveLength(1);
  });

  it('an undiscovered owner recovers invitation controls after a failed probe and explicit retry', async () => {
    let invitationCalls = 0;
    const fake = installFakeServer({
      ...defaults(),
      [`GET ${base}/members`]: (req: RecordedRequest) => {
        if (req.documentPath !== '/members') return page(members());
        const pageNumber = Number(req.query.get('cursor')?.replace('p', '') ?? 1);
        return json(200, { items: [{ userId: `user-${pageNumber}`, login: `page_${pageNumber}`, role: 'member' }], nextCursor: `p${pageNumber + 1}` });
      },
      [`GET ${base}/invitations`]: () => (++invitationCalls === 1 ? problem(503) : page([invitation('one')])),
    });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    expect(await screen.findByText('Invitations couldn’t be loaded. Try again.')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Create invite link' })).not.toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Members' })).toBeVisible();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeVisible();
    expect(screen.queryByRole('heading', { name: 'Need to invite someone?' })).not.toBeInTheDocument();
    expect(screen.queryByText('Ask a household administrator to send them an invite link.')).not.toBeInTheDocument();
    expect(fake.requests.filter((r) => r.path === `${base}/invitations`)).toHaveLength(1);
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByRole('button', { name: 'Create invite link' })).toBeEnabled();
    expect(screen.getByText('Pending')).toBeVisible();
    expect(fake.requests.filter((r) => r.path === `${base}/invitations`)).toHaveLength(2);
  });

  it('an undiscovered role shows loading instead of retry while the retry probe is pending', async () => {
    let invitationCalls = 0;
    let release!: (response: Response) => void;
    const pending = new Promise<Response>((resolve) => { release = resolve; });
    const fake = installFakeServer({
      ...defaults(),
      [`GET ${base}/members`]: (req: RecordedRequest) => {
        if (req.documentPath !== '/members') return page(members());
        const pageNumber = Number(req.query.get('cursor')?.replace('p', '') ?? 1);
        return json(200, { items: [{ userId: `user-${pageNumber}`, login: `page_${pageNumber}`, role: 'member' }], nextCursor: `p${pageNumber + 1}` });
      },
      [`GET ${base}/invitations`]: () => (++invitationCalls === 1 ? problem(503) : pending),
    });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    expect(await screen.findByText('Invitations couldn’t be loaded. Try again.')).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    await waitFor(() => expect(fake.requests.filter((r) => r.path === `${base}/invitations`)).toHaveLength(2));
    expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument();
    expect(screen.queryByText('Invitations couldn’t be loaded. Try again.')).not.toBeInTheDocument();
    expect(screen.getByText('Loading…')).toBeVisible();
    await act(async () => { release(page([invitation('one')])); await pending; });
    expect(await screen.findByRole('button', { name: 'Create invite link' })).toBeEnabled();
    expect(fake.requests.filter((r) => r.path === `${base}/invitations`)).toHaveLength(2);
  });

  it('an undiscovered role with a persistently failing probe keeps a visible retry', async () => {
    const fake = installFakeServer({
      ...defaults(),
      [`GET ${base}/members`]: (req: RecordedRequest) => {
        if (req.documentPath !== '/members') return page(members());
        const pageNumber = Number(req.query.get('cursor')?.replace('p', '') ?? 1);
        return json(200, { items: [{ userId: `user-${pageNumber}`, login: `page_${pageNumber}`, role: 'member' }], nextCursor: `p${pageNumber + 1}` });
      },
      [`GET ${base}/invitations`]: problem(503),
    });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    expect(await screen.findByText('Invitations couldn’t be loaded. Try again.')).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    await waitFor(() => expect(fake.requests.filter((r) => r.path === `${base}/invitations`)).toHaveLength(2));
    expect(await screen.findByText('Invitations couldn’t be loaded. Try again.')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeEnabled();
    expect(screen.queryByRole('button', { name: 'Create invite link' })).not.toBeInTheDocument();
  });

  it('caps viewer discovery and uses owner_required to confirm non-owner status', async () => {
    const discoveryCursors: Array<string | null> = [];
    let initialMemberCalls = 0;
    const fake = installFakeServer({
      ...defaults(),
      [`GET ${base}/members`]: (req: RecordedRequest) => {
        if (req.documentPath !== '/members') return page(members());
        initialMemberCalls += 1;
        const cursor = req.query.get('cursor');
        discoveryCursors.push(cursor);
        const pageNumber = Number(cursor?.replace('p', '') ?? 1);
        return json(200, { items: [{ userId: `user-${pageNumber}`, login: `page_${pageNumber}`, role: 'member' }], nextCursor: `p${pageNumber + 1}` });
      },
      [`GET ${base}/invitations`]: problem(403, { code: 'owner_required' }),
    });
    renderApp();
    await screen.findByRole('link', { name: 'Household members' });
    await userEvent.click(screen.getByRole('link', { name: 'Home' }));
    await screen.findByRole('heading', { level: 1, name: 'Overview' });
    await openMembersAfterHomeRead(fake);
    expect(await screen.findByRole('heading', { name: 'Need to invite someone?' })).toBeVisible();
    expect(await screen.findByText('Ask a household administrator to send them an invite link.')).toBeVisible();
    expect(initialMemberCalls).toBe(20);
    expect(within(screen.getByRole('list', { name: 'Members' })).getAllByRole('listitem')).toHaveLength(1);
    expect(screen.getByText('page_1')).toBeVisible();
    expect(screen.queryByText('page_20')).not.toBeInTheDocument();
    expect(fake.requests.filter((r) => r.path === `${base}/invitations`)).toHaveLength(1);
  });

  it('discovers the owner on a later page without rendering discovery rows', async () => {
    const ownerPage = () => json(200, { items: [{ userId: 'other', login: 'petr', role: 'member' }], nextCursor: 'next' });
    const routes = { ...defaults(), [`GET ${base}/members`]: (req: { query: URLSearchParams }) => req.query.get('cursor') === 'next' ? json(200, { items: [{ userId: session().userId, login: 'anna', role: 'admin' }], nextCursor: null }) : ownerPage() };
    const fake = installFakeServer(routes);
    renderApp();
    await screen.findByRole('link', { name: 'Household members' });
    await waitFor(() => expect(fake.requests.filter((r) => r.method === 'GET' && r.path === `${base}/members`)).toHaveLength(2));
    const membersRequestsBeforeNavigation = fake.requests.filter((r) => r.path === `${base}/members`);
    const beforeMembersPage = membersRequestsBeforeNavigation.length;
    await userEvent.click(screen.getByRole('link', { name: 'Household members' }));
    expect(await screen.findByRole('button', { name: 'Create invite link' })).toBeVisible();
    const memberList = screen.getByRole('list', { name: 'Members' });
    expect(within(memberList).getByText('petr')).toBeVisible();
    expect(within(memberList).queryByText('anna')).not.toBeInTheDocument();
    expect(within(memberList).getAllByRole('listitem')).toHaveLength(1);
    const membersPageRequests = fake.requests.filter((r) => r.method === 'GET' && r.path === `${base}/members`);
    expect(membersPageRequests.slice(beforeMembersPage).map((r) => r.query.get('cursor'))).toEqual([null, 'next']);
    expect(fake.requests.filter((r) => r.path === `${base}/invitations`)).toHaveLength(1);
  });

  it.each(['owner_required', 'forbidden'])('a members-list 403 (%s) shows the refused-request notice and can retry', async (code) => {
    let failing = true;
    const fake = installFakeServer({ ...defaults(), [`GET ${base}/members`]: () => failing ? problem(403, { code }) : page(members()) });
    renderApp();
    await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Tendo refused this request. If this keeps happening, ask whoever runs Tendo to check its address settings.');
    expect(alert).toBeVisible();
    failing = false;
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByRole('heading', { name: 'Members' })).toBeVisible();
    expect(within(screen.getByRole('list', { name: 'Members' })).getAllByRole('listitem')).toHaveLength(2);
    expect(fake.requests.filter((r) => r.method === 'GET' && r.path === `${base}/members`).length).toBeGreaterThanOrEqual(2);
  });

  it('a list 404 shows the household-specific not-found message', async () => {
    installFakeServer({ ...defaults(), [`GET ${base}/members`]: problem(404) });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('This household isn’t available to your account.');
    expect(alert).toBeVisible();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeVisible();
    expect(screen.queryByText('Members could not be loaded. Check your connection and try again.')).not.toBeInTheDocument();
  });

  it('invitation list error is retryable, not empty', async () => {
    let count = 0; installFakeServer({ ...defaults(), [`GET ${base}/invitations`]: () => (++count === 1 ? problem(503) : page([])) });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    expect(await screen.findByText('Invitations couldn’t be loaded. Try again.')).toBeVisible(); expect(screen.queryByText('No invitations to show.')).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Try again' })); expect(await screen.findByText('No invitations to show.')).toBeVisible(); expect(screen.getByText('Expired invite links aren’t listed. Create a new link if someone still needs one.')).toBeVisible();
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
    await waitFor(() => expect(screen.getByRole('button', { name: 'Create invite link' })).toBeDisabled());
    release(page([invitation('first')]));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Create invite link' })).toBeEnabled());
    expect(screen.getByText('Pending')).toBeVisible();
  });

  it('a failed invitation Next keeps the stack unchanged for a successful retry', async () => {
    let calls = 0;
    const fake = installFakeServer({
      ...defaults(),
      [`GET ${base}/invitations`]: (req: { query: URLSearchParams }) => {
        if (req.query.get('cursor') !== 'p2') return json(200, { items: [invitation('first')], nextCursor: 'p2' });
        if (++calls === 1) return problem(503);
        return page([invitation('second', 'accepted')]);
      },
    });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    const invitationsRegion = screen.getByRole('region', { name: 'Invite someone' });
    const invitations = () => within(invitationsRegion).getByRole('list', { name: 'Invitations' });
    const pager = within(invitationsRegion).getByRole('navigation', { name: 'Invitations' });
    const next = within(pager).getByRole('button', { name: 'Next page' });
    await userEvent.click(next);
    expect(await screen.findByText('Invitations couldn’t be loaded. Try again.')).toBeVisible();
    expect(within(invitationsRegion).getByText('Page 1')).toBeVisible();
    expect(within(invitationsRegion).queryByRole('button', { name: 'First page' })).not.toBeInTheDocument();
    expect(within(pager).getByRole('button', { name: 'Next page' })).toBeEnabled();
    await userEvent.click(within(pager).getByRole('button', { name: 'Next page' }));
    await waitFor(() => expect(within(pager).getByText('Page 2')).toBeVisible());
    await waitFor(() => expect(fake.requests.filter((r) => r.path === `${base}/invitations` && r.query.get('cursor') === 'p2')).toHaveLength(2));
    const acceptedRow = await within(invitations()).findByRole('listitem');
    expect(acceptedRow).toBeVisible();
    expect(acceptedRow).toHaveTextContent('Accepted');
    expect(within(pager).getByText('Page 2')).toBeVisible();
    expect(fake.requests.filter((r) => r.path === `${base}/invitations` && r.query.get('cursor') === 'p2')).toHaveLength(2);
    expect(within(invitationsRegion).getByText('Page 2')).toBeVisible();
    await userEvent.click(within(pager).getByRole('button', { name: 'First page' }));
    expect(await within(invitations()).findByText('Pending')).toBeVisible();
    expect(within(pager).getByText('Page 1')).toBeVisible();
    expect(fake.requests.filter((r) => r.path === `${base}/invitations` && r.query.get('cursor') === 'p2')).toHaveLength(2);
  });

  it('member and invitation paging keeps focus ownership separate for simultaneous requests', async () => {
    let releaseMembers!: (response: Response) => void;
    let releaseInvitations!: (response: Response) => void;
    const membersPending = new Promise<Response>((resolve) => { releaseMembers = resolve; });
    const invitationsPending = new Promise<Response>((resolve) => { releaseInvitations = resolve; });
    let memberPageRequests = 0;
    installFakeServer({
      ...defaults(),
      [`GET ${base}/members`]: (req: RecordedRequest) => req.query.get('cursor') === 'm2'
        ? (++memberPageRequests === 1 ? membersPending : json(200, { items: [{ userId: 'member2', login: 'member_two', role: 'member' }], nextCursor: null }))
        : json(200, { items: members(), nextCursor: req.documentPath === '/members' ? 'm2' : null }),
      [`GET ${base}/invitations`]: (req: { query: URLSearchParams }) => req.query.get('cursor') === 'i2'
        ? invitationsPending : json(200, { items: [invitation('one')], nextCursor: 'i2' }),
    });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Create invite link' })).toBeEnabled());
    const membersSection = screen.getByRole('region', { name: 'Members' });
    const invitations = screen.getByRole('list', { name: 'Invitations' });
    const memberNext = within(membersSection).getByRole('button', { name: 'Next page' });
    const invitationNext = within(screen.getByRole('region', { name: 'Invite someone' })).getByRole('button', { name: 'Next page' });
    // Both requests are in flight at once; the user last activated invitation paging.
    await userEvent.click(memberNext);
    await userEvent.click(invitationNext);
    await act(async () => { releaseMembers(json(200, { items: [{ userId: 'member2', login: 'member_two', role: 'member' }], nextCursor: null })); await membersPending; });
    expect(await within(membersSection).findByText('member_two')).toBeVisible();
    // The member result must not pull focus away from the invitation control the user moved to.
    expect(invitationNext).toHaveFocus();
    expect(screen.getByRole('heading', { name: 'Members' })).not.toHaveFocus();
    await act(async () => { releaseInvitations(json(200, { items: [invitation('two', 'accepted')], nextCursor: null })); await invitationsPending; });
    expect(await within(invitations).findByText('Accepted')).toBeVisible();
    await waitFor(() => expect(screen.getByRole('heading', { name: 'Invitations' })).toHaveFocus());
  });

  it('member and invitation page controls replace their own rows independently', async () => {
    const firstInvitation = invitation('one');
    const secondInvitation = invitation('two', 'accepted');
    const fake = installFakeServer({
      ...defaults(),
      [`GET ${base}/members`]: (req: { query: URLSearchParams }) => req.query.get('cursor') === 'm2'
        ? page([{ userId: 'next-member', login: 'next_member', role: 'member' }])
        : json(200, { items: members(), nextCursor: 'm2' }),
      [`GET ${base}/invitations`]: (req: { query: URLSearchParams }) => req.query.get('cursor') === 'i2'
        ? page([secondInvitation]) : json(200, { items: [firstInvitation], nextCursor: 'i2' }),
    });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    const membersSection = screen.getByRole('region', { name: 'Members' });
    await userEvent.click(within(membersSection).getByRole('button', { name: 'Next page' }));
    expect(await within(membersSection).findByText('next_member')).toBeVisible();
    expect(within(membersSection).queryByText('anna')).not.toBeInTheDocument();
    expect(within(membersSection).queryByText('petr')).not.toBeInTheDocument();
    expect(within(membersSection).getAllByRole('listitem')).toHaveLength(1);
    const invitationsRegion = screen.getByRole('region', { name: 'Invite someone' });
    const invitations = within(invitationsRegion).getByRole('list', { name: 'Invitations' });
    await userEvent.click(within(screen.getByRole('navigation', { name: 'Invitations' })).getByRole('button', { name: 'Next page' }));
    expect(await within(invitations).findByText('Accepted')).toBeVisible();
    expect(within(invitations).getAllByRole('listitem')).toHaveLength(1);
    expect(within(invitations).queryByText('Pending')).not.toBeInTheDocument();
    expect(fake.requests.filter((r) => r.path === `${base}/members`).map((r) => r.query.get('cursor'))).toContain('m2');
    expect(fake.requests.filter((r) => r.path === `${base}/invitations`).map((r) => r.query.get('cursor'))).toContain('i2');
    // Expired invitations are filtered by the server before paging, so every page request must ask for it.
    const invitationGets = fake.requests.filter((r) => r.method === 'GET' && r.path === `${base}/invitations`);
    expect(invitationGets.length).toBeGreaterThanOrEqual(2);
    expect(invitationGets.every((r) => r.query.getAll('includeExpired').join() === 'false')).toBe(true);
  });

  it('never renders an expired invitation even if a page contains one', async () => {
    installFakeServer({ ...defaults(), [`GET ${base}/invitations`]: page([invitation('one'), invitation('old', 'expired'), invitation('two', 'revoked')]) });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    const invitations = within(screen.getByRole('region', { name: 'Invite someone' })).getByRole('list', { name: 'Invitations' });
    expect(await within(invitations).findByText('Revoked')).toBeVisible();
    expect(within(invitations).getAllByRole('listitem')).toHaveLength(2);
    expect(within(invitations).queryByText('Expired')).not.toBeInTheDocument();
  });

  it('member and invitation paging supports three pages and return to first', async () => {
    const fake = installFakeServer({
      ...defaults(),
      [`GET ${base}/members`]: (req: { query: URLSearchParams }) => req.query.get('cursor') === 'm2'
        ? json(200, { items: [{ userId: 'next-member', login: 'next_member', role: 'member' }], nextCursor: 'm3' })
        : req.query.get('cursor') === 'm3'
          ? page([{ userId: 'last-member', login: 'last_member', role: 'member' }])
          : json(200, { items: members(), nextCursor: 'm2' }),
      [`GET ${base}/invitations`]: (req: { query: URLSearchParams }) => req.query.get('cursor') === 'i2'
        ? json(200, { items: [invitation('two', 'accepted')], nextCursor: 'i3' })
        : req.query.get('cursor') === 'i3' ? page([invitation('three', 'revoked')]) : json(200, { items: [invitation('one')], nextCursor: 'i2' }),
    });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    const membersSection = screen.getByRole('region', { name: 'Members' });
    await userEvent.click(within(membersSection).getByRole('button', { name: 'Next page' }));
    expect(await within(membersSection).findByText('next_member')).toBeVisible();
    expect(within(membersSection).queryByText('anna')).not.toBeInTheDocument();
    expect(within(membersSection).queryByText('petr')).not.toBeInTheDocument();
    expect(within(membersSection).getAllByRole('listitem')).toHaveLength(1);
    const invitationsRegion = screen.getByRole('region', { name: 'Invite someone' });
    const invitations = within(invitationsRegion).getByRole('list', { name: 'Invitations' });
    await userEvent.click(within(screen.getByRole('navigation', { name: 'Invitations' })).getByRole('button', { name: 'Next page' }));
    expect(await within(invitations).findByText('Accepted')).toBeVisible();
    expect(within(invitations).getAllByRole('listitem')).toHaveLength(1);
    expect(within(invitations).queryByText('Pending')).not.toBeInTheDocument();
    await userEvent.click(within(membersSection).getByRole('button', { name: 'Next page' }));
    expect(await within(membersSection).findByText('last_member')).toBeVisible();
    await userEvent.click(within(membersSection).getByRole('button', { name: 'Previous page' }));
    expect(await within(membersSection).findByText('next_member')).toBeVisible();
    await userEvent.click(within(membersSection).getByRole('button', { name: 'First page' }));
    expect(await within(membersSection).findByText('anna')).toBeVisible();
    expect(within(membersSection).getByText(/\(you\)/)).toBeVisible();
    expect(within(membersSection).queryByText('next_member')).not.toBeInTheDocument();
    await userEvent.click(within(screen.getByRole('navigation', { name: 'Invitations' })).getByRole('button', { name: 'Next page' }));
    expect(await within(invitations).findByText('Revoked')).toBeVisible();
    await userEvent.click(within(screen.getByRole('navigation', { name: 'Invitations' })).getByRole('button', { name: 'Previous page' }));
    expect(await within(invitations).findByText('Accepted')).toBeVisible();
    await userEvent.click(within(screen.getByRole('navigation', { name: 'Invitations' })).getByRole('button', { name: 'First page' }));
    expect(await within(invitations).findByText('Pending')).toBeVisible();
    expect(fake.requests.filter((r) => r.path === `${base}/members`).map((r) => r.query.get('cursor'))).toContain('m3');
    expect(fake.requests.filter((r) => r.path === `${base}/invitations`).map((r) => r.query.get('cursor'))).toContain('i3');
  });

  it('a refresh after revoke wins over an older Next page response', async () => {
    let releaseMore!: (response: Response) => void;
    const more = new Promise<Response>((resolve) => { releaseMore = resolve; });
    let firstPages = 0;
    let secondPages = 0;
    const fake = installFakeServer({
      ...defaults(),
      [`GET ${base}/invitations`]: (req: { query: URLSearchParams }) => req.query.get('cursor') === 'p2'
        ? (++secondPages === 1 ? more : page([invitation('later', 'accepted')]))
        : ++firstPages === 1 ? json(200, { items: [invitation('one')], nextCursor: 'p2' }) : json(200, { items: [invitation('one', 'revoked')], nextCursor: 'p2' }),
      [`DELETE ${base}/invitations/one`]: json(204),
    });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Next page' }));
    await waitFor(() => expect(fake.requests.some((request) => request.path === `${base}/invitations` && request.query.get('cursor') === 'p2')).toBe(true));
    expect(screen.getByRole('button', { name: 'Revoke' })).toBeEnabled();
    await userEvent.click(screen.getByRole('button', { name: 'Revoke' }));
    await userEvent.click(screen.getByRole('button', { name: 'Yes' }));
    await waitFor(() => expect(screen.getByText('Revoked')).toBeVisible());
    await act(async () => {
      releaseMore(json(200, { items: [invitation('stale')], nextCursor: null }));
      await more;
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    const invitationsRegion = screen.getByRole('region', { name: 'Invite someone' });
    const invitations = within(invitationsRegion).getByRole('list', { name: 'Invitations' });
    const rows = within(invitations).getAllByRole('listitem');
    expect(rows.map((row) => row.textContent?.startsWith('Revoked'))).toEqual([true]);
    expect(within(screen.getByRole('region', { name: 'Invite someone' }).querySelector('nav')!).queryByText('Page 2')).not.toBeInTheDocument();
    expect(within(screen.getByRole('region', { name: 'Invite someone' })).queryByRole('button', { name: 'First page' })).not.toBeInTheDocument();
    const cursorCount = fake.requests.filter((request) => request.path === `${base}/invitations` && request.query.get('cursor') === 'p2').length;
    expect(cursorCount).toBe(1);
    await userEvent.click(within(screen.getByRole('navigation', { name: 'Invitations' })).getByRole('button', { name: 'Next page' }));
    expect(await within(invitations).findByText('Accepted')).toBeVisible();
    expect(within(screen.getByRole('region', { name: 'Invite someone' })).getByText('Page 2')).toBeVisible();
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
    const notice = await screen.findByText('This household isn’t available to your account.');
    expect(notice).toBeVisible();
    expect(screen.getByRole('status')).toContainElement(notice);
    expect(screen.queryByRole('button', { name: 'Check again' })).not.toBeInTheDocument();
  });

  it('401 on members list signs out', async () => {
    const routes: Record<string, Response> = defaults(); routes[`GET ${base}/members`] = problem(401); routes['GET /api/v1/auth/setup'] = json(200, { required: false }); installFakeServer(routes);
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' })); expect(await screen.findByLabelText('Password')).toBeVisible();
  });

  it('an invitation-list 401 signs out', async () => {
    installFakeServer({ ...defaults(), [`GET ${base}/invitations`]: problem(401), 'GET /api/v1/auth/setup': json(200, { required: false }) });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    expect(await screen.findByLabelText('Password')).toBeVisible();
  });

  it('an invitation-list 404 identifies the unavailable household', async () => {
    installFakeServer({ ...defaults(), [`GET ${base}/invitations`]: problem(404) });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    expect(await screen.findByText('This household isn’t available to your account.')).toBeVisible();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('create 401 signs out', async () => {
    installFakeServer({ ...defaults(), [`POST ${base}/invitations`]: problem(401), 'GET /api/v1/auth/setup': json(200, { required: false }) });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Create invite link' }));
    expect(await screen.findByLabelText('Password')).toBeVisible();
  });

  it.each([
    [401, undefined, 'signed-out'],
    [403, 'owner_required', 'admin'],
    [403, 'admin_required', 'admin'],
    [403, undefined, 'refused'],
    [404, undefined, 'not-found'],
    [503, undefined, 'That didn’t work. Check your connection and try again.'],
  ])('revoke status %s maps to %s outcome', async (status, code, outcome) => {
    installFakeServer({ ...defaults(), [`GET ${base}/invitations`]: page([invitation('one')]), [`DELETE ${base}/invitations/one`]: problem(status, code ? { code } : {}) , 'GET /api/v1/auth/setup': json(200, { required: false }) });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Revoke' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Yes' }));
    if (outcome === 'signed-out') expect(await screen.findByLabelText('Password')).toBeVisible();
    if (outcome === 'admin') expect(await screen.findByText('Only household administrators can manage invitations.')).toBeVisible();
    if (outcome === 'refused') {
      const notice = await screen.findByText('Tendo refused this request. If this keeps happening, ask whoever runs Tendo to check its address settings.');
      expect(notice).toBeVisible();
      expect(notice.closest('[role="status"]')).not.toBeNull();
    }
    if (outcome === 'not-found') {
      const notice = await screen.findByText('This household isn’t available to your account.');
      expect(notice).toBeVisible();
      expect(notice.closest('[role="status"]')).not.toBeNull();
    }
    if (typeof outcome === 'string' && outcome.includes('didn’t work')) {
      const notice = await screen.findByText(outcome);
      expect(notice).toBeVisible();
      expect(notice.closest('[role="status"]')).not.toBeNull();
    }
  });

  it('ignores late create and revoke results after unmount', async () => {
    for (const method of ['create', 'revoke'] as const) {
      let release!: (response: Response) => void;
      const pending = new Promise<Response>((resolve) => { release = resolve; });
      const onSignedOut = vi.fn();
      const fake = installFakeServer({
        ...defaults(),
        [`GET ${base}/invitations`]: page([invitation('one')]),
        [`POST ${base}/invitations`]: method === 'create' ? () => pending : json(201, { token: 'x'.repeat(43) }),
        [`DELETE ${base}/invitations/one`]: method === 'revoke' ? () => pending : json(204),
      });
      const view = render(<I18nProvider><MembersScreen householdId={HOUSEHOLD_ID} userId={session().userId} login={session().login} onSignedOut={onSignedOut} /></I18nProvider>);
      if (method === 'create') await userEvent.click(await screen.findByRole('button', { name: 'Create invite link' }));
      else {
        await screen.findByText('Pending');
        await userEvent.click(screen.getByRole('button', { name: 'Revoke' }));
        await userEvent.click(screen.getByRole('button', { name: 'Yes' }));
      }
      view.unmount();
      const before = fake.requests.length;
      await act(async () => { release(problem(401)); await pending; });
      expect(onSignedOut).not.toHaveBeenCalled();
      expect(fake.requests).toHaveLength(before);
    }
  });

  it('ignores an older invitation Next response after a newer refresh begins', async () => {
    let release!: (response: Response) => void;
    const pending = new Promise<Response>((resolve) => { release = resolve; });
    let pageTwoCalls = 0;
    let firstPageCalls = 0;
    let releaseRefresh!: (response: Response) => void;
    const refreshPending = new Promise<Response>((resolve) => { releaseRefresh = resolve; });
    const fake = installFakeServer({
      ...defaults(),
      [`GET ${base}/invitations`]: (req: RecordedRequest) => req.query.get('cursor') === 'p2'
        ? (++pageTwoCalls === 1 ? pending : page([invitation('fresh', 'accepted')]))
        : (++firstPageCalls === 1 ? json(200, { items: [invitation('first')], nextCursor: 'p2' }) : refreshPending),
      [`DELETE ${base}/invitations/first`]: json(204),
    });
    render(<I18nProvider><MembersScreen householdId={HOUSEHOLD_ID} userId={session().userId} login={session().login} onSignedOut={() => {}} /></I18nProvider>);
    await screen.findByText('Pending');
    const invitations = screen.getByRole('list', { name: 'Invitations' });
    await userEvent.click(within(screen.getByRole('navigation', { name: 'Invitations' })).getByRole('button', { name: 'Next page' }));
    await waitFor(() => expect(pageTwoCalls).toBe(1));
    await userEvent.click(within(invitations).getByRole('button', { name: 'Revoke' }));
    await userEvent.click(within(invitations).getByRole('button', { name: 'Yes' }));
    await waitFor(() => expect(fake.requests.filter((request) => request.path === `${base}/invitations`)).toHaveLength(3));
    await act(async () => { releaseRefresh(page([invitation('fresh', 'accepted')])); await refreshPending; });
    expect(await screen.findByText('Accepted')).toBeVisible();
    await act(async () => { release(page([invitation('stale', 'revoked')])); await pending; });
    expect(screen.queryByText('Revoked')).not.toBeInTheDocument();
    expect(fake.requests.filter((request) => request.path === `${base}/invitations`)).toHaveLength(3);
  });

  it('member page failure announces an error without stealing focus after the user moves away', async () => {
    let release!: (response: Response) => void;
    const pending = new Promise<Response>((resolve) => { release = resolve; });
    installFakeServer({ ...defaults(), [`GET ${base}/members`]: (req: RecordedRequest) => req.query.get('cursor') === 'p2'
      ? pending : json(200, { items: members(), nextCursor: 'p2' }) });
    renderApp(); await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
    const next = within(screen.getByRole('navigation', { name: 'Members' })).getByRole('button', { name: 'Next page' });
    await userEvent.click(next);
    screen.getByRole('button', { name: 'Create invite link' }).focus();
    await act(async () => { release(problem(503)); await pending; });
    const notice = await screen.findByText('That didn’t work. Check your connection and try again.');
    expect(notice).toBeVisible();
    expect(notice.closest('[role="status"]')).not.toBeNull();
    expect(screen.getByRole('heading', { name: 'Members' })).not.toHaveFocus();
  });

  it('discovers the viewer on page 20 and still renders only the first page', async () => {
    const fake = installFakeServer({
      ...defaults(),
      [`GET ${base}/members`]: (req: RecordedRequest) => {
        if (req.documentPath !== '/members') return page(members());
        const n = Number(req.query.get('cursor')?.replace('p', '') ?? 1);
        return json(200, { items: [{ userId: n === 20 ? session().userId : `user-${n}`, login: `page_${n}`, role: n === 20 ? 'admin' : 'member' }], nextCursor: n < 20 ? `p${n + 1}` : null });
      },
    });
    renderApp(); await openMembersAfterHomeRead(fake);
    expect(await screen.findByRole('button', { name: 'Create invite link' })).toBeVisible();
    expect(fake.requests.filter((request) => request.documentPath === '/members' && request.path === `${base}/members`)).toHaveLength(20);
    const memberList = screen.getByRole('list', { name: 'Members' });
    expect(within(memberList).getByText('page_1')).toBeVisible();
    expect(within(memberList).queryByText('page_20')).not.toBeInTheDocument();
  });
});
