import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { en } from '../i18n/en';
import { App } from './App';
import { I18nProvider } from '../i18n';
import { HOUSEHOLD_ID, household, installFakeServer, json, session } from '../test/fakeServer';
import { clearConnectEntry, markConnectReturn, readConnectEntry, saveConnectHandle } from '../features/mcp/connectEntry';
import { browserNavigation } from '../features/auth/oidcApi';
import { itemRoutes } from '../test/fakeItems';

const handle = 'A'.repeat(43);
const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const consentPath = `/api/v1/mcp/authorization-requests/${handle}`;
const request = {
  clientName: 'Claude Code', clientNameVerified: false, redirectUri: 'http://localhost:54123/callback',
  scopes: ['tendo:read'], expiresAt: '2026-11-20T10:05:00Z',
};

function renderApp() { return render(<I18nProvider><App /></I18nProvider>); }

const householdData = {
  [`GET ${base}`]: json(200, household),
  [`GET ${base}/items/summary`]: json(200, {
    evaluatedOn: '2026-01-15', total: 0, needsAttention: 0, inProgress: 0, waiting: 0, upcoming: 0, paused: 0, done: 0, archived: 0,
  }),
  [`GET ${base}/items`]: json(200, { items: [], nextCursor: null }),
  [`GET ${base}/subjects`]: json(200, { items: [], nextCursor: null }),
};

const signedInRoutes = (extra: Record<string, unknown> = {}) => ({
  'GET /api/v1/session': json(200, session()),
  ...householdData,
  'GET /api/v1/auth/oidc': json(200, { enabled: false }),
  'GET /api/v1/auth/setup': json(200, { required: false }),
  ...extra,
});

const signedOutRoutes = (extra: Record<string, unknown> = {}) => ({
  'GET /api/v1/session': json(401, { type: 'about:blank', title: 'x', status: 401 }),
  'GET /api/v1/auth/setup': json(200, { required: false }),
  'GET /api/v1/auth/oidc': json(200, { enabled: false }),
  ...extra,
});

const loginOk = { 'POST /api/v1/session': json(201, session()) };

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
  window.history.replaceState(null, '', '/');
  sessionStorage.clear();
  clearConnectEntry();
});
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe('signed-out consent entry', () => {
  it('shows the sign-in notice on /connect and strips the fragment', async () => {
    window.history.replaceState(null, '', `/connect#request=${handle}`);
    installFakeServer(signedOutRoutes());
    renderApp();
    expect(await screen.findByText(en['connect.signIn.notice'], { exact: true })).toBeVisible();
    expect(window.location.pathname).toBe('/connect');
    expect(window.location.hash).toBe('');
    expect(readConnectEntry()?.handle).toBe(handle);
  });

  it('a local sign-in from /connect lands on the consent screen, not Home', async () => {
    window.history.replaceState(null, '', `/connect#request=${handle}`);
    installFakeServer(signedOutRoutes({ ...loginOk, ...householdData, [`GET ${consentPath}`]: json(200, request) }));
    renderApp();
    await userEvent.type(await screen.findByLabelText('Username'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'correct-horse-battery');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Connect Claude Code?' })).toBeVisible();
    await waitFor(() => expect(window.location.pathname).toBe('/connect'));
    expect(screen.queryByRole('navigation', { name: 'Household navigation' })).toBeNull();
  });

  it('a sign-in at /login without starting on /connect goes Home even with a fresh stored entry', async () => {
    saveConnectHandle(handle);
    window.history.replaceState(null, '', '/login');
    installFakeServer(signedOutRoutes({ ...loginOk, ...householdData }));
    renderApp();
    await userEvent.type(await screen.findByLabelText('Username'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'correct-horse-battery');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toBeVisible();
    expect(window.location.pathname).toBe('/');
  });

  it('a sign-in at /login after an abandoned consent sign-in goes Home and drops the pending consent', async () => {
    saveConnectHandle(handle);
    markConnectReturn();
    window.history.replaceState(null, '', '/login');
    installFakeServer(signedOutRoutes({ ...loginOk, ...householdData }));
    renderApp();
    await userEvent.type(await screen.findByLabelText('Username'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'correct-horse-battery');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toBeVisible();
    expect(window.location.pathname).toBe('/');
    expect(readConnectEntry()).toBeNull();
  });
});

describe('session loss on the consent page', () => {
  it('a 401 on the consent GET returns to the /connect sign-in with the notice and no fragment', async () => {
    window.history.replaceState(null, '', '/connect');
    saveConnectHandle(handle);
    installFakeServer(signedInRoutes({ [`GET ${consentPath}`]: json(401, { title: 'x', status: 401 }) }));
    renderApp();
    expect(await screen.findByText(en['connect.signIn.notice'], { exact: true })).toBeVisible();
    expect(window.location.pathname).toBe('/connect');
    expect(window.location.hash).toBe('');
  });

  it('a 401 on the decision POST returns to the /connect sign-in, and signing in again shows the consent', async () => {
    window.history.replaceState(null, '', '/connect');
    saveConnectHandle(handle);
    installFakeServer(signedInRoutes({
      [`GET ${consentPath}`]: json(200, request),
      [`POST ${consentPath}/decision`]: json(401, { title: 'x', status: 401 }),
      ...loginOk,
    }));
    const first = renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Connect Claude Code?' })).toBeVisible();
    await waitFor(() => expect(screen.getByRole('button', { name: en['connect.allow'] })).toBeEnabled(), { timeout: 3000 });
    await userEvent.click(screen.getByRole('button', { name: en['connect.allow'] }));
    expect(await screen.findByText(en['connect.signIn.notice'], { exact: true })).toBeVisible();
    expect(window.location.hash).toBe('');
    first.unmount();
  });

  it('"Not you? Sign out" returns to sign-in, and a different login sees its own consent', async () => {
    window.history.replaceState(null, '', '/connect');
    saveConnectHandle(handle);
    const other = { ...session(), login: 'boris' };
    installFakeServer(signedInRoutes({
      [`GET ${consentPath}`]: json(200, request),
      'DELETE /api/v1/session': json(204),
      'POST /api/v1/session': json(201, other),
    }));
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: en['connect.notYou'] }));
    expect(await screen.findByText(en['connect.signIn.notice'], { exact: true })).toBeVisible();
    expect(readConnectEntry()?.handle).toBe(handle);
    await userEvent.type(screen.getByLabelText('Username'), 'boris');
    await userEvent.type(screen.getByLabelText('Password'), 'correct-horse-battery');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Connect Claude Code?' })).toBeVisible();
    expect(screen.getByText(/Signed in as/)).toHaveTextContent('Signed in as boris.');
  });

  it('an OIDC start from /connect sets the return flag and navigates to the provider', async () => {
    window.history.replaceState(null, '', `/connect#request=${handle}`);
    const authorizationUrl = 'https://id.example/authorize?client_id=tendo&state=opaque';
    const assign = vi.spyOn(browserNavigation, 'assign').mockImplementation(() => undefined);
    installFakeServer(signedOutRoutes({
      'GET /api/v1/auth/oidc': json(200, { enabled: true, displayName: 'Pocket ID' }),
      'POST /api/v1/auth/oidc/start': json(200, { authorizationUrl }),
    }));
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Sign in with Pocket ID' }));
    await waitFor(() => expect(assign).toHaveBeenCalledWith(authorizationUrl));
    expect(readConnectEntry()?.returnAfterSignIn).toBe(true);
  });
});

