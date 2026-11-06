import { Button } from '../../components/ui/button';
import { EmptyState } from '../../components/ui/empty-state';
import { Notice } from '../../components/ui/notice';
import type { Translate } from '../../i18n';
import { InvitationRows } from './InvitationRows';
import { MembersPager } from './MembersPager';
import type { InvitationList } from './useInvitationList';
import type { InviteActions } from './useInviteActions';

function InviteLink({ actions, t }: { actions: InviteActions; t: Translate }) {
  const { link, linkRef, copied, copyLink, copyMessage } = actions;
  return <div className="mt-4 rounded-xl border border-line bg-raised p-4">
    <p className="text-ink">{t('members.invite.share')}</p>
    <div className="mt-3 flex flex-wrap gap-2">
      <input ref={linkRef} readOnly value={link ?? ''} aria-label={t('members.invite.link')}
        className={'min-h-11 min-w-0 flex-1 basis-48 rounded-xl border border-field bg-paper px-3 text-sm break-all text-ink '
          + 'focus-visible:outline-2 focus-visible:outline-accent'} />
      <Button variant="quiet" onClick={copyLink}>{copied ? t('members.invite.copiedButton') : t('members.invite.copy')}</Button>
    </div>
    <p className="mt-3 text-sm text-muted">{t('members.invite.caveat')}</p>
    <p aria-live="polite" aria-atomic="true" className="sr-only">{copyMessage ?? ''}</p>
    {copyMessage ? <Notice role={null} className="mt-2">{copyMessage}</Notice> : null}
  </div>;
}

/** Invitations retry state; shared with the unknown-role tail of the screen. */
export function InvitationsError({ notice, onRetry, className, t }: { notice: string | null; onRetry: () => void; className: string; t: Translate }) {
  return <div className={className}>
    <p className="text-muted">{notice === t('members.error.requestRefused') ? notice : t('members.invitations.error')}</p>
    <Button variant="quiet" className="mt-2" onClick={onRetry}>{t('app.error.retry')}</Button>
  </div>;
}

function InvitationsBody({ inv, actions, listReady, notice, formatDate, t }: {
  inv: InvitationList; actions: InviteActions; listReady: boolean; notice: string | null; formatDate: (date: string) => string; t: Translate;
}) {
  if (!listReady) return <p role="status" className="mt-2 text-muted">{t('members.loading')}</p>;
  if (inv.invitationsError) return <InvitationsError className="mt-2" notice={notice} onRetry={inv.refreshInvitations} t={t} />;
  if (inv.invitations.length) return <InvitationRows invitations={inv.invitations} actions={actions} formatDate={formatDate} t={t} />;
  return <EmptyState title={t('members.invitations.empty')} />;
}

export function InvitePanel({ inv, actions, listReady, notice, formatDate, t }: {
  inv: InvitationList; actions: InviteActions; listReady: boolean; notice: string | null; formatDate: (date: string) => string; t: Translate;
}) {
  const { busy, recoveryInvitationId, canCheckAgain, link, invite, revoke } = actions;
  const { invitationHeadingRef, invitationStack, invitationCursor, invitationBusy, previousInvitations, nextInvitations } = inv;
  return <section role="region" aria-labelledby="members-invitations-section" className="border-t border-line pt-2 sm:pt-6">
    <h2 id="members-invitations-section" className="font-display text-h2">{t('members.invite.title')}</h2>
    <p className="mt-0.5 max-w-prose text-meta text-muted sm:mt-2 sm:text-base">{t('members.invite.intro')}</p>
    <Button className="mt-1 w-full whitespace-normal text-center sm:mt-4 sm:w-auto" disabled={busy || !listReady} onClick={() => void invite()}>
      {busy ? t('members.invite.creating') : t('members.invite.action')}
    </Button>
    {recoveryInvitationId
      ? <Button variant="quiet" className="mt-3" disabled={busy} onClick={() => void revoke(recoveryInvitationId)}>
        {t('members.invite.revokeRecovered')}
      </Button>
      : null}
    {canCheckAgain
      ? <Button variant="quiet" className="mt-3" disabled={busy || !listReady} onClick={() => void invite(true)}>{t('members.invite.checkAgain')}</Button>
      : null}
    {link ? <InviteLink actions={actions} t={t} /> : null}
    <h3 id="invitations-history-heading" ref={invitationHeadingRef} tabIndex={-1} className="mt-3 border-t border-line pt-2 font-display text-xl outline-none sm:mt-8 sm:pt-6">{t('members.invitations.title')}</h3>
    <InvitationsBody inv={inv} actions={actions} listReady={listReady} notice={notice} formatDate={formatDate} t={t} />
    {listReady && (invitationStack.length > 0 || invitationCursor)
      ? <MembersPager kind="invitation" stack={invitationStack} cursor={invitationCursor} busy={invitationBusy}
        onPrevious={previousInvitations} onNext={nextInvitations} t={t} />
      : null}
  </section>;
}
