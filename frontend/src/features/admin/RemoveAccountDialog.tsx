import type { RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { Dialog } from '../../components/ui/dialog';
import { Notice } from '../../components/ui/notice';
import { useI18n, type Translate } from '../../i18n';
import type { RemovalTarget } from './useAccountRemoval';

/**
 * Asks before a membership goes, and says in plain words what changes: the household closes, items stay without a
 * responsible person, history and the person stay, links into the household stop working, and other households are
 * untouched. The viewer's own row asks about leaving and keeps its own wording.
 */
export function RemoveAccountDialog({ pending, viewerId, householdName, busy, triggerRef, onConfirm, onCancel }: {
  pending: RemovalTarget | null;
  viewerId: string;
  /** Null when the screen cannot name the household (a direct load of the installation page): the title says “this household”. */
  householdName: string | null;
  busy: boolean;
  triggerRef: RefObject<HTMLElement | null>;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const { t } = useI18n();
  const self = pending?.userId === viewerId;
  const login = pending?.login ?? '';
  const lastAdmin = pending?.role === 'admin';
  const title = pending ? dialogTitle(t, self, login, householdName) : '';
  return <Dialog
    open={pending !== null}
    onOpenChange={(open) => { if (!open) onCancel(); }}
    title={title}
    busy={busy}
    triggerRef={triggerRef}
  >
    <div className="space-y-4">
      <p className="max-w-prose text-body text-ink">
        {t(self ? 'admin.remove.dialog.leaveBody' : 'admin.remove.dialog.body', { login })}
      </p>
      <ul className="max-w-prose list-disc space-y-1.5 pl-5 text-body text-muted">
        <li>{t(self ? 'admin.remove.dialog.itemsSelf' : 'admin.remove.dialog.items')}</li>
        <li>{t('admin.remove.dialog.history')}</li>
        <li>{t('admin.remove.dialog.links')}</li>
        <li>{t(self ? 'admin.remove.dialog.householdsSelf' : 'admin.remove.dialog.households')}</li>
      </ul>
      {lastAdmin ? <Notice>{t('admin.remove.dialog.lastAdmin')}</Notice> : null}
      <div className="flex flex-wrap justify-end gap-2">
        <Button variant="quiet" disabled={busy} onClick={onCancel}>{t('admin.action.cancel')}</Button>
        <Button disabled={busy} onClick={onConfirm}>
          {busy
            ? t(self ? 'admin.remove.leaving' : 'admin.remove.working')
            : t(self ? 'admin.remove.leave' : 'admin.remove.confirm')}
        </Button>
      </div>
    </div>
  </Dialog>;
}

/** The household's name when the screen knows it; otherwise “this household”, never an empty name. */
function dialogTitle(t: Translate, self: boolean, login: string, householdName: string | null): string {
  if (householdName === null) return t(self ? 'admin.remove.dialog.leaveTitleUnnamed' : 'admin.remove.dialog.titleUnnamed', { login });
  return t(self ? 'admin.remove.dialog.leaveTitle' : 'admin.remove.dialog.title', { login, household: householdName });
}
