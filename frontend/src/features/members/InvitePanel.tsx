import { Button } from '../../components/ui/button';
import { EmptyState } from '../../components/ui/empty-state';
import { NoticeRegion } from '../../components/ui/notice';
import type { Translate } from '../../i18n';
import { InvitationRows } from './InvitationRows';
import { CursorPager } from '../../components/ui/cursor-pager';
import type { InvitationList } from './useInvitationList';
import type { InviteActions } from './useInviteActions';

function InviteLink({ actions, t }: { actions: InviteActions; t: Translate }) {
  const { link, linkRef, copied, copyLink, copyMessage } = actions;
  return <div className="mt-4 rounded-xl border border-line bg-sand p-4">
    <p className="text-small font-semibold text-ink">{t('members.invite.share')}</p>
    <div className="mt-3 flex flex-wrap gap-2">
      <input ref={linkRef} readOnly value={link ?? ''} aria-label={t('members.invite.link')}
        className={'min-h-11 min-w-0 flex-1 basis-48 rounded-xl border border-field bg-raised px-3 text-small break-all text-ink '
          + 'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus'} />
      <Button variant="secondary" onClick={copyLink}>{copied ? t('members.invite.copiedButton') : t('members.invite.copy')}</Button>
    </div>
    <p className="mt-3 text-small text-muted">{t('members.invite.caveat')}</p>
    <NoticeRegion message={copyMessage ?? ''} className="mt-2 empty:mt-0" />
  </div>;
}

/** Invitations retry state; shared with the unknown-role tail of the screen. */
export function InvitationsError({ notice, onRetry, className, t }: { notice: string | null; onRetry: () => void; className: string; t: Translate }) {
  return <div className={className}>
    <p className="text-small text-muted">{notice === t('members.error.requestRefused') ? notice : t('members.invitations.error')}</p>
    <Button variant="quiet" size="small" className="mt-2" onClick={onRetry}>{t('app.error.retry')}</Button>
  </div>;
}

function InvitationsBody({ inv, actions, listReady, notice, formatDate, t }: {
  inv: InvitationList; actions: InviteActions; listReady: boolean; notice: string | null; formatDate: (date: string) => string; t: Translate;
}) {
  if (!listReady) return <p role="status" className="mt-3 text-small text-muted">{t('members.loading')}</p>;
  if (inv.invitationsError) return <InvitationsError className="mt-3 rounded-xl border border-line bg-paper p-4" notice={notice} onRetry={inv.refreshInvitations} t={t} />;
  if (inv.invitations.length) return <InvitationRows invitations={inv.invitations} actions={actions} formatDate={formatDate} t={t} />;
  return <div className="mt-3"><EmptyState title={t('members.invitations.empty')} /></div>;
}

export function InvitePanel({ inv, actions, listReady, notice, formatDate, t }: {
  inv: InvitationList; actions: InviteActions; listReady: boolean; notice: string | null; formatDate: (date: string) => string; t: Translate;
}) {
  const { busy, recoveryInvitationId, canCheckAgain, link, invite, revoke } = actions;
  const { invitationHeadingRef, invitationStack, invitationCursor, invitationBusy, previousInvitations, nextInvitations } = inv;
  return <section role="region" aria-labelledby="members-invitations-section">
    <div className="rounded-2xl border border-line bg-raised p-5 sm:p-6">
      <h2 id="members-invitations-section" className="text-h2 text-ink">{t('members.invite.title')}</h2>
      <p className="mt-1 max-w-prose text-small text-muted">{t('members.invite.intro')}</p>
      <Button className="mt-4 w-full whitespace-normal text-center sm:w-auto" disabled={busy || !listReady} onClick={() => void invite()}>
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
    </div>
    <div className="mt-6 border-t border-line pt-5">
      <h3 id="invitations-history-heading" ref={invitationHeadingRef} tabIndex={-1} className="text-lg font-bold text-ink outline-none">{t('members.invitations.title')}</h3>
      <InvitationsBody inv={inv} actions={actions} listReady={listReady} notice={notice} formatDate={formatDate} t={t} />
      {listReady && (invitationStack.length > 0 || invitationCursor)
        ? <CursorPager kind="invitation" stack={invitationStack} cursor={invitationCursor} busy={invitationBusy}
          onPrevious={previousInvitations} onNext={nextInvitations} t={t} />
        : null}
    </div>
  </section>;
}
