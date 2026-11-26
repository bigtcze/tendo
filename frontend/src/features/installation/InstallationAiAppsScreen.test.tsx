import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import {
  HOUSEHOLD_ID, household, instanceAdminSession, installFakeServer, json, session, type RecordedRequest,
} from '../../test/fakeServer';
import { itemRoutes } from '../../test/fakeItems';
import type { components } from '../../../../api/generated/api';

type Settings = components['schemas']['McpInstallationPolicy'];

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const readPath = 'GET /api/v1/admin/mcp-policy';
const savePath = 'PUT /api/v1/admin/mcp-policy';
const problem = (status: number, code?: string) => json(status, { type: 'about:blank', title: 'x', status, ...(code ? { code } : {}) });
const settings = (change: Partial<Settings> = {}): Settings => ({
  enabled: true, operatorEnabled: true, defaultAccountEnabled: true, defaultMaxScopes: ['tendo:read', 'tendo:write'], ...change,
});
const answer = (body: Settings, version: number) => json(200, body, { ETag: `"${version}"` });

/** The Admin center's AI apps section on its own address, with every route a test does not override already here. */
function renderAt(routes: Parameters<typeof installFakeServer>[0], path = '/admin/ai-apps', viewer = instanceAdminSession()) {
  const fake = installFakeServer({
    'GET /api/v1/session': json(200, viewer),
    [`GET ${base}`]: json(200, household),
    ...itemRoutes(base, []),
    // The record of changes is InstallationAiAppsChanges.test.tsx's; here it is always empty.
    'GET /api/v1/admin/mcp-policy-events': json(200, { items: [], nextCursor: null }),
    ...routes,
  });
  window.history.replaceState(null, '', path);
  render(<I18nProvider><App /></I18nProvider>);
  return fake;
}

const installationSwitch = () => screen.getByRole('switch', { name: 'Allow AI apps in Tendo' });
const accountsSwitch = () => screen.getByRole('switch', { name: 'Let accounts use AI apps' });
const read = () => screen.getByRole('checkbox', { name: /^Read/ });
const change = () => screen.getByRole('checkbox', { name: /^Change/ });
const saveButton = () => screen.getByRole('button', { name: 'Save' });
const saves = (fake: ReturnType<typeof installFakeServer>) =>
  fake.requests.filter((request) => request.method === 'PUT' && request.path === '/api/v1/admin/mcp-policy');
const sections = () => screen.getByRole('navigation', { name: 'Admin center sections' });

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

