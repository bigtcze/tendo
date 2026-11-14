import type { RefObject } from 'react';
import { Button } from '../../components/ui/button';
import type { Translate } from '../../i18n';
import type { Invitation } from './membersApi';
import type { InviteActions } from './useInviteActions';

function track(refs: RefObject<Map<string, HTMLButtonElement>>, id: string) {
  return (element: HTMLButtonElement | null) => { if (element) refs.current.set(id, element); else refs.current.delete(id); };
}

/** The created/expires line identifies which invitation a revoke confirmation is about. */
const invitationDetailsId = (id: string) => `invitation-${id}-details`;

// Status is text first and colour second: the word carries the state, the tone only reinforces it.
const STATUS_TONE: Record<Invitation['status'], string> = {
  pending: 'text-waiting',
  accepted: 'text-done',
  revoked: 'text-paused',
  expired: 'text-paused',
};

function RevokeControl({ inv, actions, t }: { inv: Invitation; actions: InviteActions; t: Translate }) {
  const { busy, revokeId, setRevokeId, revoke, yesButtonsRef, revokeButtonsRef, restoreFocusIdRef } = actions;
  if (revokeId !== inv.id) {
    return <Button ref={track(revokeButtonsRef, inv.id)} variant="quiet" size="small" disabled={busy}
      onClick={() => { setRevokeId(inv.id); window.setTimeout(() => yesButtonsRef.current.get(inv.id)?.focus(), 0); }}>
      {t('members.invitation.revoke')}
    </Button>;
  }
  const questionId = `invitation-${inv.id}-revoke-question`;
  return <div className="flex min-h-11 flex-wrap items-center gap-2">
    <span id={questionId} className="text-small text-ink">{t('members.invitation.revokeConfirm')}</span>
    <Button ref={track(yesButtonsRef, inv.id)} variant="secondary" size="small" disabled={busy}
      aria-describedby={`${questionId} ${invitationDetailsId(inv.id)}`} onClick={() => void revoke(inv.id)}>
      {t('members.invitation.yes')}
    </Button>
    <Button variant="quiet" size="small" onClick={() => { restoreFocusIdRef.current = inv.id; setRevokeId(null); }}>{t('members.invitation.cancel')}</Button>
  </div>;
}

export function InvitationRows({ invitations, actions, formatDate, t }: {
  invitations: Invitation[];
  actions: InviteActions;
  formatDate: (date: string) => string;
  t: Translate;
}) {
  return <ul aria-labelledby="invitations-history-heading" className="mt-2 divide-y divide-line border-t border-line">{invitations.map((inv) => (
    <li key={inv.id} className="flex min-w-0 flex-wrap items-center justify-between gap-x-4 gap-y-2 py-3">
      <p className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2 gap-y-0.5">
        <span data-invitation-status className={`text-small font-semibold ${STATUS_TONE[inv.status]}`}>{t(`members.invitation.status.${inv.status}`)}</span>
        <span id={invitationDetailsId(inv.id)} data-invitation-details className="text-meta text-muted">{t('members.invitation.created', { date: formatDate(inv.createdAt) })} · {
          t('members.invitation.expires', { date: formatDate(inv.expiresAt) })}</span>
      </p>
      {inv.status === 'pending' ? <RevokeControl inv={inv} actions={actions} t={t} /> : null}
    </li>))}</ul>;
}
