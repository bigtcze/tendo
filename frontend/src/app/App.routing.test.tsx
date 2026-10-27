import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Translate } from '../i18n';
import { App } from './App';
import { getLoginNotice, type Props as SignedOutProps } from './SignedOutApp';
import { I18nProvider } from '../i18n';
import { HOUSEHOLD_ID, household, installFakeServer, json, session } from '../test/fakeServer';
import { itemRoutes } from '../test/fakeItems';

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
function renderApp() { return render(<I18nProvider><App /></I18nProvider>); }
beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
  window.history.replaceState(null, '', '/');
  sessionStorage.clear();
});
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

const validId = '0198a2f0-0000-7000-8000-000000000002';
const item = {
  id: validId, subjectId: 's-1', responsibleUserId: null, title: 'Routing item', notes: null,
  attentionOn: null, recurrence: null, workflowState: 'open' as const,
  attention: 'needs_attention' as const, archived: false, done: false, lastCompletedOn: null,
  createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z',
};
const signedInRoutes = (extra: Record<string, unknown> = {}) => ({
  'GET /api/v1/session': json(200, session()), [`GET ${base}`]: json(200, household),
  ...itemRoutes(base, [item]),
  [`GET ${base}/items/${validId}`]: json(200, item, { ETag: '"1"' }),
  [`GET ${base}/subjects`]: json(200, { items: [], nextCursor: null }),
  [`GET ${base}/subjects/s-1`]: json(200, { id: 's-1', name: 'Test', type: 'person', linkedUserId: null, archived: false }, { ETag: '"subject"' }),
  [`GET ${base}/items/${validId}/completions`]: json(200, { items: [], nextCursor: null }),
  [`GET ${base}/members`]: json(200, { items: [], nextCursor: null }),
  'GET /api/v1/auth/oidc': json(200, { enabled: false }), ...extra,
});

