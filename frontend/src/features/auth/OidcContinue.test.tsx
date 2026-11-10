import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { en } from '../../i18n/en';
import { emptyHome, household, HOUSEHOLD_ID, installFakeServer, json, session } from '../../test/fakeServer';

const problem = (status: number, code?: string) => json(status, {
  type: 'about:blank', title: 'x', status, ...(code ? { code } : {}),
}, { 'Content-Type': 'application/problem+json' });
const signedOut = { 'GET /api/v1/session': problem(401), 'GET /api/v1/auth/setup': json(200, { required: false }) };
const provider = { 'GET /api/v1/auth/oidc': json(200, { enabled: true, displayName: 'Pocket ID' }) };
const pendingStatus = (extra: Record<string, unknown> = {}) =>
  json(200, { expiresAt: '2026-11-10T12:30:00Z', emailVerified: false, invitation: false, ...extra });
const signedInHome = {
  'GET /api/v1/session': json(200, session()),
  [`GET /api/v1/households/${HOUSEHOLD_ID}`]: json(200, household),
  ...emptyHome,
};

function renderApp() {
  return render(<I18nProvider><App /></I18nProvider>);
}

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
  window.history.replaceState(null, '', '/login#oidc=continue');
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
  window.history.replaceState(null, '', '/');
});

