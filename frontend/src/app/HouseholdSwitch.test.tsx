import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { components } from '../../../api/generated/api';
import { I18nProvider } from '../i18n';
import { HOUSEHOLD_ID, household, installFakeServer, json, memberHousehold, session } from '../test/fakeServer';
import { itemRoutes } from '../test/fakeItems';
import { App } from './App';

const OTHER_ID = '0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b62';
const THIRD_ID = '0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b63';

const householdAt = (id: string, name: string, viewerRole: components['schemas']['Household']['viewerRole'] = 'admin') =>
  ({ ...household, id, name, viewerRole });
const other = householdAt(OTHER_ID, 'Chalupa');
const third = householdAt(THIRD_ID, 'Dům u lesa a staré lípy');

type Routes = Parameters<typeof installFakeServer>[0];

/** Home in two households: the active one from the session, and one more to switch to. */
function appRoutes(overrides: Routes = {}): Routes {
  return {
    'GET /api/v1/session': json(200, session()),
    [`GET /api/v1/households/${HOUSEHOLD_ID}`]: json(200, household),
    [`GET /api/v1/households/${OTHER_ID}`]: json(200, other),
    [`GET /api/v1/households/${THIRD_ID}`]: json(200, third),
    'GET /api/v1/households': json(200, { items: [household, other], nextCursor: null }),
    'PATCH /api/v1/session': json(200, { ...session(), defaultHouseholdId: OTHER_ID }),
    ...itemRoutes(`/api/v1/households/${HOUSEHOLD_ID}`, []),
    ...itemRoutes(`/api/v1/households/${OTHER_ID}`, []),
    ...itemRoutes(`/api/v1/households/${THIRD_ID}`, []),
    ...overrides,
  };
}

const problem = (status: number) => json(status, { type: 'about:blank', title: 'x', status });

function renderApp() {
  return render(
    <I18nProvider>
      <App />
    </I18nProvider>,
  );
}

/** The trigger, opened the way a reader opens it, and the panel it controls. */
async function openPanel(label = 'Household: Veselí') {
  const trigger = await screen.findByRole('button', { name: label });
  await userEvent.click(trigger);
  const panel = document.getElementById(trigger.getAttribute('aria-controls')!)!;
  await waitFor(() => expect(panel).toBeVisible());
  return { trigger, panel };
}

/** The household list arrives asynchronously; nothing below may race it. */
async function householdsReady(panel: HTMLElement) {
  await waitFor(() => expect(within(panel).queryByText('Loading your households…')).not.toBeInTheDocument());
}

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
  window.history.replaceState(null, '', '/');
});

