import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { cs } from '../../i18n/cs';
import { en } from '../../i18n/en';
import {
  HOUSEHOLD_ID, household, instanceAdminSession, installFakeServer, json, type RecordedRequest,
} from '../../test/fakeServer';
import { itemRoutes } from '../../test/fakeItems';
import type { McpGrant } from '../mcp/mcpGrantsApi';

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const ACCOUNT = '0198a2f0-0000-7000-8000-0000000000b0';
const grantsPath = `/api/v1/admin/accounts/${ACCOUNT}/mcp-grants`;
const LIST = `GET ${grantsPath}`;
const ALL = `DELETE ${grantsPath}`;
const one = (id: string) => `DELETE ${grantsPath}/${id}`;
const pagePath = `/admin/accounts/${ACCOUNT}/ai-apps`;
const problem = (status: number, code?: string) => json(status, { type: 'about:blank', title: 'x', status, ...(code ? { code } : {}) });
const policy = json(200, {
  userId: ACCOUNT, enabled: null, maxScopes: null, userEnabled: true, userScopes: ['tendo:read', 'tendo:write'],
  effectiveScopes: ['tendo:read', 'tendo:write'],
}, { ETag: '"1"' });
const grant = (id: string, clientName: string, scopes: McpGrant['scopes'] = ['tendo:read']): McpGrant => ({
  id, clientName, clientNameVerified: false, scopes, createdAt: '2026-11-20T10:00:00Z', expiresAt: '2027-02-18T10:00:00Z',
});
const A = grant('0198a2f0-0000-7000-8000-00000000000a', 'Claude Code', ['tendo:read', 'tendo:write']);
const B = grant('0198a2f0-0000-7000-8000-00000000000b', 'Notes helper');
const C = grant('0198a2f0-0000-7000-8000-00000000000c', 'Mail bot');
const page = (items: McpGrant[], nextCursor: string | null = null) => json(200, { items, nextCursor });

function renderAt(routes: Parameters<typeof installFakeServer>[0]) {
  const fake = installFakeServer({
    'GET /api/v1/session': json(200, instanceAdminSession()),
    [`GET ${base}`]: json(200, household),
    ...itemRoutes(base, []),
    [`GET /api/v1/admin/accounts/${ACCOUNT}/mcp-policy`]: policy,
    'GET /api/v1/auth/setup': json(200, { required: false }),
    'GET /api/v1/auth/oidc': json(200, { enabled: false }),
    ...routes,
  });
  window.history.replaceState(null, '', pagePath);
  render(<I18nProvider><App /></I18nProvider>);
  return fake;
}

const section = () => screen.getByRole('region', { name: en['connectedApps.title'] });
const rows = () => [...document.querySelectorAll<HTMLElement>('li[data-grant-id]')];
const disconnectFor = (name: string) => screen.getByRole('button', { name: `Disconnect ${name}` });
const allButton = () => within(section()).getByRole('button', { name: en['accountApps.all.open'] });
const deletes = (fake: ReturnType<typeof installFakeServer>) =>
  fake.requests.filter((request: RecordedRequest) => request.method === 'DELETE');
const lists = (fake: ReturnType<typeof installFakeServer>) =>
  fake.requests.filter((request: RecordedRequest) => request.method === 'GET' && request.path === grantsPath);

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

