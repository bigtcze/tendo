import { useState } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Layout } from '../../app/Layout';
import { useHouseholdData } from '../../app/HouseholdData';
import { useI18n } from '../../i18n';
import { AdminAccountList } from './AdminAccountList';
import { AdminIntro, AdminLoadError, AdminListSkeleton, AdminOnlyAdmins, formatResetExpiry } from './AdminStates';
import { AdminSectionSwitcher } from './AdminSectionSwitcher';
import { changeAccountRole, readAccount, readViewerMembership, removeAccount } from './adminApi';
import { PasswordResetDialog } from './PasswordResetDialog';
import { RemoveAccountDialog } from './RemoveAccountDialog';
import { RoleChangeDialog } from './RoleChangeDialog';
import { useAccountList } from './useAccountList';
import { useAccountRemoval, type AccountRemoval } from './useAccountRemoval';
import { usePasswordReset, type PasswordReset } from './usePasswordReset';
import { useRoleChange, type NoticeSpec, type RoleChange } from './useRoleChange';

type ChangeState = Pick<RoleChange, 'pending' | 'busy' | 'triggerRef' | 'confirm' | 'cancel'>;

/**
 * Admin center. The account list itself is the authorization probe: the server decides, and a member's
 * refusal shows the calm only-administrators state without any account data.
 */
export function AdminScreen({ householdId, viewerId, login, onSignedOut, onSelfLeft }: {
  householdId: string;
  viewerId: string;
  login: string;
  onSignedOut: () => void;
  /** The viewer's own membership is gone: the shell adopts the fresh session and opens Home. */
  onSelfLeft: (household: string, alreadyGone: boolean) => void;
}) {
  const { t, locale } = useI18n();
  const household = useHouseholdData();
  const householdName = household?.data.kind === 'ready' ? household.data.household.name : null;
  const list = useAccountList(householdId, onSignedOut, () => household?.refresh());
  const [notice, setNotice] = useState<NoticeSpec | null>(null);
  // An authoritative `admin_required` means the viewerRole hint may be stale (another admin demoted the viewer),
  // so re-read the household: the rail entry and the Account admin card follow the real role.
  const forbidden = () => { list.markForbidden(); household?.refresh(); };
  // The routes are injected, so the installation Admin center reuses both state machines with its own reads.
  const change = useRoleChange({
    viewerId, onSignedOut, onNotice: setNotice, onReload: list.reload, onAccount: list.replaceItem,
    onForbidden: forbidden,
    // Demoting yourself closes this household's Admin center; a promotion (only reachable through a stale row) is an
    // ordinary row update.
    onSelfChanged: (account, nextRole) => {
      if (nextRole === 'member') {
        forbidden();
        setNotice({ key: 'admin.notice.selfDemoted', tone: 'success' });
        return;
      }
      list.replaceItem(account);
      setNotice({ key: 'admin.notice.promoted', params: { login: account.login }, tone: 'success' });
    },
    read: (userId) => readAccount(householdId, userId),
    change: (userId, role, etag) => changeAccountRole(householdId, userId, role, etag),
  });
  const reset = usePasswordReset({
    householdId, onSignedOut, onNotice: setNotice, onReload: list.reload, onAccount: list.replaceItem, onForbidden: forbidden,
  });
  const removal = useAccountRemoval({
    viewerId, onSignedOut, onNotice: setNotice, onReload: list.reload, onForbidden: forbidden,
    onSelfLeft: (alreadyGone) => { if (householdName !== null) onSelfLeft(householdName, alreadyGone); },
    read: (target, self) => (self ? readViewerMembership(householdId) : readAccount(householdId, target.userId)),
    remove: (userId, etag) => removeAccount(householdId, userId, etag),
  });
  const busy = list.busy || change.busy || reset.busy || removal.busy;

  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}>
    <section className="settle">
      <AdminIntro notice={notice} t={t} />
      <AdminSectionSwitcher current="household" />
      {list.state === 'loading' ? <AdminListSkeleton label={t('admin.loading')} /> : null}
      {list.state === 'error' ? <AdminLoadError notice={t('admin.error.load')} onRetry={list.retry} t={t} /> : null}
      {list.state === 'forbidden' ? <AdminOnlyAdmins t={t} /> : null}
      {list.state === 'ready' ? <>
        <AdminAccountList
          accounts={list.accounts}
          viewerId={viewerId}
          busy={busy}
          headingRef={list.headingRef}
          stack={list.stack}
          nextCursor={list.nextCursor}
          onPrevious={list.previous}
          onNext={list.next}
          onChange={change.request}
          onReset={reset.request}
          onCancelReset={(account) => { void reset.cancelLink(account); }}
          onRemove={householdName === null ? undefined : removal.request}
          formatExpiry={(iso) => formatResetExpiry(iso, locale)}
          t={t}
        />
        {list.pageFailure ? <AdminLoadError notice={t('admin.error.load')} onRetry={list.retryPage} t={t} /> : null}
      </> : null}
    </section>
    <AdminDialogs viewerId={viewerId} householdName={householdName} change={change} reset={reset} removal={removal} />
  </Layout>;
}

/** The three dialogs this screen can open; the hooks above own every outcome they can announce. */
function AdminDialogs({ viewerId, householdName, change, reset, removal }: {
  viewerId: string;
  householdName: string | null;
  change: ChangeState;
  reset: PasswordReset;
  removal: AccountRemoval;
}) {
  return <>
    <RoleChangeDialog
      pending={change.pending}
      viewerId={viewerId}
      busy={change.busy}
      triggerRef={change.triggerRef}
      onConfirm={() => { void change.confirm(); }}
      onCancel={change.cancel}
    />
    <PasswordResetDialog
      pending={reset.pending}
      issued={reset.issued}
      busy={reset.busy}
      triggerRef={reset.triggerRef}
      onConfirm={() => { void reset.confirm(); }}
      onCancel={reset.cancelRequest}
      onCloseLink={reset.closeLink}
    />
    {householdName === null ? null : <RemoveAccountDialog
      pending={removal.pending}
      viewerId={viewerId}
      householdName={householdName}
      busy={removal.busy}
      triggerRef={removal.triggerRef}
      onConfirm={() => { void removal.confirm(); }}
      onCancel={removal.cancel}
    />}
  </>;
}
