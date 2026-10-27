import { useEffect, type RefObject } from 'react';
import type { Member, Invitation } from './membersApi';
import type { Translate } from '../../i18n';

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

/** Moves focus to a page's heading once its page settles, unless the user moved focus elsewhere. */
function usePageHeadingFocus(pageFocusRef: RefObject<HTMLElement | null>, headingRef: RefObject<HTMLElement | null>, items: unknown, busy: boolean) {
  useEffect(() => {
    if (!pageFocusRef.current || busy) return;
    const active = document.activeElement;
    if (active === pageFocusRef.current || active === document.body || active === null) headingRef.current?.focus();
    pageFocusRef.current = null;
  }, [items, busy, pageFocusRef, headingRef]);
}

/**
 * Screen focus effects, in their observable order: member page, invitation
 * page, notice, new link, then the Revoke button after a cancelled confirm.
 */
export function useMembersFocus(f: FocusInputs) {
  const { noticeRef, linkRef, pendingLinkFocusRef, restoreFocusIdRef, revokeButtonsRef, notice, t, link, revokeId } = f;
  usePageHeadingFocus(f.memberPageFocusRef, f.memberHeadingRef, f.members, f.memberBusy);
  usePageHeadingFocus(f.invitationPageFocusRef, f.invitationHeadingRef, f.invitations, f.invitationBusy);
  useEffect(() => {
    if (notice && notice !== t('members.invite.manualCopy')) noticeRef.current?.focus();
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
