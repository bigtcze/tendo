import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { Button } from '../../components/ui/button';
import { Eyebrow } from '../../components/ui/eyebrow';
import { Notice } from '../../components/ui/notice';
import { useI18n, type Translate } from '../../i18n';
import { nonBreaking } from '../items/formatDate';
import { InvitationsError, InvitePanel } from './InvitePanel';
import { MemberListSection } from './MemberListSection';
import { useInvitationList } from './useInvitationList';
import { useInviteActions } from './useInviteActions';
import { useMemberList } from './useMemberList';
import { useMembersCore } from './useMembersCore';
import { useMembersFocus } from './useMembersFocus';

function LoadError({ notice, onRetry, t }: { notice: string; onRetry: () => void; t: Translate }) {
  return <div className="mt-8 space-y-3">
    <Notice tone="error" aria-atomic="true">{notice}</Notice>
    <Button onClick={onRetry}>{t('app.error.retry')}</Button>
  </div>;
}

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
    <Eyebrow>{t('members.eyebrow')}</Eyebrow>
    <Heading className="mt-2 text-h1 leading-tight text-ink">{t('members.title')}</Heading>
    <p className="mt-2 max-w-prose text-base text-muted">{t('members.subtitle')}</p>
    <div ref={noticeRef} tabIndex={-1} role="status" aria-atomic="true" className="mt-5 outline-none empty:mt-0">
      {notice && state !== 'error' ? <Notice tone={noticeTone === 'success' ? 'success' : 'neutral'} role={null}>{notice}</Notice> : null}
    </div>
    {state === 'loading' ? <p role="status" className="mt-5 text-small text-muted">{t('members.loading')}</p> : null}
    {state === 'error' ? <LoadError notice={listErrorNotice} onRetry={() => { setNotice(null); setAttempt((n) => n + 1); }} t={t} /> : null}
    {state === 'ready' ? <div className="mt-6 space-y-4 sm:mt-8 sm:space-y-6">
      <MemberListSection members={members.members} userId={userId} headingRef={members.memberHeadingRef} stack={members.memberStack}
        cursor={members.memberCursor} busy={members.memberBusy} onPrevious={members.previousMembers} onNext={members.nextMembers} t={t} />
      {admin === true ? <InvitePanel inv={inv} actions={actions} listReady={listReady} notice={notice} formatDate={formatDate} t={t} />
        : admin === false ? <section className="rounded-2xl border border-line bg-sand px-5 py-4" aria-labelledby="members-help-heading">
            <h2 id="members-help-heading" className="text-lg font-bold text-ink">{t('members.help.title')}</h2>
            <p className="mt-1 max-w-prose text-small text-muted">{t('members.help.body')}</p>
          </section>
          : inv.invitationsError ? <InvitationsError className="rounded-2xl border border-line bg-raised p-5" notice={notice} onRetry={inv.refreshInvitations} t={t} />
            : <p role="status" className="text-small text-muted">{t('members.loading')}</p>}
    </div> : null}
  </section></Layout>;
}
