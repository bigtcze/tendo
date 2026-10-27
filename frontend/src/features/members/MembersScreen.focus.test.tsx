import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, emptyHome, household, installFakeServer, json, session, type RecordedRequest } from '../../test/fakeServer';

// Focus-ordering details of MembersScreen that the main screen suite does not pin.
const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const members = () => [{ userId: session().userId, login: 'anna', role: 'owner' }, { userId: 'other', login: 'petr', role: 'member' }];
const invitation = (id: string, status = 'pending') => ({ id, status, createdAt: '2026-01-01T00:00:00Z', expiresAt: '2026-01-08T00:00:00Z' });
const problem = (status: number) => json(status, { type: 'about:blank', title: 'x', status });
function deferred() {
  let release!: (response: Response) => void;
  const pending = new Promise<Response>((resolve) => { release = resolve; });
  return { pending, release };
}
function defaults() {
  return { 'GET /api/v1/session': json(200, session()), [`GET ${base}`]: json(200, household), ...emptyHome };
}
async function openMembers() {
  render(<I18nProvider><App /></I18nProvider>);
  await userEvent.click(await screen.findByRole('link', { name: 'Household members' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'Create invite link' })).toBeEnabled());
}

beforeEach(() => { window.history.replaceState(null, '', '/'); vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']); });
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe('members screen focus ordering', () => {
  it('a failed member page never focuses the Members heading, even while focus stays on the pager', async () => {
    const memberPage = deferred();
    installFakeServer({
      ...defaults(),
      [`GET ${base}/members`]: (req: RecordedRequest) => req.query.get('cursor') === 'p2'
        ? memberPage.pending : json(200, { items: members(), nextCursor: req.documentPath === '/members' ? 'p2' : null }),
      [`GET ${base}/invitations`]: json(200, { items: [], nextCursor: null }),
    });
    await openMembers();
    const heading = screen.getByRole('heading', { name: 'Members' });
    const headingFocus = vi.fn();
    heading.addEventListener('focus', headingFocus);
    await userEvent.click(within(heading.parentElement!).getByRole('button', { name: 'Next page' }));
    await act(async () => { memberPage.release(problem(503)); await memberPage.pending; });
    expect(await screen.findByText('That didn’t work. Check your connection and try again.')).toBeVisible();
    expect(headingFocus).not.toHaveBeenCalled();
  });

  it('when both pages settle in one commit with no focused control, the Members heading wins', async () => {
    const memberPage = deferred();
    const invitationPage = deferred();
    installFakeServer({
      ...defaults(),
      [`GET ${base}/members`]: (req: RecordedRequest) => req.query.get('cursor') === 'm2'
        ? memberPage.pending : json(200, { items: members(), nextCursor: req.documentPath === '/members' ? 'm2' : null }),
      [`GET ${base}/invitations`]: (req: RecordedRequest) => req.query.get('cursor') === 'i2'
        ? invitationPage.pending : json(200, { items: [invitation('one')], nextCursor: 'i2' }),
    });
    await openMembers();
    const memberHeading = screen.getByRole('heading', { name: 'Members' });
    const invitationHeading = screen.getByRole('heading', { name: 'Invitations' });
    await userEvent.click(within(memberHeading.parentElement!).getByRole('button', { name: 'Next page' }));
    await userEvent.click(within(invitationHeading.parentElement!).getByRole('button', { name: 'Next page' }));
    (document.activeElement as HTMLElement | null)?.blur();
    await act(async () => {
      memberPage.release(json(200, { items: [{ userId: 'member2', login: 'member_two', role: 'member' }], nextCursor: null }));
      invitationPage.release(json(200, { items: [invitation('two', 'accepted')], nextCursor: null }));
      await Promise.all([memberPage.pending, invitationPage.pending]);
    });
    expect(await screen.findByText('member_two')).toBeVisible();
    expect(await screen.findByText('Accepted')).toBeVisible();
    expect(memberHeading).toHaveFocus();
  });

  it('two member-page activations batched before the busy commit apply only the newest response', async () => {
    const pages = [deferred(), deferred()];
    let pageCalls = 0;
    installFakeServer({
      ...defaults(),
      [`GET ${base}/members`]: (req: RecordedRequest) => req.query.get('cursor') === 'p2'
        ? pages[pageCalls++].pending : json(200, { items: members(), nextCursor: req.documentPath === '/members' ? 'p2' : null }),
      [`GET ${base}/invitations`]: json(200, { items: [], nextCursor: null }),
    });
    await openMembers();
    const next = within(screen.getByRole('heading', { name: 'Members' }).parentElement!).getByRole('button', { name: 'Next page' });
    act(() => { next.click(); next.click(); });
    await waitFor(() => expect(pageCalls).toBe(2));
    await act(async () => {
      pages[1].release(json(200, { items: [{ userId: 'fresh', login: 'fresh_member', role: 'member' }], nextCursor: null }));
      await pages[1].pending;
    });
    expect(await screen.findByText('fresh_member')).toBeVisible();
    await act(async () => {
      pages[0].release(json(200, { items: [{ userId: 'stale', login: 'stale_member', role: 'member' }], nextCursor: null }));
      await pages[0].pending;
    });
    expect(screen.getByText('fresh_member')).toBeVisible();
    expect(screen.queryByText('stale_member')).not.toBeInTheDocument();
  });
});
