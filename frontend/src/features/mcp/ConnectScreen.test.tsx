import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ScreenTransitionContext } from '../../app/Heading';
import type { ConnectOutcome } from '../../app/useConnectVisit';
import { I18nProvider } from '../../i18n';
import { en } from '../../i18n/en';
import { cs } from '../../i18n/cs';
import { installFakeServer, json, session } from '../../test/fakeServer';
import { browserNavigation } from '../auth/oidcApi';
import { clearConnectEntry } from './connectEntry';
import { ConnectScreen } from './ConnectScreen';

const handle = 'A'.repeat(43);
const path = `/api/v1/mcp/authorization-requests/${handle}`;
const redirectTo = 'https://xn--pklad-zsa.example/cb?code=c&state=s';
const request = {
  clientName: 'Claude Code', clientNameVerified: false as const, redirectUri: 'https://xn--pklad-zsa.example/cb',
  scopes: ['tendo:read', 'tendo:write'] as ('tendo:read' | 'tendo:write')[],
  allowedScopes: ['tendo:read', 'tendo:write'] as ('tendo:read' | 'tendo:write')[], expiresAt: '2026-11-20T10:05:00Z',
};

let focused = true;

beforeEach(() => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  focused = true;
  vi.spyOn(document, 'hasFocus').mockImplementation(() => focused);
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
  vi.spyOn(browserNavigation, 'assign').mockImplementation(() => undefined);
  localStorage.removeItem('tendo.locale');
  sessionStorage.clear();
  clearConnectEntry();
  window.history.replaceState(null, '', '/connect');
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

type Props = { outcome?: ConnectOutcome; onSignOut?: () => void; onSessionEnded?: () => void; onHome?: () => void };

/** Renders the screen with the app's providers; the transition context lets the heading take focus as on a real visit. */
function renderConnect({ outcome = { kind: 'request', handle }, onSignOut = vi.fn(), onSessionEnded = vi.fn(), onHome = vi.fn() }: Props = {}) {
  return render(
    <I18nProvider>
      <ScreenTransitionContext value={{ current: true }}>
        <ConnectScreen outcome={outcome} session={session()} onSignOut={onSignOut} onSessionEnded={onSessionEnded} onHome={onHome} />
      </ScreenTransitionContext>
    </I18nProvider>,
  );
}

function consentRoutes(extra: Record<string, Parameters<typeof installFakeServer>[0][string]> = {}) {
  return { [`GET ${path}`]: json(200, request), ...extra };
}

/**
 * Moves the fake clock in small steps until Allow arms. The delay timer is armed in an effect after the ready render,
 * so a single fixed advance can end just short of it under shouldAdvanceTime; stepping keeps the gate deterministic.
 */
async function waitForAllow() {
  for (let step = 0; step < 30 && allowButton().hasAttribute('disabled'); step += 1) {
    await act(() => vi.advanceTimersByTime(100));
  }
}

/** Fake timers stall userEvent's internal delays, so clicks use the DOM event directly. */
function click(element: HTMLElement) {
  fireEvent.click(element);
}

function allowButton() {
  return screen.getByRole('button', { name: en['connect.allow'] });
}

describe('ready state', () => {
  it('names the app, marks the name unverified and shows the punycode host as given', async () => {
    installFakeServer(consentRoutes());
    renderConnect();
    expect(await screen.findByRole('heading', { level: 1, name: 'Connect Claude Code?' })).toBeVisible();
    expect(screen.getByText(en['connect.unverified'])).toBeVisible();
    expect(screen.getByText('xn--pklad-zsa.example', { exact: false })).toBeVisible();
    expect(screen.getByText(en['connect.scope.read'])).toBeVisible();
    expect(screen.getByText(en['connect.scope.write'])).toBeVisible();
    expect(screen.getByText(en['connect.scopes.coverage'])).toBeVisible();
    expect(screen.getByText(/Signed in as/)).toHaveTextContent('Signed in as anna.');
    const expires = new Intl.DateTimeFormat('en', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(request.expiresAt));
    expect(screen.getByText(/This request expires at/)).toHaveTextContent(`This request expires at ${expires}.`);
  });

  it('localizes the heading, the marker and the expiry in Czech', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer(consentRoutes());
    renderConnect();
    expect(await screen.findByRole('heading', { level: 1, name: 'Připojit Claude Code?' })).toBeVisible();
    expect(screen.getByText(cs['connect.unverified'])).toBeVisible();
    expect(screen.getByText(cs['connect.scope.read'])).toBeVisible();
    const expires = new Intl.DateTimeFormat('cs', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(request.expiresAt));
    expect(screen.getByText(/Tato žádost vyprší/)).toHaveTextContent(`Tato žádost vyprší ${expires}.`);
    expect(screen.getByRole('button', { name: cs['connect.allow'] })).toBeDisabled();
  });
});

