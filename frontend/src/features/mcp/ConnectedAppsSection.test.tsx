import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { I18nProvider } from '../../i18n';
import { en } from '../../i18n/en';
import { cs } from '../../i18n/cs';
import { installFakeServer, json } from '../../test/fakeServer';
import type { McpGrant } from './mcpGrantsApi';
import { ConnectedAppsSection } from './ConnectedAppsSection';

const LIST = 'GET /api/v1/mcp/grants';
const revokePath = (id: string) => `DELETE /api/v1/mcp/grants/${id}`;
const problem = (status: number, code: string) => json(status, { type: 'about:blank', title: 'x', status, code });
const grant = (id: string, clientName: string, scopes: McpGrant['scopes'] = ['tendo:read']): McpGrant => ({
  id, clientName, clientNameVerified: false, scopes, createdAt: '2026-11-20T10:00:00Z', expiresAt: '2027-02-18T10:00:00Z',
});
const A = grant('0198a2f0-0000-7000-8000-00000000000a', 'Claude Code', ['tendo:read', 'tendo:write']);
const B = grant('0198a2f0-0000-7000-8000-00000000000b', 'Notes helper');
const C = grant('0198a2f0-0000-7000-8000-00000000000c', 'Mail bot');
const page = (items: McpGrant[], nextCursor: string | null = null) => json(200, { items, nextCursor });

let pageHeading: HTMLHeadingElement | null = null;
const onSignedOut = vi.fn();

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
  onSignedOut.mockClear();
  pageHeading = null;
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

/** The section beside a page heading, the way AccountScreen mounts it: the heading is the last-resort focus target. */
function renders() {
  const pageRef = { current: null as HTMLHeadingElement | null };
  const view = render(<I18nProvider>
    <h1 ref={(node) => { pageRef.current = node; pageHeading = node; }} tabIndex={-1}>Your account</h1>
    <ConnectedAppsSection onSignedOut={onSignedOut} pageHeadingRef={pageRef} />
  </I18nProvider>);
  return view;
}
const rows = () => [...document.querySelectorAll<HTMLElement>('li[data-grant-id]')];
const rowNamed = (name: string) => rows().find((row) => row.querySelector('p')?.textContent === name) as HTMLElement;
const disconnectFor = (name: string) => screen.getByRole('button', { name: `Disconnect ${name}` });

describe('ConnectedAppsSection visibility', () => {
  it('renders nothing visible for an account with no connected apps, and no heading', async () => {
    const fake = installFakeServer({ [LIST]: page([]) });
    const { container } = renders();
    // The empty answer has no visible trace, so wait for the read itself and let its state update settle.
    await waitFor(() => expect(fake.requests).toHaveLength(1));
    await act(async () => {});
    expect(screen.queryByRole('heading', { name: en['connectedApps.title'] })).toBeNull();
    expect(container.querySelector('section')?.className).toContain('hidden');
    expect(container.querySelector('ul')).toBeNull();
  });

  it('shows the heading with one row per app, the unverified marker, dates and access in words', async () => {
    installFakeServer({ [LIST]: page([A, B]) });
    renders();
    expect(await screen.findByRole('heading', { level: 2, name: en['connectedApps.title'] })).toBeVisible();
    expect(screen.getAllByRole('listitem')).toHaveLength(2);
    expect(screen.getAllByText(en['connectedApps.unverified'])).toHaveLength(2);
    const first = rowNamed('Claude Code');
    expect(first).toHaveTextContent(en['connectedApps.access.write']);
    expect(first).not.toHaveTextContent('tendo:');
    expect(rowNamed('Notes helper')).toHaveTextContent(en['connectedApps.access.read']);
    expect(first).toHaveTextContent(/Connected .*2026/);
    expect(first).toHaveTextContent(/Access ends by .*2027/);
    expect(rowNamed('Claude Code').querySelector('p[dir="auto"]')?.textContent).toBe('Claude Code');
  });

  it('shows the error and a retry when the first load fails, then recovers on Try again', async () => {
    let fail = true;
    installFakeServer({ [LIST]: () => (fail ? problem(503, 'unavailable') : page([A])) });
    renders();
    expect(await screen.findByRole('heading', { level: 2, name: en['connectedApps.title'] })).toBeVisible();
    expect(screen.getByRole('alert')).toHaveTextContent(en['connectedApps.error.load']);
    fail = false;
    await userEvent.click(screen.getByRole('button', { name: en['connectedApps.retry'] }));
    expect(await screen.findByText('Claude Code')).toBeVisible();
  });

  it('shows the error for a network failure too', async () => {
    installFakeServer({ [LIST]: () => Promise.reject(new Error('offline')) });
    renders();
    expect(await screen.findByRole('alert')).toHaveTextContent(en['connectedApps.error.load']);
  });

  it('sends a 401 on the first load to the signed-out flow', async () => {
    installFakeServer({ [LIST]: problem(401, 'unauthenticated') });
    renders();
    await waitFor(() => expect(onSignedOut).toHaveBeenCalledTimes(1));
  });
});