describe('one account’s connected apps', () => {
  it('lists the account’s apps, in words about someone else, and pages with Show more', async () => {
    const fake = renderAt({
      [LIST]: page([A, B], 'g1next'),
      [`${LIST}?limit=20&cursor=g1next`]: page([C]),
    });
    expect(await screen.findByText('Claude Code')).toBeVisible();
    expect(screen.getByText(en['accountApps.body'])).toBeVisible();
    expect(rows()).toHaveLength(2);
    expect(rows()[0]).toHaveTextContent(en['connectedApps.access.write']);
    expect(rows()[1]).toHaveTextContent(en['accountApps.access.read']);
    expect(section()).not.toHaveTextContent(/\byour\b/i);
    expect(lists(fake)[0].query.get('limit')).toBe('20');

    await userEvent.click(screen.getByRole('button', { name: en['connectedApps.more'] }));
    expect(await screen.findByText('Mail bot')).toBeVisible();
    expect(lists(fake)[1].query.get('cursor')).toBe('g1next');
    expect(screen.queryByRole('button', { name: en['connectedApps.more'] })).not.toBeInTheDocument();
  });

  it('says plainly when the account has no connected apps, and offers no Disconnect all', async () => {
    renderAt({ [LIST]: page([]) });
    expect(await screen.findByText(en['accountApps.empty'])).toBeVisible();
    expect(within(section()).queryByRole('button')).not.toBeInTheDocument();
  });

  it('shows a retry when the list cannot be read, and recovers', async () => {
    let fail = true;
    renderAt({ [LIST]: () => (fail ? problem(503, 'unavailable') : page([A])) });
    expect(await within(await screen.findByRole('region', { name: en['connectedApps.title'] })).findByText(en['connectedApps.error.load'])).toBeVisible();
    fail = false;
    await userEvent.click(within(section()).getByRole('button', { name: en['connectedApps.retry'] }));
    expect(await screen.findByText('Claude Code')).toBeVisible();
  });

  it('disconnects one app only after confirming, with the canonical Origin, and removes it on 204', async () => {
    const fake = renderAt({ [LIST]: page([A, B]), [one(A.id)]: json(204) });
    await screen.findByText('Claude Code');
    await userEvent.click(disconnectFor('Claude Code'));
    const dialog = screen.getByRole('dialog', { name: 'Disconnect Claude Code?' });
    expect(dialog).toHaveTextContent('Claude Code loses access to this account right away.');
    expect(deletes(fake)).toHaveLength(0);
    await userEvent.click(within(dialog).getByRole('button', { name: en['connectedApps.dialog.confirm'] }));

    expect(await screen.findByText('Claude Code was disconnected.')).toBeVisible();
    expect(rows().map((row) => row.dataset.grantId)).toEqual([B.id]);
    expect(deletes(fake)).toHaveLength(1);
    expect(deletes(fake)[0].headers.get('Origin')).toBe(window.location.origin);
    // Focus lands on the row that took the removed row's place.
    expect(disconnectFor('Notes helper')).toHaveFocus();
  });

  it('keeps a refused or failed disconnect in the dialog, with the row still listed', async () => {
    let answer = problem(429, 'rate_limited');
    renderAt({ [LIST]: page([A]), [one(A.id)]: () => answer });
    await screen.findByText('Claude Code');
    await userEvent.click(disconnectFor('Claude Code'));
    const dialog = screen.getByRole('dialog', { name: 'Disconnect Claude Code?' });
    const confirm = within(dialog).getByRole('button', { name: en['connectedApps.dialog.confirm'] });
    await userEvent.click(confirm);
    expect(await within(dialog).findByText(en['connectedApps.error.rateLimited'])).toBeVisible();

    answer = problem(403, 'origin_mismatch');
    await userEvent.click(confirm);
    expect(await within(dialog).findByText(en['connectedApps.error.rejected'])).toBeVisible();

    answer = problem(503, 'unavailable');
    await userEvent.click(confirm);
    expect(await within(dialog).findByText(en['connectedApps.error.action'])).toBeVisible();
    expect(rows()).toHaveLength(1);
  });

  it('reloads quietly when the app was already disconnected', async () => {
    let listed = [A, B];
    const fake = renderAt({
      [LIST]: () => page(listed),
      [one(A.id)]: () => { listed = [B]; return problem(404, 'not_found'); },
    });
    await screen.findByText('Claude Code');
    await userEvent.click(disconnectFor('Claude Code'));
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: en['connectedApps.dialog.confirm'] }));
    expect(await screen.findByText('Claude Code was already disconnected. The list has been refreshed.')).toBeVisible();
    await waitFor(() => expect(lists(fake)).toHaveLength(2));
    expect(rows().map((row) => row.dataset.grantId)).toEqual([B.id]);
  });

  it('says the account is gone when a disconnect finds it deleted', async () => {
    let gone = false;
    renderAt({
      [LIST]: () => (gone ? problem(404, 'not_found') : page([A])),
      [one(A.id)]: () => { gone = true; return problem(404, 'not_found'); },
    });
    await screen.findByText('Claude Code');
    await userEvent.click(disconnectFor('Claude Code'));
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: en['connectedApps.dialog.confirm'] }));
    const heading = await screen.findByRole('heading', { level: 2, name: en['accountAiApps.gone.title'] });
    expect(heading).toHaveFocus();
    expect(screen.queryByRole('region', { name: en['connectedApps.title'] })).not.toBeInTheDocument();
    expect(screen.queryByRole('radio')).not.toBeInTheDocument();
  });

  it('gives the installation refusal when the grant is gone during a disconnect', async () => {
    renderAt({ [LIST]: page([A]), [one(A.id)]: problem(403, 'instance_admin_required') });
    await screen.findByText('Claude Code');
    await userEvent.click(disconnectFor('Claude Code'));
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: en['connectedApps.dialog.confirm'] }));
    expect(await screen.findByRole('heading', { level: 2, name: en['install.onlyAdmins.title'] })).toHaveFocus();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('puts focus on the refusal after a disconnect even when the settings above could not load', async () => {
    renderAt({
      [`GET /api/v1/admin/accounts/${ACCOUNT}/mcp-policy`]: problem(503, 'unavailable'),
      [LIST]: page([A]),
      [one(A.id)]: problem(403, 'instance_admin_required'),
    });
    await screen.findByText('Claude Code');
    expect(await screen.findByText(en['accountAiApps.error.load'])).toBeVisible();
    await userEvent.click(disconnectFor('Claude Code'));
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: en['connectedApps.dialog.confirm'] }));
    expect(await screen.findByRole('heading', { level: 2, name: en['install.onlyAdmins.title'] })).toHaveFocus();
  });

  it('signs out when the session ended during a disconnect', async () => {
    renderAt({ [LIST]: page([A]), [one(A.id)]: problem(401, 'unauthenticated') });
    await screen.findByText('Claude Code');
    await userEvent.click(disconnectFor('Claude Code'));
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: en['connectedApps.dialog.confirm'] }));
    expect(await screen.findByLabelText('Password')).toBeVisible();
  });

  it('disconnects every app after one confirmation, then says so and shows the empty list', async () => {
    let listed = [A, B];
    const fake = renderAt({
      [LIST]: () => page(listed),
      [ALL]: () => { listed = []; return json(204); },
    });
    await screen.findByText('Claude Code');
    await userEvent.click(allButton());
    const dialog = screen.getByRole('dialog', { name: en['accountApps.all.title'] });
    expect(dialog).toHaveTextContent(en['accountApps.all.again']);
    expect(deletes(fake)).toHaveLength(0);
    await userEvent.click(within(dialog).getByRole('button', { name: en['accountApps.all.confirm'] }));

    expect(await screen.findByText(en['accountApps.notice.allDisconnected'])).toBeVisible();
    expect(await screen.findByText(en['accountApps.empty'])).toBeVisible();
    expect(rows()).toHaveLength(0);
    expect(deletes(fake)).toHaveLength(1);
    expect(deletes(fake)[0].path).toBe(grantsPath);
    expect(deletes(fake)[0].headers.get('Origin')).toBe(window.location.origin);
    expect(screen.getByRole('heading', { level: 2, name: en['connectedApps.title'] })).toHaveFocus();
  });

  it('keeps focus on the section heading while the list is read again after Disconnect all', async () => {
    let release: (response: Response) => void = () => undefined;
    let reads = 0;
    renderAt({
      [LIST]: () => (++reads === 1 ? page([A, B]) : new Promise<Response>((resolve) => { release = resolve; })),
      [ALL]: json(204),
    });
    await screen.findByText('Claude Code');
    await userEvent.click(allButton());
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: en['accountApps.all.confirm'] }));
    const heading = screen.getByRole('heading', { level: 2, name: en['connectedApps.title'] });
    await waitFor(() => expect(reads).toBe(2));
    await waitFor(() => expect(heading).toHaveFocus());
    expect(rows()).toHaveLength(0);
    await act(async () => { release(page([])); });
    expect(await screen.findByText(en['accountApps.empty'])).toBeVisible();
    expect(heading).toHaveFocus();
  });

  it('keeps a foreign refusal of Disconnect all in the dialog', async () => {
    renderAt({ [LIST]: page([A]), [ALL]: problem(403, 'origin_mismatch') });
    await screen.findByText('Claude Code');
    await userEvent.click(allButton());
    const dialog = screen.getByRole('dialog', { name: en['accountApps.all.title'] });
    await userEvent.click(within(dialog).getByRole('button', { name: en['accountApps.all.confirm'] }));
    expect(await within(dialog).findByText(en['connectedApps.error.rejected'])).toBeVisible();
    expect(rows()).toHaveLength(1);
  });

  it('gives the installation refusal with focus when the grant is gone during Disconnect all', async () => {
    renderAt({ [LIST]: page([A]), [ALL]: problem(403, 'instance_admin_required') });
    await screen.findByText('Claude Code');
    await userEvent.click(allButton());
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: en['accountApps.all.confirm'] }));
    expect(await screen.findByRole('heading', { level: 2, name: en['install.onlyAdmins.title'] })).toHaveFocus();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('signs out when the session ended during Disconnect all', async () => {
    renderAt({ [LIST]: page([A]), [ALL]: problem(401, 'unauthenticated') });
    await screen.findByText('Claude Code');
    await userEvent.click(allButton());
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: en['accountApps.all.confirm'] }));
    expect(await screen.findByLabelText('Password')).toBeVisible();
  });

  it('keeps every row when Disconnect all is refused, and returns focus to it on Cancel', async () => {
    let answer = problem(503, 'unavailable');
    renderAt({ [LIST]: page([A, B]), [ALL]: () => answer });
    await screen.findByText('Claude Code');
    await userEvent.click(allButton());
    const dialog = screen.getByRole('dialog', { name: en['accountApps.all.title'] });
    await userEvent.click(within(dialog).getByRole('button', { name: en['accountApps.all.confirm'] }));
    expect(await within(dialog).findByText(en['connectedApps.error.action'])).toBeVisible();
    answer = problem(429, 'rate_limited');
    await userEvent.click(within(dialog).getByRole('button', { name: en['accountApps.all.confirm'] }));
    expect(await within(dialog).findByText(en['connectedApps.error.rateLimited'])).toBeVisible();
    expect(rows()).toHaveLength(2);

    await userEvent.click(within(dialog).getByRole('button', { name: en['admin.action.cancel'] }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(allButton()).toHaveFocus();
  });

  it('says the account is gone when Disconnect all finds it deleted', async () => {
    renderAt({ [LIST]: page([A]), [ALL]: problem(404, 'not_found') });
    await screen.findByText('Claude Code');
    await userEvent.click(allButton());
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: en['accountApps.all.confirm'] }));
    expect(await screen.findByRole('heading', { level: 2, name: en['accountAiApps.gone.title'] })).toHaveFocus();
  });

  it('gives the installation refusal when the list is refused for the grant', async () => {
    renderAt({ [LIST]: problem(403, 'instance_admin_required') });
    const heading = await screen.findByRole('heading', { level: 2, name: en['install.onlyAdmins.title'] });
    expect(heading).toBeVisible();
    await act(async () => {});
    expect(screen.queryByRole('radio')).not.toBeInTheDocument();
    // A refusal on arrival leaves focus where the page put it.
    expect(heading).not.toHaveFocus();
  });

  it('reads in Czech', async () => {
    vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['cs-CZ']);
    renderAt({ [LIST]: page([B]) });
    expect(await screen.findByText(cs['accountApps.access.read'])).toBeVisible();
    expect(screen.getByRole('button', { name: cs['accountApps.all.open'] })).toBeVisible();
    expect(screen.getByRole('button', { name: 'Odpojit aplikaci Notes helper' })).toBeVisible();
  });
});
