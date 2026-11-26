import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { components } from '../../../../api/generated/api';
import { I18nProvider } from '../../i18n';
import { cs } from '../../i18n/cs';
import { installFakeServer, json, type RecordedRequest } from '../../test/fakeServer';
import { OwnAiAppsSection } from './OwnAiAppsSection';
import { isOwnAiAppsRelevant } from './useOwnAiApps';

type Settings = components['schemas']['McpSettings'];

const path = '/api/v1/mcp/settings';
const READ = `GET ${path}`;
const SAVE = `PUT ${path}`;
const BOTH: Settings['scopes'] = ['tendo:read', 'tendo:write'];
const settings = (change: Partial<Settings> = {}): Settings => ({
  enabled: true, scopes: BOTH, maximumScopes: BOTH, effectiveScopes: BOTH, ...change,
});
const answer = (body: Settings, version: number) => json(200, body, { ETag: `"${version}"` });
const problem = (status: number, code?: string) => json(status, { type: 'about:blank', title: 'x', status, ...(code ? { code } : {}) });
/** The server's answer to a save: the stored choice, with what apps may do now as the maximum allows. */
const stored = (req: RecordedRequest, base: Settings = settings()): Settings => {
  const body = JSON.parse(req.body) as { enabled: boolean; scopes: Settings['scopes'] };
  const effective = body.enabled ? base.maximumScopes.filter((scope) => body.scopes.includes(scope)) : [];
  return { ...base, ...body, effectiveScopes: effective };
};

const onSignedOut = vi.fn();
function renders(routes: Parameters<typeof installFakeServer>[0]) {
  const fake = installFakeServer(routes);
  const view = render(<I18nProvider><OwnAiAppsSection onSignedOut={onSignedOut} /></I18nProvider>);
  return { fake, view };
}
const heading = () => screen.findByRole('heading', { level: 2, name: 'AI apps' });
const ownSwitch = () => screen.getByRole('switch', { name: 'Let my AI apps work with Tendo' });
const read = () => screen.getByRole('checkbox', { name: /^Read/ });
const change = () => screen.getByRole('checkbox', { name: /^Change/ });
const saveButton = () => screen.getByRole('button', { name: 'Save' });
const nowText = () => document.querySelector('[data-own-ai-apps-now]');
const saves = (fake: ReturnType<typeof installFakeServer>) => fake.requests.filter((r) => r.method === 'PUT');
const reads = (fake: ReturnType<typeof installFakeServer>) => fake.requests.filter((r) => r.method === 'GET');
/** Lets a read run through fetch, the JSON and the settle, so "nothing shown" is an answer and not a pending one. */
async function settled() {
  for (let i = 0; i < 5; i += 1) await act(() => new Promise<void>((resolve) => { setTimeout(resolve, 0); }));
}

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
  onSignedOut.mockClear();
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

