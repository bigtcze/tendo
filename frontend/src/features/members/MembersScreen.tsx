import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { LoadError } from '../../components/ui/load-error';
import { LoadingLine } from '../../components/ui/loading-line';
import { Notice } from '../../components/ui/notice';
import { useI18n } from '../../i18n';
import { nonBreaking } from '../items/formatDate';
import { InvitationsError, InvitePanel } from './InvitePanel';
import { MemberListSection } from './MemberListSection';
import { useInvitationList } from './useInvitationList';
import { useInviteActions } from './useInviteActions';
import { useMemberList } from './useMemberList';
import { useMembersCore } from './useMembersCore';
import { useMembersFocus } from './useMembersFocus';

export function MembersScreen({ householdId, userId, login, onSignedOut }: { householdId: string; userId: string; login: string; onSignedOut: () => void }) {
  const { t, locale } = useI18n();
  const core = useMembersCore(onSignedOut, t);
  const inv = useInvitationList(householdId, core);
  const members = useMemberList(householdId, userId, onSignedOut, t, core, inv);
  const { state, admin, notice, noticeTone, setNotice, setAttempt, noticeRef } = core;
  const listErrorNotice = notice ?? t('members.error.load');
  const listReady = state === 'ready' && (admin !== true || inv.invitationsLoaded);
  const actions = useInviteActions(householdId, onSignedOut, t, core, inv, listReady);
  useMembersFocus({
    members: members.members, memberBusy: members.memberBusy, memberPageFocusRef: members.memberPageFocusRef, memberHeadingRef: members.memberHeadingRef,
    invitations: inv.invitations, invitationBusy: inv.invitationBusy,
    invitationPageFocusRef: inv.invitationPageFocusRef, invitationHeadingRef: inv.invitationHeadingRef,
    notice: state === 'error' ? null : notice, noticeRef, t, link: actions.link, linkRef: actions.linkRef, pendingLinkFocusRef: actions.pendingLinkFocusRef,
    revokeId: actions.revokeId, restoreFocusIdRef: actions.restoreFocusIdRef, revokeButtonsRef: actions.revokeButtonsRef,
  });
  const formatDate = (date: string) => nonBreaking(new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(new Date(date)));
  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}><section className="settle">
    <Heading className="text-h1 font-semibold text-ink">{t('members.title')}</Heading>
    <div ref={noticeRef} tabIndex={-1} role="status" aria-atomic="true" className="mt-6 outline-none empty:mt-0">
      {notice && state !== 'error' ? <Notice tone={noticeTone === 'success' ? 'success' : 'neutral'} role={null}>{notice}</Notice> : null}
    </div>
    {state === 'loading' ? <LoadingLine className="mt-6">{t('members.loading')}</LoadingLine> : null}
    {state === 'error' ? <LoadError className="mt-8" message={listErrorNotice}
      retryLabel={t('app.error.retry')} onRetry={() => { setNotice(null); setAttempt((n) => n + 1); }} /> : null}
    {state === 'ready' ? <div className="mt-6 space-y-8 sm:mt-8 sm:space-y-10">
      <MemberListSection members={members.members} userId={userId} headingRef={members.memberHeadingRef} stack={members.memberStack}
        cursor={members.memberCursor} busy={members.memberBusy} onPrevious={members.previousMembers} onNext={members.nextMembers} t={t} />
      {admin === true ? <InvitePanel inv={inv} actions={actions} listReady={listReady} notice={notice} formatDate={formatDate} t={t} />
        : admin === false ? <section className="min-w-0" aria-labelledby="members-help-heading">
            <h2 id="members-help-heading" className="text-h2 text-ink">{t('members.help.title')}</h2>
            <p className="mt-1 max-w-prose text-small text-muted">{t('members.help.body')}</p>
          </section>
          : inv.invitationsError ? <InvitationsError notice={notice} onRetry={inv.refreshInvitations} className="min-w-0" t={t} />
            : <LoadingLine>{t('members.loading')}</LoadingLine>}
    </div> : null}
  </section></Layout>;
}
