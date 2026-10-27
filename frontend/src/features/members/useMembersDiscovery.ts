import { useEffect, type Dispatch, type SetStateAction } from 'react';
import { listMembers, type Member, type PageResult } from './membersApi';
import type { MembersCore } from './useMembersCore';
import type { InvitationList } from './useInvitationList';

/** Pages scanned to find the viewer's own membership before settling the role. */
const DISCOVERY_PAGES = 20;

export type MemberSetters = {
  setMembers: Dispatch<SetStateAction<Member[]>>;
  setMemberCursor: Dispatch<SetStateAction<string | null>>;
  setCurrentMemberCursor: Dispatch<SetStateAction<string | undefined>>;
  setMemberStack: Dispatch<SetStateAction<string[]>>;
  setMemberBusy: Dispatch<SetStateAction<boolean>>;
};

/**
 * Initial load: shows the first member page and scans up to DISCOVERY_PAGES
 * pages for the viewer, then loads invitations unless the viewer is a plain
 * member. Each `listMembers` await is followed directly by the
 * cancellation/generation check, with no extra async boundary.
 */
export function useMembersDiscovery(householdId: string, userId: string, core: MembersCore, inv: InvitationList, m: MemberSetters) {
  const { attempt, mountedRef, onSignedOutRef, tRef, setState, setOwner, setNotice } = core;
  const { memberGenerationRef, memberPageGenerationRef, invitationGenerationRef } = core;
  const { loadInvitations, setInvitationBusy, setInvitations, setInvitationCursor, setInvitationsError, setInvitationsLoaded } = inv;
  const { setMembers, setMemberCursor, setCurrentMemberCursor, setMemberStack, setMemberBusy } = m;
  useEffect(() => {
    const generation = ++memberGenerationRef.current;
    memberPageGenerationRef.current += 1;
    let cancelled = false;
    const stale = () => cancelled || memberGenerationRef.current !== generation;
    function fail(page: PageResult<Member>) {
      if (page.kind === 'unauthenticated') { if (mountedRef.current) onSignedOutRef.current(); return; }
      setState('error');
      if (page.kind === 'notFound') setNotice(tRef.current('members.error.householdNotFound'));
      else if (page.kind === 'rejected' || page.kind === 'forbidden') setNotice(tRef.current('members.error.requestRefused'));
      else setNotice(null);
    }
    function apply(firstPage: Member[], firstNext: string | null, viewer: Member | undefined) {
      setMembers(firstPage);
      setMemberCursor(firstNext);
      setCurrentMemberCursor(undefined);
      setMemberStack([]);
      setOwner(viewer ? viewer.role === 'owner' : null);
      setState('ready');
      setInvitations([]);
      setInvitationCursor(null);
      setInvitationsError(false);
      setInvitationsLoaded(viewer !== undefined && viewer.role !== 'owner');
    }
    (async () => {
      setMemberBusy(false);
      setInvitationBusy(false);
      setState('loading');
      setOwner(null);
      let cursor: string | undefined;
      let discovered: Member | undefined;
      let firstNext: string | null = null;
      let firstPage: Member[] = [];
      for (let pageNumber = 0; pageNumber < DISCOVERY_PAGES; pageNumber++) {
        const page = await listMembers(householdId, cursor);
        if (stale()) return;
        if (page.kind !== 'ok') { fail(page); return; }
        if (pageNumber === 0) { firstPage = page.items; firstNext = page.nextCursor; }
        discovered = page.items.find((member) => member.userId === userId) ?? discovered;
        if (discovered || !page.nextCursor) break;
        cursor = page.nextCursor;
      }
      if (stale()) return;
      apply(firstPage, firstNext, discovered);
      const invitationRequest = ++invitationGenerationRef.current;
      // An owner beyond the discovery cap is settled by the owner-only invitations list.
      if (discovered?.role !== 'member') await loadInvitations('', invitationRequest, []);
    })();
    return () => { cancelled = true; memberPageGenerationRef.current += 1; };
  }, [householdId, userId, attempt, loadInvitations, mountedRef, onSignedOutRef, tRef, setState, setOwner, setNotice,
    memberGenerationRef, memberPageGenerationRef, invitationGenerationRef, setInvitationBusy, setInvitations, setInvitationCursor,
    setInvitationsError, setInvitationsLoaded, setMembers, setMemberCursor, setCurrentMemberCursor, setMemberStack, setMemberBusy]);
}
