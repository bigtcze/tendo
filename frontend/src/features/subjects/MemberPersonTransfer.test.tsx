import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { I18nProvider } from '../../i18n';
import type { Member } from '../members/membersApi';
import type { Subject } from './subjectsApi';
import { MemberPersonTransfer } from './MemberPersonTransfer';
import type { MemberPersonPreview, MemberPersonTransferApi, TransferOutcome } from './useMemberPersonTransfer';

const subject: Subject = {
  id: 's-1', name: 'Tomáš', type: 'person', linkedUserId: null, archived: false,
  createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z',
};
const member: Member = { userId: 'u-1', login: 'tomas', role: 'member' };

function renderTransfer(api: MemberPersonTransferApi, members: Member[] = [member], membersLoaded = true) {
  return render(
    <I18nProvider>
      <MemberPersonTransfer subject={subject} etag={'"3"'} members={members} membersLoaded={membersLoaded} api={api} />
    </I18nProvider>,
  );
}

describe('member person transfer', () => {
  it('previews the member’s current person, explains it is not a merge, then sends the confirmed transfer', async () => {
    let finish!: (outcome: TransferOutcome) => void;
    const transfer = vi.fn(() => new Promise<TransferOutcome>((resolve) => { finish = resolve; }));
    const api = { preview: vi.fn(async () => ({ kind: 'ok' as const, name: 'Tomas Novák' })), transfer };
    renderTransfer(api);

    await userEvent.click(screen.getByRole('button', { name: 'This is a household member…' }));
    const select = screen.getByRole('combobox', { name: 'Household member' });
    const confirm = screen.getByRole('button', { name: 'Connect' });
    expect(confirm).toBeDisabled();

    await userEvent.selectOptions(select, member.userId);
    await waitFor(() => expect(api.preview).toHaveBeenCalledWith(member.userId));
    expect(await screen.findByText(/This is not a merge/)).toHaveTextContent('“Tomas Novák”');
    expect(confirm).toBeEnabled();

    await userEvent.click(confirm);
    expect(transfer).toHaveBeenCalledWith(subject, '"3"', member);
    expect(screen.getByRole('button', { name: 'Connecting…' })).toBeDisabled();
    await act(async () => { finish('done'); });
  });

  it('keeps the selected member’s preview when an older preview resolves last', async () => {
    const alice: Member = { userId: 'u-a', login: 'alice', role: 'member' };
    const bob: Member = { userId: 'u-b', login: 'bob', role: 'member' };
    let resolveAlice!: (preview: MemberPersonPreview) => void;
    let resolveBob!: (preview: MemberPersonPreview) => void;
    const alicePreview = new Promise<MemberPersonPreview>((resolve) => { resolveAlice = resolve; });
    const bobPreview = new Promise<MemberPersonPreview>((resolve) => { resolveBob = resolve; });
    const transfer = vi.fn(async () => 'done' as const);
    const api = {
      preview: vi.fn((userId: string) => userId === alice.userId ? alicePreview : bobPreview),
      transfer,
    };
    renderTransfer(api, [alice, bob]);

    await userEvent.click(screen.getByRole('button', { name: 'This is a household member…' }));
    const select = screen.getByRole('combobox', { name: 'Household member' });
    await userEvent.selectOptions(select, alice.userId);
    await userEvent.selectOptions(select, bob.userId);
    await waitFor(() => expect(api.preview).toHaveBeenCalledTimes(2));
    const confirm = screen.getByRole('button', { name: 'Connect' });
    expect(confirm).toBeDisabled();

    // The newer choice resolves first; the older, slower one must not overwrite it.
    await act(async () => { resolveBob({ kind: 'ok', name: 'Bob’s current person' }); await bobPreview; });
    expect(await screen.findByText(/This is not a merge/)).toHaveTextContent('“Bob’s current person”');
    expect(confirm).toBeEnabled();

    await act(async () => { resolveAlice({ kind: 'ok', name: 'Alice’s current person' }); await alicePreview; });
    expect(screen.getByText(/This is not a merge/)).toHaveTextContent('“Bob’s current person”');
    expect(screen.getByText(/This is not a merge/)).not.toHaveTextContent('Alice’s current person');
    expect(confirm).toBeEnabled();

    await userEvent.click(confirm);
    expect(transfer).toHaveBeenCalledWith(subject, '"3"', bob);
  });

  it('explains a refused transfer and keeps the dialog content for another try', async () => {
    const api = {
      preview: vi.fn(async () => ({ kind: 'ok' as const, name: 'Tomas Novák' })),
      transfer: vi.fn(async () => 'failed' as const),
    };
    renderTransfer(api);
    await userEvent.click(screen.getByRole('button', { name: 'This is a household member…' }));
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Household member' }), member.userId);
    await userEvent.click(await screen.findByRole('button', { name: 'Connect' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Couldn’t connect this record.');
    expect(screen.getByRole('button', { name: 'Connect' })).toBeEnabled();
  });

  it('blocks confirming when the member has no person record', async () => {
    const api = {
      preview: vi.fn(async () => ({ kind: 'missing' as const })),
      transfer: vi.fn(async () => 'done' as const),
    };
    renderTransfer(api);
    await userEvent.click(screen.getByRole('button', { name: 'This is a household member…' }));
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Household member' }), member.userId);
    expect(await screen.findByRole('alert')).toHaveTextContent(/doesn’t have a person record/);
    expect(screen.getByRole('button', { name: 'Connect' })).toBeDisabled();
  });

  it('is not offered when the members list is unavailable or empty', () => {
    const api = { preview: vi.fn(), transfer: vi.fn() };
    const { rerender } = renderTransfer(api, [], false);
    expect(screen.queryByRole('button', { name: 'This is a household member…' })).not.toBeInTheDocument();
    rerender(
      <I18nProvider>
        <MemberPersonTransfer subject={subject} etag={'"3"'} members={[]} membersLoaded api={api} />
      </I18nProvider>,
    );
    expect(screen.queryByRole('button', { name: 'This is a household member…' })).not.toBeInTheDocument();
  });
});
