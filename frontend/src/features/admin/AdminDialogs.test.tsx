import { render, screen } from '@testing-library/react';
import type { ReactElement } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { I18nProvider, type MessageKey } from '../../i18n';
import { RemoveAccountDialog } from './RemoveAccountDialog';
import { RoleChangeDialog } from './RoleChangeDialog';
import type { RemovalTarget } from './useAccountRemoval';
import type { PendingChange } from './useRoleChange';

/**
 * The two role and membership dialogs on their own. The household Admin screen always knows the household's name and
 * owns the cost of demoting itself; the installation Admin center may not know the name and keeps its own access.
 */
const target = (userId: string, role: RemovalTarget['role'] = 'member'): RemovalTarget => ({ userId, login: userId, role });
const triggerRef = { current: null };

function renderDialog(node: ReactElement) {
  return render(<I18nProvider>{node}</I18nProvider>);
}

function removalDialog(pending: RemovalTarget, viewerId: string, householdName: string | null) {
  return <RemoveAccountDialog
    pending={pending}
    viewerId={viewerId}
    householdName={householdName}
    busy={false}
    triggerRef={triggerRef}
    onConfirm={vi.fn()}
    onCancel={vi.fn()}
  />;
}

function roleDialog(pending: PendingChange, viewerId: string, selfDemoteBody?: MessageKey) {
  return <RoleChangeDialog
    pending={pending}
    viewerId={viewerId}
    busy={false}
    triggerRef={triggerRef}
    onConfirm={vi.fn()}
    onCancel={vi.fn()}
    {...(selfDemoteBody ? { selfDemoteBody } : {})}
  />;
}

describe('RemoveAccountDialog without a household name', () => {
  it('removes someone else from “this household” instead of an empty name', async () => {
    renderDialog(removalDialog(target('petr'), 'anna', null));
    const dialog = await screen.findByRole('dialog', { name: 'Remove petr from this household?' });
    // The unnamed title is the whole name; a missing household never leaves a dangling “from ?”.
    expect(dialog).not.toHaveTextContent(/\bfrom\s*\?/u);
    expect(dialog).not.toHaveTextContent('Veselí');
  });

  it('titles the viewer’s own removal as leaving “this household”', async () => {
    renderDialog(removalDialog(target('anna'), 'anna', null));
    const dialog = await screen.findByRole('dialog', { name: 'Leave this household?' });
    expect(dialog).not.toHaveTextContent(/\bLeave\s*\?/u);
    expect(dialog).not.toHaveTextContent('Veselí');
  });

  it('still names the household when the screen knows it', async () => {
    renderDialog(removalDialog(target('petr'), 'anna', 'Veselí'));
    expect(await screen.findByRole('dialog', { name: 'Remove petr from Veselí?' })).toBeVisible();
    expect(screen.queryByRole('dialog', { name: 'Remove petr from this household?' })).not.toBeInTheDocument();
  });
});

describe('RoleChangeDialog self-demotion warning', () => {
  const selfDemote: PendingChange = { account: { userId: 'anna', login: 'anna', role: 'admin' }, nextRole: 'member' };

  it('warns that demoting yourself ends the Admin center by default', async () => {
    renderDialog(roleDialog(selfDemote, 'anna'));
    const dialog = await screen.findByRole('dialog', { name: 'Make anna a member?' });
    expect(dialog).toHaveTextContent('You’ll lose access to the Admin center. A household always needs at least one administrator.');
  });

  it('lets a screen replace what demoting yourself costs', async () => {
    renderDialog(roleDialog(selfDemote, 'anna', 'install.dialog.selfDemote.body'));
    const dialog = await screen.findByRole('dialog', { name: 'Make anna a member?' });
    expect(dialog).toHaveTextContent('You’ll become a member of this household. You can still manage its accounts here, under Households.');
    expect(dialog).not.toHaveTextContent('You’ll lose access to the Admin center.');
  });

  it('keeps the ordinary demotion wording for someone else', async () => {
    const other: PendingChange = { account: { userId: 'petr', login: 'petr', role: 'admin' }, nextRole: 'member' };
    renderDialog(roleDialog(other, 'anna', 'install.dialog.selfDemote.body'));
    const dialog = await screen.findByRole('dialog', { name: 'Make petr a member?' });
    expect(dialog).toHaveTextContent('petr will no longer be able to invite people, change roles or help with passwords.');
    expect(dialog).not.toHaveTextContent('You’ll become a member of this household.');
  });
});
