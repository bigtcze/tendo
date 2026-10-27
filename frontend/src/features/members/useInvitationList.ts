import { useCallback, useRef, useState } from 'react';
import { listInvitations, type Invitation, type PageResult } from './membersApi';
import { nextPage, previousPage } from './membersModel';
import type { MembersCore } from './useMembersCore';

export function useInvitationList(householdId: string, core: MembersCore) {
  const { mountedRef, invitationGenerationRef, onSignedOutRef, tRef, setOwner, setNotice } = core;
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [invitationCursor, setInvitationCursor] = useState<string | null>(null);
  const [invitationStack, setInvitationStack] = useState<string[]>([]);
  const [currentInvitationCursor, setCurrentInvitationCursor] = useState<string | undefined>();
  const [invitationBusy, setInvitationBusy] = useState(false);
  const [invitationsError, setInvitationsError] = useState(false);
  const [invitationsLoaded, setInvitationsLoaded] = useState(false);
  const invitationHeadingRef = useRef<HTMLHeadingElement>(null);
  const invitationPageFocusRef = useRef<HTMLElement | null>(null);

  const applyPage = useCallback((page: PageResult<Invitation>, cursor: string, stack?: string[]): boolean => {
    if (page.kind === 'unauthenticated') { onSignedOutRef.current(); return false; }
    if (page.kind === 'forbidden') {
      setOwner(false); setInvitations([]); setInvitationCursor(null); setInvitationStack([]); setCurrentInvitationCursor(undefined);
      setInvitationsError(false); setInvitationsLoaded(true); return false;
    }
    if (page.kind === 'rejected' || page.kind === 'notFound') {
      setInvitationsError(true);
      setInvitationsLoaded(true);
      setNotice(tRef.current(page.kind === 'rejected' ? 'members.error.requestRefused' : 'members.error.householdNotFound'));
      return false;
    }
    if (page.kind === 'ok') {
      // Only owners can list invitations, so a successful list also settles an unknown role.
      setOwner(true);
      setInvitations(page.items);
      setInvitationCursor(page.nextCursor);
      setCurrentInvitationCursor(cursor || undefined);
      setInvitationStack(stack ?? []);
      setInvitationsError(false);
      setInvitationsLoaded(true);
      return true;
    }
    setInvitationsError(true);
    setInvitationsLoaded(true);
    return false;
  }, [onSignedOutRef, tRef, setOwner, setNotice]);

  const loadInvitations = useCallback(async (cursor: string, generation: number, stack?: string[]): Promise<boolean> => {
    const page = await listInvitations(householdId, cursor || undefined);
    if (!mountedRef.current || invitationGenerationRef.current !== generation) return false;
    return applyPage(page, cursor, stack);
    // Every input except householdId is a stable ref or state setter, so identity changes only with householdId.
  }, [householdId, mountedRef, invitationGenerationRef, applyPage]);

  function refreshInvitations() {
    const generation = ++invitationGenerationRef.current;
    setInvitationBusy(false);
    invitationPageFocusRef.current = null;
    setInvitationStack([]);
    setCurrentInvitationCursor(undefined);
    setInvitationsLoaded(false);
    // Clearing the error swaps the retry button for the loading status, so a pending retry cannot be repeated.
    setInvitationsError(false);
    setInvitations([]);
    setInvitationCursor(null);
    void loadInvitations('', generation);
  }

  async function pageInvitations(cursor: string | undefined, stack: string[], activated: HTMLElement) {
    if (invitationBusy) return;
    const generation = ++invitationGenerationRef.current;
    invitationPageFocusRef.current = activated;
    setInvitationBusy(true);
    const loaded = await loadInvitations(cursor ?? '', generation, stack);
    if (mountedRef.current && invitationGenerationRef.current === generation) {
      setInvitationBusy(false);
      if (!loaded) invitationPageFocusRef.current = null;
    }
  }
  function nextInvitations(event: React.MouseEvent<HTMLButtonElement>) {
    if (!invitationCursor) return;
    const page = nextPage(invitationStack, currentInvitationCursor, invitationCursor);
    void pageInvitations(page.cursor, page.stack, event.currentTarget);
  }
  function previousInvitations(event: React.MouseEvent<HTMLButtonElement>) {
    const page = previousPage(invitationStack);
    void pageInvitations(page.cursor, page.stack, event.currentTarget);
  }

  return {
    invitations, setInvitations, invitationCursor, setInvitationCursor, invitationStack, invitationBusy, setInvitationBusy,
    invitationsError, setInvitationsError, invitationsLoaded, setInvitationsLoaded, invitationHeadingRef, invitationPageFocusRef,
    loadInvitations, refreshInvitations, nextInvitations, previousInvitations,
  };
}

export type InvitationList = ReturnType<typeof useInvitationList>;
