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

type Policy = components['schemas']['McpAccountPolicy'];

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const ACCOUNT = '0198a2f0-0000-7000-8000-0000000000b0';
const policyPath = `/api/v1/admin/accounts/${ACCOUNT}/mcp-policy`;
const readPath = `GET ${policyPath}`;
const savePath = `PUT ${policyPath}`;
const pagePath = `/admin/accounts/${ACCOUNT}/ai-apps`;
const problem = (status: number, code?: string) => json(status, { type: 'about:blank', title: 'x', status, ...(code ? { code } : {}) });
const policy = (change: Partial<Policy> = {}): Policy => ({
  userId: ACCOUNT, enabled: null, maxScopes: null, userEnabled: true, userScopes: ['tendo:read', 'tendo:write'],
  effectiveScopes: ['tendo:read', 'tendo:write'], ...change,
});
const answer = (body: Policy, version: number) => json(200, body, { ETag: `"${version}"` });
/** The server's answer to a save: the stored administrator columns over the rest, with what apps may do now. */
const stored = (req: RecordedRequest, change: Partial<Policy> = {}) =>
  ({ ...policy(), ...JSON.parse(req.body) as Partial<Policy>, ...change });

function renderAt(routes: Parameters<typeof installFakeServer>[0], path = pagePath, viewer = instanceAdminSession()) {
  const fake = installFakeServer({
    'GET /api/v1/session': json(200, viewer),
    [`GET ${base}`]: json(200, household),
    ...itemRoutes(base, []),
    ...routes,
  });
  window.history.replaceState(null, '', path);
  render(<I18nProvider><App /></I18nProvider>);
  return fake;
}

const radio = (group: string, name: string) =>
  within(screen.getByRole('group', { name: group })).getByRole('radio', { name });
const useGroup = 'AI apps for this account';
const maxGroup = 'The most apps may do';
const saveButton = () => screen.getByRole('button', { name: 'Save' });
const saves = (fake: ReturnType<typeof installFakeServer>) =>
  fake.requests.filter((request) => request.method === 'PUT' && request.path === policyPath);
const reads = (fake: ReturnType<typeof installFakeServer>) =>
  fake.requests.filter((request) => request.method === 'GET' && request.path === policyPath);
const nowText = () => document.querySelector('[data-account-ai-apps-now]');
const ownText = () => document.querySelector('[data-account-ai-apps-own]');

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