describe('OIDC continue interstitial', () => {
  it('reads the pending sign-in once, cleans the fragment and offers both decisions', async () => {
    const fake = installFakeServer({ ...signedOut, ...provider, 'GET /api/v1/auth/oidc/pending': pendingStatus() });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Finish signing in with Pocket ID' })).toBeVisible();
    expect(screen.getByText(en['oidc.continue.intro'], { exact: true })).toBeVisible();
    expect(screen.getByRole('button', { name: en['oidc.continue.create.action'] })).toBeVisible();
    expect(screen.getByRole('button', { name: en['oidc.continue.link.action'] })).toBeVisible();
    expect(screen.getByRole('button', { name: en['oidc.continue.cancel'] })).toBeVisible();
    expect(window.location.pathname).toBe('/login');
    expect(window.location.hash).toBe('');
    expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc/pending')).toHaveLength(1);
    // The plain sign-in form is not rendered behind the decision.
    expect(screen.queryByLabelText(en['login.password'])).not.toBeInTheDocument();
  });

  it('mentions the carried invitation', async () => {
    installFakeServer({ ...signedOut, ...provider, 'GET /api/v1/auth/oidc/pending': pendingStatus({ invitation: true }) });
    renderApp();
    expect(await screen.findByText(en['oidc.continue.invitation'], { exact: true })).toBeVisible();
  });

  it('a missing continuation shows the expiry notice on the ordinary sign-in screen', async () => {
    installFakeServer({ ...signedOut, ...provider, 'GET /api/v1/auth/oidc/pending': problem(404, 'pending_not_found') });
    renderApp();
    expect(await screen.findByText(en['oidc.continue.expired'], { exact: true })).toBeVisible();
    expect(screen.getByLabelText(en['login.login'])).toBeVisible();
    expect(screen.getByRole('heading', { level: 1, name: 'Welcome back' })).toBeVisible();
  });

  it('shows a calm unavailable state with retry when the pending read fails', async () => {
    let reads = 0;
    installFakeServer({
      ...signedOut, ...provider,
      'GET /api/v1/auth/oidc/pending': () => (++reads === 1 ? problem(503, 'unavailable') : pendingStatus()),
    });
    renderApp();
    expect(await screen.findByText(en['oidc.continue.unavailable'], { exact: true })).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: en['oidc.continue.retry'] }));
    expect(await screen.findByRole('button', { name: en['oidc.continue.create.action'] })).toBeVisible();
  });

  it('cancelling clears the continuation and returns to password sign-in', async () => {
    const fake = installFakeServer({ ...signedOut, ...provider, 'GET /api/v1/auth/oidc/pending': pendingStatus(), 'DELETE /api/v1/auth/oidc/pending': json(204) });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: en['oidc.continue.cancel'] }));
    expect(await screen.findByLabelText(en['login.password'])).toBeVisible();
    expect(screen.getByRole('status')).toBeEmptyDOMElement();
    const cancel = fake.requests.find((request) => request.method === 'DELETE' && request.path === '/api/v1/auth/oidc/pending');
    expect(cancel?.headers.get('Origin')).toBe(window.location.origin);
  });

  it('creating an account signs in and lands on the household-invitation screen', async () => {
    const fake = installFakeServer({
      ...signedOut, ...provider,
      'GET /api/v1/auth/oidc/pending': pendingStatus(),
      'POST /api/v1/auth/oidc/pending/account': json(201, { session: session(false), invitation: 'none' }),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: en['oidc.continue.create.action'] }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Join your household by invitation' })).toBeVisible();
    expect(await screen.findByRole('button', { name: en['home.noHousehold.discard.action'] })).toBeVisible();
    expect(window.location.pathname).toBe('/');
    expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc/pending/account')).toHaveLength(1);
  });

  it('creating an account with an invalid invitation explains it on the join screen', async () => {
    installFakeServer({
      ...signedOut, ...provider,
      'GET /api/v1/auth/oidc/pending': pendingStatus({ invitation: true }),
      'POST /api/v1/auth/oidc/pending/account': json(201, { session: session(false), invitation: 'invalid' }),
    });
    renderApp();
    expect(await screen.findByText(en['oidc.continue.invitation'], { exact: true })).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: en['oidc.continue.create.action'] }));
    expect(await screen.findByText(en['home.notice.invitationInvalid'], { exact: true })).toBeVisible();
    expect(screen.getByRole('heading', { level: 1, name: 'Join your household by invitation' })).toBeVisible();
  });

  it('creating an account with an accepted invitation opens the household and confirms the join', async () => {
    installFakeServer({
      ...signedInHome, ...signedOut, ...provider,
      'GET /api/v1/auth/oidc/pending': pendingStatus({ invitation: true }),
      'POST /api/v1/auth/oidc/pending/account': json(201, { session: session(), invitation: 'accepted' }),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: en['oidc.continue.create.action'] }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible();
    expect(await screen.findByText(en['home.notice.invitationAccepted'], { exact: true })).toBeVisible();
  });

  it('linking with an existing account signs in and reports an unsaved email calmly', async () => {
    const fake = installFakeServer({
      ...signedInHome, ...signedOut, ...provider,
      'GET /api/v1/auth/oidc/pending': pendingStatus({ emailVerified: false }),
      'POST /api/v1/auth/oidc/pending/link': json(201, { session: session(), invitation: 'none', emailSaved: false }),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: en['oidc.continue.link.action'] }));
    await userEvent.type(screen.getByLabelText(en['oidc.continue.link.login']), 'anna');
    await userEvent.type(screen.getByLabelText(en['oidc.continue.link.password']), 'correct horse battery');
    await userEvent.click(screen.getByRole('button', { name: en['oidc.continue.link.submit'] }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible();
    expect(await screen.findByText(en['oidc.continue.emailNotSaved'], { exact: true })).toBeVisible();
    const link = fake.requests.find((request) => request.path === '/api/v1/auth/oidc/pending/link');
    expect(JSON.parse(link!.body)).toEqual({ login: 'anna', password: 'correct horse battery' });
  });

  it('a wrong local password is generic, clears the field and keeps the screen usable', async () => {
    installFakeServer({
      ...signedOut, ...provider,
      'GET /api/v1/auth/oidc/pending': pendingStatus(),
      'POST /api/v1/auth/oidc/pending/link': problem(401, 'invalid_credentials'),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: en['oidc.continue.link.action'] }));
    await userEvent.type(screen.getByLabelText(en['oidc.continue.link.password']), 'wrong password');
    await userEvent.click(screen.getByRole('button', { name: en['oidc.continue.link.submit'] }));
    expect(await screen.findByText(en['oidc.continue.error.invalid'], { exact: true })).toBeVisible();
    const password = screen.getByLabelText(en['oidc.continue.link.password']);
    expect(password).toHaveValue('');
    expect(password).toHaveFocus();
    expect(password).toHaveAttribute('aria-invalid', 'true');
  });

  it('a rate-limited attempt keeps the form and explains the wait', async () => {
    installFakeServer({
      ...signedOut, ...provider,
      'GET /api/v1/auth/oidc/pending': pendingStatus(),
      'POST /api/v1/auth/oidc/pending/link': problem(429, 'rate_limited'),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: en['oidc.continue.link.action'] }));
    await userEvent.type(screen.getByLabelText(en['oidc.continue.link.password']), 'pw');
    await userEvent.click(screen.getByRole('button', { name: en['oidc.continue.link.submit'] }));
    expect(await screen.findByText(en['oidc.continue.error.rateLimited'], { exact: true })).toBeVisible();
    expect(screen.getByLabelText(en['oidc.continue.link.password'])).toBeVisible();
  });

  it('an identity conflict ends the continuation with a specific safe explanation', async () => {
    const fake = installFakeServer({
      ...signedOut, ...provider,
      'GET /api/v1/auth/oidc/pending': pendingStatus(),
      'POST /api/v1/auth/oidc/pending/link': problem(409, 'identity_conflict'),
      'DELETE /api/v1/auth/oidc/pending': json(204),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: en['oidc.continue.link.action'] }));
    await userEvent.type(screen.getByLabelText(en['oidc.continue.link.password']), 'pw');
    await userEvent.click(screen.getByRole('button', { name: en['oidc.continue.link.submit'] }));
    expect(await screen.findByText(en['oidc.continue.error.conflict'], { exact: true })).toBeVisible();
    // The continuation is consumed by the conflict; no retry is offered.
    expect(screen.queryByRole('button', { name: en['oidc.continue.retry'] })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: en['oidc.continue.create.action'] })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: en['oidc.continue.backToSignIn'] }));
    expect(await screen.findByLabelText(en['login.password'])).toBeVisible();
    // Leaving is local: the server already dropped the continuation, so no cancel request is needed.
    expect(fake.requests.some((request) => request.method === 'DELETE' && request.path === '/api/v1/auth/oidc/pending')).toBe(false);
  });

  it('a concurrent link during account creation ends with the same calm conflict', async () => {
    installFakeServer({
      ...signedOut, ...provider,
      'GET /api/v1/auth/oidc/pending': pendingStatus(),
      'POST /api/v1/auth/oidc/pending/account': problem(409, 'identity_conflict'),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: en['oidc.continue.create.action'] }));
    expect(await screen.findByText(en['oidc.continue.error.conflict'], { exact: true })).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: en['oidc.continue.backToSignIn'] }));
    expect(await screen.findByLabelText(en['login.password'])).toBeVisible();
  });

  it('an expired continuation discovered while linking returns to sign-in with the expiry notice', async () => {
    installFakeServer({
      ...signedOut, ...provider,
      'GET /api/v1/auth/oidc/pending': pendingStatus(),
      'POST /api/v1/auth/oidc/pending/link': problem(404, 'pending_not_found'),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: en['oidc.continue.link.action'] }));
    await userEvent.type(screen.getByLabelText(en['oidc.continue.link.password']), 'pw');
    await userEvent.click(screen.getByRole('button', { name: en['oidc.continue.link.submit'] }));
    expect(await screen.findByText(en['oidc.continue.expired'], { exact: true })).toBeVisible();
    expect(screen.getByLabelText(en['login.password'])).toBeVisible();
  });

  it('reports an already signed-in browser with a reload path', async () => {
    installFakeServer({ ...signedOut, ...provider, 'GET /api/v1/auth/oidc/pending': problem(409, 'already_signed_in') });
    renderApp();
    expect(await screen.findByText(en['oidc.continue.alreadySignedIn'], { exact: true })).toBeVisible();
    expect(screen.getByRole('link', { name: en['oidc.continue.reload'] })).toHaveAttribute('href', '/');
  });

  it('renders the Czech interstitial', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer({ ...signedOut, ...provider, 'GET /api/v1/auth/oidc/pending': pendingStatus() });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Dokončete přihlášení přes Pocket ID' })).toBeVisible();
    expect(screen.getByRole('button', { name: 'Vytvořit nový účet v Tendu' })).toBeVisible();
  });
});

