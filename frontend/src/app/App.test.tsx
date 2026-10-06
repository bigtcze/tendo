import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { I18nProvider } from '../i18n';
import { HOUSEHOLD_ID, household, installFakeServer, json, session } from '../test/fakeServer';
import { App } from './App';

function renderApp() {
  return render(
    <I18nProvider>
      <App />
    </I18nProvider>,
  );
}

const problem = (status: number) => json(status, { type: 'about:blank', title: 'x', status });
const householdPath = `GET /api/v1/households/${HOUSEHOLD_ID}`;

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('signed out', () => {
  it('shows the not-set-up screen and no login form when setup is required', async () => {
    installFakeServer({
      'GET /api/v1/session': problem(401),
      'GET /api/v1/auth/setup': json(200, { required: true }),
    });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'This Tendo hasn’t been set up yet.' })).toBeVisible();
    expect(screen.getByText(/person who installed it needs to finish setup/)).toBeVisible();
    expect(screen.queryByLabelText('Password')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Sign in' })).not.toBeInTheDocument();
  });

  it('shows a retryable error when the session check fails', async () => {
    const fake = installFakeServer({ 'GET /api/v1/session': problem(503) });
    renderApp();
    const retry = await screen.findByRole('button', { name: 'Try again' });
    expect(fake.requests.map((r) => r.path)).toEqual(['/api/v1/session']);
    await userEvent.click(retry);
    await waitFor(() => expect(fake.requests).toHaveLength(2));
  });

  it('shows the login form with proper fields when setup is complete', async () => {
    installFakeServer({
      'GET /api/v1/session': problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
    });
    renderApp();
    const loginField = await screen.findByLabelText('Login');
    expect(loginField).toHaveAttribute('autocomplete', 'username');
    expect(screen.getByLabelText('Password')).toHaveAttribute('autocomplete', 'current-password');
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Sign in to Tendo');
  });

  it('wrong password shows an alert and clears the password', async () => {
    installFakeServer({
      'GET /api/v1/session': problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'POST /api/v1/session': problem(401),
    });
    renderApp();
    await userEvent.type(await screen.findByLabelText('Login'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'wrong-password{Enter}');
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Login or password is incorrect.');
    expect(screen.getByLabelText('Password')).toHaveValue('');
    expect(screen.getByLabelText('Password')).toHaveAttribute('aria-describedby', alert.id);
    expect(screen.getByLabelText('Login')).toHaveValue('anna');
  });

  it('429 shows the rate-limit message', async () => {
    installFakeServer({
      'GET /api/v1/session': problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'POST /api/v1/session': problem(429),
    });
    renderApp();
    await userEvent.type(await screen.findByLabelText('Login'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'pw');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Too many attempts. Try again in a moment.');
  });

  it('other failures show the unreachable message', async () => {
    installFakeServer({
      'GET /api/v1/session': problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'POST /api/v1/session': problem(403),
    });
    renderApp();
    await userEvent.type(await screen.findByLabelText('Login'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'pw{Enter}');
    expect(await screen.findByRole('alert')).toHaveTextContent('Tendo can’t be reached right now. Try again.');
  });

  it('disables submit while the request is in flight', async () => {
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    installFakeServer({
      'GET /api/v1/session': problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'POST /api/v1/session': async () => {
        await gate;
        return problem(401);
      },
    });
    renderApp();
    await userEvent.type(await screen.findByLabelText('Login'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'pw{Enter}');
    expect(await screen.findByRole('button', { name: 'Signing in…' })).toBeDisabled();
    release();
    expect(await screen.findByRole('button', { name: 'Sign in' })).toBeEnabled();
  });
});

describe('signed in', () => {
  it('login posts exactly {login,password} then shows the household home with no switcher', async () => {
    const fake = installFakeServer({
      'GET /api/v1/session': problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'POST /api/v1/session': json(201, session()),
      [householdPath]: json(200, household),
    });
    renderApp();
    await userEvent.type(await screen.findByLabelText('Login'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'correct horse battery{Enter}');

    expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible();
    expect(screen.getByText('Nothing needs attention right now.')).toBeVisible();

    const post = fake.requests.find((r) => r.method === 'POST')!;
    expect(post.path).toBe('/api/v1/session');
    expect(JSON.parse(post.body)).toEqual({ login: 'anna', password: 'correct horse battery' });
    expect(fake.requests.some((r) => r.path === `/api/v1/households/${HOUSEHOLD_ID}`)).toBe(true);

    expect(screen.getByRole('heading', { level: 1, name: 'Veselí' })).toHaveFocus();
    expect(screen.getByText('Signed in as anna')).toBeVisible();
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
    expect(screen.queryByText(/switch/i)).not.toBeInTheDocument();
    expect(screen.queryByText(HOUSEHOLD_ID)).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /add/i })).not.toBeInTheDocument();
  });

  it('an existing session goes straight to home', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [householdPath]: json(200, household),
    });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible();
    expect(document.body).toHaveFocus();
    expect(screen.getByRole('banner')).toHaveTextContent('Tendo');
    expect(screen.getByRole('main')).toBeInTheDocument();
  });

  it('session without a default household explains it calmly and offers sign out', async () => {
    const fake = installFakeServer({ 'GET /api/v1/session': json(200, session(false)) });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1 })).toHaveTextContent(
      'Your account isn’t part of a household yet.',
    );
    expect(screen.getByRole('button', { name: 'Sign out' })).toBeVisible();
    expect(fake.requests.every((r) => !r.path.startsWith('/api/v1/households'))).toBe(true);
  });

  it('household 404 shows the no-household message', async () => {
    installFakeServer({ 'GET /api/v1/session': json(200, session()), [householdPath]: problem(404) });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1 })).toHaveTextContent(
      'Your account isn’t part of a household yet.',
    );
  });

  it('sign out calls DELETE /api/v1/session and returns to login', async () => {
    const fake = installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [householdPath]: json(200, household),
      'DELETE /api/v1/session': json(204),
      'GET /api/v1/auth/setup': json(200, { required: false }),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Sign out' }));
    expect(await screen.findByLabelText('Password')).toBeVisible();
    expect(screen.getByRole('heading', { level: 1, name: 'Sign in to Tendo' })).toHaveFocus();
    expect(fake.requests.some((r) => r.method === 'DELETE' && r.path === '/api/v1/session')).toBe(true);
    expect(screen.queryByText('Veselí')).not.toBeInTheDocument();
  });

  it('failed sign out keeps the signed-in state and says so', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [householdPath]: json(200, household),
      'DELETE /api/v1/session': problem(503),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Sign out' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t sign you out. Try again.');
    expect(screen.getByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible();
  });

  it('401 on household fetch returns to login', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [householdPath]: problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
    });
    renderApp();
    expect(await screen.findByLabelText('Password')).toBeVisible();
    expect(screen.queryByRole('heading', { name: 'Veselí' })).not.toBeInTheDocument();
  });

  it('other household failures show a retryable error', async () => {
    let calls = 0;
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [householdPath]: () => (++calls === 1 ? problem(500) : json(200, household)),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Try again' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible();
  });
});