describe('ConnectedAppsSection disconnect', () => {
  it('asks first, removes the row on 204, announces it and closes the dialog', async () => {
    let listed = [A, B];
    installFakeServer({
      [LIST]: () => page(listed),
      [revokePath(A.id)]: () => { listed = [B]; return json(204); },
    });
    renders();
    await userEvent.click(await screen.findByRole('button', { name: 'Disconnect Claude Code' }));
    const dialog = await screen.findByRole('dialog');
    expect(dialog).toHaveTextContent('Disconnect Claude Code?');
    expect(dialog).toHaveTextContent(en['connectedApps.dialog.body'].replace('{client}', 'Claude Code'));
    await userEvent.click(within(dialog).getByRole('button', { name: 'Disconnect' }));
    expect(await screen.findByText('Claude Code was disconnected.')).toBeVisible();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await waitFor(() => expect(rows()).toHaveLength(1));
  });

  it('moves focus to the next row after a removal', async () => {
    let listed = [A, B, C];
    installFakeServer({
      [LIST]: () => page(listed),
      [revokePath(B.id)]: () => { listed = [A, C]; return json(204); },
    });
    renders();
    await userEvent.click(await screen.findByRole('button', { name: 'Disconnect Notes helper' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Disconnect' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await waitFor(() => expect(disconnectFor('Mail bot')).toHaveFocus());
  });

  it('moves focus to the previous row when the last row is removed', async () => {
    let listed = [A, B];
    installFakeServer({
      [LIST]: () => page(listed),
      [revokePath(B.id)]: () => { listed = [A]; return json(204); },
    });
    renders();
    await userEvent.click(await screen.findByRole('button', { name: 'Disconnect Notes helper' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Disconnect' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await waitFor(() => expect(disconnectFor('Claude Code')).toHaveFocus());
  });

  it('keeps the notice mounted and falls back to the Account heading when the last app is removed', async () => {
    const listed = [A];
    installFakeServer({
      [LIST]: () => page(listed),
      [revokePath(A.id)]: () => { listed.length = 0; return json(204); },
    });
    renders();
    await userEvent.click(await screen.findByRole('button', { name: 'Disconnect Claude Code' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Disconnect' }));
    expect(await screen.findByText('Claude Code was disconnected.')).toBeVisible();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    // The section heading leaves with the rows, so focus lands on the Account page heading instead.
    expect(screen.queryByRole('heading', { level: 2, name: en['connectedApps.title'] })).toBeNull();
    await waitFor(() => expect(pageHeading).toHaveFocus());
  });

  it('announces the last removal politely, then hides the empty section once the notice has had time to be read', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    try {
      const listed = [A];
      installFakeServer({
        [LIST]: () => page(listed),
        [revokePath(A.id)]: () => { listed.length = 0; return json(204); },
      });
      const { container } = renders();
      await userEvent.click(await screen.findByRole('button', { name: 'Disconnect Claude Code' }));
      await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Disconnect' }));
      const announcement = await screen.findByText('Claude Code was disconnected.');
      // The live region is the element a screen reader hears: polite, atomic, and still mounted with the message.
      const region = announcement.closest('[role="status"]');
      expect(region, 'announcement sits in a polite live region').not.toBeNull();
      expect(region?.getAttribute('aria-atomic')).toBe('true');
      const section = container.querySelector('section');
      expect(section?.classList.contains('hidden')).toBe(false);

      await act(async () => { await vi.advanceTimersByTimeAsync(8000); });
      await waitFor(() => expect(screen.queryByText('Claude Code was disconnected.')).toBeNull());
      // The live region is still in the DOM (empty), so the next announcement has somewhere to go.
      expect(container.querySelector('[role="status"]')).not.toBeNull();
      expect(section?.classList.contains('hidden')).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it('clears an earlier outcome notice when the next disconnect is requested', async () => {
    let listed = [A, B];
    installFakeServer({
      [LIST]: () => page(listed),
      [revokePath(B.id)]: () => { listed = [A]; return json(204); },
    });
    renders();
    await userEvent.click(await screen.findByRole('button', { name: 'Disconnect Notes helper' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Disconnect' }));
    expect(await screen.findByText('Notes helper was disconnected.')).toBeVisible();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());

    // A second, unrelated request: the old sentence must not sit beside its dialog.
    await userEvent.click(screen.getByRole('button', { name: 'Disconnect Claude Code' }));
    expect(screen.queryByText('Notes helper was disconnected.')).toBeNull();
    const dialog = await screen.findByRole('dialog');
    expect(dialog).toHaveTextContent('Disconnect Claude Code?');
    // Cancel keeps the list as it was and leaves no notice behind either.
    await userEvent.click(within(dialog).getByRole('button', { name: en['admin.action.cancel'] }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(screen.queryByText('Notes helper was disconnected.')).toBeNull();
    expect(rows()).toHaveLength(1);
  });

  it('reloads quietly on 404 and shows the neutral already-disconnected notice', async () => {
    let listed = [A, B];
    const fake = installFakeServer({
      [LIST]: () => page(listed),
      [revokePath(A.id)]: problem(404, 'mcp_grant_not_found'),
    });
    renders();
    await userEvent.click(await screen.findByRole('button', { name: 'Disconnect Claude Code' }));
    listed = [B];
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Disconnect' }));
    expect(await screen.findByText('Claude Code was already disconnected. The list has been refreshed.')).toBeVisible();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await waitFor(() => expect(rows()).toHaveLength(1));
    expect(fake.requests.filter((request) => request.method === 'GET')).toHaveLength(2);
  });

  it('shows the rate-limit message in the dialog and keeps it usable', async () => {
    installFakeServer({ [LIST]: page([A]), [revokePath(A.id)]: problem(429, 'rate_limited') });
    renders();
    await userEvent.click(await screen.findByRole('button', { name: 'Disconnect Claude Code' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Disconnect' }));
    expect(await within(dialog).findByText(en['connectedApps.error.rateLimited'])).toBeVisible();
    expect(within(dialog).getByRole('button', { name: 'Disconnect' })).toBeEnabled();
    expect(rows()).toHaveLength(1);
  });

  it('shows the actionable unavailable message on 503 and a network failure', async () => {
    installFakeServer({ [LIST]: page([A]), [revokePath(A.id)]: problem(503, 'unavailable') });
    renders();
    await userEvent.click(await screen.findByRole('button', { name: 'Disconnect Claude Code' }));
    const dialog = await screen.findByRole('dialog');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Disconnect' }));
    expect(await within(dialog).findByText(en['connectedApps.error.action'])).toBeVisible();
    expect(rows()).toHaveLength(1);
  });

  it('keeps the trigger enabled while a quiet reload is held, and returns focus to it', async () => {
    let releaseReload!: (response: Response) => void;
    let reads = 0;
    const listed = [A, B];
    installFakeServer({
      [LIST]: () => {
        reads += 1;
        if (reads === 1) return page(listed);
        // Held: the quiet reload that the 404 starts stays in flight.
        return new Promise<Response>((resolve) => { releaseReload = resolve; });
      },
      [revokePath(A.id)]: problem(404, 'mcp_grant_not_found'),
    });
    renders();
    const trigger = await screen.findByRole('button', { name: 'Disconnect Claude Code' });
    await userEvent.click(trigger);
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Disconnect' }));
    expect(await screen.findByText('Claude Code was already disconnected. The list has been refreshed.')).toBeVisible();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    // The quiet reload is still held: the remaining row's trigger is not disabled by it (L020).
    expect(disconnectFor('Notes helper')).toBeEnabled();
    releaseReload(page([B]));
    await waitFor(() => expect(rows()).toHaveLength(1));
  });

  it('signs out on 401 from a disconnect', async () => {
    installFakeServer({ [LIST]: page([A]), [revokePath(A.id)]: problem(401, 'unauthenticated') });
    renders();
    await userEvent.click(await screen.findByRole('button', { name: 'Disconnect Claude Code' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Disconnect' }));
    await waitFor(() => expect(onSignedOut).toHaveBeenCalledTimes(1));
  });
});

describe('ConnectedAppsSection long names', () => {
  // A 120-character name with no break opportunities widened the dialog and the success notice past a 320px viewport
  // (page scrollWidth 1219 vs 320) until the client name could wrap anywhere. jsdom has no layout, so this pins the rule.
  const LONG = `Connector${'X'.repeat(111)}`;

  it('lets a long unbroken client name wrap in the dialog body and the success notice', async () => {
    const longApp = grant('0198a2f0-0000-7000-8000-00000000000d', LONG);
    let listed = [longApp];
    installFakeServer({
      [LIST]: () => page(listed),
      [revokePath(longApp.id)]: () => { listed = []; return json(204); },
    });
    renders();
    await userEvent.click(await screen.findByRole('button', { name: `Disconnect ${LONG}` }));
    const dialog = await screen.findByRole('dialog');
    const body = within(dialog).getByText(en['connectedApps.dialog.body'].replace('{client}', LONG));
    expect(body.classList.contains('[overflow-wrap:anywhere]'), 'dialog body wraps the client name').toBe(true);
    await userEvent.click(within(dialog).getByRole('button', { name: 'Disconnect' }));
    const notice = await screen.findByText(`${LONG} was disconnected.`);
    expect(notice.parentElement?.classList.contains('[overflow-wrap:anywhere]'), 'success notice wraps the client name').toBe(true);
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
  });
});

describe('ConnectedAppsSection pagination', () => {
  it('appends the next page on Show more and hides it on the last page', async () => {
    installFakeServer({
      [LIST]: (req) => (req.query.get('cursor') === 'c1' ? page([C]) : page([A, B], 'c1')),
    });
    renders();
    await userEvent.click(await screen.findByRole('button', { name: en['connectedApps.more'] }));
    expect(await screen.findByText('Mail bot')).toBeVisible();
    expect(rows()).toHaveLength(3);
    expect(screen.queryByRole('button', { name: en['connectedApps.more'] })).toBeNull();
  });

  it('shows an inline error with Try again when a later page fails', async () => {
    let fail = true;
    installFakeServer({
      [LIST]: (req) => {
        if (req.query.get('cursor') !== 'c1') return page([A], 'c1');
        return fail ? problem(503, 'unavailable') : page([B]);
      },
    });
    renders();
    await userEvent.click(await screen.findByRole('button', { name: en['connectedApps.more'] }));
    expect(await screen.findByText(en['connectedApps.error.more'])).toBeVisible();
    fail = false;
    await userEvent.click(screen.getByRole('button', { name: en['connectedApps.retry'] }));
    expect(await screen.findByText('Notes helper')).toBeVisible();
  });
});

describe('ConnectedAppsSection Czech', () => {
  it('renders the Czech heading, marker and disconnect name', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer({ [LIST]: page([A]) });
    renders();
    expect(await screen.findByRole('heading', { level: 2, name: cs['connectedApps.title'] })).toBeVisible();
    expect(screen.getByText(cs['connectedApps.unverified'])).toBeVisible();
    expect(screen.getByRole('button', { name: cs['connectedApps.disconnectLabel'].replace('{client}', 'Claude Code') })).toBeVisible();
  });
});
