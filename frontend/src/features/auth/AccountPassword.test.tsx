import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, household, installFakeServer, json, passwordChange, session } from '../../test/fakeServer';
import { AccountPasswordSection } from './AccountPasswordSection';

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const OLD = 'an-old-long-unique-passphrase';
const NEW = 'a-new-long-unique-passphrase';
const changePath = 'POST /api/v1/auth/password';

function accountRoutes(extra: Record<string, unknown> = {}) {
  return {
    'GET /api/v1/session': json(200, session()),
    [`GET ${base}`]: json(200, household),
    ...extra,
  };
}

function openAccount() {
  window.history.replaceState(null, '', '/account');
  return render(<I18nProvider><App /></I18nProvider>);
}

/** The shared chrome owns fields of its own, so every query stays inside the page content (see LEARNINGS L011). */
const main = () => screen.getByRole('main');
const field = (label: string) => within(main()).getByLabelText(label);
const submitButton = () => within(main()).getByRole('button', { name: /Change password|Changing…/ });

function fill(current: string, next: string, confirm = next) {
  fireEvent.change(field('Current password'), { target: { value: current } });
  fireEvent.change(field('New password'), { target: { value: next } });
  fireEvent.change(field('Type the new password again'), { target: { value: confirm } });
}

const changes = (fake: ReturnType<typeof installFakeServer>) =>
  fake.requests.filter((request) => `${request.method} ${request.path}` === changePath);

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('Account password section', () => {
  it('offers the three password fields to a password sign-in inside the flat section stack', async () => {
    installFakeServer(accountRoutes());
    openAccount();
    await screen.findByRole('heading', { level: 1, name: 'Your account' });

    const section = main().querySelector('section[aria-labelledby="account-password-heading"]');
    if (!section) throw new Error('password section missing');
    expect(section.classList.contains('border-t'), 'separated by the shared top rule').toBe(true);
    expect(within(main()).getByRole('heading', { level: 2, name: 'Password' })).toBeVisible();
    expect(within(main()).getByText('This password signs you in to Tendo. Changing it signs you out on every other device.')).toBeVisible();

    expect(field('Current password')).toHaveAttribute('type', 'password');
    expect(field('Current password')).toHaveAttribute('autocomplete', 'current-password');
    expect(field('New password')).toHaveAttribute('autocomplete', 'new-password');
    expect(field('Type the new password again')).toHaveAttribute('autocomplete', 'new-password');
    expect(within(main()).getByText('Use 15 to 128 characters.')).toBeVisible();
    expect(submitButton()).toBeEnabled();
  });

  it('explains a provider sign-in instead of offering a form it could not honour', async () => {
    installFakeServer(accountRoutes({ 'GET /api/v1/session': json(200, { ...session(), signInMethod: 'oidc' }) }));
    openAccount();
    await screen.findByRole('heading', { level: 1, name: 'Your account' });

    expect(within(main()).getByText(
      'You signed in with single sign-on, so you can’t change a password here. '
      + 'If your account also has a Tendo password, sign out and sign in with it to change it.',
    )).toBeVisible();
    expect(within(main()).queryByLabelText('Current password')).not.toBeInTheDocument();
    expect(within(main()).queryByRole('button', { name: 'Change password' })).not.toBeInTheDocument();
  });

  it('asks a pre-upgrade sign-in to sign in with the password again', async () => {
    installFakeServer(accountRoutes({ 'GET /api/v1/session': json(200, { ...session(), signInMethod: 'legacy' }) }));
    openAccount();
    await screen.findByRole('heading', { level: 1, name: 'Your account' });

    expect(within(main()).getByText(
      'This sign-in can’t change the password. Sign out and sign in with your password, then try again.',
    )).toBeVisible();
    expect(within(main()).queryByLabelText('Current password')).not.toBeInTheDocument();
  });

  it('blocks a mismatch locally, points at the confirmation field and sends nothing', async () => {
    const fake = installFakeServer(accountRoutes());
    openAccount();
    await screen.findByRole('heading', { level: 1, name: 'Your account' });

    fill(OLD, NEW, 'a different long passphrase');
    await userEvent.click(submitButton());

    expect(await within(main()).findByText('The two new passwords don’t match. Type them again.')).toBeVisible();
    const confirm = field('Type the new password again');
    expect(confirm).toHaveFocus();
    expect(confirm).toHaveAttribute('aria-invalid', 'true');
    expect(confirm).toHaveAttribute('aria-describedby', 'account-password-confirm-error');
    expect(changes(fake)).toHaveLength(0);
  });

  it('checks an empty current password, then the new-password policy before the match', async () => {
    const fake = installFakeServer(accountRoutes());
    openAccount();
    await screen.findByRole('heading', { level: 1, name: 'Your account' });

    fill('', NEW);
    await userEvent.click(submitButton());
    expect(await within(main()).findByText('Enter your current password.')).toBeVisible();
    expect(field('Current password')).toHaveFocus();
    expect(field('Current password')).toHaveAttribute('aria-describedby', 'account-password-current-error');

    fill(OLD, 'too short', 'a different long passphrase');
    await userEvent.click(submitButton());
    expect(await within(main()).findByText('Passwords need to be 15 to 128 characters long.')).toBeVisible();
    // The policy is answered before the match, exactly like the server, so the message cannot mislead.
    expect(field('New password')).toHaveFocus();
    expect(field('New password')).toHaveAttribute('aria-describedby', 'account-password-new-hint account-password-new-error');
    expect(changes(fake)).toHaveLength(0);
  });

  it('changes the password, clears the fields and says every other device was signed out', async () => {
    const change = passwordChange({ currentPassword: OLD });
    const fake = installFakeServer({ ...accountRoutes(), ...change.routes });
    openAccount();
    await screen.findByRole('heading', { level: 1, name: 'Your account' });

    fill(OLD, NEW);
    await userEvent.click(submitButton());

    const message = 'Your password is changed. You’re still signed in here, and every other device has been signed out.';
    expect(await within(main()).findByText(message)).toBeVisible();
    // Polite, and announced without taking focus away from the form.
    expect(within(main()).getByText(message).closest('[role="status"]')).toHaveAttribute('aria-live', 'polite');
    expect(new URL(window.location.href).pathname).toBe('/account');
    expect(field('Current password')).toHaveValue('');
    expect(field('New password')).toHaveValue('');
    expect(field('Type the new password again')).toHaveValue('');
    expect(within(main()).queryByText('Enter your current password.')).not.toBeInTheDocument();

    expect(changes(fake)).toHaveLength(1);
    expect(JSON.parse(changes(fake)[0]!.body)).toEqual({ currentPassword: OLD, newPassword: NEW });
    // The account now answers to the new password, and the app was handed the rotated session.
    expect(change.account.password).toBe(NEW);
    expect(change.account.rotations).toBe(1);
  });

  it('keeps a wrong current password a field answer and leaves the session signed in', async () => {
    installFakeServer({ ...accountRoutes(), ...passwordChange({ currentPassword: OLD }).routes });
    openAccount();
    await screen.findByRole('heading', { level: 1, name: 'Your account' });

    fill('not-the-current-one', NEW);
    await userEvent.click(submitButton());

    expect(await within(main()).findByText('That isn’t your current password. Check it and try again.')).toBeVisible();
    expect(field('Current password')).toHaveFocus();
    expect(field('Current password')).toHaveAttribute('aria-invalid', 'true');
    expect(within(main()).getByRole('heading', { level: 1, name: 'Your account' })).toBeVisible();
    expect(submitButton()).toBeEnabled();
  });

  it('maps a server policy refusal back to the new password field', async () => {
    installFakeServer({
      ...accountRoutes(),
      [changePath]: json(422, {
        type: 'about:blank', title: 'Validation Failed', status: 422, code: 'invalid_length', field: 'newPassword',
      }),
    });
    openAccount();
    await screen.findByRole('heading', { level: 1, name: 'Your account' });

    fill(OLD, NEW);
    await userEvent.click(submitButton());

    expect(await within(main()).findByText('Passwords need to be 15 to 128 characters long.')).toBeVisible();
    expect(field('New password')).toHaveFocus();
    expect(field('New password')).toHaveAttribute('aria-invalid', 'true');
    expect(submitButton()).toBeEnabled();
  });

  it('replaces the form when the session is no longer a local one', async () => {
    // The page loaded as a password session; by the time the change arrives the session kind has changed.
    installFakeServer({
      ...accountRoutes(),
      ...passwordChange({ currentPassword: OLD, signInMethod: 'oidc' }).routes,
    });
    openAccount();
    await screen.findByRole('heading', { level: 1, name: 'Your account' });

    fill(OLD, NEW);
    await userEvent.click(submitButton());

    expect(await within(main()).findByText(
      'This sign-in can’t change the password. Sign out and sign in with your password, then try again.',
    )).toBeVisible();
    expect(within(main()).queryByLabelText('Current password')).not.toBeInTheDocument();
    expect(within(main()).queryByRole('button', { name: 'Change password' })).not.toBeInTheDocument();
  });

  it.each([
    ['a spent throttle', passwordChange({ currentPassword: OLD, rateLimited: true }).routes,
      'Too many attempts. Wait a little, then try again.'],
    ['an unavailable Tendo', { [changePath]: json(503, { type: 'about:blank', title: 'x', status: 503, code: 'unavailable' }) },
      'Tendo can’t be reached right now. Check your connection and try again.'],
  ])('%s keeps the form usable so a retry is the whole recovery', async (_case, route, message) => {
    installFakeServer({ ...accountRoutes(), ...route });
    openAccount();
    await screen.findByRole('heading', { level: 1, name: 'Your account' });

    fill(OLD, NEW);
    await userEvent.click(submitButton());

    expect(await within(main()).findByText(message)).toBeVisible();
    expect(within(main()).getByText(message).closest('[role="alert"]')).not.toBeNull();
    expect(submitButton()).toBeEnabled();
    expect(field('New password')).toHaveValue(NEW);
  });

  it('returns to sign-in when the session ended while the form was open', async () => {
    installFakeServer({
      ...accountRoutes(),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
      [changePath]: json(401, { type: 'about:blank', title: 'Unauthorized', status: 401, code: 'unauthenticated' }),
    });
    openAccount();
    await screen.findByRole('heading', { level: 1, name: 'Your account' });

    fill(OLD, NEW);
    await userEvent.click(submitButton());

    expect(await screen.findByRole('heading', { level: 1, name: 'Welcome back' })).toBeVisible();
    expect(within(main()).queryByLabelText('Current password')).not.toBeInTheDocument();
  });

  it('submits once while the request is pending', async () => {
    let release!: (response: Response) => void;
    const pending = new Promise<Response>((resolve) => { release = resolve; });
    const fake = installFakeServer({ ...accountRoutes(), [changePath]: () => pending });
    openAccount();
    await screen.findByRole('heading', { level: 1, name: 'Your account' });

    fill(OLD, NEW);
    const submit = submitButton();
    // Two clicks before React can repaint: the guard, not the disabled attribute, has to stop the second one.
    fireEvent.click(submit);
    fireEvent.click(submit);

    await waitFor(() => expect(within(main()).getByRole('button', { name: 'Changing…' })).toBeDisabled());
    expect(changes(fake)).toHaveLength(1);
    await act(async () => {
      release(json(200, { ...session(), expiresAt: '2031-01-01T00:00:00Z' }));
    });
    expect(await within(main()).findByText('Your password is changed.', { exact: false })).toBeVisible();
    expect(changes(fake)).toHaveLength(1);
  });

  it('renders in Czech', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer(accountRoutes());
    openAccount();
    await screen.findByRole('heading', { level: 1, name: 'Váš účet' });

    expect(within(main()).getByRole('heading', { level: 2, name: 'Heslo' })).toBeVisible();
    expect(within(main()).getByLabelText('Současné heslo')).toBeVisible();
    expect(within(main()).getByLabelText('Nové heslo')).toBeVisible();
    expect(within(main()).getByLabelText('Zadejte nové heslo znovu')).toBeVisible();
    expect(within(main()).getByText('Použijte 15 až 128 znaků.')).toBeVisible();
    expect(within(main()).getByRole('button', { name: 'Změnit heslo' })).toBeVisible();
  });
});

describe('Account password section outcome host', () => {
  it('hands the rotated session back so the app keeps the new expiry', async () => {
    const change = passwordChange({ currentPassword: OLD, expiresAt: '2031-01-01T00:00:00Z' });
    installFakeServer(change.routes);
    const onChanged = vi.fn();
    const onSignedOut = vi.fn();
    render(
      <I18nProvider>
        <AccountPasswordSection session={session()} onChanged={onChanged} onSignedOut={onSignedOut} />
      </I18nProvider>,
    );

    fireEvent.change(screen.getByLabelText('Current password'), { target: { value: OLD } });
    fireEvent.change(screen.getByLabelText('New password'), { target: { value: NEW } });
    fireEvent.change(screen.getByLabelText('Type the new password again'), { target: { value: NEW } });
    await userEvent.click(screen.getByRole('button', { name: 'Change password' }));

    await waitFor(() => expect(onChanged).toHaveBeenCalledTimes(1));
    expect(onChanged).toHaveBeenCalledWith(expect.objectContaining({
      userId: session().userId,
      login: 'anna',
      expiresAt: '2031-01-01T00:00:00Z',
      signInMethod: 'password',
    }));
    expect(onSignedOut).not.toHaveBeenCalled();
  });
});