describe('allowed scopes', () => {
  it('lists only what the account allows and names what the app will not get', async () => {
    installFakeServer({ [`GET ${path}`]: json(200, { ...request, allowedScopes: ['tendo:read'] }) });
    renderConnect();
    await screen.findByRole('heading', { level: 1, name: 'Connect Claude Code?' });
    const granted = screen.getByRole('list', { name: en['connect.scopes.intro'] });
    expect(within(granted).getAllByRole('listitem').map((item) => item.textContent)).toEqual([en['connect.scope.read']]);
    const withheld = screen.getByRole('list', { name: en['connect.scopes.withheld'] });
    expect(within(withheld).getAllByRole('listitem').map((item) => item.textContent)).toEqual([en['connect.scope.write']]);
    expect(screen.getByText(en['connect.scopes.coverage'])).toBeVisible();
    await waitForAllow();
    expect(allowButton()).toBeEnabled();
  });

  it('Allow names exactly the scopes it showed, so a wider policy at the decision grants nothing more', async () => {
    const fake = installFakeServer({
      [`GET ${path}`]: json(200, { ...request, allowedScopes: ['tendo:read'] }),
      [`POST ${path}/decision`]: json(200, { redirectTo }),
    });
    renderConnect();
    await screen.findByRole('heading', { level: 1, name: 'Connect Claude Code?' });
    await waitForAllow();
    click(allowButton());
    await waitFor(() => expect(browserNavigation.assign).toHaveBeenCalledWith(redirectTo));
    const post = fake.requests.find((r) => r.method === 'POST');
    expect(JSON.parse(post?.body ?? '{}')).toEqual({ decision: 'approve', scopes: ['tendo:read'] });
  });

  it('shows no withheld list when everything asked for is allowed', async () => {
    installFakeServer(consentRoutes());
    renderConnect();
    await screen.findByRole('heading', { level: 1, name: 'Connect Claude Code?' });
    expect(screen.queryByText(en['connect.scopes.withheld'])).toBeNull();
    expect(within(screen.getByRole('list', { name: en['connect.scopes.intro'] })).getAllByRole('listitem')).toHaveLength(2);
  });
});

describe('blocked', () => {
  const blocked = { ...request, scopes: ['tendo:write'], allowedScopes: [] };

  it('says the app cannot be connected, offers no Allow and only a way back to the app', async () => {
    const fake = installFakeServer({
      [`GET ${path}`]: json(200, blocked),
      [`POST ${path}/decision`]: json(200, { redirectTo }),
    });
    renderConnect();
    const heading = await screen.findByRole('heading', { level: 1, name: 'Claude Code can’t be connected' });
    expect(heading).toHaveFocus();
    expect(screen.getByText(en['connect.blocked.body'])).toBeVisible();
    expect(screen.getByText(en['connect.blocked.hint'])).toBeVisible();
    // The person's own settings can be the block, so the hint names where they are.
    expect(en['connect.blocked.hint']).toMatch(/AI apps in Your account/);
    const asked = screen.getByRole('list', { name: en['connect.blocked.asked'] });
    expect(within(asked).getAllByRole('listitem').map((item) => item.textContent)).toEqual([en['connect.scope.write']]);
    expect(screen.queryByText(en['connect.scopes.coverage'])).toBeNull();
    expect(screen.queryByText(en['connect.allowHint'])).toBeNull();
    await act(() => vi.advanceTimersByTime(3000));
    expect(screen.queryByRole('button', { name: en['connect.allow'] })).toBeNull();
    expect(screen.queryByRole('button', { name: en['connect.deny'] })).toBeNull();
    expect(screen.getByText(/Signed in as/)).toHaveTextContent('Signed in as anna.');
    click(screen.getByRole('button', { name: en['connect.back'] }));
    await waitFor(() => expect(browserNavigation.assign).toHaveBeenCalledWith(redirectTo));
    const posts = fake.requests.filter((r) => r.method === 'POST');
    expect(posts.map((r) => JSON.parse(r.body))).toEqual([{ decision: 'deny' }]);
  });

  it('keeps the way back after a failed answer and says why', async () => {
    installFakeServer({
      [`GET ${path}`]: json(200, blocked),
      [`POST ${path}/decision`]: json(503, { title: 'x', status: 503 }),
    });
    renderConnect();
    await screen.findByRole('heading', { level: 1, name: 'Claude Code can’t be connected' });
    click(screen.getByRole('button', { name: en['connect.back'] }));
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent(en['connect.unavailable']);
    expect(alert).toHaveFocus();
    expect(screen.getByRole('button', { name: en['connect.back'] })).toBeEnabled();
  });

  it('is localized in Czech', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer({ [`GET ${path}`]: json(200, blocked) });
    renderConnect();
    expect(await screen.findByRole('heading', { level: 1, name: 'Claude Code nelze připojit' })).toBeVisible();
    expect(screen.getByText(cs['connect.blocked.body'])).toBeVisible();
    expect(screen.getByRole('button', { name: cs['connect.back'] })).toBeEnabled();
  });
});