describe('localization', () => {
  const signedOut = {
    'GET /api/v1/session': problem(401),
    'GET /api/v1/auth/setup': json(200, { required: false }),
  };

  it('renders Czech for a cs-CZ browser and sets html lang', async () => {
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['cs-CZ', 'en']);
    installFakeServer(signedOut);
    renderApp();
    expect(await screen.findByLabelText('Přihlašovací jméno')).toBeVisible();
    expect(screen.getByLabelText('Heslo')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Přihlásit se' })).toBeVisible();
    expect(document.documentElement.lang).toBe('cs');
  });

  it('stored preference wins over browser language', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer(signedOut);
    renderApp();
    expect(await screen.findByLabelText('Heslo')).toBeVisible();
  });

  it('switching language changes text, persists, and updates html lang', async () => {
    installFakeServer(signedOut);
    renderApp();
    expect(await screen.findByLabelText('Password')).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Čeština' }));
    expect(screen.getByLabelText('Heslo')).toBeVisible();
    expect(localStorage.getItem('tendo.locale')).toBe('cs');
    expect(document.documentElement.lang).toBe('cs');
    await userEvent.click(screen.getByRole('button', { name: 'English' }));
    expect(screen.getByLabelText('Password')).toBeVisible();
    expect(localStorage.getItem('tendo.locale')).toBe('en');
    expect(document.documentElement.lang).toBe('en');
  });

  it('renders the Czech empty home state', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [householdPath]: json(200, household),
    });
    renderApp();
    expect(await screen.findByText('Teď nic nevyžaduje pozornost.')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Odhlásit se' })).toBeVisible();
  });
});
