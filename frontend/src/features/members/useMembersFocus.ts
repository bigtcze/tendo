import { useEffect, type RefObject } from 'react';
import type { Member, Invitation } from './membersApi';
import type { Translate } from '../../i18n';
import { usePagedHeadingFocus } from '../../app/usePagedHeadingFocus';

type FocusInputs = {
  members: Member[];
  memberBusy: boolean;
  memberPageFocusRef: RefObject<HTMLElement | null>;
  memberHeadingRef: RefObject<HTMLHeadingElement | null>;
  invitations: Invitation[];
  invitationBusy: boolean;
  invitationPageFocusRef: RefObject<HTMLElement | null>;
  invitationHeadingRef: RefObject<HTMLHeadingElement | null>;
  notice: string | null;
  noticeRef: RefObject<HTMLDivElement | null>;
  t: Translate;
  link: string | null;
  linkRef: RefObject<HTMLInputElement | null>;
  pendingLinkFocusRef: RefObject<boolean>;
  revokeId: string | null;
  restoreFocusIdRef: RefObject<string | null>;
  revokeButtonsRef: RefObject<Map<string, HTMLButtonElement>>;
};

/**
 * Screen focus effects, in their observable order: member page, invitation
 * page, notice, new link, then the Revoke button after a cancelled confirm.
 */
export function useMembersFocus(f: FocusInputs) {
  const { noticeRef, linkRef, pendingLinkFocusRef, restoreFocusIdRef, revokeButtonsRef, notice, t, link, revokeId } = f;
  usePagedHeadingFocus(f.memberPageFocusRef, f.memberHeadingRef, f.members, f.memberBusy);
  usePagedHeadingFocus(f.invitationPageFocusRef, f.invitationHeadingRef, f.invitations, f.invitationBusy);
  useEffect(() => {
    if (notice) noticeRef.current?.focus();
  }, [notice, t, noticeRef]);
  useEffect(() => {
    if (pendingLinkFocusRef.current && link) {
      pendingLinkFocusRef.current = false;
      linkRef.current?.focus();
    }
  }, [link, pendingLinkFocusRef, linkRef]);
  useEffect(() => {
    if (restoreFocusIdRef.current && revokeId === null) {
      const id = restoreFocusIdRef.current;
      restoreFocusIdRef.current = null;
      revokeButtonsRef.current.get(id)?.focus();
    }
  }, [revokeId, restoreFocusIdRef, revokeButtonsRef]);
}