describe('Allow gate', () => {
  it('starts disabled, carries the hint, and never takes focus', async () => {
    installFakeServer(consentRoutes());
    renderConnect();
    await screen.findByRole('heading', { level: 1, name: 'Connect Claude Code?' });
    const allow = allowButton();
    expect(allow).toBeDisabled();
    expect(allow).not.toHaveFocus();
    expect(allow).toHaveAttribute('aria-describedby', expect.stringContaining('connect-allow-hint'));
    expect(screen.getByRole('heading', { level: 1 })).toHaveFocus();
  });

  it('becomes enabled after the delay and the hint goes away', async () => {
    installFakeServer(consentRoutes());
    renderConnect();
    await screen.findByRole('heading', { level: 1, name: 'Connect Claude Code?' });
    await waitForAllow();
    expect(allowButton()).toBeEnabled();
    expect(screen.queryByText(en['connect.allowHint'])).toBeNull();
  });
});

describe('decisions', () => {
  it('Allow sends approve, navigates to the redirect and leaves no buttons behind', async () => {
    const fake = installFakeServer(consentRoutes({
      [`POST ${path}/decision`]: json(200, { redirectTo }),
    }));
    renderConnect();
    await screen.findByRole('heading', { level: 1, name: 'Connect Claude Code?' });
    await waitForAllow();
    expect(allowButton()).toBeEnabled();
    click(allowButton());
    await waitFor(() => expect(browserNavigation.assign).toHaveBeenCalledWith(redirectTo));
    expect(await screen.findByRole('heading', { level: 1, name: en['connect.returning'] })).toBeVisible();
    expect(screen.getByText(en['connect.returningHint'])).toBeVisible();
    expect(within(screen.getByRole('main')).queryByRole('button')).toBeNull();
    expect(fake.requests.find((r) => r.method === 'POST')?.body).toContain('"decision":"approve"');
  });

  it('Deny sends deny without waiting for the Allow delay', async () => {
    const fake = installFakeServer(consentRoutes({ [`POST ${path}/decision`]: json(200, { redirectTo }) }));
    renderConnect();
    await screen.findByRole('heading', { level: 1, name: 'Connect Claude Code?' });
    click(screen.getByRole('button', { name: en['connect.deny'] }));
    await waitFor(() => expect(browserNavigation.assign).toHaveBeenCalledWith(redirectTo));
    const post = fake.requests.find((r) => r.method === 'POST');
    expect(JSON.parse(post?.body ?? '{}')).toEqual({ decision: 'deny' });
  });

  it('a decision 429 shows the inline alert and brings the buttons back', async () => {
    installFakeServer(consentRoutes({ [`POST ${path}/decision`]: json(429, { title: 'x', status: 429 }) }));
    renderConnect();
    await screen.findByRole('heading', { level: 1, name: 'Connect Claude Code?' });
    click(screen.getByRole('button', { name: en['connect.deny'] }));
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent(en['connect.rateLimited']);
    expect(alert).toHaveFocus();
    expect(screen.getByRole('button', { name: en['connect.deny'] })).toBeEnabled();
    expect(screen.getByRole('button', { name: en['connect.allow'] })).toBeInTheDocument();
  });
});

describe('sign-out while deciding', () => {
  it('disables Not you? Sign out while a decision is in flight', async () => {
    let release!: (value: Response) => void;
    const held = new Promise<Response>((resolve) => { release = resolve; });
    installFakeServer(consentRoutes({ [`POST ${path}/decision`]: () => held }));
    renderConnect();
    await screen.findByRole('heading', { level: 1, name: 'Connect Claude Code?' });
    click(screen.getByRole('button', { name: en['connect.deny'] }));
    expect(screen.getByRole('button', { name: en['connect.notYou'] })).toBeDisabled();
    await act(async () => { release(json(200, { redirectTo })); });
  });
});

