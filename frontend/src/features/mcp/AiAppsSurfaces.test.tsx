import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { components } from '../../../../api/generated/api';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { en } from '../../i18n/en';
import { HOUSEHOLD_ID, household, instanceAdminSession, installFakeServer, json } from '../../test/fakeServer';
import { itemRoutes } from '../../test/fakeItems';
import { ConnectedAppsSection } from './ConnectedAppsSection';
import type { McpGrant } from './mcpGrantsApi';
import { OwnAiAppsSection } from './OwnAiAppsSection';

/**
 * V5-07 slice: the AI apps views follow the calm sections the rest of Account and the Admin center already use
 * (UX.md "Shell and navigation": the same professional language on every route, MCP settings included). Sections
 * keep one measure so their rules end together, section titles share one heading scale, a list row keeps its facts
 * on one muted meta line, an empty list is a quiet muted line and a field refusal uses the shared field-error size.
 * Behavior stays covered by the per-screen suites; these assertions lock the shared presentation.
 */

type Settings = components['schemas']['McpSettings'];
const BOTH: Settings['scopes'] = ['tendo:read', 'tendo:write'];
const settings: Settings = { enabled: true, scopes: BOTH, maximumScopes: BOTH, effectiveScopes: BOTH };
const problem = (status: number, code: string) => json(status, { type: 'about:blank', title: 'x', status, code });
const grant = (id: string, clientName: string): McpGrant => ({
  id, clientName, clientNameVerified: false, scopes: BOTH, createdAt: '2026-11-20T10:00:00Z', expiresAt: '2027-02-18T10:00:00Z',
});
const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const ACCOUNT = '0198a2f0-0000-7000-8000-0000000000b0';

function adminApp(routes: Parameters<typeof installFakeServer>[0], path: string) {
  installFakeServer({
    'GET /api/v1/session': json(200, instanceAdminSession()),
    [`GET ${base}`]: json(200, household),
    ...itemRoutes(base, []),
    ...routes,
  });
  window.history.replaceState(null, '', path);
  return render(<I18nProvider><App /></I18nProvider>);
}

const accountAiApps = (extra: Parameters<typeof installFakeServer>[0] = {}) => adminApp({
  [`GET /api/v1/admin/accounts/${ACCOUNT}/mcp-policy`]: json(200, {
    userId: ACCOUNT, enabled: null, maxScopes: null, userEnabled: true, userScopes: BOTH, effectiveScopes: BOTH,
  }, { ETag: '"1"' }),
  [`GET /api/v1/admin/accounts/${ACCOUNT}/mcp-grants`]: json(200, { items: [], nextCursor: null }),
  ...extra,
}, `/admin/accounts/${ACCOUNT}/ai-apps`);

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

