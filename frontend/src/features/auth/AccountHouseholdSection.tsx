import { useState } from 'react';
import { Button } from '../../components/ui/button';
import { NoticeRegion } from '../../components/ui/notice';
import { useHouseholdData } from '../../app/HouseholdData';
import { useI18n } from '../../i18n';
import type { AccountRole } from '../admin/adminApi';
import { readViewerMembership, removeAccount } from '../admin/adminApi';
import { RemoveAccountDialog } from '../admin/RemoveAccountDialog';
import { useAccountRemoval } from '../admin/useAccountRemoval';
import type { NoticeSpec } from '../admin/useRoleChange';
import type { Session } from './sessionApi';

function MembershipSection({ session, household, role, refresh, onSignedOut, onSelfLeft }: {
  session: Session;
  household: { id: string; name: string };
  role: AccountRole;
  refresh: () => void;
  onSignedOut: () => void;
  onSelfLeft: (household: string, alreadyGone: boolean) => void;
}) {
  const { t } = useI18n();
  const [notice, setNotice] = useState<NoticeSpec | null>(null);
  const removal = useAccountRemoval({
    viewerId: session.userId,
    onSignedOut,
    onNotice: setNotice,
    onReload: refresh,
    onForbidden: () => {
      refresh();
      setNotice({ key: 'admin.error.action', tone: 'neutral' });
    },
    onSelfLeft: (alreadyGone) => onSelfLeft(household.name, alreadyGone),
    read: () => readViewerMembership(household.id),
    remove: (userId, etag) => removeAccount(household.id, userId, etag),
  });

  return <section aria-labelledby="account-household-heading" className="border-t border-line pt-6">
    <h2 id="account-household-heading" className="text-h2 text-ink">{t('account.household.title')}</h2>
    <p className="mt-1 max-w-prose text-small text-muted">{t('account.household.body', { household: household.name })}</p>
    <NoticeRegion className="mt-4 empty:mt-0" message={notice ? t(notice.key, notice.params) : ''} />
    <Button
      variant="secondary"
      className="mt-4"
      disabled={removal.busy}
      onClick={(event) => removal.request({ userId: session.userId, login: session.login, role }, event.currentTarget)}
    >
      {t('account.household.leave')}
    </Button>
    <RemoveAccountDialog
      pending={removal.pending}
      viewerId={session.userId}
      householdName={household.name}
      busy={removal.busy}
      triggerRef={removal.triggerRef}
      onConfirm={() => { void removal.confirm(); }}
      onCancel={removal.cancel}
    />
  </section>;
}

/**
 * The viewer's own membership, in the quiet settings stack on Account: the one place a plain member can leave a
 * household, since the Admin center stays closed to them. Nothing is offered without a household to name.
 */
export function AccountHouseholdSection({ session, onSignedOut, onSelfLeft }: {
  session: Session;
  onSignedOut: () => void;
  onSelfLeft: (household: string, alreadyGone: boolean) => void;
}) {
  const household = useHouseholdData();
  if (household?.data.kind !== 'ready') return null;
  const { id, name, viewerRole } = household.data.household;
  return <MembershipSection
    session={session}
    household={{ id, name }}
    role={viewerRole}
    refresh={household.refresh}
    onSignedOut={onSignedOut}
    onSelfLeft={onSelfLeft}
  />;
}
