import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, household, installFakeServer, json, session } from '../../test/fakeServer';

function renderApp() {
  return render(
    <I18nProvider>
      <App />
    </I18nProvider>,
  );
}

const problem = (status: number, extra: Record<string, unknown> = {}) =>
  json(status, { type: 'about:blank', title: 'x', status, ...extra });
const validation = (field: string) => problem(422, { code: 'invalid', field });

const setupRequired = {
  'GET /api/v1/session': problem(401),
  'GET /api/v1/auth/setup': json(200, { required: true }),
};

function stubTimezone(zone: string) {
  vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions').mockReturnValue({
    timeZone: zone,
  } as Intl.ResolvedDateTimeFormatOptions);
}

async function fillForm(overrides: { token?: string } = {}) {
  await userEvent.type(await screen.findByLabelText('Setup code'), overrides.token ?? '  secret-code  ');
  await userEvent.type(screen.getByLabelText('What should we call your household?'), 'The Novák family');
  await userEvent.type(screen.getByLabelText('Login'), 'anna');
  await userEvent.type(screen.getByLabelText('Password'), 'correct horse battery');
}

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
  stubTimezone('America/New_York');
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('onboarding', () => {
  it('shows the welcome form with the detected time zone and no login form or switcher', async () => {
    installFakeServer(setupRequired);
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Welcome to Tendo' })).toBeVisible();
    expect(screen.getByLabelText('What should we call your household?')).toHaveAttribute('autocomplete', 'off');
    expect(screen.getByLabelText('Setup code')).toHaveAttribute('autocomplete', 'off');
    expect(screen.getByLabelText('Login')).toHaveAttribute('autocomplete', 'username');
    expect(screen.getByLabelText('Password')).toHaveAttribute('autocomplete', 'new-password');
    expect(screen.getByLabelText('Time zone')).toHaveValue('America/New_York');
    expect(screen.getByText('Your time zone: America/New_York')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Create household' })).toBeEnabled();
    expect(screen.queryByRole('button', { name: 'Sign in' })).not.toBeInTheDocument();
    expect(screen.queryByText(/tenant|household id|switch/i)).not.toBeInTheDocument();
    expect(screen.queryByText(HOUSEHOLD_ID)).not.toBeInTheDocument();
  });

  it('falls back to UTC when the browser cannot tell the time zone', async () => {
    stubTimezone('');
    installFakeServer(setupRequired);
    renderApp();
    expect(await screen.findByLabelText('Time zone')).toHaveValue('UTC');
  });

  it('sends the corrected time zone and token, then signs in and shows the household', async () => {
    const fake = installFakeServer({
      ...setupRequired,
      'PUT /api/v1/auth/setup': json(201, { required: false }),
      'POST /api/v1/session': json(201, session()),
      [`GET /api/v1/households/${HOUSEHOLD_ID}`]: json(200, household),
    });
    renderApp();
    await fillForm();
    await userEvent.selectOptions(screen.getByLabelText('Time zone'), 'Europe/Prague');
    expect(screen.getByText('Your time zone: Europe/Prague')).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Create household' }));

    expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible();
    const put = fake.requests.find((r) => r.method === 'PUT')!;
    expect(put.path).toBe('/api/v1/auth/setup');
    expect(put.headers.get('X-Tendo-Setup-Token')).toBe('secret-code');
    expect(JSON.parse(put.body)).toEqual({
      login: 'anna',
      password: 'correct horse battery',
      householdName: 'The Novák family',
      timezone: 'Europe/Prague',
    });
    const post = fake.requests.find((r) => r.method === 'POST')!;
    expect(post.path).toBe('/api/v1/session');
    expect(JSON.parse(post.body)).toEqual({ login: 'anna', password: 'correct horse battery' });
    expect(fake.requests.indexOf(put)).toBeLessThan(fake.requests.indexOf(post));
    expect(screen.getByText('Nothing needs attention right now.')).toBeVisible();
  });

  it('falls back to the login screen when automatic sign-in fails', async () => {
    installFakeServer({
      ...setupRequired,
      'PUT /api/v1/auth/setup': json(201, { required: false }),
      'POST /api/v1/session': problem(503),
    });
    renderApp();
    await fillForm();
    await userEvent.click(screen.getByRole('button', { name: 'Create household' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Sign in to Tendo' })).toHaveFocus();
  });

  it('401 asks for the right setup code, focuses it and keeps the other fields', async () => {
    installFakeServer({ ...setupRequired, 'PUT /api/v1/auth/setup': problem(401, { code: 'unauthorized' }) });
    renderApp();
    await fillForm();
    await userEvent.selectOptions(screen.getByLabelText('Time zone'), 'Europe/Prague');
    await userEvent.click(screen.getByRole('button', { name: 'Create household' }));

    const alert = await screen.findByText('That setup code isn’t right. Check it and try again.');
    expect(alert).toHaveAttribute('role', 'alert');
    const code = screen.getByLabelText('Setup code');
    expect(code).toHaveFocus();
    expect(code).toHaveAttribute('aria-invalid', 'true');
    expect(code.getAttribute('aria-describedby')).toContain(alert.id);
    expect(code).toHaveValue('  secret-code  ');
    expect(screen.getByLabelText('What should we call your household?')).toHaveValue('The Novák family');
    expect(screen.getByLabelText('Login')).toHaveValue('anna');
    expect(screen.getByLabelText('Time zone')).toHaveValue('Europe/Prague');
    expect(screen.getByRole('button', { name: 'Create household' })).toBeEnabled();
  });

  it.each([
    ['timezone', 'Time zone', 'That time zone isn’t recognised. Pick one from the list.'],
    ['login', 'Login', /That login won’t work/],
    ['password', 'Password', 'Passwords need to be 15 to 128 characters long.'],
    ['householdName', 'What should we call your household?', /household name won’t work/],
  ])('422 on %s shows a specific message and marks that field', async (field, label, message) => {
    installFakeServer({ ...setupRequired, 'PUT /api/v1/auth/setup': validation(field) });
    renderApp();
    await fillForm();
    await userEvent.click(screen.getByRole('button', { name: 'Create household' }));

    const alert = await screen.findByRole('alert');
    await waitFor(() => expect(alert).toHaveTextContent(message));
    const input = screen.getByLabelText(label);
    expect(input).toHaveAttribute('aria-invalid', 'true');
    expect(input.getAttribute('aria-describedby')).toContain(alert.id);
    expect(input).toHaveFocus();
    expect(screen.getByLabelText('Setup code')).toHaveAttribute('aria-invalid', 'false');
  });

  it('503 setup_unavailable says setup is not switched on', async () => {
    installFakeServer({
      ...setupRequired,
      'PUT /api/v1/auth/setup': problem(503, { code: 'setup_unavailable' }),
    });
    renderApp();
    await fillForm();
    await userEvent.click(screen.getByRole('button', { name: 'Create household' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Setup isn’t switched on for this Tendo. Whoever installed it needs to set TENDO_SETUP_TOKEN.',
    );
    expect(screen.getByLabelText('Login')).toHaveValue('anna');
  });

  it('other 503 and network failures say Tendo can’t be reached', async () => {
    installFakeServer({ ...setupRequired, 'PUT /api/v1/auth/setup': problem(503, { code: 'unavailable' }) });
    renderApp();
    await fillForm();
    await userEvent.click(screen.getByRole('button', { name: 'Create household' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Tendo can’t be reached right now. Try again.');
  });

  it('429 shows the rate-limit message', async () => {
    installFakeServer({ ...setupRequired, 'PUT /api/v1/auth/setup': problem(429, { code: 'rate_limited' }) });
    renderApp();
    await fillForm();
    await userEvent.click(screen.getByRole('button', { name: 'Create household' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Too many attempts. Try again in a moment.');
  });

  it('409 moves on to the login screen', async () => {
    installFakeServer({ ...setupRequired, 'PUT /api/v1/auth/setup': problem(409, { code: 'setup_complete' }) });
    renderApp();
    await fillForm();
    await userEvent.click(screen.getByRole('button', { name: 'Create household' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Sign in to Tendo' })).toBeVisible();
    expect(screen.queryByLabelText('Setup code')).not.toBeInTheDocument();
  });

  it('asks for missing fields without calling the server', async () => {
    const fake = installFakeServer(setupRequired);
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Create household' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Enter the setup code.');
    expect(screen.getByLabelText('Setup code')).toHaveFocus();
    expect(fake.requests.some((r) => r.method === 'PUT')).toBe(false);
  });

  it('disables submit while the request is in flight so it cannot be sent twice', async () => {
    let release!: () => void;
    const gate = new Promise<void>((r) => (release = r));
    const fake = installFakeServer({
      ...setupRequired,
      'PUT /api/v1/auth/setup': async () => {
        await gate;
        return problem(429);
      },
    });
    renderApp();
    await fillForm();
    await userEvent.type(screen.getByLabelText('Password'), '{Enter}');
    const busy = await screen.findByRole('button', { name: 'Creating…' });
    expect(busy).toBeDisabled();
    await userEvent.type(screen.getByLabelText('Password'), '{Enter}');
    expect(fake.requests.filter((r) => r.method === 'PUT')).toHaveLength(1);
    release();
    expect(await screen.findByRole('button', { name: 'Create household' })).toBeEnabled();
    expect(fake.requests.filter((r) => r.method === 'PUT')).toHaveLength(1);
  });

  it('renders Czech labels for a Czech browser', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer(setupRequired);
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Vítejte v Tendu' })).toBeVisible();
    expect(screen.getByLabelText('Instalační kód')).toBeVisible();
    expect(screen.getByLabelText('Jak budeme vaší domácnosti říkat?')).toBeVisible();
    expect(screen.getByLabelText('Přihlašovací jméno')).toBeVisible();
    expect(screen.getByLabelText('Heslo')).toBeVisible();
    expect(screen.getByLabelText('Časové pásmo')).toHaveValue('America/New_York');
    expect(screen.getByRole('button', { name: 'Vytvořit domácnost' })).toBeVisible();
  });
});