describe('AI apps calm surfaces', () => {
  it('keeps the own AI apps form on the section measure, so its rules end where the section rule ends', async () => {
    installFakeServer({ 'GET /api/v1/mcp/settings': json(200, settings, { ETag: '"1"' }) });
    render(<I18nProvider><OwnAiAppsSection onSignedOut={() => undefined} /></I18nProvider>);
    const heading = await screen.findByRole('heading', { level: 2, name: en['ownAiApps.title'] });
    const section = heading.closest('section') as HTMLElement;
    expect(section).toHaveClass('max-w-3xl');
    const form = within(section).getByRole('form');
    expect(form.className).not.toMatch(/\bmax-w-/);
  });

  it('gives every section on one account’s AI apps the same heading scale', async () => {
    accountAiApps();
    const titles = [en['accountAiApps.now.title'], en['accountAiApps.own.title'], en['accountAiApps.settings.title'], en['connectedApps.title']];
    for (const name of titles) {
      const heading = await screen.findByRole('heading', { level: 2, name });
      expect(heading).toHaveClass('text-h2');
      expect(heading).not.toHaveClass('text-body');
    }
  });

  it('keeps when an app was connected and when its access ends on one muted meta line', async () => {
    installFakeServer({ 'GET /api/v1/mcp/grants': json(200, { items: [grant('0198a2f0-0000-7000-8000-00000000000a', 'Claude Code')], nextCursor: null }) });
    const pageRef = { current: null };
    render(<I18nProvider><ConnectedAppsSection onSignedOut={() => undefined} pageHeadingRef={pageRef} /></I18nProvider>);
    await screen.findByRole('heading', { level: 2, name: en['connectedApps.title'] });
    const row = document.querySelector('li[data-grant-id]') as HTMLElement;
    const meta = row.querySelector('[data-grant-meta]') as HTMLElement;
    expect(meta).not.toBeNull();
    expect(meta).toHaveClass('text-meta', 'text-muted');
    expect(meta).toHaveTextContent(/^Connected .*2026\s*·\s*Access ends by .*2027$/);
    // The separator is visual only; the sentence is not read with a stray dot.
    expect(meta.querySelector('[aria-hidden="true"]')).toHaveTextContent('·');
    // Nothing else in the row repeats a date on a meta line of its own.
    expect(row.querySelectorAll('.text-meta')).toHaveLength(1);
  });

  it('fills the phone width with Show more on the own connected apps, like every other list', async () => {
    installFakeServer({
      'GET /api/v1/mcp/grants': json(200, { items: [grant('0198a2f0-0000-7000-8000-00000000000a', 'Claude Code')], nextCursor: 'next' }),
    });
    const pageRef = { current: null };
    render(<I18nProvider><ConnectedAppsSection onSignedOut={() => undefined} pageHeadingRef={pageRef} /></I18nProvider>);
    const more = await screen.findByRole('button', { name: en['connectedApps.more'] });
    expect(more).toHaveClass('w-full', 'sm:w-auto');
  });

  it('says an account without connected apps as a quiet muted line', async () => {
    accountAiApps();
    const empty = await screen.findByText(en['accountApps.empty']);
    expect(empty).toHaveClass('text-muted');
    expect(empty).not.toHaveClass('text-ink');
  });

  it('says an installation without AI apps changes as a quiet muted line', async () => {
    adminApp({
      'GET /api/v1/admin/mcp-policy': json(200, {
        enabled: true, operatorEnabled: true, defaultAccountEnabled: true, defaultMaxScopes: BOTH,
      }, { ETag: '"1"' }),
      'GET /api/v1/admin/mcp-policy-events': json(200, { items: [], nextCursor: null }),
    }, '/admin/ai-apps');
    const empty = await screen.findByText(en['aiApps.changes.empty']);
    expect(empty).toHaveClass('text-muted');
    expect(empty).not.toHaveClass('text-ink');
  });

  it('says a refused access choice at the shared field-error size', async () => {
    installFakeServer({
      'GET /api/v1/mcp/settings': json(200, settings, { ETag: '"1"' }),
      'PUT /api/v1/mcp/settings': problem(422, 'invalid_value'),
    });
    render(<I18nProvider><OwnAiAppsSection onSignedOut={() => undefined} /></I18nProvider>);
    await screen.findByRole('heading', { level: 2, name: en['ownAiApps.title'] });
    await userEvent.click(screen.getByRole('button', { name: en['aiApps.save'] }));
    const error = await screen.findByText(en['aiApps.access.missing']);
    expect(error).toHaveClass('text-small', 'text-danger');
    expect(error).not.toHaveClass('text-meta');
  });

  it('says a refused account maximum at the shared field-error size', async () => {
    accountAiApps({
      [`PUT /api/v1/admin/accounts/${ACCOUNT}/mcp-policy`]: json(422, {
        type: 'about:blank', title: 'x', status: 422, code: 'invalid_value', field: 'maxScopes',
      }),
    });
    await userEvent.click(await screen.findByRole('button', { name: en['aiApps.save'] }));
    const error = await screen.findByText(en['accountAiApps.max.refused']);
    expect(error).toHaveClass('text-small', 'text-danger');
    expect(error).not.toHaveClass('text-meta');
  });
});
