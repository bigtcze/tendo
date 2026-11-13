import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, household, emptyHome, installFakeServer, json, session } from '../../test/fakeServer';
import { itemRoutes } from '../../test/fakeItems';

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
  await userEvent.type(await screen.findByLabelText('One-time setup code'), overrides.token ?? '  secret-code  ');
  await userEvent.type(screen.getByLabelText('Household name'), 'The Novák family');
  await userEvent.type(screen.getByLabelText('Username'), 'anna');
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
  it('shows the timezone hint in Czech when the selected locale is cs-CZ', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    stubTimezone('Europe/Prague');
    installFakeServer(setupRequired);
    renderApp();
    expect(await screen.findByText('Použije se pro data v domácnosti: Prague · středoevropský čas', { exact: true })).toBeVisible();
    expect(screen.getByRole('option', { name: 'Prague · středoevropský čas' })).toHaveValue('Europe/Prague');
    expect(screen.queryByText(/Used for dates in your household:/)).not.toBeInTheDocument();
  });

  it('shows the welcome form with a readable detected time zone and no login form or switcher', async () => {
    installFakeServer(setupRequired);
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Welcome to Tendo' })).toBeVisible();
    expect(screen.getByLabelText('Household name')).toHaveAttribute('autocomplete', 'off');
    expect(screen.getByLabelText('One-time setup code')).toHaveAttribute('autocomplete', 'off');
    expect(screen.getByLabelText('Username')).toHaveAttribute('autocomplete', 'username');
    expect(screen.getByLabelText('Password')).toHaveAttribute('autocomplete', 'new-password');
    expect(screen.getByLabelText('Time zone')).toHaveValue('America/New_York');
    expect(screen.getByText(/Used for dates in your household: New York · /)).toBeVisible();
    expect(screen.getByText('Used for dates in your household: New York · Eastern Time')).toBeVisible();
    expect(screen.getByRole('option', { name: /New York · / })).toHaveValue('America/New_York');
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
      ...emptyHome,
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
    });
    renderApp();
    await fillForm();
    await userEvent.selectOptions(screen.getByLabelText('Time zone'), 'Europe/Prague');
    expect(screen.getByText(/Used for dates in your household: Prague · /)).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Create household' }));

    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toBeVisible();
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
    expect(await screen.findByText('Nothing needs attention right now.')).toBeVisible();
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
    expect(await screen.findByRole('heading', { level: 1, name: 'Welcome back' })).toHaveFocus();
  });

  it('401 asks for the right setup code, focuses it and keeps the other fields', async () => {
    installFakeServer({ ...setupRequired, 'PUT /api/v1/auth/setup': problem(401, { code: 'unauthorized' }) });
    renderApp();
    await fillForm();
    await userEvent.selectOptions(screen.getByLabelText('Time zone'), 'Europe/Prague');
    await userEvent.click(screen.getByRole('button', { name: 'Create household' }));

    const alert = await screen.findByText('That setup code isn’t right. Check it and try again.');
    expect(alert).toHaveAttribute('role', 'alert');
    const code = screen.getByLabelText('One-time setup code');
    expect(code).toHaveFocus();
    expect(code).toHaveAttribute('aria-invalid', 'true');
    expect(code.getAttribute('aria-describedby')).toContain(alert.id);
    expect(code).toHaveValue('  secret-code  ');
    expect(screen.getByLabelText('Household name')).toHaveValue('The Novák family');
    expect(screen.getByLabelText('Username')).toHaveValue('anna');
    expect(screen.getByLabelText('Time zone')).toHaveValue('Europe/Prague');
    expect(screen.getByText(/Used for dates in your household: Prague · /)).toBeVisible();
    expect(screen.getByRole('button', { name: 'Create household' })).toBeEnabled();
  });

  it.each([
    ['timezone', 'Time zone', 'That time zone isn’t recognised. Pick one from the list.'],
    ['login', 'Username', /That username won’t work/],
    ['password', 'Password', 'Passwords need to be 15 to 128 characters long.'],
    ['householdName', 'Household name', /household name won’t work/],
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
    expect(screen.getByLabelText('One-time setup code')).toHaveAttribute('aria-invalid', 'false');
  });

  it('503 setup_unavailable gives plain guidance', async () => {
    installFakeServer({
      ...setupRequired,
      'PUT /api/v1/auth/setup': problem(503, { code: 'setup_unavailable' }),
    });
    renderApp();
    await fillForm();
    await userEvent.click(screen.getByRole('button', { name: 'Create household' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'First-time setup is closed. Ask whoever looks after Tendo to reopen it if you still need to create an account.',
    );
    expect(screen.getByLabelText('Username')).toHaveValue('anna');
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
    expect(await screen.findByRole('heading', { level: 1, name: 'Welcome back' })).toBeVisible();
    expect(screen.queryByLabelText('One-time setup code')).not.toBeInTheDocument();
  });

  it('asks for missing fields without calling the server', async () => {
    const fake = installFakeServer(setupRequired);
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Create household' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Enter the setup code.');
    expect(screen.getByLabelText('One-time setup code')).toHaveFocus();
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
    expect(screen.getByLabelText('Jednorázový kód pro první nastavení')).toBeVisible();
    expect(screen.getByLabelText('Název domácnosti')).toBeVisible();
    expect(screen.getByLabelText('Uživatelské jméno')).toBeVisible();
    expect(screen.getByLabelText('Heslo')).toBeVisible();
    expect(screen.getByLabelText('Časové pásmo')).toHaveValue('America/New_York');
    expect(screen.getByRole('button', { name: 'Vytvořit domácnost' })).toBeVisible();
  });
  it('pins exact aria-describedby wiring: hint first, error region only on the failing field', async () => {
    installFakeServer({ ...setupRequired, 'PUT /api/v1/auth/setup': validation('timezone') });
    renderApp();
    const describedBy = () => ({
      setupCode: screen.getByLabelText('One-time setup code').getAttribute('aria-describedby'),
      householdName: screen.getByLabelText('Household name').getAttribute('aria-describedby'),
      username: screen.getByLabelText('Username').getAttribute('aria-describedby'),
      password: screen.getByLabelText('Password').getAttribute('aria-describedby'),
      timezone: screen.getByLabelText('Time zone').getAttribute('aria-describedby'),
    });
    await screen.findByLabelText('One-time setup code');
    expect(describedBy()).toEqual({
      setupCode: 'setup-code-hint', householdName: null, username: 'onboarding-login-hint',
      password: 'onboarding-password-hint', timezone: 'onboarding-timezone-hint',
    });
    await userEvent.type(screen.getByLabelText('One-time setup code'), 'secret-code');
    await userEvent.click(screen.getByRole('button', { name: 'Create household' }));
    await waitFor(() => expect(screen.getByLabelText('Household name')).toHaveFocus());
    expect(describedBy()).toEqual({
      setupCode: 'setup-code-hint', householdName: 'onboarding-error', username: 'onboarding-login-hint',
      password: 'onboarding-password-hint', timezone: 'onboarding-timezone-hint',
    });
    await userEvent.type(screen.getByLabelText('Household name'), 'The Novák family');
    await userEvent.type(screen.getByLabelText('Username'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'correct horse battery');
    await userEvent.click(screen.getByRole('button', { name: 'Create household' }));
    await waitFor(() => expect(screen.getByLabelText('Time zone')).toHaveFocus());
    expect(screen.getByLabelText('Time zone')).toHaveAttribute('aria-invalid', 'true');
    expect(describedBy()).toEqual({
      setupCode: 'setup-code-hint', householdName: null, username: 'onboarding-login-hint',
      password: 'onboarding-password-hint', timezone: 'onboarding-timezone-hint onboarding-error',
    });
  });
});
