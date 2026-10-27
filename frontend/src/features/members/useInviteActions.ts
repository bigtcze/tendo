import { useRef, useState } from 'react';
import type { Translate } from '../../i18n';
import { createInvitation, revokeInvitation, type CreateInvitationResult } from './membersApi';
import { newIdempotencyKey } from './membersModel';
import type { MembersCore } from './useMembersCore';
import type { InvitationList } from './useInvitationList';
import { useInviteLink } from './useInviteLink';

type RevokeResult = Awaited<ReturnType<typeof revokeInvitation>>;

export function useInviteActions(householdId: string, onSignedOut: () => void, t: Translate, core: MembersCore, inv: InvitationList, listReady: boolean) {
  const { mountedRef, memberGenerationRef, owner, setOwner, setNotice, noticeRef } = core;
  const [busy, setBusy] = useState(false);
  const [canCheckAgain, setCanCheckAgain] = useState(false);
  const [revokeId, setRevokeId] = useState<string | null>(null);
  const [recoveryInvitationId, setRecoveryInvitationId] = useState<string | null>(null);
  const unresolvedCreateKey = useRef<string | null>(null);
  const yesButtonsRef = useRef(new Map<string, HTMLButtonElement>());
  const revokeButtonsRef = useRef(new Map<string, HTMLButtonElement>());
  const restoreFocusIdRef = useRef<string | null>(null);
  const inviteLink = useInviteLink(t, setNotice);

  function loseOwnership() { setOwner(false); inv.setInvitations([]); setNotice(t('members.invite.ownerRequired')); unresolvedCreateKey.current = null; }
  function settleKey() { unresolvedCreateKey.current = null; setCanCheckAgain(false); }

  function applyCreate(result: CreateInvitationResult, key: string) {
    if (result.kind === 'unauthenticated') { onSignedOut(); return; }
    if (result.kind === 'forbidden') { loseOwnership(); return; }
    if (result.kind === 'rejected' || result.kind === 'notFound') {
      setNotice(t(result.kind === 'rejected' ? 'members.error.requestRefused' : 'members.error.householdNotFound'));
      settleKey();
      return;
    }
    if (result.kind === 'created') {
      setRecoveryInvitationId(null);
      settleKey();
      inviteLink.showLink(result.invitation.token, result.invitation.id);
      setNotice(t('members.invite.created'));
    } else if (result.kind === 'existing') {
      setRecoveryInvitationId(result.invitation.id);
      settleKey();
      setNotice(t('members.invite.existingNoLink'));
    } else {
      unresolvedCreateKey.current = key;
      setCanCheckAgain(true);
      setNotice(t('members.invite.unavailable'));
    }
    inv.refreshInvitations();
  }

  async function invite(replay = false) {
    if (busy || owner !== true || !listReady) return;
    setBusy(true); setNotice(null); inviteLink.clearLink();
    const key = replay && unresolvedCreateKey.current ? unresolvedCreateKey.current : newIdempotencyKey();
    if (!replay) { unresolvedCreateKey.current = null; setCanCheckAgain(false); }
    const memberRequestGeneration = memberGenerationRef.current;
    const result = await createInvitation(householdId, key);
    if (!mountedRef.current || memberGenerationRef.current !== memberRequestGeneration) return;
    setBusy(false);
    applyCreate(result, key);
  }

  function applyRevoke(result: RevokeResult, id: string) {
    if (result === 'unauthenticated') { onSignedOut(); return; }
    if (result === 'forbidden') { loseOwnership(); setCanCheckAgain(false); return; }
    if (result === 'rejected') { setNotice(t('members.error.requestRefused')); return; }
    if (result === 'notFound') { setNotice(t('members.error.householdNotFound')); return; }
    if (result !== 'ok') { setNotice(t('members.error.action')); return; }
    if (inviteLink.linkInvitationId === id) inviteLink.clearLink();
    if (recoveryInvitationId === id) setRecoveryInvitationId(null);
    setNotice(t('members.invite.revoked'));
    restoreFocusIdRef.current = null;
    noticeRef.current?.focus();
    inv.refreshInvitations();
  }

  async function revoke(id: string) {
    if (busy) return;
    setBusy(true);
    const generation = memberGenerationRef.current;
    const result = await revokeInvitation(householdId, id);
    if (!mountedRef.current || memberGenerationRef.current !== generation) return;
    setBusy(false); setRevokeId(null);
    applyRevoke(result, id);
  }

  return {
    busy, link: inviteLink.link, copied: inviteLink.copied, canCheckAgain, revokeId, setRevokeId, recoveryInvitationId,
    linkRef: inviteLink.linkRef, pendingLinkFocusRef: inviteLink.pendingLinkFocusRef, copyLink: inviteLink.copyLink,
    yesButtonsRef, revokeButtonsRef, restoreFocusIdRef, invite, revoke,
  };
}

export type InviteActions = ReturnType<typeof useInviteActions>;
