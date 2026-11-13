import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react';
import { StrictMode } from 'react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { en } from '../../i18n/en';
import { cs } from '../../i18n/cs';
import { HOUSEHOLD_ID, emptyHome, household, installFakeServer, json, session } from '../../test/fakeServer';
import { browserNavigation } from './oidcApi';
import { LoginScreen } from './LoginScreen';

const problem = (status: number, code?: string, extra: Record<string, unknown> = {}) =>
  json(status, { type: 'about:blank', title: 'x', status, ...(code ? { code } : {}), ...extra }, { 'Content-Type': 'application/problem+json' });
const signedOut = { 'GET /api/v1/session': problem(401), 'GET /api/v1/auth/setup': json(200, { required: false }) };
const signedIn = { 'GET /api/v1/session': json(200, session()), [`GET /api/v1/households/${HOUSEHOLD_ID}`]: json(200, household), ...emptyHome };
const enabled = { 'GET /api/v1/auth/oidc': json(200, { enabled: true, displayName: 'Pocket ID' }) };
const authorizationUrl = 'https://id.example/authorize?client_id=tendo&state=opaque';

function renderApp() {
  return render(<I18nProvider><App /></I18nProvider>);
}

let assign: ReturnType<typeof vi.spyOn>;
beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
  assign = vi.spyOn(browserNavigation, 'assign').mockImplementation(() => {});
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
  window.history.replaceState(null, '', '/');
});