describe('signed-in OIDC notices', () => {
  it('consumes the accepted-invitation fragment once and dismisses the notice', async () => {
    window.history.replaceState(null, '', '/#oidcNotice=invitation_accepted');
    installFakeServer({ ...signedInHome, ...provider });
    renderApp();
    expect(await screen.findByText(en['home.notice.invitationAccepted'], { exact: true })).toBeVisible();
    expect(window.location.pathname).toBe('/');
    expect(window.location.hash).toBe('');
    await userEvent.click(screen.getByRole('button', { name: en['notice.dismiss'] }));
    expect(screen.queryByText(en['home.notice.invitationAccepted'], { exact: true })).not.toBeInTheDocument();
  });

  it('shows an invalid-invitation fragment as a calm notice', async () => {
    window.history.replaceState(null, '', '/#oidcNotice=invitation_invalid');
    installFakeServer({ ...signedInHome, ...provider });
    renderApp();
    expect(await screen.findByText(en['home.notice.invitationInvalid'], { exact: true })).toBeVisible();
  });

  it('ignores an unknown notice value and leaves the URL untouched', async () => {
    window.history.replaceState(null, '', '/#oidcNotice=something_else');
    installFakeServer({ ...signedInHome, ...provider });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible();
    expect(screen.queryByText(en['home.notice.invitationAccepted'], { exact: true })).not.toBeInTheDocument();
    expect(window.location.hash).toBe('#oidcNotice=something_else');
  });
});

