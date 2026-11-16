import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { App } from '../../app/App';
import { I18nProvider } from '../../i18n';
import { HOUSEHOLD_ID, household, instanceAdminSession, installFakeServer, json, memberHousehold, session } from '../../test/fakeServer';
import { itemRoutes } from '../../test/fakeItems';
import type { components } from '../../../../api/generated/api';

type DirectoryAccount = components['schemas']['DirectoryAccount'];

const base = `/api/v1/households/${HOUSEHOLD_ID}`;
const directoryPath = 'GET /api/v1/admin/accounts';
const deletionPath = 'POST /api/v1/admin/account-deletions';
const problem = (status: number, code?: string) => json(status, { type: 'about:blank', title: 'x', status, ...(code ? { code } : {}) });
const mismatch = () => json(422, { type: 'about:blank', title: 'x', status: 422, code: 'mismatch', field: 'confirmLogin' });

const anna: DirectoryAccount = { userId: session().userId, login: 'anna' };
const petr: DirectoryAccount = { userId: '0198a2f0-0000-7000-8000-000000000002', login: 'petr' };
const jana: DirectoryAccount = { userId: '0198a2f0-0000-7000-8000-000000000003', login: 'jana' };

/** The account directory with the deletion contract layered over it by each test. */
function renderAt(routes: Parameters<typeof installFakeServer>[0], path = '/admin/accounts', viewer = instanceAdminSession()) {
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

const rows = () => [...screen.getByRole('main').querySelectorAll<HTMLElement>('li[data-install-directory-id]')];
const logins = () => [...screen.getByRole('main').querySelectorAll('[data-install-login]')].map((node) => node.textContent);
const posts = (fake: ReturnType<typeof installFakeServer>) =>
  fake.requests.filter((request) => request.method === 'POST' && request.path === '/api/v1/admin/account-deletions');
const directoryReads = (fake: ReturnType<typeof installFakeServer>) =>
  fake.requests.filter((request) => request.method === 'GET' && request.path === '/api/v1/admin/accounts').length;
const deleteTrigger = (login: string) => screen.getByRole('button', { name: `Delete account: ${login}` });

/** Opens the dialog through the row's own action, once the directory has brought that row. */
async function openFor(login: string) {
  const trigger = deleteTrigger(login);
  await userEvent.click(trigger);
  const dialog = await screen.findByRole('dialog');
  return { trigger, dialog, confirm: within(dialog).getByLabelText(`Type ${login} to confirm`) };
}
const submitButton = (dialog: HTMLElement) => within(dialog).getByRole('button', { name: 'Delete account' });

beforeEach(() => {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(['en-US']);
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  localStorage.removeItem('tendo.locale');
});

describe('deleting an installation account', () => {
  it('asks in the dialog itself, names the account, and says what happens everywhere', async () => {
    const fake = renderAt({ [directoryPath]: json(200, { items: [anna, petr], nextCursor: null }) });
    await waitFor(() => expect(rows()).toHaveLength(2));
    const { dialog, confirm } = await openFor('petr');

    expect(dialog).toHaveTextContent('Delete petr’s account?');
    expect(within(dialog).getByText('This deletes the whole account, everywhere in Tendo:')).toBeVisible();
    for (const sentence of [
      'They can no longer sign in, with a password or with single sign-on, and every device they’re signed in on is signed out now.',
      'They leave every household they belong to. Items they were responsible for stay, but no one is responsible for them.',
      'Household history stays, without their account.',
      'Their person entry stays in each household, so a household administrator can rename or archive it.',
      'This can’t be undone.',
    ]) {
      expect(within(dialog).getByText(sentence)).toBeVisible();
    }

    // It is an account everywhere, never a membership of the household the reader happens to be looking at.
    expect(within(dialog).queryByText(/this household/i)).not.toBeInTheDocument();
    expect(dialog).not.toHaveTextContent(/etag|tombstone|anonymi[sz]|endpoint/i);

    // Nothing is deleted until the login is repeated, and the confirmation field is where the reader starts.
    expect(confirm).toHaveValue('');
    await waitFor(() => expect(confirm).toHaveFocus());
    expect(submitButton(dialog)).toBeDisabled();
    expect(posts(fake)).toHaveLength(0);

    // A login is not a sentence: a phone never capitalises it, autocorrects it, or underlines it as misspelt.
    expect(confirm).toHaveAttribute('autocapitalize', 'none');
    expect(confirm).toHaveAttribute('autocorrect', 'off');
    expect(confirm).toHaveAttribute('spellcheck', 'false');
  });

  it('keeps the confirmation disabled until the login is typed exactly', async () => {
    renderAt({ [directoryPath]: json(200, { items: [petr], nextCursor: null }) });
    await waitFor(() => expect(rows()).toHaveLength(1));
    const { dialog, confirm } = await openFor('petr');
    const submit = submitButton(dialog);

    await userEvent.type(confirm, 'Petr');
    expect(submit).toBeDisabled();
    await userEvent.clear(confirm);
    await userEvent.type(confirm, 'petr ');
    expect(submit).toBeDisabled();
    await userEvent.clear(confirm);
    await userEvent.type(confirm, 'petr');
    expect(submit).toBeEnabled();
  });

  it('closes on Cancel without asking the server, and hands the focus back to the row', async () => {
    const fake = renderAt({ [directoryPath]: json(200, { items: [petr], nextCursor: null }) });
    await waitFor(() => expect(rows()).toHaveLength(1));
    const { trigger, dialog, confirm } = await openFor('petr');
    await userEvent.type(confirm, 'petr');

    await userEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(trigger).toHaveFocus();
    expect(posts(fake)).toHaveLength(0);
  });

  it('deletes exactly the account the reader confirmed, then says so and reads the list again', async () => {
    let reads = 0;
    const fake = renderAt({
      [directoryPath]: () => json(200, { items: ++reads === 1 ? [anna, petr] : [anna], nextCursor: null }),
      [deletionPath]: json(204),
    });
    await waitFor(() => expect(rows()).toHaveLength(2));
    const { dialog, confirm } = await openFor('petr');
    await userEvent.type(confirm, 'petr');

    await userEvent.click(submitButton(dialog));

    expect(await screen.findByText('petr’s account was deleted.')).toBeVisible();
    // The success sentence is announced by the screen's live host, not only painted.
    expect(screen.getByText('petr’s account was deleted.').closest('[role="status"]')).not.toBeNull();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    const [post] = posts(fake);
    expect(post.path).toBe('/api/v1/admin/account-deletions');
    expect(JSON.parse(post.body)).toEqual({ userId: petr.userId, confirmLogin: 'petr' });
    await waitFor(() => expect(logins()).toEqual(['anna']));
    expect(reads).toBe(2);
    // The row and its trigger are gone, so the reader lands on the list heading instead of the page body.
    expect(screen.getByRole('heading', { level: 2, name: 'Accounts' })).toHaveFocus();
  });

  it('deletes once even when the press is delivered twice', async () => {
    let release!: (response: Response) => void;
    const held = new Promise<Response>((resolve) => { release = resolve; });
    const fake = renderAt({
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [deletionPath]: () => held,
    });
    await waitFor(() => expect(rows()).toHaveLength(1));
    const { dialog, confirm } = await openFor('petr');
    await userEvent.type(confirm, 'petr');
    const submit = submitButton(dialog);
    const press = () => { submit.dispatchEvent(new MouseEvent('click', { bubbles: true })); };

    // Two presses that reach the handler in the same batch, before the disabled state can be rendered between them.
    await act(async () => { press(); press(); });
    expect(posts(fake)).toHaveLength(1);

    release(json(204));
    expect(await screen.findByText('petr’s account was deleted.')).toBeVisible();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(posts(fake)).toHaveLength(1);
  });

  it('hands the reader back with the other rows still usable while the list reads again', async () => {
    let release!: (response: Response) => void;
    const held = new Promise<Response>((resolve) => { release = resolve; });
    let reads = 0;
    renderAt({
      [directoryPath]: () => {
        reads += 1;
        return reads === 1 ? json(200, { items: [anna, petr, jana], nextCursor: null }) : held;
      },
      [deletionPath]: json(204),
    });
    await waitFor(() => expect(rows()).toHaveLength(3));
    const { dialog, confirm } = await openFor('petr');
    await userEvent.type(confirm, 'petr');
    await userEvent.click(submitButton(dialog));

    expect(await screen.findByText('petr’s account was deleted.')).toBeVisible();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    // The quiet reload never disables the rows it is replacing: another account can be deleted right away.
    await waitFor(() => expect(reads).toBe(2));
    expect(deleteTrigger('jana')).toBeEnabled();

    release(json(200, { items: [anna, jana], nextCursor: null }));
    await waitFor(() => expect(logins()).toEqual(['anna', 'jana']));
    expect(deleteTrigger('jana')).toBeEnabled();
  });

  it('keeps the dialog when a household would lose its last administrator, and deletes on the next try', async () => {
    let calls = 0;
    const fake = renderAt({
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [deletionPath]: () => (++calls === 1 ? problem(409, 'last_admin') : json(204)),
    });
    await waitFor(() => expect(rows()).toHaveLength(1));
    const { dialog, confirm } = await openFor('petr');
    await userEvent.type(confirm, 'petr');
    // The host is already here before the refusal, so the answer lands in a region the reader is watching.
    const host = dialog.querySelector<HTMLElement>('[data-delete-refusal-host]');
    expect(host).toHaveAttribute('role', 'status');

    await userEvent.click(submitButton(dialog));
    expect(await within(dialog).findByText(
      'petr is the only administrator of a household. Make someone else an administrator there first.',
    )).toBeVisible();
    // The pressed control is disabled while the request runs, so the refusal sentence itself takes the focus.
    await waitFor(() => expect(dialog.querySelector('[data-delete-refusal]')).toHaveFocus());
    expect(dialog.querySelector('[data-delete-refusal]')!.closest('[role="status"]')).toBe(host);
    // Nothing changed, so the dialog keeps everything the reader typed; fixing the cause is one press away.
    expect(screen.getByRole('dialog')).toBeVisible();
    expect(confirm).toHaveValue('petr');
    expect(submitButton(dialog)).toBeEnabled();
    expect(directoryReads(fake)).toBe(1);

    await userEvent.click(submitButton(dialog));
    expect(await screen.findByText('petr’s account was deleted.')).toBeVisible();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    expect(posts(fake)).toHaveLength(2);
  });

  it('answers the same refusal again, with the focus on it each time', async () => {
    renderAt({
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [deletionPath]: () => problem(409, 'last_admin'),
    });
    await waitFor(() => expect(rows()).toHaveLength(1));
    const { dialog, confirm } = await openFor('petr');
    await userEvent.type(confirm, 'petr');
    const host = dialog.querySelector<HTMLElement>('[data-delete-refusal-host]');

    await userEvent.click(submitButton(dialog));
    await waitFor(() => expect(dialog.querySelector('[data-delete-refusal]')).toHaveFocus());

    // A repeated refusal is a new answer, not a remembered one: the sentence takes the focus once more.
    await userEvent.click(submitButton(dialog));
    expect(await within(dialog).findByText(
      'petr is the only administrator of a household. Make someone else an administrator there first.',
    )).toBeVisible();
    await waitFor(() => expect(dialog.querySelector('[data-delete-refusal]')).toHaveFocus());
    expect(dialog.querySelector('[data-delete-refusal]')!.closest('[role="status"]')).toBe(host);
  });

  it.each([
    {
      what: 'the last installation administrator',
      answer: () => problem(409, 'last_instance_admin'),
      message: 'This is the last installation administrator, so the account can’t be deleted.',
    },
    {
      what: 'the reader themselves',
      answer: () => problem(409, 'cannot_delete_self'),
      message: 'You can’t delete your own account here. Another installation administrator has to do it.',
    },
    {
      what: 'another refusal',
      answer: () => json(422, { type: 'about:blank', title: 'x', status: 422, code: 'invalid_reference', field: 'userId' }),
      message: 'The account couldn’t be deleted, and nothing was changed.',
    },
  ])('keeps the dialog and what was typed when the server refuses because of $what', async ({ answer, message }) => {
    const fake = renderAt({
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [deletionPath]: answer,
    });
    await waitFor(() => expect(rows()).toHaveLength(1));
    const { dialog, confirm } = await openFor('petr');
    await userEvent.type(confirm, 'petr');

    await userEvent.click(submitButton(dialog));
    expect(await within(dialog).findByText(message)).toBeVisible();
    expect(screen.getByRole('dialog')).toBeVisible();
    expect(confirm).toHaveValue('petr');
    expect(directoryReads(fake)).toBe(1);
  });

  it('answers a mismatched confirmation on the field it is about, and forgets it once edited', async () => {
    renderAt({
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [deletionPath]: mismatch,
    });
    await waitFor(() => expect(rows()).toHaveLength(1));
    const { dialog, confirm } = await openFor('petr');
    await userEvent.type(confirm, 'petr');

    await userEvent.click(submitButton(dialog));
    await waitFor(() => expect(confirm).toHaveAttribute('aria-invalid', 'true'));
    const describedBy = confirm.getAttribute('aria-describedby');
    expect(document.getElementById(describedBy!)).toHaveTextContent(
      'That doesn’t match the login. Type it exactly as shown.',
    );
    // The field the reader has to fix takes the focus, and the dialog stays over the list.
    await waitFor(() => expect(confirm).toHaveFocus());
    expect(dialog).toBeVisible();

    await userEvent.type(confirm, 'x');
    expect(confirm).toHaveAttribute('aria-invalid', 'false');
  });

  it('keeps the dialog and the confirmation when the server cannot answer', async () => {
    const fake = renderAt({
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [deletionPath]: problem(503),
    });
    await waitFor(() => expect(rows()).toHaveLength(1));
    const { dialog, confirm } = await openFor('petr');
    await userEvent.type(confirm, 'petr');

    await userEvent.click(submitButton(dialog));
    expect(await within(dialog).findByText(
      'The account couldn’t be deleted. Check your connection and try again.',
    )).toBeVisible();
    expect(screen.getByRole('dialog')).toBeVisible();
    expect(confirm).toHaveValue('petr');
    // Nothing can have been deleted, so the list is not read again.
    expect(directoryReads(fake)).toBe(1);
  });

  it('closes and refreshes when the account is already gone', async () => {
    let reads = 0;
    renderAt({
      [directoryPath]: () => json(200, { items: ++reads === 1 ? [petr] : [], nextCursor: null }),
      [deletionPath]: problem(404),
    });
    await waitFor(() => expect(rows()).toHaveLength(1));
    const { dialog, confirm } = await openFor('petr');
    await userEvent.type(confirm, 'petr');

    await userEvent.click(submitButton(dialog));
    expect(await screen.findByText('That account no longer exists. The list has been refreshed.')).toBeVisible();
    expect(screen.getByText('That account no longer exists. The list has been refreshed.').closest('[role="status"]')).not.toBeNull();
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument());
    await waitFor(() => expect(logins()).toEqual([]));
    expect(reads).toBe(2);
    expect(screen.getByRole('heading', { level: 2, name: 'Accounts' })).toHaveFocus();
  });

  it('ends the dialog and the screen when the installation grant is refused while deleting', async () => {
    let sessionReads = 0;
    renderAt({
      'GET /api/v1/session': () => {
        sessionReads += 1;
        return sessionReads === 1 ? json(200, instanceAdminSession()) : json(200, session());
      },
      [`GET ${base}`]: json(200, memberHousehold),
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [deletionPath]: problem(403, 'instance_admin_required'),
    });
    await waitFor(() => expect(rows()).toHaveLength(1));
    const { dialog, confirm } = await openFor('petr');
    await userEvent.type(confirm, 'petr');

    await userEvent.click(submitButton(dialog));
    // A dialog cannot stay over a screen that has nothing left to administer, and its heading takes the focus the
    // trigger can no longer receive.
    const refused = await screen.findByRole('heading', { level: 2, name: 'Only an installation administrator can see this.' });
    expect(refused).toBeVisible();
    await waitFor(() => expect(refused).toHaveFocus());
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(rows()).toHaveLength(0);
    await waitFor(() => expect(sessionReads).toBe(2));
  });

  it('returns to sign-in when the deletion reports an ended session', async () => {
    renderAt({
      [directoryPath]: json(200, { items: [petr], nextCursor: null }),
      [deletionPath]: problem(401),
      'GET /api/v1/auth/setup': json(200, { required: false }),
      'GET /api/v1/auth/oidc': json(200, { enabled: false }),
    });
    await waitFor(() => expect(rows()).toHaveLength(1));
    const { dialog, confirm } = await openFor('petr');
    await userEvent.type(confirm, 'petr');

    await userEvent.click(submitButton(dialog));
    expect(await screen.findByLabelText('Password')).toBeVisible();
    expect(screen.getByRole('status')).toHaveTextContent('You were signed out. Sign in again to continue. Saved changes are kept.');
  });
});
