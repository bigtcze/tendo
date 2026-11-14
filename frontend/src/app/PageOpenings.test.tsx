import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { components } from '../../../api/generated/api';
import type { Item, Subject } from '../features/items/itemsApi';
import { I18nProvider } from '../i18n';
import { itemRoutes } from '../test/fakeItems';
import { emptyHome, HOUSEHOLD_ID, household, installFakeServer, json, session } from '../test/fakeServer';
import { App } from './App';

/**
 * V5-07 slice 2: one calm page opening on every route. A screen is named by its h1 alone — no uppercase
 * micro-label above it, and no decorative sentence under it. These tests fail if either comes back.
 */

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const itemId = '0198a2f0-7c1e-7a53-9b0e-5d3f2c1a4b80';
const inviteToken = 'a'.repeat(43);
const resetToken = 'B'.repeat(43);
const page = (items: unknown[]) => json(200, { items, nextCursor: null });

const subject: Subject = { id: 's-1', name: 'Anna', type: 'person', linkedUserId: null, archived: false, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' };
const member = { userId: session().userId, login: 'anna', role: 'admin' as const };
const account: components['schemas']['Account'] = { userId: session().userId, login: 'anna', role: 'admin', signInMethods: { password: true, oidc: false }, passwordReset: null };
const detailItem: Item = {
  id: itemId, subjectId: subject.id, responsibleUserId: null, title: 'Renew passport', notes: null, attentionOn: null, recurrence: null,
  workflowState: 'open', attention: 'needs_attention', archived: false, done: false, lastCompletedOn: null,
  createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z',
};

/** The decoration this slice removed. Written out rather than read from the locale: the point is that it never returns. */
const removedDecoration = {
  allItems: 'Find and filter everything your household is looking after.',
  subjects: 'Who and what your household looks after.',
  members: 'Everyone who can use this household.',
  account: 'How you sign in, and how Tendo looks on this device.',
  admin: 'Who belongs to this household, and what each person can do.',
  login: 'Your household is waiting for you.',
  onboarding: 'A few details, then your household is ready.',
  invite: 'Create an account to join the household. You’ll be able to use the household with the person who invited you.',
} as const;

function signedInRoutes(extra: Record<string, unknown> = {}) {
  return {
    'GET /api/v1/session': json(200, session()),
    [`GET ${base}`]: json(200, household),
    ...emptyHome,
    ...itemRoutes(base, [detailItem]),
    [`GET ${base}/subjects`]: page([subject]),
    [`GET ${base}/subjects/${subject.id}`]: json(200, subject, { ETag: '"subject"' }),
    [`GET ${base}/members`]: page([member]),
    [`GET ${base}/invitations`]: page([]),
    [`GET ${base}/accounts`]: page([account]),
    [`GET ${base}/items/${itemId}`]: json(200, detailItem, { ETag: '"1"' }),
    [`GET ${base}/items/${itemId}/completions`]: page([]),
    ...extra,
  };
}

const signedOutBoot = {
  'GET /api/v1/session': json(401, { type: 'about:blank', title: 'Unauthorized', status: 401 }),
  'GET /api/v1/auth/setup': json(200, { required: false }),
  'GET /api/v1/auth/oidc': json(200, { enabled: false }),
};

function open(path: string) {
  window.history.replaceState(null, '', path);
  return render(<I18nProvider><App /></I18nProvider>);
}

/** Every violation in one list, so a failure names both the removed label and the removed sentence. */
function expectCalmOpening(heading: HTMLElement, decoration?: string | null) {
  const found = [...screen.getByRole('main').querySelectorAll('*')]
    .filter((element) => element.classList.contains('uppercase')
      && (element.compareDocumentPosition(heading) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0)
    .map((element) => element.textContent);
  if (decoration && screen.queryByText(decoration)) found.push(decoration);
  expect(found, 'uppercase label or decorative sentence on the page opening').toEqual([]);
}

describe('page-opening check', () => {
  it('fails when an uppercase label precedes the heading or a listed sentence is shown', () => {
    render(<main><p className="uppercase">Your household</p><h1>Title</h1><p>Slogan.</p></main>);
    const heading = screen.getByRole('heading', { level: 1 });
    expect(() => expectCalmOpening(heading)).toThrow();
    cleanup();
    render(<main><h1>Title</h1><p>Slogan.</p></main>);
    expect(() => expectCalmOpening(screen.getByRole('heading', { level: 1 }), 'Slogan.')).toThrow();
    expect(() => expectCalmOpening(screen.getByRole('heading', { level: 1 }))).not.toThrow();
  });
});

function expectGone(snippet: string) {
  expect(screen.queryByText(snippet), `page-opening decoration: ${snippet}`).toBeNull();
}

beforeEach(() => {
  window.history.replaceState(null, '', '/');
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
  window.history.replaceState(null, '', '/');
});

describe('signed-in page openings', () => {
  it.each([
    ['Home', '/', 'Overview', null],
    ['All items', '/items', 'All items', removedDecoration.allItems],
    ['People and things', '/people', 'People and things', removedDecoration.subjects],
    ['Members', '/members', 'Household members', removedDecoration.members],
    ['Account', '/account', 'Your account', removedDecoration.account],
    ['Admin center', '/admin', 'Admin center', removedDecoration.admin],
  ])('%s opens on its title alone', async (_screen, path, title, decoration) => {
    installFakeServer(signedInRoutes());
    open(path);
    expectCalmOpening(await screen.findByRole('heading', { level: 1, name: title }), decoration);
  });

  it('opens on its title alone without a household as well', async () => {
    installFakeServer({ 'GET /api/v1/session': json(200, session(false)) });
    open('/');
    const heading = await screen.findByRole('heading', { level: 1, name: 'Join your household by invitation' });
    expectCalmOpening(heading);
    // Kept: it is the instruction that explains how a person joins.
    expect(screen.getByText('An administrator of your household sends you a private invitation link. Open that link while you’re signed in and you’ll join the household.')).toBeVisible();
  });
});

describe('item detail page opening', () => {
  it('keeps the subject as a calm contextual line, not an uppercase label', async () => {
    installFakeServer(signedInRoutes());
    open(`/items/${itemId}`);
    const heading = await screen.findByRole('heading', { level: 1, name: 'Renew passport' });
    const header = heading.closest('header');
    if (!header) throw new Error('item detail header missing');
    const subjectLine = header.querySelector('p');
    if (!subjectLine) throw new Error('item subject line missing');
    expect(subjectLine).toHaveTextContent('Anna');
    expect(subjectLine).toHaveClass('text-small', 'text-muted', '[overflow-wrap:anywhere]');
    expect(subjectLine).not.toHaveClass('uppercase');
    expect(subjectLine).not.toHaveClass('tracking-[0.12em]');
    expectCalmOpening(heading);
  });

  it('a failed item load is its title alone, with no label above it', async () => {
    installFakeServer(signedInRoutes({ [`GET ${base}/items/${itemId}`]: json(503) }));
    open(`/items/${itemId}`);
    const heading = await screen.findByRole('heading', { level: 1, name: 'Tendo can’t be reached right now.' });
    expectCalmOpening(heading);
  });

  it('a loading item says only that it is loading, with no label above it', async () => {
    const pending = new Promise<Response>(() => undefined);
    installFakeServer(signedInRoutes({ [`GET ${base}/items/${itemId}`]: () => pending }));
    open(`/items/${itemId}`);
    expect(await screen.findByText('Loading items…')).toBeVisible();
    expectGone('One moment');
    expect(screen.getByRole('main').querySelector('.uppercase')).toBeNull();
  });
});

describe('signed-out page openings', () => {
  it('sign-in opens on its title alone', async () => {
    installFakeServer(signedOutBoot);
    open('/login');
    const heading = await screen.findByRole('heading', { level: 1, name: 'Welcome back' });
    expectCalmOpening(heading, removedDecoration.login);
  });

  it('first-run setup opens on its title alone', async () => {
    installFakeServer({ ...signedOutBoot, 'GET /api/v1/auth/setup': json(200, { required: true }) });
    open('/');
    const heading = await screen.findByRole('heading', { level: 1, name: 'Welcome to Tendo' });
    expectCalmOpening(heading, removedDecoration.onboarding);
    // UX.md "Shell and navigation": no decorative period on the headline.
    expect(heading.textContent).toBe('Welcome to Tendo');
  });

  it('an invitation opens on its title alone', async () => {
    installFakeServer({ ...signedOutBoot, 'POST /api/v1/auth/invitations/accept': json(201, {}) });
    open(`/invite#${inviteToken}`);
    const heading = await screen.findByRole('heading', { level: 1, name: 'You’ve been invited to Tendo' });
    expectCalmOpening(heading, removedDecoration.invite);
  });

  it('a signed-in invitation opens on its title alone and keeps the instruction that says what joining does', async () => {
    installFakeServer(signedInRoutes({ 'POST /api/v1/invitations/accept': json(201, {}) }));
    open(`/invite#${inviteToken}`);
    const heading = await screen.findByRole('heading', { level: 1, name: 'Signed in as anna' });
    expectCalmOpening(heading);
    // Kept: the title names only the account, so this sentence is the one thing saying this is an invitation.
    expect(screen.getByText('Join the household to share its people, things and plans. You’ll return to the household when you’re done.')).toBeVisible();
  });

  it('the sign-in decision after a provider sign-in opens on its title alone and keeps its explanation', async () => {
    installFakeServer({
      ...signedOutBoot,
      'GET /api/v1/auth/oidc': json(200, { enabled: true, displayName: 'Pocket ID' }),
      'GET /api/v1/auth/oidc/pending': json(200, { expiresAt: '2026-11-10T12:30:00Z', emailVerified: false, invitation: false }),
    });
    open('/login#oidc=continue');
    const heading = await screen.findByRole('heading', { level: 1, name: 'Finish signing in with Pocket ID' });
    expectCalmOpening(heading);
    // Kept: it explains why this screen exists and asks the person to choose.
    expect(screen.getByText('Tendo didn’t find a Tendo account connected to this sign-in yet. Choose how you’d like to continue.')).toBeVisible();
  });

  it('a reset link opens on its title alone and keeps the instruction that explains the link', async () => {
    installFakeServer(signedOutBoot);
    open(`/reset-password#${resetToken}`);
    const heading = await screen.findByRole('heading', { level: 1, name: 'Choose a new password' });
    expectCalmOpening(heading);
    expect(screen.getByText('A household administrator created this private link for you. Choose a password, then sign in with it.')).toBeVisible();
  });

  it('an expired reset link opens on its title alone and keeps the way to get a new one', async () => {
    installFakeServer(signedOutBoot);
    open('/reset-password');
    const heading = await screen.findByRole('heading', { level: 1, name: 'This reset link can’t be used' });
    expectCalmOpening(heading);
    expect(screen.getByText('This reset link is no longer valid. Ask a household administrator for a new one.')).toBeVisible();
  });

  it('an unknown address opens on its title alone', async () => {
    installFakeServer(signedOutBoot);
    open('/somewhere-else');
    const heading = await screen.findByRole('heading', { level: 1, name: 'We couldn’t find that page.' });
    expectCalmOpening(heading);
  });

  it('an unreachable Tendo opens on its title alone and keeps the retry', async () => {
    installFakeServer({ 'GET /api/v1/session': json(503) });
    open('/');
    const heading = await screen.findByRole('heading', { level: 1, name: 'Tendo can’t be reached right now.' });
    expectCalmOpening(heading);
    expect(screen.getByRole('button', { name: 'Try again' })).toBeVisible();
  });
});
