import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { installFakeServer, json, session } from '../../test/fakeServer';

const TOKEN = 'B'.repeat(43);
const RESET_PATH = '/reset-password';
const resetPath = 'POST /api/v1/auth/password-reset';

const signedOutBoot = {
  'GET /api/v1/session': json(401, { type: 'about:blank', title: 'Unauthorized', status: 401 }),
  'GET /api/v1/auth/setup': json(200, { required: false }),
  'GET /api/v1/auth/oidc': json(200, { enabled: false }),
};

function open(path: string) {
  window.history.replaceState(null, '', path);
  return render(<I18nProvider><App /></I18nProvider>);
}
function setValue(input: HTMLElement, value: string) {
  fireEvent.change(input, { target: { value } });
}
async function fill(password: string, confirm = password) {
  const fields = screen.getAllByLabelText(/password/i);
  setValue(fields[0], password);
  setValue(fields[1], confirm);
}
function submitted() {
  return screen.queryByRole('button', { name: 'Save new password' });
}

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

describe('reset-password screen', () => {
  it('reads the fragment, removes it from the address bar before any request, and sends nothing yet', async () => {
    const hashes: string[] = [];
    installFakeServer({
      'GET /api/v1/session': () => { hashes.push(window.location.hash); return json(401, { type: 'about:blank', title: 'x', status: 401 }); },
      'GET /api/v1/auth/setup': () => { hashes.push(window.location.hash); return json(200, { required: false }); },
    });
    open(`${RESET_PATH}#${TOKEN}`);
    expect(await screen.findByRole('heading', { level: 1, name: 'Choose a new password' })).toBeVisible();
    expect(window.location.pathname).toBe(RESET_PATH);
    expect(window.location.hash).toBe('');
    expect(window.location.search).toBe('');
    expect(hashes.length).toBeGreaterThan(0);
    expect(hashes.every((hash) => hash === '')).toBe(true);
    expect(screen.queryByRole('button', { name: 'Save new password' })).toBeVisible();
  });

  it('shows the missing-link state after a reload no longer carries the fragment', async () => {
    const fake = installFakeServer(signedOutBoot);
    const first = open(`${RESET_PATH}#${TOKEN}`);
    await screen.findByRole('heading', { level: 1, name: 'Choose a new password' });
    expect(window.location.hash).toBe('');
    first.unmount();

    // A reload creates a fresh mount from the scrubbed address: the token exists only in the previous memory.
    render(<I18nProvider><App /></I18nProvider>);
    expect(await screen.findByRole('heading', { level: 1, name: 'This reset link can’t be used' })).toBeVisible();
    expect(screen.getByText('This reset link is no longer valid. Ask a household administrator for a new one.')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Save new password' })).not.toBeInTheDocument();
    expect(fake.requests.some((request) => `${request.method} ${request.path}` === resetPath)).toBe(false);
  });

  it('blocks a mismatch locally and points at the confirmation field', async () => {
    const fake = installFakeServer(signedOutBoot);
    open(`${RESET_PATH}#${TOKEN}`);
    await screen.findByRole('heading', { level: 1, name: 'Choose a new password' });
    await fill('a long enough password', 'a different password');
    await userEvent.click(screen.getByRole('button', { name: 'Save new password' }));

    expect(await screen.findByText('The two passwords don’t match. Type them again.')).toBeVisible();
    expect(screen.getByLabelText('Type the new password again')).toHaveFocus();
    expect(screen.getByLabelText('Type the new password again')).toHaveAttribute('aria-invalid', 'true');
    expect(fake.requests.some((request) => `${request.method} ${request.path}` === resetPath)).toBe(false);
  });

  it('counts code points: 14 astral characters are too short and 15 are accepted', async () => {
    const fake = installFakeServer({
      ...signedOutBoot,
      [resetPath]: json(200, { login: 'petr' }),
    });
    open(`${RESET_PATH}#${TOKEN}`);
    await screen.findByRole('heading', { level: 1, name: 'Choose a new password' });

    await fill('😀'.repeat(14));
    await userEvent.click(screen.getByRole('button', { name: 'Save new password' }));
    expect(await screen.findByText('Passwords need to be 15 to 128 characters long.')).toBeVisible();
    expect(fake.requests.some((request) => `${request.method} ${request.path}` === resetPath)).toBe(false);

    await fill('😀'.repeat(15));
    await userEvent.click(screen.getByRole('button', { name: 'Save new password' }));

    await waitFor(() => expect(fake.requests.filter((request) => `${request.method} ${request.path}` === resetPath)).toHaveLength(1));
    const sent = fake.requests.find((request) => `${request.method} ${request.path}` === resetPath);
    expect(JSON.parse(sent!.body)).toEqual({ token: TOKEN, password: '😀'.repeat(15) });
    expect(await screen.findByLabelText('Username')).toHaveValue('petr');
  });

  it('maps a server length refusal back to the password field', async () => {
    installFakeServer({
      ...signedOutBoot,
      [resetPath]: json(422, { type: 'about:blank', title: 'Validation Failed', status: 422, code: 'invalid_length', field: 'password' }),
    });
    open(`${RESET_PATH}#${TOKEN}`);
    await screen.findByRole('heading', { level: 1, name: 'Choose a new password' });
    await fill('a long enough password');
    await userEvent.click(screen.getByRole('button', { name: 'Save new password' }));

    expect(await screen.findByText('Passwords need to be 15 to 128 characters long.')).toBeVisible();
    expect(screen.getByLabelText('New password')).toHaveAttribute('aria-invalid', 'true');
    expect(submitted()).toBeVisible();
  });

  it('replaces the password and lands on sign-in with the login prefilled and a calm notice', async () => {
    const fake = installFakeServer({
      ...signedOutBoot,
      [resetPath]: json(200, { login: 'petr' }),
    });
    open(`${RESET_PATH}#${TOKEN}`);
    await screen.findByRole('heading', { level: 1, name: 'Choose a new password' });
    await fill('a long enough password');
    await userEvent.click(screen.getByRole('button', { name: 'Save new password' }));

    expect(await screen.findByRole('heading', { level: 1, name: 'Welcome back' })).toBeVisible();
    expect(window.location.pathname).toBe('/login');
    expect(window.location.hash).toBe('');
    expect(screen.getByLabelText('Username')).toHaveValue('petr');
    expect(screen.getByLabelText('Password')).toHaveValue('');
    expect(screen.getByRole('status')).toHaveTextContent('Password changed. Sign in with your new password.');
    expect(fake.requests.filter((request) => `${request.method} ${request.path}` === resetPath)).toHaveLength(1);
  });

  it('works while signed in as someone else and still ends at sign-in, re-checking the stale session', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
      [resetPath]: json(200, { login: 'petr' }),
    });
    open(`${RESET_PATH}#${TOKEN}`);
    expect(await screen.findByRole('heading', { level: 1, name: 'Choose a new password' })).toBeVisible();
    // The signed-in shell is not shown: the reset link owns this page.
    expect(screen.queryByRole('navigation', { name: 'Household navigation' })).not.toBeInTheDocument();
    await fill('a long enough password');
    await userEvent.click(screen.getByRole('button', { name: 'Save new password' }));

    expect(await screen.findByRole('heading', { level: 1, name: 'Welcome back' })).toBeVisible();
    expect(screen.getByLabelText('Username')).toHaveValue('petr');
    expect(screen.getByRole('status')).toHaveTextContent('Password changed. Sign in with your new password.');
    // The stale signed-in shell is gone, so the deleted cookie cannot keep anyone in.
    expect(screen.queryByRole('button', { name: 'Sign out' })).not.toBeInTheDocument();
    expect(screen.queryByRole('navigation', { name: 'Household navigation' })).not.toBeInTheDocument();
  });

  it.each([
    [404, 'invalid_password_reset', 'This reset link is no longer valid. Ask a household administrator for a new one.'],
    [429, 'rate_limited', 'Too many attempts. Wait a little, then try again.'],
    [503, 'unavailable', 'Tendo can’t be reached right now. Check your connection and try again.'],
  ])('shows the calm outcome for a %i redemption refusal', async (status, code, message) => {
    installFakeServer({
      ...signedOutBoot,
      [resetPath]: json(status, { type: 'about:blank', title: 'x', status, code }),
    });
    open(`${RESET_PATH}#${TOKEN}`);
    await screen.findByRole('heading', { level: 1, name: 'Choose a new password' });
    await fill('a long enough password');
    await userEvent.click(screen.getByRole('button', { name: 'Save new password' }));

    expect(await screen.findByText(message)).toBeVisible();
    if (status === 404) {
      expect(screen.queryByRole('button', { name: 'Save new password' })).not.toBeInTheDocument();
      expect(screen.getByRole('link', { name: 'Go to sign-in' })).toHaveAttribute('href', '/login');
    } else {
      expect(screen.getByRole('button', { name: 'Save new password' })).toBeEnabled();
    }
  });

  it.each([
    ['missing', RESET_PATH],
    ['malformed', `${RESET_PATH}#not-a-token`],
  ])('shows the invalid state for a %s token without calling the API', async (_kind, path) => {
    const fake = installFakeServer(signedOutBoot);
    open(path);
    expect(await screen.findByRole('heading', { level: 1, name: 'This reset link can’t be used' })).toBeVisible();
    expect(screen.getByText('This reset link is no longer valid. Ask a household administrator for a new one.')).toBeVisible();
    expect(screen.getByRole('link', { name: 'Go to sign-in' })).toHaveAttribute('href', '/login');
    expect(screen.queryByRole('button', { name: 'Save new password' })).not.toBeInTheDocument();
    expect(fake.requests.some((request) => `${request.method} ${request.path}` === resetPath)).toBe(false);
  });

  it('renders the Czech reset screen', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer({
      'GET /api/v1/session': json(401, { type: 'about:blank', title: 'x', status: 401 }),
      'GET /api/v1/auth/setup': json(200, { required: false }),
    });
    open(`${RESET_PATH}#${TOKEN}`);
    expect(await screen.findByRole('heading', { level: 1, name: 'Zvolte si nové heslo' })).toBeVisible();
    const main = screen.getByRole('main');
    expect(within(main).getByLabelText('Nové heslo')).toBeVisible();
    expect(within(main).getByLabelText('Zadejte nové heslo znovu')).toBeVisible();
    expect(within(main).getByText('Použijte 15 až 128 znaků.')).toBeVisible();
  });
});
