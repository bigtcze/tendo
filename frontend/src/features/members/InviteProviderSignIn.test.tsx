import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { browserNavigation } from '../auth/oidcApi';
import { I18nProvider } from '../../i18n';
import { en } from '../../i18n/en';
import { installFakeServer, json } from '../../test/fakeServer';

const token = 'a'.repeat(43);
const problem = (status: number, code?: string) => json(status, {
  type: 'about:blank', title: 'x', status, ...(code ? { code } : {}),
}, { 'Content-Type': 'application/problem+json' });
const signedOut = { 'GET /api/v1/session': problem(401), 'GET /api/v1/auth/setup': json(200, { required: false }) };
const provider = { 'GET /api/v1/auth/oidc': json(200, { enabled: true, displayName: 'Pocket ID' }) };
const authorizationUrl = 'https://id.example/authorize?client_id=tendo&state=opaque';

function renderApp() {
  return render(<I18nProvider><App /></I18nProvider>);
}

let assign: ReturnType<typeof vi.spyOn>;
beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
  assign = vi.spyOn(browserNavigation, 'assign').mockImplementation(() => {});
  window.history.replaceState(null, '', `/invite#${token}`);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  window.history.replaceState(null, '', '/');
});

describe('invite with the household provider', () => {
  it('carries the invitation token through the provider redirect', async () => {
    const fake = installFakeServer({
      ...signedOut, ...provider,
      'POST /api/v1/auth/oidc/start': json(200, { authorizationUrl }),
    });
    renderApp();
    const button = await screen.findByRole('button', { name: 'Continue with Pocket ID' });
    await userEvent.click(button);
    await waitFor(() => expect(assign).toHaveBeenCalledWith(authorizationUrl));
    const start = fake.requests.find((request) => request.path === '/api/v1/auth/oidc/start');
    expect(JSON.parse(start!.body)).toEqual({ purpose: 'login', invitationToken: token });
    const startHeaders = fake.requests.find((request) => request.path === '/api/v1/auth/oidc/start')!.headers;
    expect(startHeaders.get('Origin')).toBe(window.location.origin);
  });

  it('an invalid invitation is explained and the password path stays available', async () => {
    installFakeServer({
      ...signedOut, ...provider,
      'POST /api/v1/auth/oidc/start': problem(422, 'invalid_invitation'),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Continue with Pocket ID' }));
    expect(await screen.findByText(en['invite.error.invalid'], { exact: true })).toBeVisible();
    expect(screen.getByLabelText(en['invite.login'])).toBeVisible();
    expect(screen.getByRole('button', { name: en['invite.create'] })).toBeEnabled();
    expect(assign).not.toHaveBeenCalled();
  });

  it('a provider outage keeps the create-account form usable', async () => {
    installFakeServer({
      ...signedOut, ...provider,
      'POST /api/v1/auth/oidc/start': problem(503, 'oidc_unavailable'),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Continue with Pocket ID' }));
    expect(await screen.findByText('Pocket ID can’t be reached right now. You can still use your password.')).toBeVisible();
    expect(screen.getByRole('button', { name: en['invite.create'] })).toBeEnabled();
  });

  it('stays out of the way when the provider is disabled', async () => {
    installFakeServer({ ...signedOut, 'GET /api/v1/auth/oidc': json(200, { enabled: false }) });
    renderApp();
    expect(await screen.findByRole('button', { name: en['invite.create'] })).toBeVisible();
    await waitFor(() => expect(screen.queryByRole('button', { name: /Continue with/ })).not.toBeInTheDocument());
    expect(screen.queryByText(en['login.or'])).not.toBeInTheDocument();
  });
});