describe('sign in with the configured provider', () => {
  it('is hidden when the provider is disabled and the password form still works', async () => {
    installFakeServer({ ...signedOut, 'GET /api/v1/auth/oidc': json(200, { enabled: false }) });
    renderApp();
    await screen.findByLabelText('Username');
    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalledTimes(3));
    expect(screen.queryByRole('button', { name: /Sign in with/ })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeEnabled();
  });

  it('shows a quiet recovery notice after a 503 while password sign-in still works', async () => {
    const fake = installFakeServer({
      ...signedOut,
      'GET /api/v1/auth/oidc': problem(503),
      'POST /api/v1/session': json(201, session()),
      [`GET /api/v1/households/${HOUSEHOLD_ID}`]: json(200, household),
      ...emptyHome,
    });
    renderApp();
    const noticeText = await screen.findByText(en['login.oidc.loadError'], { exact: true });
    expect(noticeText).toHaveTextContent('Password sign-in is still available.');
    const notice = noticeText.closest('[role="status"]');
    expect(notice).not.toBeNull();
    expect(notice).not.toHaveAttribute('role', 'alert');
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Sign in with/ })).not.toBeInTheDocument();
    const signIn = screen.getByRole('button', { name: /^Sign in$/ });
    expect(signIn).toBeEnabled();
    await userEvent.type(screen.getByLabelText('Username'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'password');
    await userEvent.click(signIn);
    await waitFor(() => expect(fake.requests.some((request) => request.path === '/api/v1/session' && request.method === 'POST')).toBe(true));
    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toBeInTheDocument();
  });

  it('retries a network failure, shows the provider, and moves focus from Retry', async () => {
    const fake = installFakeServer({
      ...signedOut,
      'GET /api/v1/auth/oidc': () => fake.requests.filter((request) => request.path === '/api/v1/auth/oidc').length === 1
        ? Promise.reject(new TypeError('network failure'))
        : json(200, { enabled: true, displayName: 'Pocket ID' }),
    });
    renderApp();
    const retry = await screen.findByRole('button', { name: 'Try again' });
    await userEvent.click(retry);
    const providerButton = await screen.findByRole('button', { name: 'Sign in with Pocket ID' });
    expect(screen.getByRole('status')).toBeEmptyDOMElement();
    await waitFor(() => expect(providerButton).toHaveFocus());
    expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc')).toHaveLength(2);
  });

  it('does not steal focus when Retry settles after focus moved to Password', async () => {
    let release!: (response: Response) => void;
    const delayed = new Promise<Response>((resolve) => { release = resolve; });
    const holder: { fake?: ReturnType<typeof installFakeServer> } = {};
    const fake = installFakeServer({
      ...signedOut,
      'GET /api/v1/auth/oidc': () => holder.fake!.requests.filter((request) => request.path === '/api/v1/auth/oidc').length === 1
        ? problem(503)
        : delayed,
    });
    holder.fake = fake;
    renderApp();
    const retry = await screen.findByRole('button', { name: 'Try again' });
    await userEvent.click(retry);
    await waitFor(() => expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc')).toHaveLength(2));
    const password = screen.getByLabelText('Password');
    password.focus();
    await act(async () => { release(json(200, { enabled: true, displayName: 'Pocket ID' })); });
    const providerButton = await screen.findByRole('button', { name: 'Sign in with Pocket ID' });
    expect(password).toHaveFocus();
    expect(providerButton).toBeVisible();
  });

  it('does not send another request when Retry is activated while a retry is pending', async () => {
    let release!: (response: Response) => void;
    const delayed = new Promise<Response>((resolve) => { release = resolve; });
    const holder: { fake?: ReturnType<typeof installFakeServer> } = {};
    const fake = installFakeServer({
      ...signedOut,
      'GET /api/v1/auth/oidc': () => holder.fake!.requests.filter((request) => request.path === '/api/v1/auth/oidc').length === 1
        ? problem(503)
        : delayed,
    });
    holder.fake = fake;
    renderApp();
    const retry = await screen.findByRole('button', { name: 'Try again' });
    await userEvent.click(retry);
    await waitFor(() => expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc')).toHaveLength(2));
    expect(retry).toHaveAttribute('aria-disabled', 'true');
    expect(retry).toHaveFocus();
    await userEvent.click(retry);
    await userEvent.keyboard('{Enter}');
    expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc')).toHaveLength(2);
    expect(retry).toHaveFocus();
    await act(async () => { release(problem(503)); });
    expect(await screen.findByText(en['login.oidc.loadError'], { exact: true })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Try again' })).toHaveFocus();
  });

  it('retry hides the notice when the provider is disabled', async () => {
    const fake = installFakeServer({
      ...signedOut,
      'GET /api/v1/auth/oidc': () => fake.requests.filter((request) => request.path === '/api/v1/auth/oidc').length === 1
        ? Promise.reject(new TypeError('network failure'))
        : json(200, { enabled: false }),
    });
    renderApp();
    const retry = await screen.findByRole('button', { name: 'Try again' });
    await userEvent.click(retry);
    await waitFor(() => expect(screen.getByRole('status')).toBeEmptyDOMElement());
    expect(screen.getByLabelText('Password')).toHaveFocus();
  });

  it('does not show a notice when provider sign-in is disabled', async () => {
    installFakeServer({ ...signedOut, 'GET /api/v1/auth/oidc': json(200, { enabled: false }) });
    renderApp();
    await screen.findByLabelText('Username');
    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalledTimes(3));
    expect(screen.getByRole('status')).toBeEmptyDOMElement();
  });

  it('ignores a stale initial status response after a newer StrictMode request', async () => {
    let release!: (response: Response) => void;
    const delayed = new Promise<Response>((resolve) => { release = resolve; });
    const holder: { fake?: ReturnType<typeof installFakeServer> } = {};
    const fake = installFakeServer({
      ...signedOut,
      'GET /api/v1/auth/oidc': () => holder.fake!.requests.filter((request) => request.path === '/api/v1/auth/oidc').length === 1
        ? delayed
        : json(200, { enabled: false }),
    });
    holder.fake = fake;
    render(<I18nProvider><StrictMode><App /></StrictMode></I18nProvider>);
    await waitFor(() => expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc')).toHaveLength(2));
    await act(async () => { release(json(200, { enabled: true, displayName: 'Stale Provider' })); });
    await waitFor(() => expect(screen.queryByRole('button', { name: /Sign in with/ })).not.toBeInTheDocument());
    expect(screen.queryByText(en['login.oidc.loadError'], { exact: true })).not.toBeInTheDocument();
  });

  it('renders the status recovery notice in Czech', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer({ ...signedOut, 'GET /api/v1/auth/oidc': problem(503) });
    renderApp();
    expect(await screen.findByText(cs['login.oidc.loadError'], { exact: true })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Zkusit znovu' })).toBeInTheDocument();
  });

  it('posts exactly {purpose:"login"} once and navigates to the provider', async () => {
    let release!: () => void;
    const gate = new Promise<void>((resolve) => { release = resolve; });
    const fake = installFakeServer({
      ...signedOut,
      ...enabled,
      'POST /api/v1/auth/oidc/start': async () => { await gate; return json(200, { authorizationUrl }); },
    });
    renderApp();
    const button = await screen.findByRole('button', { name: 'Sign in with Pocket ID' });
    await userEvent.click(button);
    await userEvent.click(button);
    expect(screen.getByRole('button', { name: 'Opening Pocket ID…' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeDisabled();
    release();
    await waitFor(() => expect(assign).toHaveBeenCalledWith(authorizationUrl));
    const starts = fake.requests.filter((r) => r.path === '/api/v1/auth/oidc/start');
    expect(starts).toHaveLength(1);
    expect(JSON.parse(starts[0]!.body)).toEqual({ purpose: 'login' });
  });

  it('ignores a delayed provider login response after unmount', async () => {
    let release!: (response: Response) => void;
    const delayed = new Promise<Response>((resolve) => { release = resolve; });
    installFakeServer({ ...signedOut, ...enabled, 'POST /api/v1/auth/oidc/start': () => delayed });
    const view = render(<I18nProvider><LoginScreen onSignedIn={vi.fn()} /></I18nProvider>);
    await userEvent.click(await screen.findByRole('button', { name: 'Sign in with Pocket ID' }));
    view.unmount();
    await act(async () => { release(json(200, { authorizationUrl })); });
    expect(assign).not.toHaveBeenCalled();
  });

  it('ignores a delayed password login response after unmount', async () => {
    let release!: (response: Response) => void;
    const delayed = new Promise<Response>((resolve) => { release = resolve; });
    const onSignedIn = vi.fn();
    installFakeServer({ ...signedOut, ...enabled, 'POST /api/v1/session': () => delayed });
    const view = render(<I18nProvider><LoginScreen onSignedIn={onSignedIn} /></I18nProvider>);
    await userEvent.type(await screen.findByLabelText('Username'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'pw');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    view.unmount();
    await act(async () => { release(json(201, session())); });
    expect(onSignedIn).not.toHaveBeenCalled();
  });

  it('a provider outage says so, keeps the password path, and refocuses the button', async () => {
    installFakeServer({ ...signedOut, ...enabled, 'POST /api/v1/auth/oidc/start': problem(503, 'oidc_unavailable') });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Sign in with Pocket ID' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Pocket ID can’t be reached right now. You can still use your password.');
    expect(await screen.findByRole('alert')).toHaveTextContent('You can still use your password.');
    expect(screen.getByRole('button', { name: 'Sign in with Pocket ID' })).toHaveFocus();
    expect(assign).not.toHaveBeenCalled();
  });

  it('a database outage (503 unavailable) is not blamed on the provider', async () => {
    installFakeServer({ ...signedOut, ...enabled, 'POST /api/v1/auth/oidc/start': problem(503, 'unavailable') });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Sign in with Pocket ID' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Tendo can’t be reached right now. Try again.');
  });
});

describe('callback results', () => {
  it('keeps the cancelled dictionary entry equal to the approved copy', () => {
    expect(en['oidc.error.cancelled']).toBe('Sign-in was cancelled. You can try again or use your password.');
  });

  it.each([
    ['identity_not_linked', en['oidc.error.identity_not_linked']],
    ['identity_conflict', en['oidc.error.identity_conflict']],
    ['cancelled', en['oidc.error.cancelled']],
    ['session_changed', en['oidc.error.session_changed']],
    ['<script>', en['oidc.error.unknown']],
  ])('/login#oidcError=%s shows a calm notice and scrubs the fragment', async (code, message) => {
    window.history.replaceState(null, '', `/login#oidcError=${code}`);
    installFakeServer({ ...signedOut, ...enabled });
    renderApp();
    expect(await screen.findByText(message)).toBeInTheDocument();
    expect(window.location.pathname).toBe('/login');
    expect(window.location.hash).toBe('');
    expect(document.body.textContent).not.toContain('<script>');
    expect(sessionStorage.length).toBe(0);
  });

  it('a legacy identity_not_linked notice no longer points at Account', async () => {
    window.history.replaceState(null, '', '/login#oidcError=identity_not_linked');
    installFakeServer({ ...signedOut, ...enabled });
    renderApp();
    const main = await screen.findByRole('main');
    expect(await within(main).findByText(en['oidc.error.identity_not_linked'], { exact: true })).toBeVisible();
    expect(within(main).queryByText(/Account/)).not.toBeInTheDocument();
  });

  it('explains an identity that cannot be linked automatically in English and Czech', async () => {
    window.history.replaceState(null, '', '/login#oidcError=identity_conflict');
    installFakeServer({ ...signedOut, ...enabled });
    renderApp();
    expect(await screen.findByText(en['oidc.error.identity_conflict'], { exact: true })).toBeVisible();
    cleanup();
    localStorage.setItem('tendo.locale', 'cs');
    window.history.replaceState(null, '', '/login#oidcError=identity_conflict');
    installFakeServer({ ...signedOut, ...enabled });
    renderApp();
    expect(await screen.findByText(cs['oidc.error.identity_conflict'], { exact: true })).toBeVisible();
  });

  it('a signed-out /account fragment is scrubbed and shows the ordinary login form without a notice', async () => {
    window.history.replaceState(null, '', '/account#oidcError=identity_conflict');
    installFakeServer({ ...signedOut, ...enabled });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Welcome back' })).toBeInTheDocument();
    expect(screen.queryByText(en['oidc.error.identity_conflict'], { exact: true })).not.toBeInTheDocument();
    expect(screen.getByRole('status')).toBeEmptyDOMElement();
    expect(window.location.hash).toBe('');
  });

  it('a signed-in visitor at /login lands on home', async () => {
    window.history.replaceState(null, '', '/login');
    installFakeServer({ ...signedIn, ...enabled });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toBeInTheDocument();
    expect(window.location.pathname).toBe('/');
  });

  it('a successful login callback (/) lands on home without any notice', async () => {
    installFakeServer({ ...signedIn, ...enabled });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toBeInTheDocument();
    expect(screen.queryByText(/sign-in/i)).not.toBeInTheDocument();
  });
});

describe('account screen', () => {
  it('offers an in-page language switch in English and Czech', async () => {
    for (const [locale, title, body, activeLabel, nextLabel] of [
        ['en', 'Language', 'Choose the language you’d like to use in Tendo.', 'English', 'Čeština'],
      ['cs', 'Jazyk', 'Vyberte si jazyk, ve kterém chcete Tendo používat.', 'Čeština', 'English'],
    ]) {
      localStorage.setItem('tendo.locale', locale);
      window.history.replaceState(null, '', '/account');
      installFakeServer({ ...signedIn, 'GET /api/v1/auth/oidc': json(200, { enabled: false }) });
      renderApp();
      await waitFor(() => expect(screen.getByRole('main').querySelector('h1')?.textContent).toBe(locale === 'cs' ? 'Váš účet' : 'Your account'));
      const main = screen.getByRole('main');
      expect(await within(main).findByRole('heading', { name: title })).toBeVisible();
      expect(within(main).getByText(body)).toBeVisible();
      const languageGroup = within(main).getByRole('group', { name: locale === 'cs' ? 'Jazyk' : 'Language' });
      expect(within(languageGroup).getByRole('button', { name: activeLabel })).toHaveAttribute('aria-pressed', 'true');
      await userEvent.click(within(languageGroup).getByRole('button', { name: nextLabel }));
      expect(document.documentElement.lang).toBe(locale === 'cs' ? 'en' : 'cs');
      expect(localStorage.getItem('tendo.locale')).toBe(locale === 'cs' ? 'en' : 'cs');
      expect(within(main).getByRole('heading', { name: locale === 'cs' ? 'Language' : 'Jazyk' })).toBeVisible();
      const switchedLanguage = within(main).getByRole('group', { name: locale === 'cs' ? 'Language' : 'Jazyk' });
      expect(within(switchedLanguage).getByRole('button', { name: nextLabel })).toHaveAttribute('aria-pressed', 'true');
      cleanup();
      localStorage.removeItem('tendo.locale');
    }
  });

  it('shows the signed-in identity and settings without manual sign-in linking or legacy requests', async () => {
    // The identity and start fixtures are deliberately available: the assertions below prove Account never calls them.
    const fake = installFakeServer({
      ...signedIn,
      ...enabled,
      'GET /api/v1/auth/oidc/identity': json(200, { linked: false }),
      'POST /api/v1/auth/oidc/start': json(200, { authorizationUrl }),
    });
    window.history.replaceState(null, '', '/account');
    renderApp();
    expect(await screen.findByText('You’re signed in as anna.')).toBeVisible();
    const main = screen.getByRole('main');
    expect(within(main).getByRole('heading', { level: 2, name: 'Appearance' })).toBeVisible();
    expect(within(main).getByRole('heading', { level: 2, name: 'Language' })).toBeVisible();
    expect(within(main).queryByLabelText('Current password')).not.toBeInTheDocument();
    expect(within(main).queryByRole('button', { name: /Connect/ })).not.toBeInTheDocument();
    expect(within(main).queryByRole('heading', { name: /sign-in with/i })).not.toBeInTheDocument();
    // The provider is enabled, yet the retired identity lookup and link start are never issued.
    expect(fake.requests.some((request) => request.path === '/api/v1/auth/oidc')).toBe(false);
    expect(fake.requests.some((request) => request.path === '/api/v1/auth/oidc/identity')).toBe(false);
    expect(fake.requests.some((request) => request.path === '/api/v1/auth/oidc/start')).toBe(false);
  });

  it('/account#oidc=connected scrubs the fragment and shows the normal account screen without a notice', async () => {
    window.history.replaceState(null, '', '/account#oidc=connected');
    const fake = installFakeServer({ ...signedIn, ...enabled });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Your account' })).toBeVisible();
    await waitFor(() => expect(window.location.hash).toBe(''));
    expect(window.location.pathname).toBe('/account');
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
    expect(screen.queryByText(/Connected\./)).not.toBeInTheDocument();
    expect(fake.requests.some((request) => request.path === '/api/v1/auth/oidc/identity')).toBe(false);
  });

  it('a signed-in /account error fragment is scrubbed without a callback notice', async () => {
    window.history.replaceState(null, '', '/account#oidcError=identity_conflict');
    installFakeServer({ ...signedIn, ...enabled });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Your account' })).toBeVisible();
    await waitFor(() => expect(window.location.hash).toBe(''));
    expect(screen.queryByText(en['oidc.error.identity_conflict'], { exact: true })).not.toBeInTheDocument();
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('renders Czech', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer({ ...signedOut, ...enabled });
    renderApp();
    expect(await screen.findByRole('button', { name: 'Přihlásit se přes Pocket ID' })).toBeInTheDocument();
  });
});