describe('discarding a first-login account', () => {
  const joinScreen = {
    'GET /api/v1/session': json(200, session(false)),
    ...provider,
  };
  const openDiscardDialog = async () => {
    expect(await screen.findByRole('heading', { level: 1, name: 'Join your household by invitation' })).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: en['home.noHousehold.discard.action'] }));
    const dialog = await screen.findByRole('dialog');
    expect(within(dialog).getByText(en['home.noHousehold.discard.confirmBody'], { exact: true })).toBeVisible();
    return dialog;
  };

  it('removes the account, signs out and reopens the continue interstitial', async () => {
    window.history.replaceState(null, '', '/');
    const fake = installFakeServer({
      ...joinScreen,
      'POST /api/v1/auth/oidc/account/discard': json(200, { expiresAt: '2026-11-10T12:30:00Z', emailVerified: false, invitation: false }),
      'GET /api/v1/auth/oidc/pending': pendingStatus(),
    });
    renderApp();
    const dialog = await openDiscardDialog();
    await userEvent.click(within(dialog).getByRole('button', { name: en['home.noHousehold.discard.confirm'] }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Finish signing in with Pocket ID' })).toBeVisible();
    expect(window.location.pathname).toBe('/login');
    const discard = fake.requests.find((request) => request.path === '/api/v1/auth/oidc/account/discard');
    expect(discard?.headers.get('Origin')).toBe(window.location.origin);
  });

  it('explains a refused discard without pretending the account is gone', async () => {
    window.history.replaceState(null, '', '/');
    installFakeServer({ ...joinScreen, 'POST /api/v1/auth/oidc/account/discard': problem(409, 'account_not_discardable') });
    renderApp();
    const dialog = await openDiscardDialog();
    await userEvent.click(within(dialog).getByRole('button', { name: en['home.noHousehold.discard.confirm'] }));
    expect(await screen.findByText(en['home.noHousehold.discard.notPossible'], { exact: true })).toBeVisible();
    expect(screen.getByRole('heading', { level: 1, name: 'Join your household by invitation' })).toBeVisible();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });

  it('keeps the account when the confirmation is dismissed', async () => {
    window.history.replaceState(null, '', '/');
    const fake = installFakeServer(joinScreen);
    renderApp();
    const dialog = await openDiscardDialog();
    await userEvent.click(within(dialog).getByRole('button', { name: en['home.noHousehold.discard.cancel'] }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(fake.requests.some((request) => request.path === '/api/v1/auth/oidc/account/discard')).toBe(false);
  });
});