describe('abandoned consent flags', () => {
  it('a header sign-out drops a flagged entry, so the next sign-in at / does not return to /connect', async () => {
    // /people, not /: a signed-in boot at / with a stored request would redirect to the consent page instead.
    window.history.replaceState(null, '', '/people');
    saveConnectHandle(handle);
    markConnectReturn();
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      ...itemRoutes(base, []),
      [`GET ${base}`]: json(200, household),
      'DELETE /api/v1/session': json(204),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Sign out' }));
    expect(await screen.findByLabelText('Username')).toBeVisible();
    expect(readConnectEntry()).toBeNull();
  });

  it('a sign-in on the invite screen with a flagged entry goes to the invite outcome, not /connect', async () => {
    saveConnectHandle(handle);
    markConnectReturn();
    const inviteToken = 'C'.repeat(43);
    window.history.replaceState(null, '', `/invite#${inviteToken}`);
    installFakeServer(signedOutRoutes({
      'POST /api/v1/auth/invitations/accept': json(201, {}),
      ...loginOk,
      ...householdData,
    }));
    renderApp();
    await userEvent.type(await screen.findByLabelText('Login'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'correct-horse-battery');
    await userEvent.click(screen.getByRole('button', { name: en['invite.create'] }));
    await waitFor(() => expect(screen.queryByRole('button', { name: en['invite.create'] })).toBeNull());
    expect(window.location.pathname).not.toBe('/connect');
    expect(readConnectEntry()).toBeNull();
  });
});

describe('signed-out /connect without a usable request', () => {
  it('an invalid_client error shows the problem, no login form, and strips the fragment', async () => {
    window.history.replaceState(null, '', '/connect#error=invalid_client');
    installFakeServer(signedOutRoutes());
    renderApp();
    expect(await screen.findByText(en['connect.problem.invalid_client'], { exact: true })).toBeVisible();
    expect(screen.queryByLabelText('Username')).toBeNull();
    expect(screen.queryByText(en['connect.signIn.notice'], { exact: true })).toBeNull();
    expect(window.location.hash).toBe('');
    expect(window.location.pathname).toBe('/connect');
  });

  it('a bare /connect with no entry shows the problem state, no login form', async () => {
    window.history.replaceState(null, '', '/connect');
    installFakeServer(signedOutRoutes());
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: en['connect.problem.title'] })).toBeVisible();
    expect(screen.queryByLabelText('Username')).toBeNull();
  });
});

describe('boot return to the consent screen', () => {
  it('a flagged fresh entry sends a signed-in boot at / to /connect exactly once', async () => {
    saveConnectHandle(handle);
    markConnectReturn();
    installFakeServer(signedInRoutes({ [`GET ${consentPath}`]: json(200, request) }));
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Connect Claude Code?' })).toBeVisible();
    expect(window.location.pathname).toBe('/connect');
  });

  it('a second boot after the flag was consumed stays on Home', async () => {
    saveConnectHandle(handle);
    markConnectReturn();
    window.history.replaceState(null, '', '/');
    installFakeServer(signedInRoutes());
    const first = renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: en['connect.problem.title'] })).toBeVisible();
    first.unmount();

    window.history.replaceState(null, '', '/');
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toBeVisible();
    expect(window.location.pathname).toBe('/');
  });

  it('a flagged entry does not redirect a boot at an item address', async () => {
    saveConnectHandle(handle);
    markConnectReturn();
    const itemId = '0198a2f0-0000-7000-8000-000000000002';
    window.history.replaceState(null, '', `/items/${itemId}`);
    installFakeServer(signedInRoutes({
      [`GET ${base}/items/${itemId}`]: json(404, { type: 'about:blank', title: 'x', status: 404 }),
    }));
    renderApp();
    await screen.findByRole('navigation', { name: 'Household navigation' });
    expect(window.location.pathname).toBe(`/items/${itemId}`);
    expect(screen.queryByRole('heading', { level: 1, name: en['connect.problem.title'] })).toBeNull();
  });
});