afterEach(() => {
  localStorage.removeItem('tendo.locale');
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe('household switcher', () => {
  it('reads the household list only when the panel opens, and offers nothing to switch with one membership', async () => {
    const fake = installFakeServer(appRoutes({ 'GET /api/v1/households': json(200, { items: [household], nextCursor: null }) }));
    renderApp();
    await screen.findByRole('heading', { level: 1, name: 'Overview' });
    const reads = () => fake.requests.filter((request) => request.path === '/api/v1/households');
    expect(reads(), 'a page load never reads the household list').toHaveLength(0);

    const { trigger, panel } = await openPanel();
    await waitFor(() => expect(reads()).toHaveLength(1));
    await householdsReady(panel);

    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    expect(within(panel).getByText('Veselí')).toBeVisible();
    expect(within(panel).getByText('Active household')).toBeVisible();
    // The only membership is the current row: the panel stays the selector it has always been.
    expect(within(panel).queryAllByRole('button')).toHaveLength(0);
    expect(within(panel).getByRole('link', { name: 'Admin center' })).toHaveAttribute('href', '/admin');
  });

  it('keeps the member copy in the panel of a household without an administrator role', async () => {
    installFakeServer(appRoutes({
      [`GET /api/v1/households/${HOUSEHOLD_ID}`]: json(200, memberHousehold),
      'GET /api/v1/households': json(200, { items: [memberHousehold], nextCursor: null }),
    }));
    renderApp();
    const { panel } = await openPanel();
    await householdsReady(panel);
    expect(within(panel).queryByRole('link', { name: 'Admin center' })).not.toBeInTheDocument();
    expect(within(panel).getByText('Ask a household administrator to change members.')).toBeVisible();
    expect(within(panel).queryAllByRole('button')).toHaveLength(0);
  });

  it('offers a quiet retry when the households cannot be read', async () => {
    let attempts = 0;
    installFakeServer(appRoutes({
      'GET /api/v1/households': () => {
        attempts += 1;
        return attempts === 1 ? problem(500) : json(200, { items: [household, other], nextCursor: null });
      },
    }));
    renderApp();
    const { panel } = await openPanel();

    expect(await within(panel).findByText('Your households couldn’t be loaded. Check your connection and try again.')).toBeVisible();
    await userEvent.click(within(panel).getByRole('button', { name: 'Try again' }));

    expect(await within(panel).findByRole('button', { name: 'Chalupa' })).toBeVisible();
    expect(attempts).toBe(2);
  });

  it('switches to another household and comes back to Home on that household', async () => {
    const fake = installFakeServer(appRoutes());
    renderApp();
    await screen.findByRole('heading', { level: 1, name: 'Overview' });
    const { panel } = await openPanel();
    await householdsReady(panel);

    expect(within(panel).getByText('Veselí').closest('[aria-current="true"]'), 'the household in use is the current row').not.toBeNull();
    const option = within(panel).getByRole('button', { name: 'Chalupa' });
    expect(option.className, 'every row is a full-size target').toContain('min-h-11');
    expect(option.querySelector('[class*="break-words"]'), 'a long name wraps instead of clipping').not.toBeNull();

    await userEvent.click(option);

    await waitFor(() => expect(fake.requests.filter((request) => request.method === 'PATCH')).toHaveLength(1));
    const patch = fake.requests.find((request) => request.method === 'PATCH')!;
    expect(patch.path).toBe('/api/v1/session');
    expect(JSON.parse(patch.body)).toEqual({ defaultHouseholdId: OTHER_ID });

    // Home, at the root, on the session the server returned — and the bar names the household it adopted.
    expect(window.location.pathname).toBe('/');
    const adopted = await screen.findByRole('button', { name: 'Household: Chalupa' });
    expect(adopted, 'the panel closes behind the switch').toHaveAttribute('aria-expanded', 'false');
    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toHaveFocus();
    await waitFor(() => expect(fake.requests.some((request) => request.path === `/api/v1/households/${OTHER_ID}/items/summary`)).toBe(true));
    // The change is spoken politely, not only seen.
    expect(screen.getByText('You’re now in Chalupa.').closest('[aria-live="polite"]')).not.toBeNull();

    // Nothing keeps reading the household that was left behind.
    const switchedAt = fake.requests.indexOf(patch);
    expect(
      fake.requests.slice(switchedAt).some((request) => request.path.startsWith(`/api/v1/households/${HOUSEHOLD_ID}/`)),
      'no request still names the previous household',
    ).toBe(false);
  });

  it('sends one session update when the same row is pressed twice', async () => {
    let release!: () => void;
    const gate = new Promise<void>((resolve) => { release = resolve; });
    const fake = installFakeServer(appRoutes({
      'PATCH /api/v1/session': async () => { await gate; return json(200, { ...session(), defaultHouseholdId: OTHER_ID }); },
    }));
    renderApp();
    await screen.findByRole('heading', { level: 1, name: 'Overview' });
    const { panel } = await openPanel();
    await householdsReady(panel);

    const option = within(panel).getByRole('button', { name: 'Chalupa' });
    await userEvent.click(option);
    await userEvent.click(option);
    release();

    await waitFor(() => expect(fake.requests.filter((request) => request.method === 'PATCH')).toHaveLength(1));
    expect(await screen.findByRole('button', { name: 'Household: Chalupa' })).toBeVisible();
  });

  it('switching from another screen returns to Home', async () => {
    installFakeServer(appRoutes({ 'GET /api/v1/auth/oidc': json(200, { enabled: false }) }));
    renderApp();
    await screen.findByRole('heading', { level: 1, name: 'Overview' });
    await userEvent.click(screen.getByRole('link', { name: 'Your account' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Your account' })).toHaveFocus();
    expect(window.location.pathname).toBe('/account');

    const { panel } = await openPanel();
    await householdsReady(panel);
    await userEvent.click(within(panel).getByRole('button', { name: 'Chalupa' }));

    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toHaveFocus();
    expect(window.location.pathname).toBe('/');
  });

  it('adopts a switch that finishes after the reader moved to another screen', async () => {
    let release!: () => void;
    const gate = new Promise<void>((resolve) => { release = resolve; });
    const fake = installFakeServer(appRoutes({
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
      'PATCH /api/v1/session': async () => { await gate; return json(200, { ...session(), defaultHouseholdId: OTHER_ID }); },
    }));
    renderApp();
    await screen.findByRole('heading', { level: 1, name: 'Overview' });
    const { panel } = await openPanel();
    await householdsReady(panel);

    await userEvent.click(within(panel).getByRole('button', { name: 'Chalupa' }));
    await userEvent.click(screen.getByRole('link', { name: 'Your account' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Your account' })).toBeVisible();
    expect(fake.requests.filter((request) => request.method === 'PATCH')).toHaveLength(1);

    release();

    // The menu that asked was replaced on the way, yet the household the server stored is adopted, not dropped.
    expect(await screen.findByRole('button', { name: 'Household: Chalupa' })).toBeVisible();
    expect(await screen.findByRole('heading', { level: 1, name: 'Overview' })).toHaveFocus();
    expect(window.location.pathname).toBe('/');
  });

  it('reuses the households read for the account on another screen, without a loading line', async () => {
    let reads = 0;
    let release!: () => void;
    const gate = new Promise<void>((resolve) => { release = resolve; });
    installFakeServer(appRoutes({
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
      'GET /api/v1/households': async () => {
        reads += 1;
        if (reads > 1) await gate;
        return json(200, { items: [household, other], nextCursor: null });
      },
    }));
    renderApp();
    await screen.findByRole('heading', { level: 1, name: 'Overview' });
    const first = await openPanel();
    await householdsReady(first.panel);
    expect(reads).toBe(1);
    await userEvent.keyboard('{Escape}');

    await userEvent.click(screen.getByRole('link', { name: 'Your account' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'Your account' })).toBeVisible();
    const { panel } = await openPanel();

    // The list belongs to the account, not to the screen: the same households are on screen straight away while
    // the quiet read is still out, so nothing waits behind it and no loading line flashes.
    expect(within(panel).queryByText('Loading your households…')).not.toBeInTheDocument();
    expect(within(panel).getByRole('button', { name: 'Chalupa' })).toHaveAttribute('aria-disabled', 'false');
    await waitFor(() => expect(reads).toBe(2));
    expect(within(panel).queryByText('Loading your households…')).not.toBeInTheDocument();

    release();
    await waitFor(() => expect(within(panel).getByRole('button', { name: 'Chalupa' })).toBeVisible());
  });

  it('keeps the panel open, explains and reads the list again when the household is gone', async () => {
    let reads = 0;
    const fake = installFakeServer(appRoutes({
      'PATCH /api/v1/session': json(422, { type: 'about:blank', title: 'Unprocessable', status: 422, code: 'invalid_reference', field: 'defaultHouseholdId' }),
      // The second read no longer offers the membership the server just refused.
      'GET /api/v1/households': () => {
        reads += 1;
        return json(200, { items: reads === 1 ? [household, other] : [household], nextCursor: null });
      },
    }));
    renderApp();
    const { trigger, panel } = await openPanel();
    await householdsReady(panel);
    expect(within(panel).getByRole('button', { name: 'Chalupa' })).toBeVisible();

    await userEvent.click(within(panel).getByRole('button', { name: 'Chalupa' }));

    expect(await within(panel).findByRole('alert')).toHaveTextContent('That household is no longer available to you.');
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    expect(trigger).toHaveAccessibleName('Household: Veselí');
    expect(window.location.pathname).toBe('/');
    // The list is read again, so a membership that went away stops being offered.
    await waitFor(() => expect(reads).toBe(2));
    await waitFor(() => expect(within(panel).queryByRole('button', { name: 'Chalupa' })).not.toBeInTheDocument());
    expect(within(panel).getByText('Veselí')).toBeVisible();
    expect(fake.requests.filter((request) => request.method === 'PATCH')).toHaveLength(1);
    // The row that went away took the keyboard reader's focus with it, so the reader is handed back to the trigger:
    // the panel, and the explanation in it, stay exactly where they were instead of closing behind a lost focus.
    expect(trigger, 'focus lands on the trigger, not at the top of the document').toHaveFocus();
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    expect(panel).toBeVisible();
    expect(within(panel).getByRole('alert')).toHaveTextContent('That household is no longer available to you.');
  });

  it('keeps the panel open when a keyboard switch cannot be reached, and a second try works', async () => {
    let attempts = 0;
    let release!: () => void;
    const gate = new Promise<void>((resolve) => { release = resolve; });
    installFakeServer(appRoutes({
      'PATCH /api/v1/session': async () => {
        attempts += 1;
        if (attempts === 1) await gate;
        return attempts === 1 ? problem(503) : json(200, { ...session(), defaultHouseholdId: OTHER_ID });
      },
    }));
    renderApp();
    const { trigger, panel } = await openPanel();
    await householdsReady(panel);
    const option = within(panel).getByRole('button', { name: 'Chalupa' });

    // A keyboard reader starts the switch from the row itself. The row stays mounted and marked `aria-disabled`
    // while the request is out: a `disabled` row would drop the focus, and the open panel with it.
    option.focus();
    await userEvent.keyboard('{Enter}');
    await waitFor(() => expect(within(panel).getByRole('button', { name: 'Chalupa' })).toHaveAttribute('aria-disabled', 'true'));
    expect(option, 'focus stays on the row that is on its way').toHaveFocus();
    release();

    expect(await within(panel).findByRole('alert'))
      .toHaveTextContent('Couldn’t move to that household. Check your connection and try again.');
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    expect(trigger).toHaveAccessibleName('Household: Veselí');
    expect(window.location.pathname).toBe('/');
    const retry = within(panel).getByRole('button', { name: 'Chalupa' });
    expect(retry, 'nothing changed, so the row is still there to try again').toHaveAttribute('aria-disabled', 'false');

    await userEvent.click(retry);
    expect(await screen.findByRole('button', { name: 'Household: Chalupa' })).toBeVisible();
    expect(attempts).toBe(2);
  });

  it('signs out when the session update is refused as signed out', async () => {
    installFakeServer(appRoutes({ 'PATCH /api/v1/session': problem(401) }));
    renderApp();
    const { panel } = await openPanel();
    await householdsReady(panel);

    await userEvent.click(within(panel).getByRole('button', { name: 'Chalupa' }));

    expect(await screen.findByText('You were signed out. Sign in again to continue. Saved changes are kept.')).toBeVisible();
    expect(await screen.findByLabelText('Username')).toBeVisible();
    expect(window.location.pathname).toBe('/');
  });

  it('signs out when opening the panel is refused as signed out', async () => {
    installFakeServer(appRoutes({ 'GET /api/v1/households': problem(401) }));
    renderApp();
    await screen.findByRole('heading', { level: 1, name: 'Overview' });
    await userEvent.click(screen.getByRole('button', { name: 'Household: Veselí' }));

    expect(await screen.findByText('You were signed out. Sign in again to continue. Saved changes are kept.')).toBeVisible();
  });

  it('appends the next page only when more households are asked for', async () => {
    const fake = installFakeServer(appRoutes({
      'GET /api/v1/households': json(200, { items: [household, other], nextCursor: 'page-2' }),
      'GET /api/v1/households?cursor=page-2': json(200, { items: [third], nextCursor: null }),
    }));
    renderApp();
    const { panel } = await openPanel();
    await householdsReady(panel);
    const moreReads = () => fake.requests.filter((request) => request.path === '/api/v1/households' && request.query.get('cursor') === 'page-2');

    expect(within(panel).queryByRole('button', { name: third.name }), 'later pages are never read eagerly').not.toBeInTheDocument();
    expect(moreReads()).toHaveLength(0);

    await userEvent.click(within(panel).getByRole('button', { name: 'Show more households' }));

    expect(await within(panel).findByRole('button', { name: 'Dům u lesa a staré lípy' })).toBeVisible();
    expect(within(panel).getByRole('button', { name: 'Chalupa' }), 'the first page stays').toBeVisible();
    expect(within(panel).queryByRole('button', { name: 'Show more households' }), 'the last page offers no more').not.toBeInTheDocument();
    // The row that asked for the page left with it, so the reader lands on the household it added.
    expect(within(panel).getByRole('button', { name: 'Dům u lesa a staré lípy' })).toHaveFocus();
    expect(moreReads()).toHaveLength(1);
  });

  it('hands the reader back to the trigger when the last page adds no household', async () => {
    installFakeServer(appRoutes({
      'GET /api/v1/households': json(200, { items: [household, other], nextCursor: 'page-2' }),
      'GET /api/v1/households?cursor=page-2': json(200, { items: [], nextCursor: null }),
    }));
    renderApp();
    const { trigger, panel } = await openPanel();
    await householdsReady(panel);

    await userEvent.click(within(panel).getByRole('button', { name: 'Show more households' }));

    // The row that asked for the page leaves with it, and this page added no row to hand the reader to: focus goes
    // back to the trigger, so the open panel is not left behind a reader dropped at the top of the document.
    await waitFor(() => expect(within(panel).queryByRole('button', { name: 'Show more households' })).not.toBeInTheDocument());
    expect(trigger, 'focus lands on the trigger, not at the top of the document').toHaveFocus();
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    expect(panel).toBeVisible();
    expect(within(panel).getByRole('button', { name: 'Chalupa' })).toBeVisible();
  });

  it('signs out when a later page is refused as signed out', async () => {
    installFakeServer(appRoutes({
      'GET /api/v1/households': json(200, { items: [household, other], nextCursor: 'page-2' }),
      'GET /api/v1/households?cursor=page-2': problem(401),
    }));
    renderApp();
    const { panel } = await openPanel();
    await householdsReady(panel);

    await userEvent.click(within(panel).getByRole('button', { name: 'Show more households' }));

    expect(await screen.findByText('You were signed out. Sign in again to continue. Saved changes are kept.')).toBeVisible();
    expect(await screen.findByLabelText('Username')).toBeVisible();
  });

  it('offers the households in Czech', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer(appRoutes());
    renderApp();
    await screen.findByRole('heading', { level: 1, name: 'Přehled' });
    const { panel } = await openPanel('Domácnost: Veselí');
    await householdsReady(panel);

    expect(within(panel).getByText('Aktivní domácnost')).toBeVisible();
    expect(within(panel).getByRole('button', { name: 'Chalupa' })).toBeVisible();
  });
});
