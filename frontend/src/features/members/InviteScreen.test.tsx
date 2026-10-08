import { act, StrictMode } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, emptyHome, household, installFakeServer, json, session } from '../../test/fakeServer';
const token = 'a'.repeat(43); const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const problem = (status: number, extra: Record<string, unknown> = {}) => json(status, { type: 'about:blank', title: 'x', status, ...extra });
const member = { userId: session().userId, login: 'anna', role: 'owner' };
function signedInRoutes(extra: Record<string, unknown> = {}) { return { 'GET /api/v1/session': json(200, session()), [`GET ${base}`]: json(200, household), ...emptyHome, [`GET ${base}/members`]: json(200, { items: [member], nextCursor: null }), ...extra }; }
function signedOutRoutes(extra: Record<string, unknown> = {}) { return { 'GET /api/v1/session': problem(401), 'GET /api/v1/auth/setup': json(200, { required: false }), ...extra }; }
function renderApp(strict = false) { const app = <I18nProvider><App /></I18nProvider>; return render(strict ? <StrictMode>{app}</StrictMode> : app); }
beforeEach(() => { vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']); });
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); localStorage.removeItem('tendo.locale'); });

describe('invitation acceptance', () => {
  it('preserves token through StrictMode, strips hash, creates account with exact requests and lands home', async () => {
    window.history.replaceState(null, '', `/invite#${token}`);
    const fake = installFakeServer(signedOutRoutes({ 'POST /api/v1/auth/invitations/accept': json(201, {}), 'POST /api/v1/session': json(201, session()), [`GET ${base}`]: json(200, household) }));
    renderApp(true); expect(await screen.findByRole('heading', { level: 1, name: 'You’ve been invited to Tendo' })).toBeVisible();
    await waitFor(() => { expect(location.pathname).toBe('/invite'); expect(location.hash).toBe(''); });
    await userEvent.type(screen.getByLabelText('Login'), 'newuser'); await userEvent.type(screen.getByLabelText('Password'), 'a sufficiently long password'); await userEvent.click(screen.getByRole('button', { name: 'Create account and join' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible();
    const accept = fake.requests.find((r) => r.method === 'POST' && r.path === '/api/v1/auth/invitations/accept')!;
    expect(JSON.parse(accept.body)).toEqual({ token, login: 'newuser', password: 'a sufficiently long password' });
    const login = fake.requests.find((r) => r.method === 'POST' && r.path === '/api/v1/session')!; expect(JSON.parse(login.body)).toEqual({ login: 'newuser', password: 'a sufficiently long password' });
    expect(fake.requests.findIndex((r) => r === accept)).toBeLessThan(fake.requests.findIndex((r) => r === login));
    expect(localStorage.getItem('tendo.locale') ?? '').not.toContain(token);
    expect(sessionStorage.getItem('inviteToken') ?? '').not.toContain(token);
    expect(JSON.stringify(Object.entries(localStorage))).not.toContain(token);
    expect(JSON.stringify(Object.entries(sessionStorage))).not.toContain(token);
  });

  it('Back restores the token belonging to that history entry and Forward restores the next visit', async () => {
    window.history.replaceState(null, '', '/');
    const tokenA = 'c'.repeat(43); const tokenB = 'd'.repeat(43);
    const fake = installFakeServer(signedOutRoutes({ 'POST /api/v1/auth/invitations/accept': problem(422, { field: 'login' }) }));
    renderApp();
    window.history.pushState(null, '', `/invite#${tokenA}`); window.dispatchEvent(new PopStateEvent('popstate'));
    await screen.findByLabelText('Login');
    await waitFor(() => expect(window.location.hash).toBe(''));
    window.location.hash = tokenB;
    await waitFor(() => expect(window.location.hash).toBe(''));
    await userEvent.type(screen.getByLabelText('Login'), 'newuser'); await userEvent.type(screen.getByLabelText('Password'), 'a sufficiently long password');
    await userEvent.click(screen.getByRole('button', { name: 'Create account and join' }));
    await waitFor(() => expect(fake.requests.filter((r) => r.method === 'POST' && r.path === '/api/v1/auth/invitations/accept')).toHaveLength(1));
    expect(JSON.parse(fake.requests.filter((r) => r.method === 'POST' && r.path === '/api/v1/auth/invitations/accept')[0]!.body).token).toBe(tokenB);
    expect(fake.requests.filter((r) => r.method === 'POST' && r.path === '/api/v1/auth/invitations/accept')[0]!.body).not.toContain(token);

    await act(async () => { window.history.back(); await new Promise((resolve) => setTimeout(resolve, 0)); });
    await waitFor(() => expect(location.pathname).toBe('/invite'));
    await userEvent.type(screen.getByLabelText('Login'), 'newuser'); await userEvent.type(screen.getByLabelText('Password'), 'a sufficiently long password');
    await userEvent.click(screen.getByRole('button', { name: 'Create account and join' }));
    await waitFor(() => expect(fake.requests.filter((r) => r.method === 'POST' && r.path === '/api/v1/auth/invitations/accept')).toHaveLength(2));
    expect(JSON.parse(fake.requests.filter((r) => r.method === 'POST' && r.path === '/api/v1/auth/invitations/accept')[1]!.body).token).toBe(tokenA);

    await act(async () => { window.history.forward(); await new Promise((resolve) => setTimeout(resolve, 0)); });
    await waitFor(() => expect(location.pathname).toBe('/invite'));
    await userEvent.type(screen.getByLabelText('Login'), 'newuser'); await userEvent.type(screen.getByLabelText('Password'), 'a sufficiently long password');
    await userEvent.click(screen.getByRole('button', { name: 'Create account and join' }));
    await waitFor(() => expect(fake.requests.filter((r) => r.method === 'POST' && r.path === '/api/v1/auth/invitations/accept')).toHaveLength(3));
    expect(JSON.parse(fake.requests.filter((r) => r.method === 'POST' && r.path === '/api/v1/auth/invitations/accept')[2]!.body).token).toBe(tokenB);
  });

  it('after remounting, a later token visit cannot resolve a lost history entry', async () => {
    window.history.replaceState(null, '', '/');
    const tokenA = 'e'.repeat(43); const tokenB = 'f'.repeat(43);
    const routes = signedOutRoutes({ 'POST /api/v1/auth/invitations/accept': problem(404) });
    const fake = installFakeServer(routes);
    const firstMount = renderApp();
    window.history.pushState(null, '', `/invite#${tokenA}`); window.dispatchEvent(new PopStateEvent('popstate'));
    await waitFor(() => expect(location.hash).toBe(''));
    firstMount.unmount();

    const secondMount = renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'This invite link isn’t complete' })).toBeVisible();
    window.location.hash = tokenB;
    await waitFor(() => expect(location.hash).toBe(''));
    await screen.findByRole('heading', { level: 1, name: 'You’ve been invited to Tendo' });

    await act(async () => { window.history.back(); await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(await screen.findByRole('heading', { level: 1, name: 'This invite link isn’t complete' })).toBeVisible();
    expect(screen.queryByLabelText('Login')).not.toBeInTheDocument();
    expect(fake.requests.some((request) => request.method === 'POST' && request.path === '/api/v1/auth/invitations/accept')).toBe(false);
    expect(fake.requests.some((request) => request.method === 'POST' && request.path === '/api/v1/auth/invitations/accept' && request.body.includes(tokenB))).toBe(false);
    secondMount.unmount();
  });

  it('unknown invite history entry is incomplete and never falls back to a previous token', async () => {
    window.history.replaceState(null, '', `/invite#${token}`);
    const fake = installFakeServer(signedOutRoutes()); renderApp();
    await screen.findByRole('heading', { level: 1, name: 'You’ve been invited to Tendo' });
    await waitFor(() => expect(location.hash).toBe(''));
    window.history.pushState({ inviteVisit: 987654 }, '', '/invite'); window.dispatchEvent(new PopStateEvent('popstate'));
    expect(await screen.findByRole('heading', { level: 1, name: 'This invite link isn’t complete' })).toBeVisible();
    expect(fake.requests.some((r) => r.path.includes('invitations/accept'))).toBe(false);
  });

  it('skip to content preserves the invite route, token, and typed login', async () => {
    window.history.replaceState(null, '', `/invite#${token}`);
    const fake = installFakeServer(signedOutRoutes({ 'POST /api/v1/auth/invitations/accept': problem(422, { field: 'login' }) }));
    renderApp();
    const scroll = vi.fn(); HTMLElement.prototype.scrollIntoView = scroll;
    const login = await screen.findByLabelText('Login'); await userEvent.type(login, 'newuser');
    const skip = screen.getByRole('link', { name: 'Skip to content' }); skip.focus(); await userEvent.keyboard('{Enter}');
    expect(location.pathname).toBe('/invite'); expect(location.hash).toBe(''); expect(login).toHaveValue('newuser');
    expect(document.getElementById('main')).toHaveFocus(); expect(scroll).toHaveBeenCalled();
    await userEvent.type(screen.getByLabelText('Password'), 'a sufficiently long password'); await userEvent.click(screen.getByRole('button', { name: 'Create account and join' }));
    expect(JSON.parse(fake.requests.find((r) => r.method === 'POST' && r.path === '/api/v1/auth/invitations/accept')!.body).token).toBe(token);
  });

  it('captures the latest invite hash on a visit without remounting App', async () => {
    window.history.replaceState(null, '', `/invite#${token}`);
    const tokenB = 'b'.repeat(43);
    const fake = installFakeServer(signedOutRoutes({ 'POST /api/v1/auth/invitations/accept': json(201, {}), 'POST /api/v1/session': problem(401), 'GET /api/v1/auth/setup': json(200, { required: false }) }));
    const rendered = renderApp();
    await screen.findByLabelText('Login');
    window.location.hash = tokenB;
    if (window.location.hash !== `#${tokenB}`) window.history.replaceState(null, '', `/invite#${tokenB}`);
    window.dispatchEvent(new HashChangeEvent('hashchange'));
    await waitFor(() => { expect(location.pathname).toBe('/invite'); expect(location.hash).toBe(''); });
    await userEvent.type(await screen.findByLabelText('Login'), 'newuser'); await userEvent.type(screen.getByLabelText('Password'), 'a sufficiently long password'); await userEvent.click(screen.getByRole('button', { name: 'Create account and join' }));
    const accept = fake.requests.find((r) => r.method === 'POST' && r.path === '/api/v1/auth/invitations/accept')!;
    expect(JSON.parse(accept.body).token).toBe(tokenB);
    rendered.unmount();
  });

  it('unmounting while account accept is pending prevents subsequent login and callbacks', async () => {
    window.history.replaceState(null, '', `/invite#${token}`);
    let release!: (response: Response) => void;
    const deferred = new Promise<Response>((resolve) => { release = resolve; });
    const fake = installFakeServer(signedOutRoutes({ 'POST /api/v1/auth/invitations/accept': () => deferred, 'POST /api/v1/session': json(201, session()) }));
    const rendered = renderApp();
    await userEvent.type(await screen.findByLabelText('Login'), 'newuser'); await userEvent.type(screen.getByLabelText('Password'), 'a sufficiently long password'); await userEvent.click(screen.getByRole('button', { name: 'Create account and join' }));
    await waitFor(() => expect(fake.requests.some((r) => r.method === 'POST' && r.path === '/api/v1/auth/invitations/accept')).toBe(true));
    window.history.pushState(null, '', '/'); window.dispatchEvent(new PopStateEvent('popstate'));
    await screen.findByRole('heading', { level: 1, name: 'Sign in to Tendo' });
    await act(async () => {
      release(json(201, {}));
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    await waitFor(() => expect(fake.requests.some((r) => r.method === 'POST' && r.path === '/api/v1/auth/invitations/accept')).toBe(true));
    expect(fake.requests.some((r) => r.method === 'POST' && r.path === '/api/v1/session')).toBe(false);
    expect(screen.getByRole('heading', { level: 1, name: 'Sign in to Tendo' })).toBeVisible();
    rendered.unmount();
  });

  it('new account invalid link focuses notice and hides form', async () => {
    window.history.replaceState(null, '', `/invite#${token}`); installFakeServer(signedOutRoutes({ 'POST /api/v1/auth/invitations/accept': problem(404) })); renderApp();
    await userEvent.type(await screen.findByLabelText('Login'), 'newuser'); await userEvent.type(screen.getByLabelText('Password'), 'long enough password'); await userEvent.click(screen.getByRole('button', { name: 'Create account and join' }));
    const notice = await screen.findByRole('status'); expect(notice).toHaveTextContent(/invite link can’t be used/); expect(notice).toHaveFocus(); expect(screen.queryByLabelText('Login')).not.toBeInTheDocument(); expect(screen.queryByRole('button', { name: 'Create account and join' })).not.toBeInTheDocument(); expect(screen.getByRole('link', { name: 'Go home' })).toBeVisible();
  });

  it('409 login unavailable marks and focuses login only', async () => {
    window.history.replaceState(null, '', `/invite#${token}`); installFakeServer(signedOutRoutes({ 'POST /api/v1/auth/invitations/accept': problem(409, { code: 'login_unavailable' }) })); renderApp();
    await userEvent.type(await screen.findByLabelText('Login'), 'newuser'); await userEvent.type(screen.getByLabelText('Password'), 'long enough password'); await userEvent.click(screen.getByRole('button', { name: 'Create account and join' }));
    const login = screen.getByLabelText('Login'); expect(login).toHaveAttribute('aria-invalid', 'true'); expect(login).toHaveFocus(); expect(screen.getByRole('alert')).toHaveTextContent('That login is taken.'); expect(screen.getByLabelText('Password')).not.toHaveAttribute('aria-invalid', 'true');
  });

  it('422 password field focuses password with a single alert', async () => {
    window.history.replaceState(null, '', `/invite#${token}`); installFakeServer(signedOutRoutes({ 'POST /api/v1/auth/invitations/accept': problem(422, { field: 'password' }) })); renderApp();
    await userEvent.type(await screen.findByLabelText('Login'), 'newuser'); await userEvent.type(screen.getByLabelText('Password'), 'short'); await userEvent.click(screen.getByRole('button', { name: 'Create account and join' }));
    const pass = screen.getByLabelText('Password'); expect(pass).toHaveAttribute('aria-invalid', 'true'); expect(pass).toHaveFocus(); expect(screen.getByRole('alert')).toHaveTextContent('Passwords need to be 15 to 128 characters long.'); expect(screen.getAllByText('Passwords need to be 15 to 128 characters long.')).toHaveLength(1);
  });

  it('429 shows a focused rate-limit notice', async () => {
    window.history.replaceState(null, '', `/invite#${token}`); installFakeServer(signedOutRoutes({ 'POST /api/v1/auth/invitations/accept': problem(429) })); renderApp();
    await userEvent.type(await screen.findByLabelText('Login'), 'newuser'); await userEvent.type(screen.getByLabelText('Password'), 'long enough password'); await userEvent.click(screen.getByRole('button', { name: 'Create account and join' }));
    const notice = await screen.findByRole('status'); expect(notice).toHaveTextContent('Too many attempts. Try again in a moment.'); expect(notice).toHaveFocus();
  });

  it('failed auto-login navigates to sign-in with one account-ready notice and one heading', async () => {
    window.history.replaceState(null, '', `/invite#${token}`); installFakeServer(signedOutRoutes({ 'POST /api/v1/auth/invitations/accept': json(201, {}), 'POST /api/v1/session': problem(401) })); renderApp();
    await userEvent.type(await screen.findByLabelText('Login'), 'newuser'); await userEvent.type(screen.getByLabelText('Password'), 'long enough password'); await userEvent.click(screen.getByRole('button', { name: 'Create account and join' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Sign in to Tendo' })).toBeVisible(); expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1); expect(screen.getByText('Your account is ready. Sign in.')).toBeVisible(); expect(location.pathname).toBe('/');
  });

  it('signed-in Join posts once, then uses the refreshed session to load home', async () => {
    window.history.replaceState(null, '', `/invite#${token}`);
    let sessionReads = 0;
    const fake = installFakeServer(signedInRoutes({ 'POST /api/v1/invitations/accept': json(201, {}), 'GET /api/v1/session': () => json(200, session(++sessionReads > 1)) })); renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Join household' })); expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible();
    const acceptIndex = fake.requests.findIndex((r) => r.method === 'POST' && r.path === '/api/v1/invitations/accept');
    const sessionReadsAfterAccept = fake.requests.map((r, index) => ({ r, index })).filter(({ r, index }) => index > acceptIndex && r.method === 'GET' && r.path === '/api/v1/session');
    expect(sessionReadsAfterAccept).toHaveLength(1);
    expect(fake.requests.filter((r) => r.method === 'POST' && r.path === '/api/v1/invitations/accept')).toHaveLength(1);
    expect(JSON.parse(fake.requests[acceptIndex]!.body)).toEqual({ token }); expect(fake.requests[acceptIndex]!.headers.has('Origin')).toBe(false);
  });

  it('after accept with a failed session refresh Continue retries only the session request', async () => {
    window.history.replaceState(null, '', `/invite#${token}`);
    let reads = 0;
    const fake = installFakeServer(signedInRoutes({ 'POST /api/v1/invitations/accept': json(201, {}), 'GET /api/v1/session': () => ++reads === 1 ? json(200, session()) : reads === 2 ? problem(503) : json(200, session()) })); renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Join household' }));
    expect(await screen.findByRole('status')).toHaveTextContent('You’ve joined the household, but Tendo couldn’t load it yet.');
    await userEvent.click(screen.getByRole('button', { name: 'Continue' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Veselí' })).toBeVisible();
    expect(fake.requests.filter((r) => r.method === 'POST' && r.path === '/api/v1/invitations/accept')).toHaveLength(1);
    expect(fake.requests.filter((r) => r.method === 'GET' && r.path === '/api/v1/session')).toHaveLength(3);
  });

  it('already member shows exactly one focused notice', async () => {
    window.history.replaceState(null, '', `/invite#${token}`); installFakeServer(signedInRoutes({ 'POST /api/v1/invitations/accept': problem(409, { code: 'already_member' }) })); renderApp(); await userEvent.click(await screen.findByRole('button', { name: 'Join household' }));
    const notice = await screen.findByRole('status'); expect(notice).toHaveTextContent('You’re already in this household.'); expect(notice).toHaveFocus(); expect(screen.getAllByText('You’re already in this household.')).toHaveLength(1); expect(screen.queryByRole('button', { name: 'Join household' })).toBeVisible();
  });

  it('failed signout for household conflict remains signed in and shows unavailable', async () => {
    window.history.replaceState(null, '', `/invite#${token}`); installFakeServer(signedInRoutes({ 'POST /api/v1/invitations/accept': problem(409, { code: 'household_conflict' }), 'DELETE /api/v1/session': problem(503) })); renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Join household' }));
    await userEvent.click(screen.getByRole('button', { name: 'Sign out and continue' }));
    expect(await screen.findByRole('status')).toHaveTextContent('Tendo can’t be reached right now. Try again.');
    expect(screen.getByRole('button', { name: 'Join household' })).toBeVisible();
    expect(screen.queryByLabelText('Login')).not.toBeInTheDocument();
  });

  it('household conflict signout returns to create form and keeps original token', async () => {
    window.history.replaceState(null, '', `/invite#${token}`); const fake = installFakeServer(signedInRoutes({ 'POST /api/v1/invitations/accept': problem(409, { code: 'household_conflict' }), 'DELETE /api/v1/session': json(204) })); renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Join household' })); expect(await screen.findByRole('status')).toHaveTextContent('This account already belongs to another household.');
    await userEvent.click(screen.getByRole('button', { name: 'Sign out and continue' })); expect(await screen.findByRole('button', { name: 'Create account and join' })).toBeVisible(); expect(screen.getByLabelText('Login')).toHaveFocus();
    await userEvent.type(screen.getByLabelText('Login'), 'newuser'); await userEvent.type(screen.getByLabelText('Password'), 'long enough password');
    await userEvent.click(screen.getByRole('button', { name: 'Create account and join' }));
    const accept = fake.requests.find((r) => r.method === 'POST' && r.path === '/api/v1/auth/invitations/accept'); expect(JSON.parse(accept!.body)).toEqual({ token, login: 'newuser', password: 'long enough password' });
    expect(fake.requests.some((r) => r.method === 'DELETE' && r.path === '/api/v1/session')).toBe(true);
    expect(location.pathname).toBe('/invite');
  });

  it('401 on signed-in accept switches to create-account form retaining the token', async () => {
    window.history.replaceState(null, '', `/invite#${token}`); const fake = installFakeServer(signedInRoutes({ 'POST /api/v1/invitations/accept': problem(401), 'POST /api/v1/auth/invitations/accept': problem(404), 'GET /api/v1/auth/setup': json(200, { required: false }) })); renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Join household' })); expect(await screen.findByRole('button', { name: 'Create account and join' })).toBeVisible();
    await userEvent.type(screen.getByLabelText('Login'), 'newuser'); await userEvent.type(screen.getByLabelText('Password'), 'long enough password');
    await userEvent.click(screen.getByRole('button', { name: 'Create account and join' }));
    expect(JSON.parse(fake.requests.find((r) => r.method === 'POST' && r.path === '/api/v1/invitations/accept')!.body)).toEqual({ token });
    expect(JSON.parse(fake.requests.find((r) => r.method === 'POST' && r.path === '/api/v1/auth/invitations/accept')!.body)).toEqual({ token, login: 'newuser', password: 'long enough password' });
  });

  it.each(['', '#short'])('incomplete token %s shows message and makes no accept call', async (hash) => {
    window.history.replaceState(null, '', `/invite${hash}`); const fake = installFakeServer(signedOutRoutes()); renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'This invite link isn’t complete' })).toBeVisible(); expect(fake.requests.some((r) => r.path.includes('invitations/accept'))).toBe(false);
  });

  it('renders invite screen text in Czech', async () => {
    localStorage.setItem('tendo.locale', 'cs'); window.history.replaceState(null, '', `/invite#${token}`); installFakeServer(signedOutRoutes()); renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Někdo vás pozval do Tenda' })).toBeVisible();
  });
});
