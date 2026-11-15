import { useState } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Layout } from '../../app/Layout';
import { useHouseholdData } from '../../app/HouseholdData';
import { useI18n } from '../../i18n';
import { AdminAccountList } from './AdminAccountList';
import { AdminIntro, AdminLoadError, AdminListSkeleton, AdminOnlyAdmins, formatResetExpiry } from './AdminStates';
import { PasswordResetDialog } from './PasswordResetDialog';
import { RemoveAccountDialog } from './RemoveAccountDialog';
import { RoleChangeDialog } from './RoleChangeDialog';
import { useAccountList } from './useAccountList';
import { useAccountRemoval } from './useAccountRemoval';
import { usePasswordReset } from './usePasswordReset';
import { useRoleChange, type NoticeSpec } from './useRoleChange';

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
  const shared = {
    householdId, onSignedOut, onNotice: setNotice, onReload: list.reload, onAccount: list.replaceAccount, onForbidden: forbidden,
  };

  const change = useRoleChange({ ...shared, viewerId, onSelfDemoted: forbidden });
  const reset = usePasswordReset(shared);
  const removal = useAccountRemoval({
    householdId, onSignedOut, viewerId, onNotice: setNotice, onReload: list.reload, onForbidden: forbidden,
    onSelfLeft: (alreadyGone) => { if (householdName !== null) onSelfLeft(householdName, alreadyGone); },
  });
  const busy = list.busy || change.busy || reset.busy || removal.busy;

  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}>
    <section className="settle">
      <AdminIntro notice={notice} t={t} />
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
  </Layout>;
}