describe('load and session states', () => {
  it('a 404 on load says the request is gone and links Home', async () => {
    installFakeServer({ [`GET ${path}`]: json(404, { title: 'x', status: 404 }) });
    const onHome = vi.fn();
    renderConnect({ onHome });
    expect(await screen.findByRole('heading', { level: 1, name: en['connect.problem.title'] })).toBeVisible();
    expect(screen.getByText(en['connect.gone'])).toBeVisible();
    expect(screen.queryByRole('button', { name: en['connect.allow'] })).toBeNull();
    click(screen.getByRole('link', { name: en['nav.home'] }));
    expect(onHome).toHaveBeenCalledTimes(1);
  });

  it('"Not you? Sign out" calls logout and then onSignOut', async () => {
    const fake = installFakeServer(consentRoutes({ 'DELETE /api/v1/session': json(204) }));
    const onSignOut = vi.fn();
    renderConnect({ onSignOut });
    await screen.findByRole('heading', { level: 1, name: 'Connect Claude Code?' });
    click(screen.getByRole('button', { name: en['connect.notYou'] }));
    await waitFor(() => expect(onSignOut).toHaveBeenCalledTimes(1));
    expect(fake.requests.some((r) => r.method === 'DELETE' && r.path === '/api/v1/session')).toBe(true);
  });

  it('a failed sign-out shows an inline error and keeps the consent', async () => {
    installFakeServer(consentRoutes({ 'DELETE /api/v1/session': json(500, { title: 'x', status: 500 }) }));
    const onSignOut = vi.fn();
    renderConnect({ onSignOut });
    await screen.findByRole('heading', { level: 1, name: 'Connect Claude Code?' });
    click(screen.getByRole('button', { name: en['connect.notYou'] }));
    expect(await screen.findByRole('alert')).toHaveTextContent(en['header.signOutError']);
    expect(onSignOut).not.toHaveBeenCalled();
  });
});

describe('long and unusual values', () => {
  it('renders a 100-character client name', async () => {
    const longName = 'Long app name '.repeat(8).slice(0, 100);
    expect(longName).toHaveLength(100);
    installFakeServer({ [`GET ${path}`]: json(200, { ...request, clientName: longName }) });
    renderConnect();
    expect(await screen.findByRole('heading', { level: 1, name: `Connect ${longName}?` })).toBeVisible();
  });

  it('shows a loopback destination with its host and port', async () => {
    installFakeServer({ [`GET ${path}`]: json(200, { ...request, redirectUri: 'http://127.0.0.1:54123/callback' }) });
    renderConnect();
    expect(await screen.findByText('127.0.0.1:54123')).toBeVisible();
    expect(screen.getByText(/An app on this computer/)).toHaveTextContent('An app on this computer (127.0.0.1:54123).');
  });

  it('shows a private-use scheme with the scheme and the claim warning', async () => {
    installFakeServer({ [`GET ${path}`]: json(200, { ...request, redirectUri: 'com.example.app:/oauth2redirect' }) });
    renderConnect();
    expect(await screen.findByText('com.example.app')).toBeVisible();
    expect(screen.getByText(/An app on this device/)).toHaveTextContent('An app on this device (com.example.app:).');
    expect(screen.getByText(en['connect.destination.schemeNote'])).toBeVisible();
  });
});

describe('problem outcomes', () => {
  const cases: [ConnectOutcome, string][] = [
    [{ kind: 'error', code: 'invalid_client' }, en['connect.problem.invalid_client']],
    [{ kind: 'error', code: 'invalid_redirect_uri' }, en['connect.problem.invalid_client']],
    [{ kind: 'error', code: 'temporarily_unavailable' }, en['connect.problem.temporarily_unavailable']],
    [{ kind: 'error', code: 'server_error' }, en['connect.problem.server_error']],
    [{ kind: 'error', code: 'invalid_request' }, en['connect.problem.request']],
    [{ kind: 'error', code: 'unsupported_response_type' }, en['connect.problem.request']],
    [{ kind: 'error', code: 'invalid_scope' }, en['connect.problem.request']],
    [{ kind: 'error', code: 'invalid_target' }, en['connect.problem.request']],
    [{ kind: 'invalid' }, en['connect.problem.request']],
    [{ kind: 'none' }, en['connect.gone']],
  ];

  it.each(cases)('%j shows its message, a Home link and no decision buttons', (outcome, message) => {
    renderConnect({ outcome });
    expect(screen.getByRole('heading', { level: 1, name: en['connect.problem.title'] })).toBeVisible();
    expect(screen.getByText(message)).toBeVisible();
    expect(screen.getByRole('link', { name: en['nav.home'] })).toBeVisible();
    expect(within(document.body).queryByRole('button', { name: en['connect.allow'] })).toBeNull();
    expect(within(document.body).queryByRole('button', { name: en['connect.deny'] })).toBeNull();
  });
});
