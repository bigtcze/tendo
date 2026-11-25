import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import {
  HOUSEHOLD_ID, household, instanceAdminSession, installFakeServer, json, type RecordedRequest,
} from '../../test/fakeServer';
import { itemRoutes } from '../../test/fakeItems';
import type { components } from '../../../../api/generated/api';

type Settings = components['schemas']['McpInstallationPolicy'];
type Change = components['schemas']['McpPolicyEvent'];

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const readPath = 'GET /api/v1/admin/mcp-policy';
const savePath = 'PUT /api/v1/admin/mcp-policy';
const changesPath = 'GET /api/v1/admin/mcp-policy-events';
const OWNER = '0198a2f0-0000-7000-8000-000000000001';
const MEMBER = '0198a2f0-0000-7000-8000-000000000002';
const problem = (status: number, code?: string) => json(status, { type: 'about:blank', title: 'x', status, ...(code ? { code } : {}) });
const settings = (change: Partial<Settings> = {}): Settings => ({
  enabled: true, operatorEnabled: true, defaultAccountEnabled: true, defaultMaxScopes: ['tendo:read', 'tendo:write'], ...change,
});
const answer = (body: Settings, version: number) => json(200, body, { ETag: `"${version}"` });

let sequence = 0;
/** One audit row; ids only have to be unique, the list never shows them. */
function change(fields: Partial<Change>): Change {
  sequence += 1;
  return {
    id: `0198a2f0-7c1e-7a53-9b0e-${String(sequence).padStart(12, '0')}`,
    policy: 'installation', actorUserId: OWNER, actorLogin: 'anna', targetUserId: null, targetLogin: null,
    enabled: true, defaultAccountEnabled: true, scopes: ['tendo:read', 'tendo:write'], occurredAt: '2026-11-24T10:00:00Z',
    ...fields,
  };
}
const page = (items: Change[], nextCursor: string | null = null) => json(200, { items, nextCursor });
const formatted = (iso: string, locale = 'en') =>
  new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(iso));

function renderAt(routes: Parameters<typeof installFakeServer>[0]) {
  const fake = installFakeServer({
    'GET /api/v1/session': json(200, instanceAdminSession()),
    [`GET ${base}`]: json(200, household),
    ...itemRoutes(base, []),
    [readPath]: answer(settings(), 1),
    ...routes,
  });
  window.history.replaceState(null, '', '/admin/ai-apps');
  render(<I18nProvider><App /></I18nProvider>);
  return fake;
}

const changesList = () => screen.getByRole('list', { name: 'Recent changes' });
const rows = () => within(changesList()).getAllByRole('listitem');
const changeReads = (fake: { requests: RecordedRequest[] }) =>
  fake.requests.filter((request) => request.method === 'GET' && request.path === '/api/v1/admin/mcp-policy-events');
const refusal = 'Only an installation administrator can see this.';

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