describe('own AI apps on Account', () => {
  it('shows nothing where AI apps are not allowed and the person never changed their own choice', async () => {
    const { fake, view } = renders({ [READ]: answer(settings({ maximumScopes: [], effectiveScopes: [] }), 1) });
    await waitFor(() => expect(reads(fake)).toHaveLength(1));
    await settled();
    expect(view.container).toBeEmptyDOMElement();
    expect(onSignedOut).not.toHaveBeenCalled();
  });

  it.each([
    ['allowed, default choice', settings(), true],
    ['allowed read only, default choice', settings({ maximumScopes: ['tendo:read'] }), true],
    ['nothing allowed, default choice', settings({ maximumScopes: [], effectiveScopes: [] }), false],
    ['nothing allowed, switched off', settings({ enabled: false, maximumScopes: [], effectiveScopes: [] }), true],
    ['nothing allowed, read only', settings({ scopes: ['tendo:read'], maximumScopes: [], effectiveScopes: [] }), true],
    ['nothing allowed, change only', settings({ scopes: ['tendo:write'], maximumScopes: [], effectiveScopes: [] }), true],
  ])('applies to the account when %s: %s', (_name, value, expected) => {
    expect(isOwnAiAppsRelevant(value)).toBe(expected);
  });

  it('hands a signed-out read to the app and shows nothing', async () => {
    const { view } = renders({ [READ]: problem(401, 'unauthenticated') });
    await waitFor(() => expect(onSignedOut).toHaveBeenCalledTimes(1));
    await settled();
    expect(view.container).toBeEmptyDOMElement();
  });

  it('says a refused read could not be loaded, without a retry', async () => {
    renders({ [READ]: problem(403, 'forbidden') });
    expect(await screen.findByText('Your AI app settings couldn’t be loaded.')).toBeVisible();
    expect(screen.queryByRole('button', { name: 'Try again' })).toBeNull();
  });

  it('shows what apps can do now and the person’s own choice in plain words', async () => {
    renders({ [READ]: answer(settings(), 3) });
    const title = await heading();
    expect(title.closest('section')).toHaveAttribute('aria-labelledby', title.id);
    expect(nowText()).toHaveTextContent('Read and change.');
    expect(ownSwitch()).toBeChecked();
    expect(ownSwitch()).toHaveAccessibleDescription(/paused, not disconnected/);
    expect(read()).toBeChecked();
    expect(change()).toBeChecked();
    expect(screen.getByRole('group', { name: 'What my apps may do' })).toBeVisible();
    // Nothing is withheld by the administrator here, so no option carries a note.
    expect(screen.queryByText(/doesn’t allow this for your account/)).toBeNull();
    expect(screen.getByRole('region', { name: 'AI apps' })).not.toHaveTextContent(/tendo:|scope|polic|MCP|override|ETag/i);
  });

  it('says which choice the administrator does not allow, and keeps it a choice', async () => {
    renders({ [READ]: answer(settings({ maximumScopes: ['tendo:read'], effectiveScopes: ['tendo:read'] }), 2) });
    await heading();
    expect(nowText()).toHaveTextContent('Read only.');
    expect(change()).toHaveAccessibleName(/Your administrator doesn’t allow this for your account/);
    expect(read()).not.toHaveAccessibleName(/administrator/);
    expect(change()).toBeEnabled();
  });

  it('shows a narrowed choice even where AI apps are not allowed, and says the choice waits', async () => {
    renders({ [READ]: answer(settings({ enabled: false, maximumScopes: [], effectiveScopes: [] }), 4) });
    await heading();
    expect(nowText()).toHaveTextContent('Nothing. Your apps can’t work with Tendo right now.');
    expect(screen.getByText(/aren’t allowed for your account right now/)).toBeVisible();
    expect(ownSwitch()).not.toBeChecked();
    // The notice already says why; no option repeats it.
    expect(screen.queryByText(/doesn’t allow this for your account/)).toBeNull();
  });

  it('saves the whole choice with the version it was read at, and shows what the server kept', async () => {
    const { fake } = renders({ [READ]: answer(settings(), 7), [SAVE]: (req) => answer(stored(req), 8) });
    await heading();
    await userEvent.click(change());
    // Nothing is sent before Save.
    expect(saves(fake)).toHaveLength(0);
    await userEvent.click(saveButton());
    expect(await screen.findByText('Your AI app settings were saved.')).toBeVisible();
    const [request] = saves(fake);
    expect(request.headers.get('If-Match')).toBe('"7"');
    expect(JSON.parse(request.body)).toEqual({ enabled: true, scopes: ['tendo:read'] });
    expect(nowText()).toHaveTextContent('Read only.');

    // Switching off pauses: the next save names the version the server just answered with.
    await userEvent.click(ownSwitch());
    await userEvent.click(saveButton());
    await waitFor(() => expect(saves(fake)).toHaveLength(2));
    expect(saves(fake)[1].headers.get('If-Match')).toBe('"8"');
    expect(JSON.parse(saves(fake)[1].body)).toEqual({ enabled: false, scopes: ['tendo:read'] });
  });

  it('stays on screen after a save that makes it one the page would not show on arrival', async () => {
    const off = settings({ enabled: false, scopes: ['tendo:read'], maximumScopes: [], effectiveScopes: [] });
    renders({ [READ]: answer(off, 2), [SAVE]: (req) => answer(stored(req, off), 3) });
    await heading();
    await userEvent.click(ownSwitch());
    await userEvent.click(change());
    await userEvent.click(saveButton());
    expect(await screen.findByText('Your AI app settings were saved.')).toBeVisible();
    expect(screen.getByRole('heading', { level: 2, name: 'AI apps' })).toBeVisible();
    expect(ownSwitch()).toBeChecked();
  });

  it('asks for at least one kind of access on the field instead of sending a save that must fail', async () => {
    const { fake } = renders({ [READ]: answer(settings({ scopes: ['tendo:read'] }), 1) });
    await heading();
    await userEvent.click(read());
    await userEvent.click(saveButton());
    expect(await screen.findByText('Choose at least one.')).toBeVisible();
    expect(saves(fake)).toHaveLength(0);
    expect(read()).toHaveFocus();
    expect(read()).toHaveAttribute('aria-invalid', 'true');
  });

  it('reads again after someone else saved in between, and says so', async () => {
    let version = 1;
    const { fake } = renders({
      [READ]: () => answer(settings(version === 1 ? {} : { enabled: false, effectiveScopes: [] }), version),
      [SAVE]: () => { version = 2; return problem(412, 'precondition_failed'); },
    });
    await heading();
    await userEvent.click(change());
    await userEvent.click(saveButton());
    expect(await screen.findByText(/changed elsewhere in the meantime/)).toBeVisible();
    expect(reads(fake)).toHaveLength(2);
    expect(ownSwitch()).not.toBeChecked();
    expect(change()).toBeChecked();
  });

  it('says a save refused for coming too often changed nothing, and keeps the edit', async () => {
    renders({ [READ]: answer(settings(), 1), [SAVE]: problem(429, 'rate_limited') });
    await heading();
    await userEvent.click(change());
    await userEvent.click(saveButton());
    expect(await screen.findByText('Too many saves just now. Nothing was changed; try again in a minute.')).toBeVisible();
    expect(screen.getByRole('alert')).toHaveTextContent('Too many saves just now.');
    expect(change()).not.toBeChecked();
    expect(saveButton()).toBeEnabled();
  });

  it('answers an unanswered save with the retryable message, and a refused one with its own', async () => {
    let next = problem(503, 'unavailable');
    renders({ [READ]: answer(settings(), 1), [SAVE]: () => next });
    await heading();
    await userEvent.click(change());
    await userEvent.click(saveButton());
    expect(await screen.findByText('The settings couldn’t be saved. Try again in a moment.')).toBeVisible();
    next = problem(422, 'invalid_value');
    await userEvent.click(saveButton());
    expect(await screen.findByText('Choose at least one.')).toBeVisible();
    // Once the controls the save disabled are usable again, the reader is back on the choice.
    await waitFor(() => expect(read()).toHaveFocus());
  });

  it('says a refused save changed nothing', async () => {
    renders({ [READ]: answer(settings(), 1), [SAVE]: problem(403, 'forbidden') });
    await heading();
    await userEvent.click(change());
    await userEvent.click(saveButton());
    expect(await screen.findByText('The settings couldn’t be saved, and nothing was changed.')).toBeVisible();
    expect(onSignedOut).not.toHaveBeenCalled();
  });

  it('hands a signed-out save to the app', async () => {
    renders({ [READ]: answer(settings(), 1), [SAVE]: problem(401, 'unauthenticated') });
    await heading();
    await userEvent.click(saveButton());
    await waitFor(() => expect(onSignedOut).toHaveBeenCalledTimes(1));
  });

  it('sends one save however quickly Save is pressed twice', async () => {
    let release!: (response: Response) => void;
    const { fake } = renders({
      [READ]: answer(settings(), 1),
      [SAVE]: () => new Promise<Response>((resolve) => { release = resolve; }),
    });
    await heading();
    const form = saveButton().closest('form')!;
    act(() => { form.requestSubmit(); form.requestSubmit(); });
    await waitFor(() => expect(saves(fake)).toHaveLength(1));
    await act(async () => { release(answer(settings(), 2)); });
    expect(await screen.findByText('Your AI app settings were saved.')).toBeVisible();
    expect(saves(fake)).toHaveLength(1);
  });

  it('offers a retry when the settings could not be read, and shows them once they can', async () => {
    let failing = true;
    renders({ [READ]: () => (failing ? problem(503, 'unavailable') : answer(settings(), 1)) });
    const retry = await screen.findByRole('button', { name: 'Try again' });
    expect(screen.getByText('Your AI app settings couldn’t be loaded. Try again in a moment.')).toBeVisible();
    failing = false;
    await userEvent.click(retry);
    // The heading stays, so the section never empties under the reader while it reads again.
    expect(screen.getByRole('heading', { level: 2, name: 'AI apps' })).toBeVisible();
    expect(await screen.findByRole('switch', { name: 'Let my AI apps work with Tendo' })).toBeChecked();
  });

  it('speaks Czech', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    renders({ [READ]: answer(settings({ maximumScopes: ['tendo:read'], effectiveScopes: ['tendo:read'] }), 1) });
    const title = await screen.findByRole('heading', { level: 2, name: cs['ownAiApps.title'] });
    const section = within(title.closest('section')!);
    expect(section.getByRole('switch', { name: cs['ownAiApps.enabled'] })).toBeChecked();
    expect(section.getByText(cs['ownAiApps.access.notAllowed'])).toBeVisible();
    expect(nowText()).toHaveTextContent(cs['accountAiApps.now.read']);
  });
});
