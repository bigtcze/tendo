import { act, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, household, installFakeServer, json, memberHousehold, session } from '../../test/fakeServer';
import { itemRoutes } from '../../test/fakeItems';
import type { components } from '../../../../api/generated/api';

/**
 * V5-07 slice: the Admin center follows the calm list Members already uses (UX.md, v5 audit §5 direction A).
 * Accounts are one flat divided list without a card frame, avatar tile or role pill; the loading, empty and
 * member-refusal states are plain sections rather than bordered cards. Behavior is covered in AdminScreen.test.tsx.
 */
type Account = components['schemas']['Account'];
const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const accountsPath = `GET ${base}/accounts`;
const owner: Account = { userId: session().userId, login: 'anna', role: 'admin', signInMethods: { password: true, oidc: false }, passwordReset: null };
const petr: Account = { userId: '0198a2f0-0000-7000-8000-000000000002', login: 'petr', role: 'member', signInMethods: { password: false, oidc: true }, passwordReset: null };
const jana: Account = { userId: '0198a2f0-0000-7000-8000-000000000003', login: 'jana', role: 'member', signInMethods: { password: true, oidc: true }, passwordReset: null };

const CARD = /rounded-2xl|bg-raised|bg-sand/;

function app(routes: Parameters<typeof installFakeServer>[0], path = '/admin') {
  installFakeServer({
    'GET /api/v1/session': json(200, session()),
    [`GET ${base}`]: json(200, household),
    ...itemRoutes(base, []),
    ...routes,
  });
  window.history.replaceState(null, '', path);
  return render(<I18nProvider><App /></I18nProvider>);
}

function rowOf(login: string) {
  const rows = screen.getByRole('main').querySelectorAll('li[data-account-id]');
  return [...rows].find((row) => row.querySelector('[data-account-login]')?.textContent === login) as HTMLElement;
}

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

describe('Admin center calm surfaces', () => {
  it('lists accounts as a flat divided list under a top rule, without a card frame', async () => {
    app({ [accountsPath]: json(200, { items: [owner, petr, jana], nextCursor: null }) });
    const list = await screen.findByRole('list', { name: 'Accounts' });
    expect(list).toHaveClass('divide-y', 'divide-line', 'border-t', 'border-line');
    expect(list.className).not.toMatch(CARD);
    expect(list).not.toHaveClass('border');
    expect(list.querySelectorAll('.rounded-2xl')).toHaveLength(0);
  });

  it('starts each row with the login, with no decorative initial tile', async () => {
    app({ [accountsPath]: json(200, { items: [owner, petr], nextCursor: null }) });
    await waitFor(() => expect(rowOf('petr')).toBeVisible());
    for (const login of ['anna', 'petr']) {
      const row = rowOf(login);
      // No decorative initial: nothing hidden from assistive technology holds just the login's first letter.
      const glyphs = [...row.querySelectorAll('[aria-hidden="true"]')].filter((node) => /^\s*\p{L}\s*$/u.test(node.textContent ?? ''));
      expect(glyphs).toHaveLength(0);
      // The first thing in the row is the identity column that holds the login, not a glyph.
      expect(row.firstElementChild?.querySelector('[data-account-login]')).not.toBeNull();
    }
  });

  it('states the role as plain muted text on the same line as the sign-in method', async () => {
    app({ [accountsPath]: json(200, { items: [owner, petr, jana], nextCursor: null }) });
    await waitFor(() => expect(rowOf('jana')).toBeVisible());
    const role = rowOf('anna').querySelector('[data-account-role]') as HTMLElement;
    expect(role).toHaveTextContent('Administrator');
    expect(role.className).not.toMatch(/rounded-full|bg-/);
    const meta = role.closest('[data-account-meta]') as HTMLElement;
    expect(meta).not.toBeNull();
    expect(meta).toHaveClass('text-meta', 'text-muted');
    expect(meta).toHaveTextContent(/^Administrator\s*·\s*Password$/);
    expect(rowOf('petr').querySelector('[data-account-meta]')).toHaveTextContent(/^Member\s*·\s*Single sign-on$/);
    expect(rowOf('jana').querySelector('[data-account-meta]')).toHaveTextContent(/^Member\s*·\s*Password and single sign-on$/);
    // The separator is decoration only: assistive technology hears the role and the method, not a dot.
    const separators = meta.querySelectorAll('[aria-hidden="true"]');
    expect(separators).toHaveLength(1);
    expect(separators[0]).toHaveTextContent('·');
  });

  it('keeps every row action at a 44 px target', async () => {
    app({ [accountsPath]: json(200, { items: [owner, jana], nextCursor: null }) });
    await waitFor(() => expect(rowOf('jana')).toBeVisible());
    const buttons = within(rowOf('jana')).getAllByRole('button');
    // A guard, not a failing-first case: the shared Button already enforces min-h-11; this pins that the row keeps it.
    expect(buttons.map((button) => button.getAttribute('aria-label'))).toEqual(expect.arrayContaining(['Reset password: jana', 'Make administrator: jana']));
    for (const button of buttons) expect(button.className).toMatch(/\bmin-h-11\b/);
  });

  it('says there are no accounts as plain muted text, not a card', async () => {
    app({ [accountsPath]: json(200, { items: [], nextCursor: null }) });
    const empty = await screen.findByText('No accounts to show.');
    expect(empty).toHaveClass('text-muted');
    expect(empty.className).not.toMatch(CARD);
    expect(empty).not.toHaveClass('border');
  });

  it('holds the list place while loading with flat skeleton rows, no card frame or avatar circles', async () => {
    let release!: (response: Response) => void;
    const pending = new Promise<Response>((resolve) => { release = resolve; });
    app({ [accountsPath]: () => pending });
    const label = await screen.findByText('Loading accounts…');
    const skeleton = label.closest('[role="status"]') as HTMLElement;
    expect(skeleton).not.toBeNull();
    expect(skeleton.className).not.toMatch(CARD);
    expect(skeleton).toHaveClass('border-t', 'border-line');
    expect(skeleton).not.toHaveClass('border');
    expect(skeleton.querySelectorAll('.rounded-full')).toHaveLength(0);
    await act(async () => { release(json(200, { items: [owner], nextCursor: null })); await pending; });
  });

  it('shows the member refusal as a plain section with a section-scale heading', async () => {
    app({
      [`GET ${base}`]: json(200, memberHousehold),
      [accountsPath]: json(403, { type: 'about:blank', title: 'Forbidden', status: 403, code: 'admin_required' }),
    });
    const heading = await screen.findByRole('heading', { level: 2, name: 'Only administrators can open the Admin center.' });
    expect(heading).toHaveClass('text-h2', 'text-ink');
    const section = screen.getByRole('region', { name: 'Only administrators can open the Admin center.' });
    expect(section.className).not.toMatch(CARD);
    expect(section).not.toHaveClass('border');
    expect(section.className).not.toMatch(/\bp-5\b|\bsm:p-6\b/);
    expect(within(section).getByText('Ask a household administrator if you need someone’s role changed.')).toHaveClass('text-muted');
  });
});
