import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, installFakeServer, json, memberHousehold, session } from '../../test/fakeServer';
import type { components } from '../../../../api/generated/api';

type Account = components['schemas']['Account'];
const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const accountPath = (userId: string) => `GET ${base}/accounts/${userId}`;
const removalPath = (userId: string) => `DELETE ${base}/accounts/${userId}`;
const member: Account = {
  userId: session().userId, login: 'anna', role: 'member', signInMethods: { password: true, oidc: false }, passwordReset: null,
};

function app() {
  window.history.replaceState(null, '', '/account');
  return render(<I18nProvider><App /></I18nProvider>);
}
const main = () => screen.getByRole('main');
/** The membership section exists only once the household is known, so every assertion waits for it. */
async function householdSection(): Promise<HTMLElement> {
  await waitFor(() => expect(main().querySelector('section[aria-labelledby="account-household-heading"]')).not.toBeNull());
  return main().querySelector('section[aria-labelledby="account-household-heading"]') as HTMLElement;
}

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

describe('Account household section', () => {
  it('offers a plain member one quiet way to leave, and lands on Home with a notice', async () => {
    let current = session();
    const fake = installFakeServer({
      'GET /api/v1/session': () => json(200, current),
      // A plain member reads their own membership version from the household;
      // the account route is admin-only and must never be called.
      [`GET ${base}`]: json(200, { ...memberHousehold, viewerMembershipVersion: 2 }),
      [accountPath(member.userId)]: json(403, { type: 'about:blank', title: 'Forbidden', status: 403, code: 'admin_required' }),
      [removalPath(member.userId)]: () => { current = session(false); return json(204); },
    });
    app();
    expect(await screen.findByRole('heading', { level: 1, name: 'Your account' })).toBeVisible();

    // The membership is the last, quiet section of the stack, separated like every other one.
    const section = await householdSection();
    expect(section.classList.contains('border-t'), 'separated by the shared top rule').toBe(true);
    expect(section.parentElement?.lastElementChild).toBe(section);
    expect(within(section).getByRole('heading', { level: 2, name: 'Household' })).toBeVisible();
    expect(within(section).getByText('You’re a member of Veselí. If you leave, you lose access to everything in it.')).toBeVisible();

    await userEvent.click(within(section).getByRole('button', { name: 'Leave household' }));
    const dialog = await screen.findByRole('dialog');
    expect(dialog).toHaveTextContent('Leave Veselí?');
    expect(dialog).toHaveTextContent('You’ll lose access to this household.');
    expect(dialog).not.toHaveTextContent('They keep their other households.');
    await userEvent.click(within(dialog).getByRole('button', { name: 'Leave household' }));

    expect(await screen.findByText('You left Veselí. You’re not in a household now — join one with an invitation.')).toBeVisible();
    expect(window.location.pathname).toBe('/');
    expect(await screen.findByRole('heading', { level: 1, name: 'Join your household by invitation' })).toBeVisible();
    // The tag is the quoted viewerMembershipVersion, read again right before the DELETE.
    const removal = fake.requests.find((request) => request.method === 'DELETE');
    expect(removal?.headers.get('If-Match')).toBe('"2"');
    const reads = fake.requests.filter((request) => request.method === 'GET' && request.path === base);
    expect(reads.length).toBeGreaterThan(0);
    expect(fake.requests.indexOf(reads[reads.length - 1])).toBeLessThan(fake.requests.indexOf(removal!));
    expect(fake.requests.some((request) => request.method === 'GET' && request.path === accountPath(member.userId))).toBe(false);
  });

  it('asks nothing when the dialog is dismissed and keeps the membership', async () => {
    const fake = installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, memberHousehold),
    });
    app();
    await screen.findByRole('heading', { level: 1, name: 'Your account' });
    const section = await householdSection();

    await userEvent.click(within(section).getByRole('button', { name: 'Leave household' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Cancel' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(section).toBeVisible();
    expect(fake.requests.some((request) => request.method === 'DELETE')).toBe(false);
    expect(fake.requests.some((request) => request.method === 'GET' && request.path === accountPath(member.userId))).toBe(false);
  });

  it('explains that the membership changed elsewhere and stays on Account', async () => {
    const fake = installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, { ...memberHousehold, viewerMembershipVersion: 3 }),
      [accountPath(member.userId)]: json(403, { type: 'about:blank', title: 'Forbidden', status: 403, code: 'admin_required' }),
      [removalPath(member.userId)]: json(412, { type: 'about:blank', title: 'Precondition Failed', status: 412, code: 'precondition_failed' }),
    });
    app();
    await screen.findByRole('heading', { level: 1, name: 'Your account' });
    const section = await householdSection();
    await userEvent.click(within(section).getByRole('button', { name: 'Leave household' }));
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Leave household' }));

    expect(await screen.findByText('Your membership changed elsewhere. Try again.')).toBeVisible();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(window.location.pathname).toBe('/account');
    expect(within(section).getByRole('button', { name: 'Leave household' })).toBeEnabled();
    const removal = fake.requests.find((request) => request.method === 'DELETE');
    expect(removal?.headers.get('If-Match')).toBe('"3"');
    expect(fake.requests.some((request) => request.method === 'GET' && request.path === accountPath(member.userId))).toBe(false);
  });

  it('renders the Czech membership section', async () => {
    localStorage.setItem('tendo.locale', 'cs');
    installFakeServer({
      'GET /api/v1/session': json(200, session()),
      [`GET ${base}`]: json(200, memberHousehold),
    });
    app();
    expect(await screen.findByRole('heading', { level: 1, name: 'Váš účet' })).toBeVisible();
    const section = await householdSection();
    expect(within(section).getByRole('heading', { level: 2, name: 'Domácnost' })).toBeVisible();
    expect(within(section).getByText('Jste členem domácnosti Veselí. Pokud ji opustíte, ztratíte přístup ke všemu v ní.')).toBeVisible();
    expect(within(section).getByRole('button', { name: 'Opustit domácnost' })).toBeVisible();
  });
});
