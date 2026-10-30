import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { StrictMode } from 'react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
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
    await screen.findByLabelText('Login');
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
    const noticeText = await screen.findByText('Other sign-in options couldn’t be loaded.');
    const notice = noticeText.closest('[role="status"]');
    expect(notice).not.toBeNull();
    expect(notice).not.toHaveAttribute('role', 'alert');
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Sign in with/ })).not.toBeInTheDocument();
    const signIn = screen.getByRole('button', { name: /^Sign in$/ });
    expect(signIn).toBeEnabled();
    await userEvent.type(screen.getByLabelText('Login'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'password');
    await userEvent.click(signIn);
    await waitFor(() => expect(fake.requests.some((request) => request.path === '/api/v1/session' && request.method === 'POST')).toBe(true));
    expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeInTheDocument();
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
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
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
    expect(await screen.findByText('Other sign-in options couldn’t be loaded.')).toBeInTheDocument();
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
    await waitFor(() => expect(screen.queryByRole('status')).not.toBeInTheDocument());
    expect(screen.getByLabelText('Password')).toHaveFocus();
  });

  it('does not show a notice when provider sign-in is disabled', async () => {
    installFakeServer({ ...signedOut, 'GET /api/v1/auth/oidc': json(200, { enabled: false }) });
    renderApp();
    await screen.findByLabelText('Login');
    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalledTimes(3));
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
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
    expect(screen.queryByText('Other sign-in options couldn’t be loaded.')).not.toBeInTheDocument();
  });

  it('renders the status recovery notice in Czech', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer({ ...signedOut, 'GET /api/v1/auth/oidc': problem(503) });
    renderApp();
    expect(await screen.findByText('Další možnosti přihlášení se nepodařilo načíst.')).toBeInTheDocument();
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
    const onSignedIn = vi.fn();
    installFakeServer({ ...signedOut, ...enabled, 'POST /api/v1/auth/oidc/start': () => delayed });
    const view = render(<I18nProvider><LoginScreen onSignedIn={onSignedIn} /></I18nProvider>);
    await userEvent.click(await screen.findByRole('button', { name: 'Sign in with Pocket ID' }));
    view.unmount();
    await act(async () => { release(json(200, { authorizationUrl })); });
    expect(assign).not.toHaveBeenCalled();
    expect(onSignedIn).not.toHaveBeenCalled();
  });

  it('ignores a delayed password login response after unmount', async () => {
    let release!: (response: Response) => void;
    const delayed = new Promise<Response>((resolve) => { release = resolve; });
    const onSignedIn = vi.fn();
    installFakeServer({ ...signedOut, ...enabled, 'POST /api/v1/session': () => delayed });
    const view = render(<I18nProvider><LoginScreen onSignedIn={onSignedIn} /></I18nProvider>);
    await userEvent.type(await screen.findByLabelText('Login'), 'anna');
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
  it.each([
    ['identity_not_linked', 'This sign-in isn’t connected to a Tendo account yet. Sign in with your password, then connect it from Account.'],
    ['cancelled', 'Sign-in was cancelled. You can try again or use your password.'],
    ['session_changed', 'You signed in or out in another tab while this was in progress. Please start again.'],
    ['<script>', 'Something went wrong with that sign-in. Please try again.'],
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

  it('a signed-out /account error lands on login with the notice', async () => {
    window.history.replaceState(null, '', '/account#oidcError=identity_conflict');
    installFakeServer({ ...signedOut, ...enabled });
    renderApp();
    expect(await screen.findByText('That sign-in is already connected to another Tendo account.')).toBeInTheDocument();
    expect(screen.getByLabelText('Login')).toBeInTheDocument();
    expect(window.location.hash).toBe('');
  });

  it('a signed-in visitor at /login lands on home', async () => {
    window.history.replaceState(null, '', '/login');
    installFakeServer({ ...signedIn, ...enabled });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeInTheDocument();
    expect(window.location.pathname).toBe('/');
  });

  it('a successful login callback (/) lands on home without any notice', async () => {
    installFakeServer({ ...signedIn, ...enabled });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeInTheDocument();
    expect(screen.queryByText(/sign-in/i)).not.toBeInTheDocument();
  });
});

describe('account screen', () => {
  it('explains where to change language in English and Czech when sign-in linking is unavailable', async () => {
    for (const [locale, title, body] of [
      ['en', 'Language', 'Switch between English and Czech at the bottom of any page.'],
      ['cs', 'Jazyk', 'Mezi češtinou a angličtinou přepnete dole na každé stránce.'],
    ]) {
      localStorage.setItem('tendo.locale', locale);
      window.history.replaceState(null, '', '/account');
      installFakeServer({ ...signedIn, 'GET /api/v1/auth/oidc': json(200, { enabled: false }) });
      window.history.replaceState(null, '', '/account');
      renderApp();
      await waitFor(() => expect(screen.getByRole('main').querySelector('h1')?.textContent).toBe(locale === 'cs' ? 'Váš účet' : 'Your account'));
      const main = screen.getByRole('main');
      expect(await within(main).findByRole('heading', { name: title })).toBeVisible();
      expect(within(main).getByText(body)).toBeVisible();
      cleanup();
      localStorage.removeItem('tendo.locale');
    }
  });
  it('opens from home, explains, and links with exactly {purpose:"link",currentPassword}', async () => {
    const fake = installFakeServer({
      ...signedIn,
      ...enabled,
      'GET /api/v1/auth/oidc/identity': json(200, { linked: false }),
      'POST /api/v1/auth/oidc/start': json(200, { authorizationUrl }),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('link', { name: 'Your account' }));
    expect(window.location.pathname).toBe('/account');
    expect(await screen.findByRole('heading', { level: 1, name: 'Your account' })).toHaveFocus();
    expect(screen.getByText('You’re signed in as anna.')).toBeInTheDocument();
    expect(await screen.findByText('Connect Pocket ID to sign in without typing your password. Your password keeps working too.')).toBeInTheDocument();
    const password = screen.getByLabelText('Current password');
    expect(password).toHaveAttribute('autocomplete', 'current-password');
    await userEvent.type(password, 'correct horse battery');
    await userEvent.click(screen.getByRole('button', { name: 'Connect Pocket ID' }));
    await waitFor(() => expect(assign).toHaveBeenCalledWith(authorizationUrl));
    const start = fake.requests.find((r) => r.path === '/api/v1/auth/oidc/start')!;
    expect(JSON.parse(start.body)).toEqual({ purpose: 'link', currentPassword: 'correct horse battery' });
  });

  it('sends only one link-start request for synchronous duplicate submits', async () => {
    let release!: (response: Response) => void;
    const delayed = new Promise<Response>((resolve) => { release = resolve; });
    const fake = installFakeServer({
      ...signedIn,
      ...enabled,
      'GET /api/v1/auth/oidc/identity': json(200, { linked: false }),
      'POST /api/v1/auth/oidc/start': () => delayed,
    });
    window.history.replaceState(null, '', '/account');
    renderApp();
    const password = await screen.findByLabelText('Current password');
    await userEvent.type(password, 'pw');
    const form = password.closest('form');
    expect(form).not.toBeNull();
    act(() => {
      fireEvent.submit(form!);
      fireEvent.submit(form!);
    });
    await waitFor(() => expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc/start')).toHaveLength(1));
    await act(async () => { release(json(200, { authorizationUrl })); });
    expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc/start')).toHaveLength(1);
  });

  it('retries a status network failure and shows the connect form', async () => {
    const fake = installFakeServer({
      ...signedIn,
      'GET /api/v1/auth/oidc': (req) => {
        const count = fake.requests.filter((entry) => entry.path === req.path).length;
        return count === 1 ? Promise.reject(new TypeError('network failure')) : json(200, { enabled: true, displayName: 'Pocket ID' });
      },
      'GET /api/v1/auth/oidc/identity': json(200, { linked: false }),
    });
    window.history.replaceState(null, '', '/account');
    renderApp();
    expect(await screen.findByText('Couldn’t check the connection right now.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByLabelText('Current password')).toBeInTheDocument();
    expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc')).toHaveLength(2);
    expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc/identity')).toHaveLength(1);
  });

  it('does not start identity lookup from a stale StrictMode status response', async () => {
    let release!: (response: Response) => void;
    const delayed = new Promise<Response>((resolve) => { release = resolve; });
    const fake = installFakeServer({
      ...signedIn,
      'GET /api/v1/auth/oidc': (req) => {
        const count = fake.requests.filter((entry) => entry.path === req.path).length;
        return count === 1 ? delayed : json(200, { enabled: true, displayName: 'Pocket ID' });
      },
      'GET /api/v1/auth/oidc/identity': json(200, { linked: false }),
    });
    window.history.replaceState(null, '', '/account');
    render(<I18nProvider><StrictMode><App /></StrictMode></I18nProvider>);
    await screen.findByLabelText('Current password');
    await waitFor(() => expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc')).toHaveLength(2));
    await act(async () => { release(json(200, { enabled: true, displayName: 'Stale Pocket ID' })); });
    expect(screen.getByLabelText('Current password')).toBeInTheDocument();
    expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc/identity')).toHaveLength(1);
  });

  it('retries an identity 503 and shows the linked state', async () => {
    const fake = installFakeServer({
      ...signedIn,
      ...enabled,
      'GET /api/v1/auth/oidc/identity': (req) => {
        const count = fake.requests.filter((entry) => entry.path === req.path).length;
        return json(count === 1 ? 503 : 200, count === 1 ? { title: 'unavailable' } : { linked: true });
      },
    });
    window.history.replaceState(null, '', '/account');
    renderApp();
    expect(await screen.findByText('Couldn’t check the connection right now.')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByText('Your account is connected to Pocket ID. You can sign in with it or with your password.')).toBeInTheDocument();
    expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc')).toHaveLength(2);
    expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc/identity')).toHaveLength(2);
  });

  it('ignores an earlier retry response that arrives after a newer retry', async () => {
    let release!: (response: Response) => void;
    const delayed = new Promise<Response>((resolve) => { release = resolve; });
    const fake = installFakeServer({
      ...signedIn,
      ...enabled,
      'GET /api/v1/auth/oidc/identity': (req) => {
        const count = fake.requests.filter((entry) => entry.path === req.path).length;
        if (count === 1) return problem(503, 'unavailable');
        if (count === 2) return delayed;
        return json(200, { linked: true });
      },
    });
    window.history.replaceState(null, '', '/account');
    renderApp();
    await screen.findByText('Couldn’t check the connection right now.');
    const retry = screen.getByRole('button', { name: 'Try again' });
    await userEvent.click(retry);
    await waitFor(() => expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc/identity')).toHaveLength(2));
    await userEvent.click(retry);
    await waitFor(() => expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc/identity')).toHaveLength(3));
    expect(await screen.findByText('Your account is connected to Pocket ID. You can sign in with it or with your password.')).toBeInTheDocument();
    release(json(200, { linked: false }));
    await waitFor(() => expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc/identity')).toHaveLength(3));
    await waitFor(() => expect(screen.getByText('Your account is connected to Pocket ID. You can sign in with it or with your password.')).toBeInTheDocument());
    expect(screen.queryByLabelText('Current password')).not.toBeInTheDocument();
    expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc')).toHaveLength(3);
    expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc/identity')).toHaveLength(3);
  });

  it('keeps focus on Retry during a retry and after a subsequent failure', async () => {
    let release!: (response: Response) => void;
    const delayed = new Promise<Response>((resolve) => { release = resolve; });
    const fake = installFakeServer({
      ...signedIn,
      ...enabled,
      'GET /api/v1/auth/oidc/identity': (req) => {
        const count = fake.requests.filter((entry) => entry.path === req.path).length;
        return count === 1 ? problem(503, 'unavailable') : count === 2 ? delayed : json(200, { linked: false });
      },
    });
    window.history.replaceState(null, '', '/account');
    renderApp();
    const retry = await screen.findByRole('button', { name: 'Try again' });
    await userEvent.click(retry);
    await waitFor(() => expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc/identity')).toHaveLength(2));
    expect(screen.getByRole('button', { name: 'Try again' })).toHaveFocus();
    expect(document.activeElement).not.toBe(document.body);
    release(problem(503, 'unavailable'));
    expect(await screen.findByText('Couldn’t check the connection right now.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Try again' })).toHaveFocus();
  });

  it('moves focus from Retry to the loaded section after a successful retry', async () => {
    const fake = installFakeServer({
      ...signedIn,
      ...enabled,
      'GET /api/v1/auth/oidc/identity': (req) => {
        const count = fake.requests.filter((entry) => entry.path === req.path).length;
        return count === 1 ? problem(503, 'unavailable') : json(200, { linked: true });
      },
    });
    window.history.replaceState(null, '', '/account');
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Try again' }));
    const heading = await screen.findByRole('heading', { level: 2, name: 'Sign-in with Pocket ID' });
    await waitFor(() => expect(heading).toHaveFocus());
  });

  it('moves focus to the signed-in line when a retry finds sign-in links turned off', async () => {
    const fake = installFakeServer({
      ...signedIn,
      'GET /api/v1/auth/oidc': (req) => {
        const count = fake.requests.filter((entry) => entry.path === req.path).length;
        return count === 1 ? problem(503, 'unavailable') : json(200, { enabled: false });
      },
    });
    window.history.replaceState(null, '', '/account');
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Try again' }));
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument());
    expect(screen.getByText(/You’re signed in as/)).toHaveFocus();
  });

  it('does not take focus after a retry when the user has moved elsewhere', async () => {
    let release!: (response: Response) => void;
    const delayed = new Promise<Response>((resolve) => { release = resolve; });
    const fake = installFakeServer({
      ...signedIn,
      ...enabled,
      'GET /api/v1/auth/oidc/identity': (req) => {
        const count = fake.requests.filter((entry) => entry.path === req.path).length;
        return count === 1 ? problem(503, 'unavailable') : delayed;
      },
    });
    window.history.replaceState(null, '', '/account');
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Try again' }));
    await waitFor(() => expect(fake.requests.filter((request) => request.path === '/api/v1/auth/oidc/identity')).toHaveLength(2));
    const signOut = screen.getByRole('button', { name: 'Sign out' });
    signOut.focus();
    release(json(200, { linked: true }));
    await screen.findByRole('heading', { level: 2, name: 'Sign-in with Pocket ID' });
    expect(signOut).toHaveFocus();
  });

  it('renders the Retry label in Czech', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer({ ...signedIn, 'GET /api/v1/auth/oidc': problem(503) });
    window.history.replaceState(null, '', '/account');
    renderApp();
    expect(await screen.findByRole('button', { name: 'Zkusit znovu' })).toBeInTheDocument();
  });

  it('a wrong current password marks the field, clears it, and focuses it', async () => {
    installFakeServer({
      ...signedIn,
      ...enabled,
      'GET /api/v1/auth/oidc/identity': json(200, { linked: false }),
      'POST /api/v1/auth/oidc/start': problem(401, 'invalid_credentials'),
    });
    window.history.replaceState(null, '', '/account');
    renderApp();
    const password = await screen.findByLabelText('Current password');
    await userEvent.type(password, 'nope{Enter}');
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('That password isn’t right.');
    expect(password).toHaveAttribute('aria-invalid', 'true');
    expect(password).toHaveAttribute('aria-describedby', alert.id);
    expect(password).toHaveValue('');
    expect(password).toHaveFocus();
    expect(assign).not.toHaveBeenCalled();
  });

  it('does not redirect after leaving Account while a link start is pending', async () => {
    let release!: (response: Response) => void;
    const delayed = new Promise<Response>((resolve) => { release = resolve; });
    installFakeServer({
      ...signedIn,
      ...enabled,
      'GET /api/v1/auth/oidc/identity': json(200, { linked: false }),
      'POST /api/v1/auth/oidc/start': () => delayed,
    });
    window.history.replaceState(null, '', '/account');
    renderApp();
    await userEvent.type(await screen.findByLabelText('Current password'), 'pw');
    await userEvent.click(screen.getByRole('button', { name: 'Connect Pocket ID' }));
    await userEvent.click(screen.getByRole('link', { name: /^Home$/ }));
    await screen.findByRole('heading', { level: 1, name: 'Veselí' });
    await act(async () => { release(json(200, { authorizationUrl })); });
    expect(assign).not.toHaveBeenCalled();
  });

  it('ignores an expired link-start session after leaving Account', async () => {
    let release!: (response: Response) => void;
    const delayed = new Promise<Response>((resolve) => { release = resolve; });
    const fake = installFakeServer({
      ...signedIn,
      ...enabled,
      'GET /api/v1/auth/oidc/identity': json(200, { linked: false }),
      'POST /api/v1/auth/oidc/start': () => delayed,
    });
    window.history.replaceState(null, '', '/account');
    renderApp();
    await userEvent.type(await screen.findByLabelText('Current password'), 'pw');
    await userEvent.click(screen.getByRole('button', { name: 'Connect Pocket ID' }));
    await userEvent.click(screen.getByRole('link', { name: /^Home$/ }));
    await screen.findByRole('heading', { level: 1, name: 'Veselí' });
    const sessionChecks = fake.requests.filter((request) => request.path === '/api/v1/session').length;
    await act(async () => { release(problem(401, 'unauthenticated')); });
    expect(fake.requests.filter((request) => request.path === '/api/v1/session')).toHaveLength(sessionChecks);
    expect(screen.getByRole('button', { name: 'Sign out' })).toBeInTheDocument();
  });

  it('an expired session during link start signs out', async () => {
    installFakeServer({
      ...signedIn,
      ...enabled,
      'GET /api/v1/auth/oidc/identity': json(200, { linked: false }),
      'POST /api/v1/auth/oidc/start': problem(401, 'unauthenticated'),
      'GET /api/v1/auth/setup': json(200, { required: false }),
    });
    window.history.replaceState(null, '', '/account');
    renderApp();
    await userEvent.type(await screen.findByLabelText('Current password'), 'pw{Enter}');
    expect(await screen.findByRole('heading', { level: 1, name: 'Sign in to Tendo' })).toBeInTheDocument();
    expect(window.location.pathname).toBe('/');
  });

  it('shows the connected state without a form', async () => {
    installFakeServer({ ...signedIn, ...enabled, 'GET /api/v1/auth/oidc/identity': json(200, { linked: true }) });
    window.history.replaceState(null, '', '/account');
    renderApp();
    expect(await screen.findByText('Your account is connected to Pocket ID. You can sign in with it or with your password.')).toBeInTheDocument();
    expect(screen.queryByLabelText('Current password')).not.toBeInTheDocument();
  });

  it('/account#oidc=connected shows success once and scrubs the fragment', async () => {
    window.history.replaceState(null, '', '/account#oidc=connected');
    installFakeServer({ ...signedIn, ...enabled, 'GET /api/v1/auth/oidc/identity': json(200, { linked: true }) });
    renderApp();
    expect(await screen.findByText('Connected. Next time you can use it to sign in.')).toBeInTheDocument();
    expect(window.location.hash).toBe('');
    expect(window.location.pathname).toBe('/account');
  });

  it('the callback notice is shown once: leaving and returning to /account does not repeat it', async () => {
    window.history.replaceState(null, '', '/account#oidc=connected');
    installFakeServer({ ...signedIn, ...enabled, 'GET /api/v1/auth/oidc/identity': json(200, { linked: true }) });
    renderApp();
    expect(await screen.findByText('Connected. Next time you can use it to sign in.')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('link', { name: /^Home$/ }));
    await screen.findByRole('heading', { level: 1, name: 'Veselí' });
    window.history.back();
    await waitFor(() => expect(window.location.pathname).toBe('/account'));
    expect(await screen.findByRole('heading', { level: 1, name: 'Your account' })).toBeInTheDocument();
    await screen.findByText('Your account is connected to Pocket ID. You can sign in with it or with your password.');
    expect(screen.queryByText('Connected. Next time you can use it to sign in.')).not.toBeInTheDocument();
  });

  it('a signed-in /account error is shown on the account screen', async () => {
    window.history.replaceState(null, '', '/account#oidcError=identity_conflict');
    installFakeServer({ ...signedIn, ...enabled, 'GET /api/v1/auth/oidc/identity': json(200, { linked: false }) });
    renderApp();
    expect(await screen.findByText('That sign-in is already connected to another Tendo account.')).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 1, name: 'Your account' })).toBeInTheDocument();
  });

  it('with the provider disabled, shows the account without any sign-in section', async () => {
    const fake = installFakeServer({ ...signedIn, 'GET /api/v1/auth/oidc': json(200, { enabled: false }) });
    window.history.replaceState(null, '', '/account');
    renderApp();
    expect(await screen.findByText('You’re signed in as anna.')).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText('Checking…')).not.toBeInTheDocument());
    expect(screen.queryByRole('heading', { level: 2, name: 'Sign-in with Pocket ID' })).not.toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 2, name: 'Language' })).toBeVisible();
    expect(fake.requests.some((r) => r.path === '/api/v1/auth/oidc/identity')).toBe(false);
  });

  it('an identity lookup failure is quiet and keeps the screen usable', async () => {
    installFakeServer({ ...signedIn, ...enabled, 'GET /api/v1/auth/oidc/identity': problem(503, 'unavailable') });
    window.history.replaceState(null, '', '/account');
    renderApp();
    expect(await screen.findByText('Couldn’t check the connection right now.')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /^Home$/ })).toBeInTheDocument();
  });

  it('renders Czech', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer({ ...signedOut, ...enabled });
    renderApp();
    expect(await screen.findByRole('button', { name: 'Přihlásit se přes Pocket ID' })).toBeInTheDocument();
  });
});
