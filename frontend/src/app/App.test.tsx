import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { I18nProvider } from '../i18n';
import { HOUSEHOLD_ID, household, installFakeServer, json, memberHousehold, session } from '../test/fakeServer';
import { itemRoutes } from '../test/fakeItems';
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
    const loginField = await screen.findByLabelText('Username');
    expect(loginField).toHaveAttribute('autocomplete', 'username');
    expect(screen.getByLabelText('Password')).toHaveAttribute('autocomplete', 'current-password');
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Welcome back');
    expect(screen.queryByRole('navigation')).not.toBeInTheDocument();
  });

  it('keeps the language switch in the signed-out footer', async () => {
    installFakeServer({
      'GET /api/v1/session': problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
    });
    renderApp();
    await screen.findByLabelText('Username');
    const footer = screen.getByRole('contentinfo');
    expect(within(footer).getByRole('group', { name: 'Language' })).toBeVisible();
    await userEvent.click(within(footer).getByRole('button', { name: 'Čeština' }));
    expect(await screen.findByLabelText('Uživatelské jméno')).toBeVisible();
    expect(document.documentElement.lang).toBe('cs');
    localStorage.removeItem('tendo.locale');
  });

  it('wrong password shows an alert and clears the password', async () => {
    installFakeServer({
      'GET /api/v1/session': problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'POST /api/v1/session': problem(401),
    });
    renderApp();
    const username = await screen.findByLabelText('Username');
    expect(document.getElementById('login-error'), 'persistent login alert host exists before any error').toBeInTheDocument();
    await userEvent.type(username, 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'wrong-password{Enter}');
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('That username or password isn’t right. Check both and try again. Forgot your password? Ask the person who runs Tendo for your household.');
    expect(screen.getByLabelText('Password')).toHaveValue('');
    expect(screen.getByLabelText('Password')).toHaveAttribute('aria-describedby', alert.id);
    expect(screen.getByLabelText('Password')).not.toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByLabelText('Username')).not.toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByLabelText('Username')).toHaveValue('anna');
  });

  it('429 shows the rate-limit message', async () => {
    installFakeServer({
      'GET /api/v1/session': problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'POST /api/v1/session': problem(429),
    });
    renderApp();
    await userEvent.type(await screen.findByLabelText('Username'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'pw');
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('There have been too many sign-in attempts. Wait a little, then try again.');
  });

  it('other failures show the unreachable message', async () => {
    installFakeServer({
      'GET /api/v1/session': problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'POST /api/v1/session': problem(403),
    });
    renderApp();
    await userEvent.type(await screen.findByLabelText('Username'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'pw{Enter}');
    expect(await screen.findByRole('alert')).toHaveTextContent('Tendo can’t be reached right now. Check your connection and try again; your account is safe.');
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
    await userEvent.type(await screen.findByLabelText('Username'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'pw{Enter}');
    expect(await screen.findByRole('button', { name: 'Signing in…' })).toBeDisabled();
    release();
    expect(await screen.findByRole('button', { name: 'Sign in' })).toBeEnabled();
  });
});

describe('signed in', () => {
  it('login posts exactly {login,password} then shows the household home with one membership and no switch rows', async () => {
    const fake = installFakeServer({
      'GET /api/v1/session': problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'POST /api/v1/session': json(201, session()),
      [householdPath]: json(200, household),
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
    });
    renderApp();
    await userEvent.type(await screen.findByLabelText('Username'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'correct horse battery{Enter}');

    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toBeVisible();
    expect(await screen.findByText('Nothing needs attention right now.')).toBeVisible();

    const post = fake.requests.find((r) => r.method === 'POST')!;
    expect(post.path).toBe('/api/v1/session');
    expect(JSON.parse(post.body)).toEqual({ login: 'anna', password: 'correct horse battery' });
    expect(fake.requests.some((r) => r.path === `/api/v1/households/${HOUSEHOLD_ID}`)).toBe(true);

    expect(screen.getByRole('heading', { level: 1, name: 'Overview' })).toHaveFocus();
    expect(screen.getByText('Signed in as anna')).toBeVisible();
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument();
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument();
    expect(screen.queryByText(/switch/i)).not.toBeInTheDocument();
    expect(screen.queryByText(HOUSEHOLD_ID)).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Add item' })).toBeVisible();
  });

  it('shows shared navigation with the current page and moves focus to each new heading', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [householdPath]: json(200, household),
      [`GET /api/v1/households/${HOUSEHOLD_ID}/accounts`]: json(200, { items: [], nextCursor: null }),
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
    });
    renderApp();
    const nav = await screen.findByRole('navigation', { name: 'Household navigation' });
    const currentHrefs = () => within(screen.getByRole('navigation', { name: 'Household navigation' })).getAllByRole('link').filter((link) => link.getAttribute('aria-current') === 'page').map((link) => link.getAttribute('href'));
    // Admin center joins the rail once the household reports the viewer's role; Archive is its own destination.
    await waitFor(() => expect(within(nav).getAllByRole('link')).toHaveLength(7));
    expect(within(nav).getAllByRole('link').map((link) => link.getAttribute('href')))
      .toEqual(['/', '/items', '/items?status=archived', '/people', '/members', '/account', '/admin']);
    expect(screen.getAllByRole('navigation')).toHaveLength(1);
    expect(screen.getByRole('main').compareDocumentPosition(nav) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(currentHrefs()).toEqual(['/']);
    for (const [name, href, heading] of [
      ['All items', '/items', 'All items'],
      ['Archive', '/items?status=archived', 'All items'],
      ['People and things', '/people', 'People and things'],
      ['Household members', '/members', 'Household members'],
      ['Your account', '/account', 'Your account'],
      ['Home', '/', 'Overview'],
    ] as const) {
      await userEvent.click(within(screen.getByRole('navigation', { name: 'Household navigation' })).getByRole('link', { name: new RegExp(`^${name}$`) }));
      expect(await screen.findByRole('heading', { level: 1, name: heading })).toHaveFocus();
      expect(window.location.pathname + window.location.search).toBe(href);
      expect(currentHrefs()).toEqual([href]);
    }
    // The archived filter decides which of the two lists is the current page.
    expect(window.location.pathname + window.location.search).toBe('/');
  });

  it.each(['en', 'cs'] as const)('every visible navigation label is part of the link name (%s)', async (locale) => {
    localStorage.setItem('tendo.locale', locale);
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [householdPath]: json(200, household),
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
    });
    renderApp();
    const nav = await screen.findByRole('navigation', { name: locale === 'cs' ? 'Navigace domácnosti' : 'Household navigation' });
    await waitFor(() => expect(within(nav).getAllByRole('link')).toHaveLength(7));
    const links = within(nav).getAllByRole('link');
    for (const link of links) {
      const name = link.getAttribute('aria-label') ?? '';
      const labels = [...link.querySelectorAll('span')].map((span) => span.textContent?.trim() ?? '');
      // Destinations with a compact phone label carry two spans; the rest carry one. Every visible label must be
      // part of the accessible name, so a short phone word never disagrees with what is announced.
      expect(labels.length, `${name} shows at least one label`).toBeGreaterThan(0);
      expect(labels.length, `${name} shows no more than a compact and a full label`).toBeLessThanOrEqual(2);
      for (const label of labels) {
        expect(label, `${name} label is not empty`).not.toBe('');
        expect(name.toLocaleLowerCase(locale), `${name} contains visible label ${label}`).toContain(label.toLocaleLowerCase(locale));
      }
      expect(link.querySelector('svg')).toHaveAttribute('aria-hidden', 'true');
    }
    localStorage.removeItem('tendo.locale');
  });

  it('keeps language switching on Account instead of a signed-in footer', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [householdPath]: json(200, household),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
    });
    renderApp();
    await screen.findByRole('heading', { level: 1, name: 'Overview' });
    expect(screen.queryByRole('contentinfo'), 'a signed-in shell has no footer language switch').not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('link', { name: 'Your account' }));
    const main = await screen.findByRole('main');
    const language = within(main).getByRole('group', { name: 'Language' });
    expect(within(language).getByRole('button', { name: 'English' })).toHaveAttribute('aria-pressed', 'true');
    expect(within(language).getByRole('button', { name: 'Čeština' })).toBeVisible();
  });

  it('shows the brand, the active household name and the account controls in the top bar', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [householdPath]: json(200, household),
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
    });
    renderApp();
    const navigation = await screen.findByRole('navigation', { name: 'Household navigation' });
    const banner = screen.getByRole('banner');
    expect(banner).toHaveTextContent('Tendo');
    // The bar names the active household; the switcher itself lives in the panel and lists only the caller's own
    // memberships, which the household list endpoint now provides.
    const householdButton = await within(banner).findByRole('button', { name: 'Household: Veselí' });
    expect(householdButton).toHaveAttribute('aria-expanded', 'false');
    // The selector is a real 44 px box of its own, not a padded hit area around a smaller control.
    expect(householdButton.className).toContain('min-h-11');
    expect(within(banner).queryByRole('combobox')).not.toBeInTheDocument();
    expect(within(banner).queryByRole('searchbox')).not.toBeInTheDocument();
    expect(document.querySelector('[data-avatar]')).toHaveTextContent('A');
    expect(within(navigation).getByRole('link', { name: 'Home' })).toHaveAttribute('aria-current', 'page');
    const account = screen.getByRole('link', { name: 'Account settings' });
    expect(account).toHaveAttribute('href', '/account');
    await userEvent.click(account);
    expect(await screen.findByRole('heading', { level: 1, name: 'Your account' })).toHaveFocus();
  });

  it('keeps the floating notice’s scroll clearance reserved on short viewports', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [householdPath]: json(200, household),
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
    });
    renderApp();
    // The clearance lives on the household shell, which arrives with the household itself.
    await screen.findByRole('button', { name: 'Household: Veselí' });
    const main = screen.getByRole('main');
    // Short viewports (landscape phones, 200–400 % zoom) turn the bottom bar static, but the Home notice keeps
    // floating over the content there. Zeroing the clearance along with the bar's offset would leave a keyboard
    // focused row control under the card (WCAG 2.4.11), so the short rule reserves the card's own height instead.
    expect(main.className).toContain('short:pb-[calc(var(--notice-clearance,0px)+1.5rem+env(safe-area-inset-bottom))]');
    expect(main.className, 'a short viewport never drops the notice clearance').not.toContain('short:pb-6');
    // The scroll margin is reserved on the shell frame for every descendant, so Tab never lands under the card. The
    // notice keeps its bar-clearing lift on short viewports, so the base margin (card height + lift) applies there
    // unchanged and no short rule may override it.
    const frame = main.parentElement!.parentElement!;
    expect(frame.className).toContain('[&_*]:scroll-mb-[calc(var(--notice-clearance,0px)+6.5rem+env(safe-area-inset-bottom))]');
    expect(frame.className, 'a short viewport keeps the base scroll margin for focused controls').not.toMatch(/short:\[&_\*\]:scroll-mb-/);
  });

  it('opens the household panel with the active household and the admin center for an administrator', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [householdPath]: json(200, household),
      // One membership: the panel is the selector it has always been, with nothing to switch to.
      'GET /api/v1/households': json(200, { items: [household], nextCursor: null }),
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
    });
    renderApp();
    const button = await screen.findByRole('button', { name: 'Household: Veselí' });
    const panel = document.getElementById(button.getAttribute('aria-controls')!)!;
    expect(panel).not.toBeVisible();
    await userEvent.click(button);
    expect(button).toHaveAttribute('aria-expanded', 'true');
    expect(panel).toBeVisible();
    expect(within(panel).getByText('Active household')).toBeVisible();
    // The same name is announced by the trigger; the panel repeats it once as the current row.
    expect(within(panel).getAllByText('Veselí')).toHaveLength(1);
    await waitFor(() => expect(within(panel).queryByText('Loading your households…')).not.toBeInTheDocument());
    expect(within(panel).queryAllByRole('button'), 'the only membership is not offered as a switch').toHaveLength(0);
    await userEvent.click(within(panel).getByRole('link', { name: 'Admin center' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Admin center' })).toHaveFocus();
    expect(window.location.pathname).toBe('/admin');
    await waitFor(() => expect(screen.getByRole('button', { name: 'Household: Veselí' })).toHaveAttribute('aria-expanded', 'false'));
  });

  it('reaches Archive, Members and the admin center from the phone More sheet', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [householdPath]: json(200, household),
      [`GET /api/v1/households/${HOUSEHOLD_ID}/accounts`]: json(200, { items: [], nextCursor: null }),
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
    });
    renderApp();
    const nav = () => screen.getByRole('navigation', { name: 'Household navigation' });
    const more = () => within(nav()).getByRole('button', { name: 'More' });
    const sheet = () => document.getElementById(more().getAttribute('aria-controls')!)!;
    await waitFor(() => expect(within(nav()).getAllByRole('link')).toHaveLength(7));
    expect(more()).toHaveAttribute('aria-expanded', 'false');
    // A closed sheet holds no links, so nothing off screen can be tabbed into.
    expect(sheet().querySelectorAll('a')).toHaveLength(0);
    await userEvent.click(more());
    expect(more()).toHaveAttribute('aria-expanded', 'true');
    expect(within(sheet()).getByRole('link', { name: 'Archive' })).toHaveAttribute('href', '/items?status=archived');
    expect(within(sheet()).getByRole('link', { name: 'Household members' })).toHaveAttribute('href', '/members');
    expect(within(sheet()).getByRole('link', { name: 'Admin center' })).toHaveAttribute('href', '/admin');
    await userEvent.click(within(sheet()).getByRole('link', { name: 'Archive' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'All items' })).toHaveFocus();
    expect(window.location.pathname + window.location.search).toBe('/items?status=archived');
    // Navigating closes the sheet, so Archive stays one tap away without hiding the page behind a panel.
    await waitFor(() => expect(more()).toHaveAttribute('aria-expanded', 'false'));
    expect(sheet().querySelectorAll('a')).toHaveLength(0);
    expect(within(nav()).getByRole('link', { name: 'Archive' })).toHaveAttribute('aria-current', 'page');
  });

  it('closes the household panel on Escape, on an outside press, and when focus leaves it', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [householdPath]: json(200, household),
      'GET /api/v1/households': json(200, { items: [household], nextCursor: null }),
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
    });
    renderApp();
    const button = await screen.findByRole('button', { name: 'Household: Veselí' });
    const panel = document.getElementById(button.getAttribute('aria-controls')!)!;
    const adminLink = () => within(panel).getByRole('link', { name: 'Admin center' });

    // Escape closes the panel and puts focus back on the trigger, never at the top of the document.
    await userEvent.click(button);
    expect(button).toHaveAttribute('aria-expanded', 'true');
    adminLink().focus();
    await userEvent.keyboard('{Escape}');
    expect(button).toHaveAttribute('aria-expanded', 'false');
    expect(button).toHaveFocus();

    // A press anywhere outside the panel closes it too.
    await userEvent.click(button);
    expect(button).toHaveAttribute('aria-expanded', 'true');
    await userEvent.click(screen.getByRole('main'));
    await waitFor(() => expect(button).toHaveAttribute('aria-expanded', 'false'));

    // Tabbing past the last control leaves the panel behind instead of keeping it open.
    await userEvent.click(button);
    expect(button).toHaveAttribute('aria-expanded', 'true');
    adminLink().focus();
    await userEvent.tab();
    await waitFor(() => expect(button).toHaveAttribute('aria-expanded', 'false'));
    expect(panel).not.toBeVisible();

    // A press on the panel's own non-focusable text is not a way out of it: focus drops to the document body, which
    // must not be mistaken for focus leaving the popover, and the press itself was inside the panel.
    await userEvent.click(button);
    expect(button).toHaveAttribute('aria-expanded', 'true');
    await userEvent.click(within(panel).getByText('Active household'));
    expect(document.activeElement, 'the press really moves focus off the trigger').toBe(document.body);
    expect(button).toHaveAttribute('aria-expanded', 'true');
    expect(panel).toBeVisible();
  });

  it('closes the phone More sheet on Escape, on an outside press, and when focus leaves it', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [householdPath]: json(200, memberHousehold),
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
    });
    renderApp();
    const nav = () => screen.getByRole('navigation', { name: 'Household navigation' });
    const more = () => within(nav()).getByRole('button', { name: 'More' });
    const sheet = () => document.getElementById(more().getAttribute('aria-controls')!)!;
    const members = () => within(sheet()).getByRole('link', { name: 'Household members' });

    await screen.findByRole('navigation', { name: 'Household navigation' });
    await userEvent.click(more());
    expect(more()).toHaveAttribute('aria-expanded', 'true');
    members().focus();
    await userEvent.keyboard('{Escape}');
    expect(more()).toHaveAttribute('aria-expanded', 'false');
    expect(more()).toHaveFocus();

    await userEvent.click(more());
    expect(more()).toHaveAttribute('aria-expanded', 'true');
    await userEvent.click(screen.getByRole('main'));
    await waitFor(() => expect(more()).toHaveAttribute('aria-expanded', 'false'));

    // Tab past the sheet's last link leaves it behind instead of keeping it open.
    await userEvent.click(more());
    expect(more()).toHaveAttribute('aria-expanded', 'true');
    members().focus();
    await userEvent.tab();
    await waitFor(() => expect(more()).toHaveAttribute('aria-expanded', 'false'));
    expect(sheet().querySelectorAll('a')).toHaveLength(0);
  });

  it('shared navigation and popstate restore the prior route and focus', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [householdPath]: json(200, household),
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
    });
    renderApp();
    const nav = await screen.findByRole('navigation', { name: 'Household navigation' });
    const people = within(nav).getByRole('link', { name: 'People and things' });
    people.focus();
    expect(people).toHaveFocus();
    await userEvent.keyboard('{Enter}');
    expect(await screen.findByRole('heading', { level: 1, name: 'People and things' })).toHaveFocus();
    expect(window.location.pathname).toBe('/people');

    await userEvent.click(within(screen.getByRole('navigation', { name: 'Household navigation' })).getByRole('link', { name: 'Household members' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Household members' })).toHaveFocus();
    window.history.back();
    await waitFor(() => expect(window.location.pathname).toBe('/people'));
    expect(await screen.findByRole('heading', { level: 1, name: 'People and things' })).toHaveFocus();
    window.history.forward();
    await waitFor(() => expect(window.location.pathname).toBe('/members'));
    expect(await screen.findByRole('heading', { level: 1, name: 'Household members' })).toHaveFocus();
  });

  it('opens the selected group in All items from the bounded Home link', async () => {
    const base = `/api/v1/households/${HOUSEHOLD_ID}`;
    const entries = Array.from({ length: 101 }, (_, index) => ({ id: `needs-${index}`, subjectId: 's-1', responsibleUserId: null, title: `Needs ${index}`, notes: null, attentionOn: null, recurrence: null, workflowState: 'open' as const, attention: 'needs_attention' as const, archived: false, done: false, lastCompletedOn: null, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' }));
    const fake = installFakeServer({ 'GET /api/v1/session': json(200, session()), [householdPath]: json(200, household), ...itemRoutes(base, entries), [`GET ${base}/members`]: json(200, { items: [], nextCursor: null }) });
    renderApp();
    await screen.findByText('Needs 0');
    const main = screen.getByRole('main');
    const group = within(main).getByRole('heading', { level: 2, name: 'Needs attention 101 items' }).closest('section')!;
    for (const remaining of [76, 51, 26]) await userEvent.click(within(group).getByRole('button', { name: `Show more (${remaining} remaining)` }));
    await userEvent.click(within(group).getByRole('link', { name: 'See all 101 in All items' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'All items' })).toHaveFocus();
    expect(window.location.pathname + window.location.search).toBe('/items?status=needs');
    const allMain = screen.getByRole('main');
    expect(within(allMain).getByLabelText('Show')).toHaveValue('needs');
    const navigation = screen.getByRole('navigation', { name: 'Household navigation' });
    expect(within(navigation).getByRole('link', { name: 'All items' })).toHaveAttribute('aria-current', 'page');
    await waitFor(() => expect(fake.requests.filter((request) => request.method === 'GET' && request.path === `${base}/items` && request.query.get('attention') === 'needs_attention')).toHaveLength(5));
    const latestListRequest = fake.requests.filter((request) => request.method === 'GET' && request.path === `${base}/items`).at(-1)!;
    expect(latestListRequest.query.get('attention')).toBe('needs_attention');
    expect(latestListRequest.query.get('workflowState')).toBe('open');
  });

  it('an existing session goes straight to home', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
      [householdPath]: json(200, household),
    });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toBeVisible();
    expect(document.body).toHaveFocus();
    expect(screen.getByRole('banner')).toHaveTextContent('Tendo');
    expect(screen.getByRole('main')).toBeInTheDocument();
  });

  it('session without a default household explains it calmly and offers sign out', async () => {
    const fake = installFakeServer({ 'GET /api/v1/session': json(200, session(false)) });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1 })).toHaveTextContent(
      'Join your household by invitation',
    );
    expect(screen.getByRole('button', { name: 'Sign out' })).toBeVisible();
    const navigation = screen.getByRole('navigation', { name: 'Household navigation' });
    expect(within(navigation).getAllByRole('link').map((link) => link.getAttribute('href'))).toEqual(['/', '/account']);
    expect(within(navigation).getByRole('link', { name: 'Home' })).toHaveAttribute('aria-current', 'page');
    expect(within(navigation).queryAllByText('Home', { exact: true })).toHaveLength(1);
    const currentHrefs = () => within(screen.getByRole('navigation', { name: 'Household navigation' })).getAllByRole('link').filter((link) => link.getAttribute('aria-current') === 'page').map((link) => link.getAttribute('href'));
    await userEvent.click(within(navigation).getByRole('link', { name: 'Your account' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Your account' })).toHaveFocus();
    expect(window.location.pathname).toBe('/account');
    expect(currentHrefs()).toEqual(['/account']);
    await userEvent.click(within(screen.getByRole('navigation', { name: 'Household navigation' })).getByRole('link', { name: 'Home' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Join your household by invitation' })).toHaveFocus();
    expect(window.location.pathname).toBe('/');
    expect(currentHrefs()).toEqual(['/']);
    expect(fake.requests.every((r) => !r.path.startsWith('/api/v1/households'))).toBe(true);
  });

  it('household 404 shows the no-household message', async () => {
    installFakeServer({ 'GET /api/v1/session': json(200, session()),
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []), [householdPath]: problem(404) });
    renderApp();
    expect(await screen.findByRole('heading', { level: 1 })).toHaveTextContent(
      'Join your household by invitation',
    );
  });

  it.each([
    {
      shell: 'household',
      routes: () => ({
        'GET /api/v1/session': json(200, session()),
        ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
        [householdPath]: json(200, household),
      }),
      ready: () => screen.findByRole('heading', { level: 1, name: 'Overview' }),
    },
    {
      shell: 'household without a membership',
      routes: () => ({ 'GET /api/v1/session': json(200, session(false)) }),
      ready: () => screen.findByRole('heading', { level: 1, name: 'Join your household by invitation' }),
    },
    {
      shell: 'signed out',
      routes: () => ({
        'GET /api/v1/session': problem(401),
        'GET /api/v1/auth/setup': json(200, { required: false }),
        'GET /api/v1/auth/oidc': json(200, { enabled: false }),
      }),
      ready: () => screen.findByRole('heading', { level: 1, name: 'Welcome back' }),
    },
  ])('the $shell top bar has a minimum height and no fixed height', async ({ routes, ready }) => {
    installFakeServer(routes());
    renderApp();
    await ready();
    const header = screen.getByRole('banner');
    const tokens = header.className.split(/\s+/).filter(Boolean);
    // A minimum keeps the bar at 56 px normally; a fixed height would let a wrapped identity block or a
    // sign-out failure message paint over main.
    expect(tokens).toContain('min-h-14');
    expect(tokens.filter((token) => /^(?:[a-z]+:)*h-(?:\d+(?:\/\d+)?|\[[^\]]+\])$/.test(token)), 'fixed header height').toEqual([]);
    expect(tokens).toContain('flex-wrap');
  });

  it('sign out calls DELETE /api/v1/session and returns to login', async () => {
    const fake = installFakeServer({
      'GET /api/v1/session': json(200, session()),
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
      [householdPath]: json(200, household),
      'DELETE /api/v1/session': json(204),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Sign out' }));
    expect(await screen.findByLabelText('Password')).toBeVisible();
    expect(screen.getByRole('heading', { level: 1, name: 'Welcome back' })).toHaveFocus();
    expect(fake.requests.some((r) => r.method === 'DELETE' && r.path === '/api/v1/session')).toBe(true);
    expect(screen.queryByText('Veselí')).not.toBeInTheDocument();
    expect(screen.getByRole('status')).toBeEmptyDOMElement();
  });

  it('failed sign out keeps the signed-in state and says so', async () => {
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
      [householdPath]: json(200, household),
      'DELETE /api/v1/session': problem(503),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Sign out' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t sign you out. Try again.');
    expect(screen.getByRole('heading', { level: 1, name: 'Overview' })).toBeVisible();
  });

  it('401 on household fetch explains the ended session, then deliberate sign out has no notice', async () => {
    let householdCalls = 0;
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
      [householdPath]: () => (++householdCalls === 1 ? problem(401) : json(200, household)),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
      'POST /api/v1/session': json(201, session()),
      'DELETE /api/v1/session': json(204),
    });
    renderApp();
    expect(await screen.findByLabelText('Password')).toBeVisible();
    expect(screen.getByRole('heading', { level: 1, name: 'Welcome back' })).toHaveFocus();
    expect(screen.getByRole('status')).toHaveTextContent('You were signed out. Sign in again to continue. Saved changes are kept.');
    expect(screen.queryByRole('heading', { name: 'Overview' })).not.toBeInTheDocument();

    await userEvent.type(screen.getByLabelText('Username'), 'anna');
    await userEvent.type(screen.getByLabelText('Password'), 'correct horse battery{Enter}');
    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Sign out' }));
    expect(await screen.findByLabelText('Password')).toBeVisible();
    expect(screen.getByRole('status')).toBeEmptyDOMElement();
  });

  it('ignores a delayed mutation 401 after deliberate sign out', async () => {
    const base = `/api/v1/households/${HOUSEHOLD_ID}`;
    const item = { id: '0198a2f0-0000-7000-8000-000000000002', subjectId: 's-1', responsibleUserId: null, title: 'Delayed item', notes: null, attentionOn: null, recurrence: null, workflowState: 'open' as const, attention: 'needs_attention' as const, archived: false, done: false, lastCompletedOn: null, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' };
    let release!: (response: Response) => void;
    const held = new Promise<Response>((resolve) => { release = resolve; });
    const fake = installFakeServer({
      'GET /api/v1/session': json(200, session()),
      ...itemRoutes(base, [item]),
      [householdPath]: json(200, household),
      [`GET ${base}/items/${item.id}`]: json(200, { ...item, attention: 'needs_attention', version: 1 }, { ETag: '"1"' }),
      [`POST ${base}/items/${item.id}/completions`]: () => held,
      'DELETE /api/v1/session': json(204),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
    });
    renderApp();
    const itemTitle = await screen.findByText('Delayed item');
    const row = within(itemTitle.closest('li')!);
    await userEvent.click(row.getByRole('button', { name: 'Done: Delayed item' }));
    await waitFor(() => expect(fake.requests.some((r) => r.method === 'POST' && r.path === `${base}/items/${item.id}/completions`)).toBe(true));
    await userEvent.click(await screen.findByRole('button', { name: 'Sign out' }));
    expect(await screen.findByLabelText('Password')).toBeVisible();
    release(problem(401));
    await waitFor(() => expect(screen.getByRole('heading', { level: 1, name: 'Welcome back' })).toHaveFocus());
    expect(screen.getByRole('status')).toBeEmptyDOMElement();
  });

  it('other household failures show a retryable error', async () => {
    let calls = 0;
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
      [householdPath]: () => (++calls === 1 ? problem(500) : json(200, household)),
    });
    renderApp();
    await userEvent.click(await screen.findByRole('button', { name: 'Try again' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toBeVisible();
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
    expect(await screen.findByLabelText('Uživatelské jméno')).toBeVisible();
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
      ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
      [householdPath]: json(200, household),
    });
    renderApp();
    expect(await screen.findByText('Teď nic nevyžaduje pozornost.')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Odhlásit se' })).toBeVisible();
  });
});