describe('App route characterization', () => {
  it.each([
    ['/', 'Veselí'], ['/items', 'All items'], ['/items/a/b', 'Veselí'],
    ['/unknown', 'Veselí'], ['/people', 'People and things'],
    ['/members', 'Household members'], ['/account', 'Your account'],
  ])('routes %s as expected', async (path, heading) => {
    window.history.replaceState(null, '', path);
    installFakeServer(signedInRoutes({
      [`GET ${base}/items/not-a-uuid`]: json(404, { type: 'about:blank', title: 'x', status: 404 }),
      [`GET ${base}/items/${validId.toUpperCase()}`]: json(200, item, { ETag: '"1"' }),
    }));
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: heading })).toBeVisible();
  });

  it.each([`/items/${validId}`, '/items/not-a-uuid'])(
    'marks All items current on detail route %s', async (path) => {
      window.history.replaceState(null, '', path);
      installFakeServer(signedInRoutes({
        [`GET ${base}/items/not-a-uuid`]: json(404, { type: 'about:blank', title: 'x', status: 404 }),
      }));
      renderApp();
      const navigation = await screen.findByRole('navigation', { name: 'Household navigation' });
      expect(within(navigation).getByRole('link', { name: 'All items' })).toHaveAttribute('aria-current', 'page');
    },
  );

  it('prioritizes account-ready over OIDC error and OIDC error over session-ended at App level', async () => {
    const noticeProps = {
      state: { kind: 'signedOut' as const }, retry: vi.fn(), signedIn: vi.fn(), signedOut: vi.fn(),
      accountReadyNotice: true,
      oidcOutcome: { kind: 'error' as const, code: 'identity_conflict' },
      sessionEndedNotice: true,
    } satisfies SignedOutProps;
    const translate: Translate = (key) => key;
    expect(getLoginNotice(noticeProps, translate)).toBe('invite.error.accountReady');
    expect(getLoginNotice({ ...noticeProps, accountReadyNotice: false }, translate)).toBe('oidc.error.identity_conflict');
    expect(getLoginNotice({ ...noticeProps, accountReadyNotice: false, oidcOutcome: null }, translate)).toBe('login.sessionEnded');

    window.history.replaceState(null, '', '/login#oidcError=identity_conflict');
    installFakeServer({
      'GET /api/v1/session': json(401, { type: 'about:blank', title: 'x', status: 401 }),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
    });
    renderApp();
    expect(await screen.findByText('That sign-in is already connected to another Tendo account.')).toBeVisible();
  });

  it('signed-out boot clears pending completion, subject-create, and item-create storage', async () => {
    seedPendingStores();
    installFakeServer({
      'GET /api/v1/session': json(401, { type: 'about:blank', title: 'x', status: 401 }),
      'GET /api/v1/auth/setup': json(200, { required: false }),
    });
    renderApp();
    expect(await screen.findByLabelText('Login')).toBeVisible();
    assertPendingStoresCleared();
  });

  it('deliberate sign out clears all pending stores', async () => {
    seedPendingStores();
    installFakeServer(signedInRoutes({ 'DELETE /api/v1/session': json(204) }));
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Sign out' }));
    expect(await screen.findByLabelText('Password')).toBeVisible();
    assertPendingStoresCleared();
  });

  it('session-ended 401 clears all pending stores', async () => {
    seedPendingStores();
    installFakeServer(signedInRoutes({
      [`GET ${base}`]: json(401, { type: 'about:blank', title: 'x', status: 401 }),
      [`GET ${base}/subjects`]: json(401, { type: 'about:blank', title: 'x', status: 401 }),
    }));
    renderApp();
    expect(await screen.findByLabelText('Password')).toBeVisible();
    expect(screen.getByRole('status')).toHaveTextContent('You were signed out. Sign in again to continue.');
    assertPendingStoresCleared();
  });

  it('signed-in visitor at /login replaces URL with home', async () => {
    window.history.replaceState(null, '', '/login');
    const replaceState = vi.spyOn(window.history, 'replaceState');
    installFakeServer(signedInRoutes());
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible();
    await waitFor(() => expect(window.location.pathname).toBe('/'));
    expect(replaceState).toHaveBeenCalledWith(null, '', '/');
  });

  it('signed-in visitor at /people without household replaces URL with home', async () => {
    window.history.replaceState(null, '', '/people');
    const replaceState = vi.spyOn(window.history, 'replaceState');
    installFakeServer({ 'GET /api/v1/session': json(200, session(false)) });
    renderApp();
    expect(await screen.findByText('Your account isn’t part of a household yet.')).toBeVisible();
    await waitFor(() => expect(window.location.pathname).toBe('/'));
    expect(replaceState).toHaveBeenCalledWith(null, '', '/');
  });

  it('item detail Back uses history.back for All items returnTo and Home push for deep links', async () => {
    installFakeServer(signedInRoutes());
    renderApp();
    await userEvent.click(await screen.findByRole('link', { name: 'All items' }));
    const listVisit = screen.getByRole('heading', { level: 1, name: 'All items' });
    await userEvent.click(await screen.findByRole('link', { name: 'Routing item' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Routing item' })).toBeVisible();
    const back = vi.spyOn(window.history, 'back');
    await userEvent.click(screen.getByRole('link', { name: 'Back to all items' }));
    expect(back).toHaveBeenCalled();
    expect(await screen.findByRole('heading', { name: 'All items' })).not.toBe(listVisit);

    window.history.replaceState(null, '', `/items/${validId}`);
    window.dispatchEvent(new PopStateEvent('popstate'));
    await screen.findByRole('heading', { level: 1, name: 'Routing item' });
    const pushState = vi.spyOn(window.history, 'pushState');
    await userEvent.click(screen.getByRole('link', { name: 'Home' }));
    expect(pushState).toHaveBeenCalledWith(null, '', '/');
    await screen.findByRole('heading', { level: 1, name: 'Veselí' });
  });

  it('late 401 cannot navigate away from a later signed-out invite visit', async () => {
    let releaseSubject!: (response: Response) => void;
    let releaseHistory!: (response: Response) => void;
    const subjectResponse = new Promise<Response>((resolve) => { releaseSubject = resolve; });
    const historyResponse = new Promise<Response>((resolve) => { releaseHistory = resolve; });
    installFakeServer(signedInRoutes({
      [`GET ${base}/subjects/s-1`]: () => subjectResponse,
      [`GET ${base}/items/${validId}/completions`]: () => historyResponse,
    }));
    window.history.replaceState(null, '', `/items/${validId}`);
    renderApp();
    await screen.findByRole('heading', { level: 1, name: 'Routing item' });
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 0)); });
    await act(async () => { releaseSubject(json(401, { type: 'about:blank', title: 'x', status: 401 })); });
    expect(await screen.findByLabelText('Password')).toBeVisible();
    window.history.pushState(null, '', '/invite');
    window.dispatchEvent(new PopStateEvent('popstate'));
    await screen.findByRole('heading', { level: 1, name: 'This invite link isn’t complete' });
    await act(async () => { releaseHistory(json(401, { type: 'about:blank', title: 'x', status: 401 })); });
    expect(await screen.findByRole('heading', { level: 1, name: 'This invite link isn’t complete' })).toBeVisible();
    expect(window.location.pathname).toBe('/invite');
  });
});

function seedPendingStores(): void {
  sessionStorage.setItem('tendo.pendingCompletion:user:household:item', 'receipt');
  sessionStorage.setItem('tendo.pendingSubjectCreate.v1:user:household', 'subject');
  sessionStorage.setItem('tendo.pendingItemCreate.v1:user:household', 'item');
}
function assertPendingStoresCleared(): void {
  expect(sessionStorage.getItem('tendo.pendingCompletion:user:household:item')).toBeNull();
  expect(sessionStorage.getItem('tendo.pendingSubjectCreate.v1:user:household')).toBeNull();
  expect(sessionStorage.getItem('tendo.pendingItemCreate.v1:user:household')).toBeNull();
}
