import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { Button } from '../../components/ui/button';
import { useI18n, type Translate } from '../../i18n';
import { InvitationsError, InvitePanel } from './InvitePanel';
import { MemberListSection } from './MemberListSection';
import { useInvitationList } from './useInvitationList';
import { useInviteActions } from './useInviteActions';
import { useMemberList } from './useMemberList';
import { useMembersCore } from './useMembersCore';
import { useMembersFocus } from './useMembersFocus';

function LoadError({ notice, onRetry, t }: { notice: string | null; onRetry: () => void; t: Translate }) {
  const generic = notice && notice !== t('members.error.householdNotFound') && notice !== t('members.error.requestRefused');
  return <div className="mt-8 space-y-3">
    <p className="text-muted">{generic ? notice : notice ?? t('members.error.load')}</p>
    <Button onClick={onRetry}>{t('app.error.retry')}</Button>
  </div>;
}

export function MembersScreen({ householdId, userId, login, onSignedOut }: { householdId: string; userId: string; login: string; onSignedOut: () => void }) {
  const { t, locale } = useI18n();
  const core = useMembersCore(onSignedOut, t);
  const inv = useInvitationList(householdId, core);
  const members = useMemberList(householdId, userId, onSignedOut, t, core, inv);
  const { state, owner, notice, setNotice, setAttempt, noticeRef } = core;
  const listReady = state === 'ready' && (owner !== true || inv.invitationsLoaded);
  const actions = useInviteActions(householdId, onSignedOut, t, core, inv, listReady);
  useMembersFocus({
    members: members.members, memberBusy: members.memberBusy, memberPageFocusRef: members.memberPageFocusRef, memberHeadingRef: members.memberHeadingRef,
    invitations: inv.invitations, invitationBusy: inv.invitationBusy,
    invitationPageFocusRef: inv.invitationPageFocusRef, invitationHeadingRef: inv.invitationHeadingRef,
    notice, noticeRef, t, link: actions.link, linkRef: actions.linkRef, pendingLinkFocusRef: actions.pendingLinkFocusRef,
    revokeId: actions.revokeId, restoreFocusIdRef: actions.restoreFocusIdRef, revokeButtonsRef: actions.revokeButtonsRef,
  });
  const formatDate = (date: string) => new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(new Date(date));
  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}><section className="settle">
    <Heading className="font-display text-h1 leading-tight">{t('members.title')}</Heading>
    <div ref={noticeRef} tabIndex={-1} role="status" className="mt-5 outline-none empty:mt-0">
      {notice && state !== 'error' ? <p className="rounded-xl bg-sand px-4 py-3">{notice}</p> : null}
    </div>
    {state === 'loading' ? <p role="status" className="mt-8 text-muted">{t('members.loading')}</p> : null}
    {state === 'error' ? <LoadError notice={notice} onRetry={() => { setNotice(null); setAttempt((n) => n + 1); }} t={t} /> : null}
    {state === 'ready' ? <div className="mt-8 space-y-8">
      <MemberListSection members={members.members} userId={userId} headingRef={members.memberHeadingRef} stack={members.memberStack}
        cursor={members.memberCursor} busy={members.memberBusy} onPrevious={members.previousMembers} onNext={members.nextMembers} t={t} />
      {owner === true ? <InvitePanel inv={inv} actions={actions} listReady={listReady} notice={notice} formatDate={formatDate} t={t} />
        : owner === false ? <p className="border-t border-line pt-5 text-muted">{t('members.ownerOnly')}</p>
          : inv.invitationsError ? <InvitationsError className="border-t border-line pt-5" notice={notice} onRetry={inv.refreshInvitations} t={t} />
            : <p role="status" className="border-t border-line pt-5 text-muted">{t('members.loading')}</p>}
    </div> : null}
  </section></Layout>;
}