describe('recent AI apps changes', () => {
  it('says who changed what and when, newest first and in plain words, without ids or scope names', async () => {
    const fake = renderAt({
      [changesPath]: page([
        change({ policy: 'user', actorUserId: MEMBER, actorLogin: 'petr', targetUserId: MEMBER, targetLogin: 'petr', enabled: true, defaultAccountEnabled: null, scopes: ['tendo:read'], occurredAt: '2026-11-24T12:00:00Z' }),
        change({ policy: 'user', actorUserId: MEMBER, actorLogin: 'petr', targetUserId: MEMBER, targetLogin: 'petr', enabled: false, defaultAccountEnabled: null, scopes: ['tendo:read'] }),
        change({ policy: 'account', targetUserId: MEMBER, targetLogin: 'petr', enabled: false, defaultAccountEnabled: null, scopes: ['tendo:write'] }),
        change({ policy: 'account', targetUserId: MEMBER, targetLogin: null, actorLogin: null, enabled: null, defaultAccountEnabled: null, scopes: null }),
        change({ enabled: false, defaultAccountEnabled: false, scopes: ['tendo:read'] }),
        change({}),
      ]),
    });
    await waitFor(() => expect(rows()).toHaveLength(6));
    const text = rows().map((row) => row.textContent);
    expect(text[0]).toBe(`petr’s own choiceThey allow read only.Changed by petr, ${formatted('2026-11-24T12:00:00Z')}`);
    expect(text[1]).toContain('They turned AI apps off for their account.');
    expect(text[2]).toBe(`AI apps for petrNot allowed for this account. The most apps may do: change only, without reading.Changed by anna, ${formatted('2026-11-24T10:00:00Z')}`);
    expect(text[3]).toBe(`AI apps for a deleted accountSame as other accounts. The most apps may do: the same as other accounts.Changed by a deleted account, ${formatted('2026-11-24T10:00:00Z')}`);
    expect(text[4]).toContain('AI apps for everyoneNot allowed in Tendo. Accounts can’t use them unless set differently. The most apps may do: read only.');
    expect(text[5]).toContain('AI apps for everyoneAllowed in Tendo. Accounts can use them. The most apps may do: read and change.');
    expect(changesList().textContent).not.toMatch(/tendo:|0198a2f0|installation|policy/);

    // One page of twenty is read, with no cursor; the last page offers no Show more.
    const [first] = changeReads(fake);
    expect(first.query.get('limit')).toBe('20');
    expect(first.query.has('cursor')).toBe(false);
    expect(screen.queryByRole('button', { name: 'Show more' })).not.toBeInTheDocument();
  });

  it('holds the place with one status line while the changes load', async () => {
    renderAt({ [changesPath]: () => new Promise<Response>(() => {}) });
    expect(await screen.findByText('Loading recent changes…')).toHaveAttribute('role', 'status');
    expect(screen.queryByRole('list', { name: 'Recent changes' })).not.toBeInTheDocument();
  });

  it('says plainly when nothing has been changed yet', async () => {
    renderAt({ [changesPath]: page([]) });
    expect(await screen.findByText('Nothing has been changed yet.')).toBeVisible();
    expect(screen.queryByRole('list', { name: 'Recent changes' })).not.toBeInTheDocument();
  });

  it('shows more by cursor below the rows already shown', async () => {
    const fake = renderAt({
      [`${changesPath}?limit=20`]: page([change({ targetLogin: null })], 'next-1'),
      [`${changesPath}?limit=20&cursor=next-1`]: page([change({ enabled: false })]),
    });
    await userEvent.click(await screen.findByRole('button', { name: 'Show more' }));
    await waitFor(() => expect(rows()).toHaveLength(2));
    expect(rows()[1]).toHaveTextContent('Not allowed in Tendo.');
    expect(changeReads(fake)[1].query.get('cursor')).toBe('next-1');
    expect(screen.queryByRole('button', { name: 'Show more' })).not.toBeInTheDocument();
  });

  it('keeps the rows when Show more fails, and the retry appends the page', async () => {
    let more = 0;
    renderAt({
      [`${changesPath}?limit=20`]: page([change({})], 'next-1'),
      [`${changesPath}?limit=20&cursor=next-1`]: () => (++more === 1 ? problem(503, 'unavailable') : page([change({ enabled: false })])),
    });
    await userEvent.click(await screen.findByRole('button', { name: 'Show more' }));
    const failed = await screen.findByText('More changes couldn’t be loaded. Try again.');
    expect(rows()).toHaveLength(1);
    await userEvent.click(within(failed.closest('[role="alert"]') as HTMLElement).getByRole('button', { name: 'Try again' }));
    await waitFor(() => expect(rows()).toHaveLength(2));
    expect(screen.queryByText('More changes couldn’t be loaded. Try again.')).not.toBeInTheDocument();
  });

  it('offers a retry when the changes cannot be read, and the settings stay usable', async () => {
    let reads = 0;
    renderAt({ [changesPath]: () => (++reads === 1 ? problem(503, 'unavailable') : page([change({})])) });
    const failed = await screen.findByText('Recent changes couldn’t be loaded. Check your connection and try again.');
    expect(screen.getByRole('switch', { name: 'Allow AI apps in Tendo' })).toBeEnabled();
    await userEvent.click(within(failed.closest('[role="alert"]') as HTMLElement).getByRole('button', { name: 'Try again' }));
    await waitFor(() => expect(rows()).toHaveLength(1));
    expect(screen.queryByText('Recent changes couldn’t be loaded. Check your connection and try again.')).not.toBeInTheDocument();
  });

  it('reads the changes again after a save, so the save shows on the list', async () => {
    let reads = 0;
    const fake = renderAt({
      [changesPath]: () => (++reads === 1 ? page([]) : page([change({ scopes: ['tendo:read'] })])),
      [savePath]: answer(settings({ defaultMaxScopes: ['tendo:read'] }), 2),
    });
    expect(await screen.findByText('Nothing has been changed yet.')).toBeVisible();
    await userEvent.click(await screen.findByRole('checkbox', { name: /^Change/ }));
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('AI app settings saved.')).toBeVisible();
    await waitFor(() => expect(rows()).toHaveLength(1));
    expect(rows()[0]).toHaveTextContent('The most apps may do: read only.');
    expect(changeReads(fake)).toHaveLength(2);
  });

  it('reads the changes again when someone else saved in between, since their change is now on the list', async () => {
    let reads = 0;
    let settingsReads = 0;
    const fake = renderAt({
      [readPath]: () => (++settingsReads === 1 ? answer(settings(), 1) : answer(settings({ enabled: false }), 2)),
      [changesPath]: () => (++reads === 1 ? page([]) : page([change({ actorLogin: 'petr', enabled: false })])),
      [savePath]: problem(412),
    });
    await userEvent.click(await screen.findByRole('checkbox', { name: /^Change/ }));
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText(/Someone else changed these settings in the meantime/)).toBeVisible();
    await waitFor(() => expect(rows()).toHaveLength(1));
    expect(rows()[0]).toHaveTextContent('Changed by petr');
    expect(changeReads(fake)).toHaveLength(2);
  });

  it('keeps the rows and offers a retry when the read after a save fails', async () => {
    let reads = 0;
    renderAt({
      [changesPath]: () => (++reads === 1 ? page([change({})]) : problem(503, 'unavailable')),
      [savePath]: answer(settings({ defaultMaxScopes: ['tendo:read'] }), 2),
    });
    await waitFor(() => expect(rows()).toHaveLength(1));
    await userEvent.click(screen.getByRole('checkbox', { name: /^Change/ }));
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('Recent changes couldn’t be loaded. Check your connection and try again.')).toBeVisible();
    expect(rows()).toHaveLength(1);
  });

  it('does not read the changes again after a save that changed nothing', async () => {
    const fake = renderAt({ [changesPath]: page([]), [savePath]: problem(503, 'unavailable') });
    await userEvent.click(await screen.findByRole('checkbox', { name: /^Change/ }));
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByText('The settings couldn’t be saved. Check your connection and try again.')).toBeVisible();
    expect(changeReads(fake)).toHaveLength(1);
  });

  it('shows the calm refusal in place of the whole section when the list is refused for the grant', async () => {
    const fake = renderAt({ [changesPath]: problem(403, 'instance_admin_required') });
    const heading = await screen.findByRole('heading', { level: 2, name: refusal });
    expect(screen.queryByRole('switch')).not.toBeInTheDocument();
    expect(screen.queryByRole('heading', { level: 2, name: 'Recent changes' })).not.toBeInTheDocument();
    await waitFor(() => expect(fake.requests.filter((request) => request.path === '/api/v1/session').length).toBeGreaterThanOrEqual(2));
    // Nothing the reader was using went away, so focus stays where the page put it.
    expect(heading).not.toHaveFocus();
  });

  it('moves focus to the calm refusal when Show more is refused for the grant', async () => {
    renderAt({
      [`${changesPath}?limit=20`]: page([change({})], 'next-1'),
      [`${changesPath}?limit=20&cursor=next-1`]: problem(403, 'instance_admin_required'),
    });
    await userEvent.click(await screen.findByRole('button', { name: 'Show more' }));
    const heading = await screen.findByRole('heading', { level: 2, name: refusal });
    await waitFor(() => expect(heading).toHaveFocus());
  });

  it('treats a refused list that is not about the grant as a failure to retry, not the calm state', async () => {
    renderAt({ [changesPath]: problem(403) });
    expect(await screen.findByText('Recent changes couldn’t be loaded. Check your connection and try again.')).toBeVisible();
    expect(screen.queryByRole('heading', { level: 2, name: refusal })).not.toBeInTheDocument();
  });

  it('signs out when the session has ended', async () => {
    renderAt({
      [changesPath]: problem(401, 'unauthenticated'),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
    });
    expect(await screen.findByLabelText('Password')).toBeVisible();
  });

  it('reads in Czech', async () => {
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['cs-CZ']);
    renderAt({
      [changesPath]: page([
        change({ policy: 'account', targetUserId: MEMBER, targetLogin: 'petr', enabled: true, defaultAccountEnabled: null, scopes: null }),
        change({ actorLogin: null, enabled: true, defaultAccountEnabled: false, scopes: ['tendo:read'] }),
      ]),
    });
    const list = await screen.findByRole('list', { name: 'Poslední změny' });
    const items = within(list).getAllByRole('listitem');
    expect(items[0]).toHaveTextContent(`AI aplikace účtu petrPovoleno pro tento účet. Nejvíc, co aplikace smějí: totéž co u ostatních účtů.Změnu provedl účet anna, ${formatted('2026-11-24T10:00:00Z', 'cs')}`);
    expect(items[1]).toHaveTextContent('AI aplikace pro všechnyV Tendu povoleno. Účty je nemohou používat, pokud nejsou nastavené jinak. Nejvíc, co aplikace smějí: jen čtení.Změnu provedl smazaný účet');
  });
});
