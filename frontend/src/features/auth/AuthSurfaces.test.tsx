import { cleanup, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { components } from '../../../../api/generated/api';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { itemRoutes } from '../../test/fakeItems';
import { emptyHome, HOUSEHOLD_ID, household, installFakeServer, json, session } from '../../test/fakeServer';

/**
 * V5-07 slice 5: one calm Account page and one sign-in family. Account is a stack of flat sections separated by a
 * top rule instead of bordered cards, and every signed-out route opens with the same page-title scale as the
 * signed-in screens. The signed-out language switch sits under the page instead of floating at the viewport edge.
 * These tests fail if the old card-in-card composition or the oversized display title comes back.
 */

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const inviteToken = 'a'.repeat(43);
const resetToken = 'B'.repeat(43);
const page = (items: unknown[]) => json(200, { items, nextCursor: null });

const member = { userId: session().userId, login: 'anna', role: 'admin' as const };
const account: components['schemas']['Account'] = {
  userId: session().userId, login: 'anna', role: 'admin', signInMethods: { password: true, oidc: false }, passwordReset: null,
};

function signedInRoutes(extra: Record<string, unknown> = {}) {
  return {
    'GET /api/v1/session': json(200, session()),
    [`GET ${base}`]: json(200, household),
    ...emptyHome,
    ...itemRoutes(base, []),
    [`GET ${base}/members`]: page([member]),
    [`GET ${base}/invitations`]: page([]),
    [`GET ${base}/accounts`]: page([account]),
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

/** The signed-in page-title scale: every signed-out h1 must use exactly this set. */
function expectTitleScale(heading: HTMLElement) {
  expect(heading.classList.contains('text-h1'), 'page title uses text-h1').toBe(true);
  expect(heading.classList.contains('font-semibold'), 'page title uses font-semibold').toBe(true);
  expect(heading.className, 'no oversized display title').not.toContain('text-[2.125rem]');
  expect(heading.className, 'no oversized display title').not.toContain('sm:text-display');
}

/**
 * No tinted sunken band sits inside the one form surface on the sign-in and setup screens: no resting sand or sunken
 * fill at any opacity (hover states are not bands).
 */
function expectNoSunkenBand() {
  const bands = [...screen.getByRole('main').querySelectorAll('*')]
    .filter((element) => [...element.classList].some((token) => /^bg-(sand|sunken)(\/|$)/.test(token)))
    .map((element) => element.textContent?.trim().slice(0, 40));
  expect(bands, 'tinted bands inside the form surface').toEqual([]);
}

/** The signed-out footer follows the page content, keeps exactly the two language buttons, and stays last. */
function expectFooterFollowsContent() {
  const main = screen.getByRole('main');
  expect(main.classList.contains('flex-1'), 'centered main does not grow to the viewport bottom').toBe(false);
  const footer = screen.getByRole('contentinfo');
  expect(footer.parentElement?.lastElementChild, 'footer is the last element in the shell').toBe(footer);
  expect(main.compareDocumentPosition(footer) & Node.DOCUMENT_POSITION_FOLLOWING, 'footer follows main').toBeTruthy();
  expect(within(footer).getAllByRole('button')).toHaveLength(2);
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

describe('account page is a stack of flat sections', () => {
  it('separates identity, admin and preferences with a top rule and h2 titles', async () => {
    installFakeServer(signedInRoutes());
    open('/account');
    await screen.findByRole('heading', { level: 1, name: 'Your account' });
    // The admin section renders once the household role is known; it is not part of the static account data.
    await screen.findByRole('heading', { level: 2, name: 'Admin center' });
    const main = screen.getByRole('main');
    for (const id of ['account-identity-heading', 'account-admin-heading', 'account-appearance-heading', 'account-language-heading']) {
      const section = main.querySelector(`section[aria-labelledby="${id}"]`);
      if (!section) throw new Error(`missing Account section ${id}`);
      expect(section.classList.contains('rounded-2xl'), `${id} is not a rounded card`).toBe(false);
      expect(section.classList.contains('bg-raised'), `${id} is not a raised card`).toBe(false);
      expect(section.classList.contains('border-t'), `${id} separated by a top rule`).toBe(true);
      expect(section.classList.contains('border-line'), `${id} uses the shared rule colour`).toBe(true);
      const title = section.querySelector(`h2#${id}`);
      if (!title) throw new Error(`missing Account section title ${id}`);
      expect(title.classList.contains('text-h2'), `${id} title uses the Members title scale`).toBe(true);
      expect(title.classList.contains('text-lg'), `${id} title is not the old card title`).toBe(false);
      expect(title.classList.contains('font-bold'), `${id} title is not the old card weight`).toBe(false);
    }
  });

  it('states the signed-in login as text without an avatar tile in main', async () => {
    installFakeServer(signedInRoutes());
    open('/account');
    const title = await screen.findByRole('heading', { level: 2, name: 'Your sign-in' });
    const section = title.closest('section');
    if (!section) throw new Error('identity section missing');
    expect(section).toHaveTextContent('You’re signed in as anna.');
    expect(section.querySelector('[aria-hidden="true"]'), 'no decorative avatar tile').toBeNull();
    expect(section.querySelector('.rounded-full, .rounded-xl'), 'no initial tile beside the login').toBeNull();
  });
});

describe('signed-out page titles share the signed-in scale', () => {
  it.each([
    ['sign-in', '/login', 'Welcome back', {}],
    ['sign-in with a provider', '/login', 'Welcome back', { 'GET /api/v1/auth/oidc': json(200, { enabled: true, displayName: 'Pocket ID' }) }],
    ['reset form', `/reset-password#${resetToken}`, 'Choose a new password', {}],
    ['expired reset link', '/reset-password', 'This reset link can’t be used', {}],
    ['invitation', `/invite#${inviteToken}`, 'You’ve been invited to Tendo', { 'POST /api/v1/auth/invitations/accept': json(201, {}) }],
    ['incomplete invitation', '/invite', 'This invite link isn’t complete', {}],
    ['first-run setup', '/', 'Welcome to Tendo', { 'GET /api/v1/auth/setup': json(200, { required: true }) }],
    ['provider continuation', '/login#oidc=continue', 'Finish signing in with Pocket ID', {
      'GET /api/v1/auth/oidc': json(200, { enabled: true, displayName: 'Pocket ID' }),
      'GET /api/v1/auth/oidc/pending': json(200, { expiresAt: '2026-11-10T12:30:00Z', emailVerified: false, invitation: false }),
    }],
    ['unknown address', '/somewhere-else', 'We couldn’t find that page.', {}],
  ] as const)('%s opens with the page-title scale', async (_screen, path, title, extra) => {
    installFakeServer({ ...signedOutBoot, ...extra });
    open(path);
    expectTitleScale(await screen.findByRole('heading', { level: 1, name: title }));
  });

  it('an unreachable Tendo opens with the page-title scale', async () => {
    installFakeServer({ 'GET /api/v1/session': json(503) });
    open('/');
    expectTitleScale(await screen.findByRole('heading', { level: 1, name: 'Tendo can’t be reached right now.' }));
  });
});

describe('sign-in family keeps one plain form surface', () => {
  it('sign-in with a provider keeps the provider option inside the one surface, after a plain divider', async () => {
    installFakeServer({ ...signedOutBoot, 'GET /api/v1/auth/oidc': json(200, { enabled: true, displayName: 'Pocket ID' }) });
    open('/login');
    await screen.findByRole('heading', { level: 1, name: 'Welcome back' });
    // The provider option appears once the provider status has loaded, after the page title.
    const provider = await screen.findByRole('button', { name: 'Sign in with Pocket ID' });
    const surfaces = [...screen.getByRole('main').querySelectorAll('div')]
      .filter((element) => element.classList.contains('rounded-2xl') && element.classList.contains('border-line'));
    expect(surfaces, 'exactly one bordered form surface').toHaveLength(1);
    expect(surfaces[0]).toContainElement(provider);
    expectNoSunkenBand();
  });

  it('first-run setup keeps the submit row inside the one surface without a tinted band', async () => {
    installFakeServer({ ...signedOutBoot, 'GET /api/v1/auth/setup': json(200, { required: true }) });
    open('/');
    await screen.findByRole('heading', { level: 1, name: 'Welcome to Tendo' });
    expectNoSunkenBand();
    expect(screen.getByRole('button', { name: 'Create household' }).closest('div.rounded-2xl')).not.toBeNull();
  });
});

describe('signed-out language switch follows the page', () => {
  it('the sign-in page keeps the footer under the content, last in the shell, with two language buttons', async () => {
    installFakeServer(signedOutBoot);
    open('/login');
    await screen.findByRole('heading', { level: 1, name: 'Welcome back' });
    expectFooterFollowsContent();
  });

  it('first-run setup is a centered column whose footer follows the content', async () => {
    installFakeServer({ ...signedOutBoot, 'GET /api/v1/auth/setup': json(200, { required: true }) });
    open('/');
    await screen.findByRole('heading', { level: 1, name: 'Welcome to Tendo' });
    expectFooterFollowsContent();
    const section = screen.getByRole('heading', { level: 1 }).closest('section');
    if (!section) throw new Error('setup screen section missing');
    expect(section.classList.contains('mx-auto'), 'the setup column is centered like the rest of the family').toBe(true);
  });
});

describe('status screens place the footer next to the state', () => {
  /** Signed out, main follows its content; a 55vh centered column would push the footer into the middle of the page. */
  function expectTopAlignedState(title: string) {
    const section = screen.getByRole('heading', { level: 1, name: title }).closest('section');
    if (!section) throw new Error('status section missing');
    expect(section.className, 'no viewport-height centering').not.toContain('min-h-[55vh]');
    expect(section.classList.contains('justify-center'), 'not vertically centered').toBe(false);
    expect(section.classList.contains('mx-auto'), 'still the centered column').toBe(true);
    expectFooterFollowsContent();
  }

  it('a signed-out unknown address opens at the top with the footer right after it', async () => {
    installFakeServer(signedOutBoot);
    open('/somewhere-else');
    await screen.findByRole('heading', { level: 1, name: 'We couldn’t find that page.' });
    expectTopAlignedState('We couldn’t find that page.');
  });

  it('an unreachable Tendo opens at the top with the footer right after it', async () => {
    installFakeServer({ 'GET /api/v1/session': json(503) });
    open('/');
    await screen.findByRole('heading', { level: 1, name: 'Tendo can’t be reached right now.' });
    expectTopAlignedState('Tendo can’t be reached right now.');
  });

  it('a signed-in unknown address still sits in the middle of the growing main', async () => {
    installFakeServer(signedInRoutes());
    open('/somewhere-else');
    const section = (await screen.findByRole('heading', { level: 1, name: 'We couldn’t find that page.' })).closest('section');
    if (!section) throw new Error('status section missing');
    expect(section.className).toContain('min-h-[55vh]');
    expect(screen.getByRole('main').classList.contains('flex-1'), 'signed-in main still fills the shell').toBe(true);
  });
});
