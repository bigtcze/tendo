import type { RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { Dialog } from '../../components/ui/dialog';
import { Notice } from '../../components/ui/notice';
import { useI18n } from '../../i18n';
import type { PendingChange } from './useRoleChange';

/**
 * Asks before a role change and names the account and the consequence. Demoting yourself gets its own
 * warning, because it ends your access to the Admin center. Every outcome closes the dialog and is announced
 * by the screen's live region.
 */
export function RoleChangeDialog({ pending, viewerId, busy, triggerRef, onConfirm, onCancel }: {
  pending: PendingChange | null;
  viewerId: string;
  busy: boolean;
  triggerRef: RefObject<HTMLElement | null>;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const { t } = useI18n();
  const promote = pending?.nextRole === 'admin';
  const self = pending?.account.userId === viewerId;
  const login = pending?.account.login ?? '';
  const title = pending
    ? t(promote ? 'admin.dialog.promote.title' : 'admin.dialog.demote.title', { login })
    : '';
  return <Dialog
    open={pending !== null}
    onOpenChange={(open) => { if (!open) onCancel(); }}
    title={title}
    busy={busy}
    triggerRef={triggerRef}
  >
    <div className="space-y-4">
      {self && !promote
        ? <Notice>{t('admin.dialog.selfDemote.body')}</Notice>
        : <p className="max-w-prose text-body text-ink">{t(promote ? 'admin.dialog.promote.body' : 'admin.dialog.demote.body', { login })}</p>}
      <div className="flex flex-wrap justify-end gap-2">
        <Button variant="quiet" disabled={busy} onClick={onCancel}>{t('admin.action.cancel')}</Button>
        <Button disabled={busy} onClick={onConfirm}>
          {busy ? t('admin.action.saving') : t(promote ? 'admin.action.promote' : 'admin.action.demote')}
        </Button>
      </div>
    </div>
  </Dialog>;
}
