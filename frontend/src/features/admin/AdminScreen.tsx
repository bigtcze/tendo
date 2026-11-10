import { useState } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { useHouseholdData } from '../../app/HouseholdData';
import { Notice } from '../../components/ui/notice';
import { useI18n } from '../../i18n';
import { AdminAccountList } from './AdminAccountList';
import { AdminLoadError, AdminListSkeleton, AdminOnlyAdmins } from './AdminStates';
import { RoleChangeDialog } from './RoleChangeDialog';
import { useAccountList } from './useAccountList';
import { useRoleChange, type NoticeSpec } from './useRoleChange';

/**
 * Admin center. The account list itself is the authorization probe: the server decides, and a member's
 * refusal shows the calm only-administrators state without any account data.
 */
export function AdminScreen({ householdId, viewerId, login, onSignedOut }: {
  householdId: string;
  viewerId: string;
  login: string;
  onSignedOut: () => void;
}) {
  const { t } = useI18n();
  const household = useHouseholdData();
  const list = useAccountList(householdId, onSignedOut, () => {
    // An authoritative `admin_required` means the viewerRole hint may be stale (another admin demoted the
    // viewer), so re-read the household: the rail entry and the Account admin card follow the real role.
    household?.refresh();
  });
  const [notice, setNotice] = useState<NoticeSpec | null>(null);

  const change = useRoleChange({
    householdId,
    viewerId,
    onSignedOut,
    onNotice: setNotice,
    onReload: list.reload,
    onAccount: list.replaceAccount,
    onForbidden: () => {
      list.markForbidden();
      household?.refresh();
    },
    onSelfDemoted: () => {
      list.markForbidden();
      // The navigation entry and this screen both follow the viewer's real role.
      household?.refresh();
    },
  });

  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}>
    <section className="settle">
      <p className="text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-muted">{t('admin.eyebrow')}</p>
      <Heading className="mt-2 text-h1 leading-tight text-ink">{t('admin.title')}</Heading>
      <p className="mt-2 max-w-prose text-base text-muted">{t('admin.subtitle')}</p>
      <div role="status" aria-atomic="true" className="mt-5 empty:mt-0">
        {notice ? <Notice tone={notice.tone === 'success' ? 'success' : 'neutral'} role={null}>{t(notice.key, notice.params)}</Notice> : null}
      </div>
      {list.state === 'loading' ? <AdminListSkeleton label={t('admin.loading')} /> : null}
      {list.state === 'error' ? <AdminLoadError notice={t('admin.error.load')} onRetry={list.retry} t={t} /> : null}
      {list.state === 'forbidden' ? <AdminOnlyAdmins t={t} /> : null}
      {list.state === 'ready' ? <>
        <AdminAccountList
          accounts={list.accounts}
          viewerId={viewerId}
          busy={list.busy || change.busy}
          headingRef={list.headingRef}
          stack={list.stack}
          nextCursor={list.nextCursor}
          onPrevious={list.previous}
          onNext={list.next}
          onChange={change.request}
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
  </Layout>;
}
