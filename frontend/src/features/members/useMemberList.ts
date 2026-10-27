import { useRef, useState } from 'react';
import type { Translate } from '../../i18n';
import { listMembers, type Member, type PageResult } from './membersApi';
import { nextPage, previousPage } from './membersModel';
import type { MembersCore } from './useMembersCore';
import type { InvitationList } from './useInvitationList';
import { useMembersDiscovery } from './useMembersDiscovery';

function memberPageNotice(result: PageResult<Member>, t: Translate) {
  if (result.kind === 'notFound') return t('members.error.householdNotFound');
  if (result.kind === 'rejected' || result.kind === 'forbidden') return t('members.error.requestRefused');
  return t('members.error.action');
}

export function useMemberList(householdId: string, userId: string, onSignedOut: () => void, t: Translate, core: MembersCore, inv: InvitationList) {
  const { mountedRef, memberGenerationRef, memberPageGenerationRef, setNotice } = core;
  const [members, setMembers] = useState<Member[]>([]);
  const [memberCursor, setMemberCursor] = useState<string | null>(null);
  const [memberStack, setMemberStack] = useState<string[]>([]);
  const [currentMemberCursor, setCurrentMemberCursor] = useState<string | undefined>();
  const [memberBusy, setMemberBusy] = useState(false);
  const memberHeadingRef = useRef<HTMLHeadingElement>(null);
  const memberPageFocusRef = useRef<HTMLElement | null>(null);

  useMembersDiscovery(householdId, userId, core, inv, { setMembers, setMemberCursor, setCurrentMemberCursor, setMemberStack, setMemberBusy });

  async function pageMembers(cursor: string | undefined, stack: string[], activated: HTMLElement) {
    if (memberBusy) return;
    const memberToken = memberGenerationRef.current;
    const pageToken = ++memberPageGenerationRef.current;
    memberPageFocusRef.current = activated;
    setMemberBusy(true);
    const result = await listMembers(householdId, cursor);
    if (!mountedRef.current || memberGenerationRef.current !== memberToken || memberPageGenerationRef.current !== pageToken) return;
    setMemberBusy(false);
    if (result.kind === 'unauthenticated') { memberPageFocusRef.current = null; onSignedOut(); }
    else if (result.kind === 'ok') { setMembers(result.items); setMemberCursor(result.nextCursor); setCurrentMemberCursor(cursor); setMemberStack(stack); }
    else { memberPageFocusRef.current = null; setNotice(memberPageNotice(result, t)); }
  }
  function nextMembers(event: React.MouseEvent<HTMLButtonElement>) {
    if (!memberCursor) return;
    const page = nextPage(memberStack, currentMemberCursor, memberCursor);
    void pageMembers(page.cursor, page.stack, event.currentTarget);
  }
  function previousMembers(event: React.MouseEvent<HTMLButtonElement>) {
    const page = previousPage(memberStack);
    void pageMembers(page.cursor, page.stack, event.currentTarget);
  }

  return { members, memberCursor, memberStack, memberBusy, memberHeadingRef, memberPageFocusRef, nextMembers, previousMembers };
}