describe('one account’s AI apps', () => {
  it('shows what apps may do now, the person’s own choice and the administrator’s, in plain words', async () => {
    renderAt({
      [readPath]: answer(policy({ enabled: true, maxScopes: ['tendo:read'], userScopes: ['tendo:read'], effectiveScopes: ['tendo:read'] }), 3),
    });
    expect(await screen.findByRole('radio', { name: 'Allowed' })).toBeChecked();

    expect(screen.getByRole('heading', { level: 1, name: 'AI apps for this account' })).toBeVisible();
    expect(screen.getByRole('heading', { level: 2, name: 'What apps can do now' })).toBeVisible();
    expect(nowText()).toHaveTextContent('Read only.');
    expect(ownText()).toHaveTextContent('They allow read only.');
    expect(radio(maxGroup, 'Read')).toBeChecked();
    expect(radio(maxGroup, 'Change')).not.toBeChecked();
    expect(radio(maxGroup, 'Read and change')).not.toBeChecked();
    // The person's own choice is a fact here, never a control.
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument();
    expect(screen.queryByRole('switch')).not.toBeInTheDocument();
    // Turning an account off pauses its apps, and the option says so before anyone chooses it.
    expect(radio(useGroup, 'Not allowed')).toHaveAccessibleDescription(/paused, not disconnected/);
    // The whole row is the target: its hint is inside the label that checks the option.
    expect(screen.getByText(/paused, not disconnected/).closest('label')?.querySelector('input')).toBe(radio(useGroup, 'Not allowed'));
    expect(within(screen.getByRole('navigation', { name: 'Admin center sections' })).getByRole('link', { name: 'Accounts' }))
      .toHaveAttribute('aria-current', 'page');
    expect(screen.getByRole('main')).not.toHaveTextContent(/tendo:|scope|polic|MCP|override/i);
  });

  it('says the account follows the installation when nothing is set for it', async () => {
    renderAt({ [readPath]: answer(policy(), 1) });
    expect(await screen.findByRole('radio', { name: 'Allowed' })).not.toBeChecked();
    expect(radio(useGroup, 'Same as other accounts')).toBeChecked();
    expect(radio(useGroup, 'Same as other accounts')).toHaveAccessibleDescription('Follows “Let accounts use AI apps” in AI apps.');
    expect(radio(maxGroup, 'Same as other accounts')).toBeChecked();
    expect(nowText()).toHaveTextContent('Read and change.');
    expect(ownText()).toHaveTextContent('They allow read and change.');
  });

  it('says plainly when apps can do nothing now, and when the person turned them off', async () => {
    renderAt({ [readPath]: answer(policy({ userEnabled: false, effectiveScopes: [] }), 1) });
    expect(await screen.findByText('Nothing. Apps can’t work with Tendo for this account right now.')).toBeVisible();
    expect(ownText()).toHaveTextContent('They turned AI apps off for their account.');
  });

  it.each([
    [['tendo:write'], 'Change only, without reading.', 'They allow change only, without reading.'],
  ] as const)('names change without read as its own case', async (scopes, now, own) => {
    renderAt({ [readPath]: answer(policy({ userScopes: [...scopes], effectiveScopes: [...scopes] }), 1) });
    expect(await screen.findByText(now)).toBeVisible();
    expect(ownText()).toHaveTextContent(own);
  });

  it('holds the place with one status line while the account loads', async () => {
    let release!: (response: Response) => void;
    const pending = new Promise<Response>((resolve) => { release = resolve; });
    renderAt({ [readPath]: () => pending });
    const label = await screen.findByText('Loading this account’s AI apps…');
    expect(label).toHaveAttribute('role', 'status');
    await act(async () => { release(answer(policy(), 1)); await pending; });
    expect(await screen.findByRole('radio', { name: 'Allowed' })).toBeVisible();
  });

  it('saves the whole choice with the version it was read at, and shows what the server kept', async () => {
    const fake = renderAt({
      [readPath]: answer(policy(), 7),
      [savePath]: (req: RecordedRequest) => answer(stored(req, { effectiveScopes: [] }), 8),
    });
    await userEvent.click(await screen.findByRole('radio', { name: 'Not allowed' }));
    await userEvent.click(radio(maxGroup, 'Read'));
    // Nothing is sent before Save.
    expect(saves(fake)).toHaveLength(0);
    await userEvent.click(saveButton());

    expect(await screen.findByText('This account’s AI apps were saved.')).toBeVisible();
    expect(saves(fake)).toHaveLength(1);
    const [request] = saves(fake);
    expect(request.headers.get('If-Match')).toBe('"7"');
    expect(JSON.parse(request.body)).toEqual({ enabled: false, maxScopes: ['tendo:read'] });
    // What apps may do now is the server's answer, never worked out on screen.
    expect(nowText()).toHaveTextContent('Nothing.');
    expect(radio(useGroup, 'Not allowed')).toBeChecked();

    // Back to the installation's settings: null for both, and the next save names the new version.
    await userEvent.click(radio(useGroup, 'Same as other accounts'));
    await userEvent.click(radio(maxGroup, 'Same as other accounts'));
    await userEvent.click(saveButton());
    await waitFor(() => expect(saves(fake)).toHaveLength(2));
    expect(saves(fake)[1].headers.get('If-Match')).toBe('"8"');
    expect(JSON.parse(saves(fake)[1].body)).toEqual({ enabled: null, maxScopes: null });
  });

  it.each([
    ['Allowed', 'Change', { enabled: true, maxScopes: ['tendo:write'] }],
    ['Allowed', 'Read and change', { enabled: true, maxScopes: ['tendo:read', 'tendo:write'] }],
  ] as const)('sends %s with %s as the server spells it', async (use, max, body) => {
    const fake = renderAt({ [readPath]: answer(policy(), 1), [savePath]: (req: RecordedRequest) => answer(stored(req), 2) });
    await userEvent.click(await screen.findByRole('radio', { name: use }));
    await userEvent.click(radio(maxGroup, max));
    await userEvent.click(saveButton());
    await waitFor(() => expect(saves(fake)).toHaveLength(1));
    expect(JSON.parse(saves(fake)[0].body)).toEqual(body);
  });

  it('sends one save however quickly Save is pressed twice', async () => {
    let release!: (response: Response) => void;
    const fake = renderAt({ [readPath]: answer(policy(), 1), [savePath]: () => new Promise<Response>((resolve) => { release = resolve; }) });
    await userEvent.click(await screen.findByRole('radio', { name: 'Allowed' }));
    const form = saveButton().closest('form')!;
    act(() => { form.requestSubmit(); form.requestSubmit(); });
    await waitFor(() => expect(saves(fake)).toHaveLength(1));
    expect(radio(useGroup, 'Allowed')).toBeDisabled();
    await act(async () => { release(answer(policy({ enabled: true }), 2)); });
    expect(await screen.findByText('This account’s AI apps were saved.')).toBeVisible();
    expect(saves(fake)).toHaveLength(1);
  });

  it('drops the last save’s message as soon as the choice is edited again', async () => {
    renderAt({ [readPath]: answer(policy(), 1), [savePath]: (req: RecordedRequest) => answer(stored(req), 2) });
    await userEvent.click(await screen.findByRole('radio', { name: 'Allowed' }));
    await userEvent.click(saveButton());
    expect(await screen.findByText('This account’s AI apps were saved.')).toBeVisible();
    await userEvent.click(radio(maxGroup, 'Read'));
    expect(screen.queryByText('This account’s AI apps were saved.')).not.toBeInTheDocument();
  });

  it('reads the account again when someone else changed it in between, and keeps nothing of the losing edit', async () => {
    let version = 3;
    const fake = renderAt({
      [readPath]: () => answer(policy(version === 3 ? {} : { enabled: false, userEnabled: false, effectiveScopes: [] }), version),
      [savePath]: () => { version = 4; return problem(412, 'precondition_failed'); },
    });
    await userEvent.click(await screen.findByRole('radio', { name: 'Allowed' }));
    await userEvent.click(saveButton());

    expect(await screen.findByText(/Someone else changed this account’s AI apps in the meantime/)).toBeVisible();
    expect(radio(useGroup, 'Not allowed')).toBeChecked();
    // The person's own change is what overtook the save here, and the facts follow it too.
    expect(ownText()).toHaveTextContent('They turned AI apps off for their account.');
    expect(reads(fake)).toHaveLength(2);

    await userEvent.click(radio(useGroup, 'Allowed'));
    await userEvent.click(saveButton());
    await waitFor(() => expect(saves(fake)).toHaveLength(2));
    expect(saves(fake)[1].headers.get('If-Match')).toBe('"4"');
  });

  it('reads the account again when a save answers without its version, so the next save is not blind', async () => {
    let version = 1;
    const fake = renderAt({
      [readPath]: () => answer(policy({ enabled: version === 1 ? null : true }), version),
      [savePath]: () => { version = 2; return json(200, policy({ enabled: true })); },
    });
    await userEvent.click(await screen.findByRole('radio', { name: 'Allowed' }));
    await userEvent.click(saveButton());
    expect(await screen.findByText('This account’s AI apps were saved.')).toBeVisible();
    await userEvent.click(radio(useGroup, 'Not allowed'));
    await userEvent.click(saveButton());
    await waitFor(() => expect(saves(fake)).toHaveLength(2));
    expect(saves(fake)[1].headers.get('If-Match')).toBe('"2"');
  });

  it('answers a refused maximum on its own field', async () => {
    renderAt({
      [readPath]: answer(policy(), 1),
      [savePath]: json(422, { type: 'about:blank', title: 'x', status: 422, code: 'invalid_value', field: 'maxScopes' }),
    });
    await userEvent.click(await screen.findByRole('radio', { name: 'Allowed' }));
    await userEvent.click(saveButton());
    expect(await screen.findByText('That choice couldn’t be saved. Choose another one.')).toBeVisible();
    // The default was chosen, so the reader lands on it, where the message is said.
    expect(radio(maxGroup, 'Same as other accounts')).toHaveFocus();
    expect(screen.getByRole('group', { name: maxGroup })).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByRole('group', { name: maxGroup })).toHaveAccessibleDescription(
      'The person can allow less for their own account, never more. That choice couldn’t be saved. Choose another one.',
    );
    await userEvent.click(radio(maxGroup, 'Read'));
    expect(screen.queryByText('That choice couldn’t be saved. Choose another one.')).not.toBeInTheDocument();
  });

  it('says a failed save changed nothing and keeps the edit for another try', async () => {
    let attempt = 0;
    renderAt({
      [readPath]: answer(policy(), 1),
      [savePath]: (req: RecordedRequest) => (++attempt === 1 ? problem(503, 'unavailable') : answer(stored(req), 2)),
    });
    await userEvent.click(await screen.findByRole('radio', { name: 'Not allowed' }));
    await userEvent.click(saveButton());
    const alert = await screen.findByText('The settings couldn’t be saved. Check your connection and try again.');
    expect(alert.closest('[role="alert"]')).not.toBeNull();
    expect(radio(useGroup, 'Not allowed')).toBeChecked();
    await userEvent.click(saveButton());
    expect(await screen.findByText('This account’s AI apps were saved.')).toBeVisible();
  });

  it.each([
    ['a missing version', problem(428, 'precondition_required')],
    ['a refused origin', problem(403)],
  ])('treats %s on save as refused, not as a connection problem or the calm state', async (_, refusal) => {
    renderAt({ [readPath]: answer(policy(), 1), [savePath]: refusal });
    await userEvent.click(await screen.findByRole('radio', { name: 'Allowed' }));
    await userEvent.click(saveButton());
    expect(await screen.findByText('The settings couldn’t be saved, and nothing was changed.')).toBeVisible();
    expect(screen.queryByText('Only an installation administrator can see this.')).not.toBeInTheDocument();
  });

  it('says the account is gone when the address names one that does not exist, without a retry', async () => {
    renderAt({ [readPath]: problem(404, 'not_found') });
    const heading = await screen.findByRole('heading', { level: 2, name: 'This account no longer exists.' });
    expect(heading).not.toHaveFocus();
    expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument();
    expect(screen.queryByRole('radio')).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Back to accounts' })).toHaveAttribute('href', '/admin/accounts');
  });

  it('moves focus to the account being gone when it is deleted during a save', async () => {
    renderAt({ [readPath]: answer(policy(), 1), [savePath]: problem(404, 'not_found') });
    await userEvent.click(await screen.findByRole('radio', { name: 'Allowed' }));
    await userEvent.click(saveButton());
    const heading = await screen.findByRole('heading', { level: 2, name: 'This account no longer exists.' });
    await waitFor(() => expect(heading).toHaveFocus());
  });

  it('moves focus to the calm refusal when a save is refused for the grant, and re-reads the session', async () => {
    const fake = renderAt({ [readPath]: answer(policy(), 1), [savePath]: problem(403, 'instance_admin_required') });
    await userEvent.click(await screen.findByRole('radio', { name: 'Allowed' }));
    await userEvent.click(saveButton());
    const heading = await screen.findByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' });
    await waitFor(() => expect(heading).toHaveFocus());
    await waitFor(() => expect(fake.requests.filter((request) => request.path === '/api/v1/session').length).toBeGreaterThanOrEqual(2));
  });

  it('shows the calm refusal on arrival without moving focus', async () => {
    renderAt({ [readPath]: problem(403, 'instance_admin_required') });
    const heading = await screen.findByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' });
    expect(heading).not.toHaveFocus();
    expect(screen.queryByRole('radio')).not.toBeInTheDocument();
  });

  it('offers a retry when the account cannot be read, and none for a read refused for another reason', async () => {
    let attempt = 0;
    renderAt({ [readPath]: () => (++attempt === 1 ? problem(503, 'unavailable') : answer(policy(), 1)) });
    expect(await screen.findByText('This account’s AI apps couldn’t be loaded. Check your connection and try again.')).toBeVisible();
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByRole('radio', { name: 'Allowed' })).toBeVisible();
  });

  it('says a read refused for another reason plainly, without a retry', async () => {
    renderAt({ [readPath]: problem(403) });
    expect(await screen.findByText('This account’s AI apps couldn’t be loaded.')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument();
  });

  it('treats a read without its version as unusable rather than saving blind', async () => {
    renderAt({ [readPath]: json(200, policy()) });
    expect(await screen.findByText('This account’s AI apps couldn’t be loaded. Check your connection and try again.')).toBeVisible();
    expect(screen.queryByRole('radio')).not.toBeInTheDocument();
  });

  it('signs out when the session has ended', async () => {
    renderAt({
      [readPath]: problem(401, 'unauthenticated'),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
    });
    expect(await screen.findByLabelText('Password')).toBeVisible();
  });

  it('is reached from an account’s row, names it, and goes back to Accounts', async () => {
    const fake = renderAt({
      [readPath]: answer(policy(), 1),
      'GET /api/v1/admin/accounts': json(200, { items: [
        { userId: '0198a2f0-0000-7000-8000-000000000001', login: 'anna' },
        { userId: ACCOUNT, login: 'bob' },
      ], nextCursor: null }),
    }, '/admin/accounts');
    const own = await screen.findByRole('link', { name: 'AI apps for anna' });
    // The viewer's own account is set the same way: the grant is about the installation, not about whose row it is.
    expect(own).toHaveAttribute('href', '/admin/accounts/0198a2f0-0000-7000-8000-000000000001/ai-apps');
    await userEvent.click(screen.getByRole('link', { name: 'AI apps for bob' }));
    expect(await screen.findByRole('heading', { level: 1, name: 'AI apps for bob' })).toHaveFocus();
    expect(window.location.pathname).toBe(pagePath);
    expect(reads(fake)).toHaveLength(1);

    await userEvent.click(screen.getByRole('link', { name: 'Back to accounts' }));
    expect(await screen.findByRole('link', { name: 'AI apps for bob' })).toBeVisible();
    expect(window.location.pathname).toBe('/admin/accounts');

    // Back and Forward carry no login, so the page reached that way keeps the generic heading.
    await act(async () => { window.history.back(); await new Promise((resolve) => setTimeout(resolve, 0)); });
    expect(await screen.findByRole('heading', { level: 1, name: 'AI apps for this account' })).toBeVisible();
  });

  it('is pointed to from the installation’s AI apps', async () => {
    renderAt({
      'GET /api/v1/admin/mcp-policy': json(200, {
        enabled: true, operatorEnabled: true, defaultAccountEnabled: true, defaultMaxScopes: ['tendo:read', 'tendo:write'],
      }, { ETag: '"1"' }),
      'GET /api/v1/admin/accounts': json(200, { items: [], nextCursor: null }),
    }, '/admin/ai-apps');
    const pointer = await screen.findByText(/To set one account differently/);
    expect(pointer).toBeVisible();
    const link = within(pointer).getByRole('link', { name: 'Accounts' });
    expect(link).toHaveAttribute('href', '/admin/accounts');
    await userEvent.click(link);
    expect(await screen.findByRole('heading', { level: 2, name: 'Accounts' })).toBeVisible();
    expect(window.location.pathname).toBe('/admin/accounts');
  });

  it('offers no Admin center sections without the installation grant', async () => {
    renderAt({ [readPath]: problem(403, 'instance_admin_required') }, pagePath, session());
    expect(await screen.findByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' })).toBeVisible();
    expect(screen.queryByRole('navigation', { name: 'Admin center sections' })).not.toBeInTheDocument();
  });

  it('reads in Czech', async () => {
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['cs-CZ']);
    renderAt({ [readPath]: answer(policy({ enabled: false, effectiveScopes: [] }), 1) });
    expect(await screen.findByRole('radio', { name: 'Nepovoleno' })).toBeChecked();
    expect(screen.getByRole('heading', { level: 1, name: 'AI aplikace tohoto účtu' })).toBeVisible();
    expect(screen.getByText('Nic. Aplikace teď s Tendem pro tento účet pracovat nemohou.')).toBeVisible();
  });
});