describe('installation AI apps', () => {
  it('shows the installation settings as the server has them, in plain words', async () => {
    renderAt({ [readPath]: answer(settings({ defaultAccountEnabled: false, defaultMaxScopes: ['tendo:read'] }), 4) });
    expect(await screen.findByRole('switch', { name: 'Allow AI apps in Tendo' })).toBeChecked();

    expect(screen.getByRole('heading', { level: 1, name: 'Admin center' })).toBeVisible();
    expect(screen.getByRole('heading', { level: 2, name: 'AI apps' })).toBeVisible();
    expect(accountsSwitch()).not.toBeChecked();
    expect(read()).toBeChecked();
    expect(change()).not.toBeChecked();
    // Pausing is not disconnecting, and the hint says so before anyone turns the switch off.
    expect(installationSwitch()).toHaveAccessibleDescription(
      'When this is off, no app can connect or work with Tendo. Apps that are already connected are paused, not disconnected, and work again when you turn this back on.',
    );
    expect(within(sections()).getByRole('link', { name: 'AI apps' })).toHaveAttribute('aria-current', 'page');
    // The server configuration allows apps, so nothing says otherwise.
    expect(screen.queryByText(/server configuration/)).not.toBeInTheDocument();
    expect(screen.getByRole('main')).not.toHaveTextContent(/tendo:|scope|polic|MCP/i);
  });

  it('holds the place with one status line while the settings load', async () => {
    let release!: (response: Response) => void;
    const pending = new Promise<Response>((resolve) => { release = resolve; });
    renderAt({ [readPath]: () => pending });

    const label = await screen.findByText('Loading AI app settings…');
    expect(label).toHaveAttribute('role', 'status');
    await act(async () => { release(answer(settings(), 1)); await pending; });
    expect(await screen.findByRole('switch', { name: 'Allow AI apps in Tendo' })).toBeVisible();
  });

  it('says when the server configuration keeps apps off, and still lets the settings change', async () => {
    renderAt({ [readPath]: answer(settings({ operatorEnabled: false }), 1) });
    expect(await screen.findByText(/switched off in the server configuration/)).toBeVisible();
    expect(installationSwitch()).toBeEnabled();
    expect(saveButton()).toBeEnabled();
  });

  it('saves the whole settings with the version they were read at, and shows what the server kept', async () => {
    const fake = renderAt({
      [readPath]: answer(settings(), 7),
      [savePath]: (req: RecordedRequest) => answer({ ...settings(), ...JSON.parse(req.body) as Partial<Settings> }, 8),
    });
    await userEvent.click(await screen.findByRole('switch', { name: 'Allow AI apps in Tendo' }));
    await userEvent.click(change());
    // Nothing is sent before Save.
    expect(saves(fake)).toHaveLength(0);
    await userEvent.click(saveButton());

    expect(await screen.findByText('AI app settings saved.')).toBeVisible();
    expect(saves(fake)).toHaveLength(1);
    const [request] = saves(fake);
    expect(request.headers.get('If-Match')).toBe('"7"');
    expect(JSON.parse(request.body)).toEqual({ enabled: false, defaultAccountEnabled: true, defaultMaxScopes: ['tendo:read'] });
    expect(installationSwitch()).not.toBeChecked();
    expect(change()).not.toBeChecked();

    // The next save names the version the server just answered with.
    await userEvent.click(installationSwitch());
    await userEvent.click(saveButton());
    await waitFor(() => expect(saves(fake)).toHaveLength(2));
    expect(saves(fake)[1].headers.get('If-Match')).toBe('"8"');
  });

  it('keeps read and change in one order whichever is chosen first', async () => {
    const fake = renderAt({
      [readPath]: answer(settings({ defaultMaxScopes: ['tendo:write'] }), 2),
      [savePath]: (req: RecordedRequest) => answer({ ...settings(), ...JSON.parse(req.body) as Partial<Settings> }, 3),
    });
    await userEvent.click(await screen.findByRole('checkbox', { name: /^Read/ }));
    await userEvent.click(saveButton());
    await waitFor(() => expect(saves(fake)).toHaveLength(1));
    expect((JSON.parse(saves(fake)[0].body) as Settings).defaultMaxScopes).toEqual(['tendo:read', 'tendo:write']);
  });

  it('asks for at least one kind of access on the field instead of sending a save that must fail', async () => {
    const fake = renderAt({ [readPath]: answer(settings({ defaultMaxScopes: ['tendo:read'] }), 1) });
    await userEvent.click(await screen.findByRole('checkbox', { name: /^Read/ }));
    await userEvent.click(saveButton());

    expect(await screen.findByText('Choose at least one.')).toBeVisible();
    expect(saves(fake)).toHaveLength(0);
    expect(read()).toHaveFocus();
    expect(read()).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByRole('group', { name: 'What apps may do' })).toHaveAccessibleDescription(
      'By default, the most an app may do for an account, unless it is set differently for that account. People can allow less for their own account, never more. Choose at least one.',
    );

    // Choosing one clears the message.
    await userEvent.click(change());
    expect(screen.queryByText('Choose at least one.')).not.toBeInTheDocument();
  });

  it('answers a refused access value on the field as well', async () => {
    renderAt({
      [readPath]: answer(settings(), 1),
      [savePath]: json(422, { type: 'about:blank', title: 'x', status: 422, code: 'invalid_value', field: 'defaultMaxScopes' }),
    });
    await userEvent.click(await screen.findByRole('switch', { name: 'Allow AI apps in Tendo' }));
    await userEvent.click(saveButton());
    expect(await screen.findByText('Choose at least one.')).toBeVisible();
    expect(read()).toHaveFocus();
    expect(read()).toHaveAttribute('aria-invalid', 'true');
    expect(read()).toHaveAccessibleDescription('Choose at least one.');
  });

  it('sends one save however quickly Save is pressed twice', async () => {
    let release!: (response: Response) => void;
    const fake = renderAt({
      [readPath]: answer(settings(), 1),
      [savePath]: () => new Promise<Response>((resolve) => { release = resolve; }),
    });
    await userEvent.click(await screen.findByRole('switch', { name: 'Allow AI apps in Tendo' }));
    const form = saveButton().closest('form')!;
    act(() => { form.requestSubmit(); form.requestSubmit(); });
    await waitFor(() => expect(saves(fake)).toHaveLength(1));
    await act(async () => { release(answer(settings({ enabled: false }), 2)); });
    expect(await screen.findByText('AI app settings saved.')).toBeVisible();
    expect(saves(fake)).toHaveLength(1);
  });

  it('drops the last save’s message as soon as the form is edited again', async () => {
    renderAt({
      [readPath]: answer(settings(), 1),
      [savePath]: (req: RecordedRequest) => answer({ ...settings(), ...JSON.parse(req.body) as Partial<Settings> }, 2),
    });
    await userEvent.click(await screen.findByRole('switch', { name: 'Allow AI apps in Tendo' }));
    await userEvent.click(saveButton());
    expect(await screen.findByText('AI app settings saved.')).toBeVisible();
    await userEvent.click(change());
    expect(screen.queryByText('AI app settings saved.')).not.toBeInTheDocument();
  });

  it('reads the settings again when a save answers without its version, so the next save is not blind', async () => {
    let version = 1;
    const fake = renderAt({
      [readPath]: () => answer(settings({ enabled: version === 1 }), version),
      [savePath]: () => { version = 2; return json(200, settings({ enabled: false })); },
    });
    await userEvent.click(await screen.findByRole('switch', { name: 'Allow AI apps in Tendo' }));
    await userEvent.click(saveButton());
    expect(await screen.findByText('AI app settings saved.')).toBeVisible();
    await userEvent.click(installationSwitch());
    await userEvent.click(saveButton());
    await waitFor(() => expect(saves(fake)).toHaveLength(2));
    expect(saves(fake)[1].headers.get('If-Match')).toBe('"2"');
  });

  it('signs out when the session ends during a save', async () => {
    renderAt({
      [readPath]: answer(settings(), 1),
      [savePath]: problem(401, 'unauthenticated'),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
    });
    await userEvent.click(await screen.findByRole('switch', { name: 'Allow AI apps in Tendo' }));
    await userEvent.click(saveButton());
    expect(await screen.findByLabelText('Password')).toBeVisible();
  });

  it('treats a save without its version as refused, not as a connection problem', async () => {
    renderAt({ [readPath]: answer(settings(), 1), [savePath]: problem(428, 'precondition_required') });
    await userEvent.click(await screen.findByRole('switch', { name: 'Allow AI apps in Tendo' }));
    await userEvent.click(saveButton());
    expect(await screen.findByText('The settings couldn’t be saved, and nothing was changed.')).toBeVisible();
  });

  it('reads the settings again when someone else saved in between, and changes nothing', async () => {
    let version = 3;
    const fake = renderAt({
      [readPath]: () => answer(settings({ enabled: version === 3, defaultAccountEnabled: version === 3 }), version),
      [savePath]: () => { version = 4; return problem(412, 'precondition_failed'); },
    });
    await userEvent.click(await screen.findByRole('checkbox', { name: /^Change/ }));
    await userEvent.click(saveButton());

    expect(await screen.findByText(/Someone else changed these settings in the meantime/)).toBeVisible();
    // The screen shows the other save, not the edit that lost.
    expect(installationSwitch()).not.toBeChecked();
    expect(accountsSwitch()).not.toBeChecked();
    expect(change()).toBeChecked();
    expect(fake.requests.filter((request) => request.method === 'GET' && request.path === '/api/v1/admin/mcp-policy')).toHaveLength(2);
  });

  it('keeps the form disabled while the settings are read again after someone else saved', async () => {
    let reads = 0;
    let release!: (response: Response) => void;
    renderAt({
      [readPath]: () => (++reads === 1 ? answer(settings(), 3) : new Promise<Response>((resolve) => { release = resolve; })),
      [savePath]: problem(412, 'precondition_failed'),
    });
    await userEvent.click(await screen.findByRole('checkbox', { name: /^Change/ }));
    await userEvent.click(saveButton());
    await waitFor(() => expect(reads).toBe(2));
    expect(installationSwitch()).toBeDisabled();
    expect(read()).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Saving…' })).toBeDisabled();
    await act(async () => { release(answer(settings({ enabled: false }), 4)); });
    expect(await screen.findByText(/Someone else changed these settings in the meantime/)).toBeVisible();
    expect(installationSwitch()).toBeEnabled();
    expect(installationSwitch()).not.toBeChecked();
  });

  it('moves focus to the calm refusal when the grant is gone by the time the settings are read again', async () => {
    let reads = 0;
    renderAt({
      [readPath]: () => (++reads === 1 ? answer(settings(), 3) : problem(403, 'instance_admin_required')),
      [savePath]: problem(412, 'precondition_failed'),
    });
    await userEvent.click(await screen.findByRole('checkbox', { name: /^Change/ }));
    await userEvent.click(saveButton());
    const heading = await screen.findByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' });
    await waitFor(() => expect(heading).toHaveFocus());
  });

  it('offers a retry when the settings cannot be read again after someone else saved', async () => {
    let reads = 0;
    renderAt({
      [readPath]: () => (++reads === 1 ? answer(settings(), 3) : problem(503, 'unavailable')),
      [savePath]: problem(412, 'precondition_failed'),
    });
    await userEvent.click(await screen.findByRole('checkbox', { name: /^Change/ }));
    await userEvent.click(saveButton());
    expect(await screen.findByText('AI app settings couldn’t be loaded. Try again in a moment.')).toBeVisible();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeVisible();
  });

  it('says a failed save changed nothing and keeps the edit for another try', async () => {
    let attempt = 0;
    renderAt({
      [readPath]: answer(settings(), 1),
      [savePath]: () => (++attempt === 1 ? problem(503, 'unavailable') : answer(settings({ enabled: false }), 2)),
    });
    await userEvent.click(await screen.findByRole('switch', { name: 'Allow AI apps in Tendo' }));
    await userEvent.click(saveButton());

    const alert = await screen.findByText('The settings couldn’t be saved. Try again in a moment.');
    expect(alert.closest('[role="alert"]')).not.toBeNull();
    expect(installationSwitch()).not.toBeChecked();
    await userEvent.click(saveButton());
    expect(await screen.findByText('AI app settings saved.')).toBeVisible();
    expect(screen.queryByText(/couldn’t be saved/)).not.toBeInTheDocument();
  });

  it('treats a refused request that is not about the grant as a refusal, not the calm state', async () => {
    renderAt({ [readPath]: answer(settings(), 1), [savePath]: problem(403) });
    await userEvent.click(await screen.findByRole('switch', { name: 'Allow AI apps in Tendo' }));
    await userEvent.click(saveButton());
    expect(await screen.findByText('The settings couldn’t be saved, and nothing was changed.')).toBeVisible();
    expect(screen.queryByText('Only an installation administrator can see this.')).not.toBeInTheDocument();
  });

  it('offers a retry when the settings cannot be read', async () => {
    let attempt = 0;
    renderAt({ [readPath]: () => (++attempt === 1 ? problem(503, 'unavailable') : answer(settings(), 1)) });
    expect(await screen.findByText('AI app settings couldn’t be loaded. Try again in a moment.')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByRole('switch', { name: 'Allow AI apps in Tendo' })).toBeVisible();
  });

  it('says a read refused for another reason plainly, without a retry that would only repeat it', async () => {
    renderAt({ [readPath]: problem(403) });
    expect(await screen.findByText('AI app settings couldn’t be loaded for this account.')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument();
    expect(screen.queryByText('Only an installation administrator can see this.')).not.toBeInTheDocument();
  });

  it('treats a read without its version as unusable rather than saving blind', async () => {
    renderAt({ [readPath]: json(200, settings()) });
    expect(await screen.findByText('AI app settings couldn’t be loaded. Try again in a moment.')).toBeVisible();
    expect(screen.queryByRole('switch')).not.toBeInTheDocument();
  });

  it('shows the calm refusal when the grant is gone, and re-reads the session', async () => {
    const fake = renderAt({ [readPath]: problem(403, 'instance_admin_required') });
    expect(await screen.findByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' })).toBeVisible();
    expect(screen.queryByRole('switch')).not.toBeInTheDocument();
    await waitFor(() => expect(fake.requests.filter((request) => request.path === '/api/v1/session').length).toBeGreaterThanOrEqual(2));
    // Nothing the reader was using went away, so focus is left where the page put it.
    expect(screen.getByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' })).not.toHaveFocus();
  });

  it('moves focus to the calm refusal when a save is refused for the grant', async () => {
    renderAt({ [readPath]: answer(settings(), 1), [savePath]: problem(403, 'instance_admin_required') });
    await userEvent.click(await screen.findByRole('switch', { name: 'Allow AI apps in Tendo' }));
    await userEvent.click(saveButton());
    const heading = await screen.findByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' });
    await waitFor(() => expect(heading).toHaveFocus());
  });

  it('signs out when the session has ended', async () => {
    renderAt({
      [readPath]: problem(401, 'unauthenticated'),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
    });
    expect(await screen.findByLabelText('Password')).toBeVisible();
  });

  it('is reached from the other installation sections and from Back', async () => {
    renderAt({
      [readPath]: answer(settings(), 1),
      'GET /api/v1/admin/households': json(200, { items: [], nextCursor: null }),
    }, '/admin/households');
    const switcher = await screen.findByRole('navigation', { name: 'Admin center sections' });
    const link = within(switcher).getByRole('link', { name: 'AI apps' });
    expect(link).toHaveAttribute('href', '/admin/ai-apps');
    await userEvent.click(link);
    expect(await screen.findByRole('switch', { name: 'Allow AI apps in Tendo' })).toBeVisible();
    expect(window.location.pathname).toBe('/admin/ai-apps');

    await act(async () => { window.history.back(); await new Promise((resolve) => setTimeout(resolve, 0)); });
    await waitFor(() => expect(window.location.pathname).toBe('/admin/households'));
    expect(await screen.findByRole('heading', { level: 2, name: 'Households' })).toBeVisible();
  });

  it('offers no AI apps section without the installation grant', async () => {
    renderAt({ [readPath]: problem(403, 'instance_admin_required') }, '/admin/ai-apps', session());
    expect(await screen.findByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' })).toBeVisible();
    expect(screen.queryByRole('navigation', { name: 'Admin center sections' })).not.toBeInTheDocument();
  });

  it('reads in Czech without clipping the long labels', async () => {
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['cs-CZ']);
    renderAt({ [readPath]: answer(settings({ operatorEnabled: false }), 1) });
    expect(await screen.findByRole('switch', { name: 'Umožnit účtům používat AI aplikace' })).toBeVisible();
    expect(screen.getByRole('heading', { level: 2, name: 'AI aplikace' })).toBeVisible();
    expect(screen.getByRole('checkbox', { name: /^Změny/ })).toBeChecked();
  });
});
