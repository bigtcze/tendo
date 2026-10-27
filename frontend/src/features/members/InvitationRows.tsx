import type { RefObject } from 'react';
import { Button } from '../../components/ui/button';
import type { Translate } from '../../i18n';
import type { Invitation } from './membersApi';
import type { InviteActions } from './useInviteActions';

function track(refs: RefObject<Map<string, HTMLButtonElement>>, id: string) {
  return (element: HTMLButtonElement | null) => { if (element) refs.current.set(id, element); else refs.current.delete(id); };
}

function RevokeControl({ inv, actions, t }: { inv: Invitation; actions: InviteActions; t: Translate }) {
  const { busy, revokeId, setRevokeId, revoke, yesButtonsRef, revokeButtonsRef, restoreFocusIdRef } = actions;
  if (revokeId !== inv.id) {
    return <Button ref={track(revokeButtonsRef, inv.id)} variant="quiet" size="small" disabled={busy}
      onClick={() => { setRevokeId(inv.id); window.setTimeout(() => yesButtonsRef.current.get(inv.id)?.focus(), 0); }}>
      {t('members.invitation.revoke')}
    </Button>;
  }
  return <div className="flex min-h-11 items-center gap-2">
    <span>{t('members.invitation.revokeConfirm')}</span>
    <Button ref={track(yesButtonsRef, inv.id)} variant="quiet" size="small" disabled={busy} onClick={() => void revoke(inv.id)}>
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
  return <ul className="mt-3 divide-y divide-line rounded-2xl border border-line bg-white/70">{invitations.map((inv) => (
    <li key={inv.id} className="flex flex-wrap items-start justify-between gap-3 p-4">
      <div className="min-w-0">
        <p>{t(`members.invitation.status.${inv.status}`)}</p>
        <p className="mt-1 text-sm text-muted">{t('members.invitation.created', { date: formatDate(inv.createdAt) })} · {
          t('members.invitation.expires', { date: formatDate(inv.expiresAt) })}</p>
      </div>
      {inv.status === 'pending' ? <RevokeControl inv={inv} actions={actions} t={t} /> : null}
    </li>))}</ul>;
}
