import { useCallback, useEffect, useRef, useState } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import { createInvitation, listInvitations, listMembers, revokeInvitation, type Invitation, type Member } from './membersApi';

type State = 'loading' | 'ready' | 'error';
function newIdempotencyKey() {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) => byte.toString(16).padStart(2, '0')).join('');
}

export function MembersScreen({ householdId, userId, login, onSignedOut }: { householdId: string; userId: string; login: string; onSignedOut: () => void }) {
  const { t, locale } = useI18n();
  const [members, setMembers] = useState<Member[]>([]);
  const [memberCursor, setMemberCursor] = useState<string | null>(null);
  const [memberStack, setMemberStack] = useState<string[]>([]);
  const [currentMemberCursor, setCurrentMemberCursor] = useState<string | undefined>();
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [invitationCursor, setInvitationCursor] = useState<string | null>(null);
  const [invitationStack, setInvitationStack] = useState<string[]>([]);
  const [currentInvitationCursor, setCurrentInvitationCursor] = useState<string | undefined>();
  const [state, setState] = useState<State>('loading');
  const [owner, setOwner] = useState<boolean | null>(null);
  const [attempt, setAttempt] = useState(0);
  const onSignedOutRef = useRef(onSignedOut);
  useEffect(() => { onSignedOutRef.current = onSignedOut; }, [onSignedOut]);
  const mounted = useRef(false);
  const memberGeneration = useRef(0);
  const memberPageGeneration = useRef(0);
  const invitationGeneration = useRef(0);
  const unresolvedCreateKey = useRef<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [memberBusy, setMemberBusy] = useState(false);
  const [invitationBusy, setInvitationBusy] = useState(false);
  const memberHeading = useRef<HTMLHeadingElement>(null);
  const invitationHeading = useRef<HTMLHeadingElement>(null);
  const memberPageFocus = useRef<HTMLElement | null>(null);
  const invitationPageFocus = useRef<HTMLElement | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [invitationsError, setInvitationsError] = useState(false);
  const [invitationsLoaded, setInvitationsLoaded] = useState(false);
  const tRef = useRef(t);
  useEffect(() => { tRef.current = t; }, [t]);
  const [link, setLink] = useState<string | null>(null);
  const [linkInvitationId, setLinkInvitationId] = useState<string | null>(null);
  const [canCheckAgain, setCanCheckAgain] = useState(false);
  const [copied, setCopied] = useState(false);
  const [revokeId, setRevokeId] = useState<string | null>(null);
  const [recoveryInvitationId, setRecoveryInvitationId] = useState<string | null>(null);
  const noticeRef = useRef<HTMLDivElement>(null);
  const linkRef = useRef<HTMLInputElement>(null);
  const yesRefs = useRef(new Map<string, HTMLButtonElement>());
  const revokeRefs = useRef(new Map<string, HTMLButtonElement>());
  const restoreFocusId = useRef<string | null>(null);
  const pendingLinkFocus = useRef(false);

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; memberGeneration.current += 1; memberPageGeneration.current += 1; invitationGeneration.current += 1; };
  }, []);

  const loadInvitations = useCallback(async (cursor: string, generation: number, stack?: string[]): Promise<boolean> => {
    const page = await listInvitations(householdId, cursor || undefined);
    if (!mounted.current || invitationGeneration.current !== generation) return false;
    if (page.kind === 'unauthenticated') { onSignedOutRef.current(); return false; }
    if (page.kind === 'forbidden') { setOwner(false); setInvitations([]); setInvitationCursor(null); setInvitationStack([]); setCurrentInvitationCursor(undefined); setInvitationsError(false); setInvitationsLoaded(true); return false; }
    if (page.kind === 'rejected') { setInvitationsError(true); setInvitationsLoaded(true); setNotice(tRef.current('members.error.requestRefused')); return false; }
    if (page.kind === 'notFound') { setInvitationsError(true); setInvitationsLoaded(true); setNotice(tRef.current('members.error.householdNotFound')); return false; }
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
  }, [householdId]);

  useEffect(() => {
    const generation = ++memberGeneration.current;
    memberPageGeneration.current += 1;
    let cancelled = false;
    (async () => {
      setMemberBusy(false);
      setInvitationBusy(false);
      setState('loading');
      setOwner(null);
      let cursor: string | undefined;
      let discovered: Member | undefined;
      let next: string | null;
      let firstNext: string | null = null;
      let firstPage: Member[] = [];
      for (let pageNumber = 0; pageNumber < 20; pageNumber++) {
        const page = await listMembers(householdId, cursor);
        if (cancelled || memberGeneration.current !== generation) return;
        if (page.kind === 'unauthenticated') { if (mounted.current) onSignedOutRef.current(); return; }
        if (page.kind === 'notFound') { setState('error'); setNotice(tRef.current('members.error.householdNotFound')); return; }
        if (page.kind === 'rejected' || page.kind === 'forbidden') { setState('error'); setNotice(tRef.current('members.error.requestRefused')); return; }
        if (page.kind !== 'ok') { setState('error'); setNotice(null); return; }
        if (pageNumber === 0) { firstPage = page.items; firstNext = page.nextCursor; }
        discovered = page.items.find((m) => m.userId === userId) ?? discovered;
        next = page.nextCursor;
        if (discovered || !next) break;
        cursor = next;
      }
      if (cancelled || memberGeneration.current !== generation) return;
      setMembers(firstPage);
      setMemberCursor(firstNext);
      setCurrentMemberCursor(undefined);
      setMemberStack([]);
      const viewer = discovered;
      setOwner(viewer ? viewer.role === 'owner' : null);
      setState('ready');
      setInvitations([]);
      setInvitationCursor(null);
      setInvitationsError(false);
      setInvitationsLoaded(viewer !== undefined && viewer.role !== 'owner');
      const invitationRequest = ++invitationGeneration.current;
      // An owner beyond the discovery cap is settled by the owner-only invitations list.
      if (viewer?.role !== 'member') await loadInvitations('', invitationRequest, []);
    })();
    return () => { cancelled = true; memberPageGeneration.current += 1; };
  }, [householdId, userId, attempt, loadInvitations]);

  function refreshInvitations() {
    const generation = ++invitationGeneration.current;
    setInvitationBusy(false);
    invitationPageFocus.current = null;
    setInvitationStack([]);
    setCurrentInvitationCursor(undefined);
    setInvitationsLoaded(false);
    setInvitations([]);
    setInvitationCursor(null);
    void loadInvitations('', generation);
  }

  useEffect(() => {
    if (!memberPageFocus.current || memberBusy) return;
    const active = document.activeElement;
    if (active === memberPageFocus.current || active === document.body || active === null) memberHeading.current?.focus();
    memberPageFocus.current = null;
  }, [members, memberBusy]);
  useEffect(() => {
    if (!invitationPageFocus.current || invitationBusy) return;
    const active = document.activeElement;
    if (active === invitationPageFocus.current || active === document.body || active === null) invitationHeading.current?.focus();
    invitationPageFocus.current = null;
  }, [invitations, invitationBusy]);
  useEffect(() => {
    if (notice && notice !== t('members.invite.manualCopy')) noticeRef.current?.focus();
  }, [notice, t]);
  useEffect(() => {
    if (pendingLinkFocus.current && link) {
      pendingLinkFocus.current = false;
      linkRef.current?.focus();
    }
  }, [link]);
  useEffect(() => {
    if (restoreFocusId.current && revokeId === null) {
      const id = restoreFocusId.current;
      restoreFocusId.current = null;
      revokeRefs.current.get(id)?.focus();
    }
  }, [revokeId]);

  async function pageMembers(cursor: string | undefined, stack: string[], activated: HTMLElement) {
    if (memberBusy) return;
    const memberToken = memberGeneration.current;
    const pageToken = ++memberPageGeneration.current;
    memberPageFocus.current = activated;
    setMemberBusy(true);
    const result = await listMembers(householdId, cursor);
    if (!mounted.current || memberGeneration.current !== memberToken || memberPageGeneration.current !== pageToken) return;
    setMemberBusy(false);
    if (result.kind === 'unauthenticated') { memberPageFocus.current = null; onSignedOut(); }
    else if (result.kind === 'ok') { setMembers(result.items); setMemberCursor(result.nextCursor); setCurrentMemberCursor(cursor); setMemberStack(stack); }
    else {
      memberPageFocus.current = null;
      if (result.kind === 'notFound') setNotice(t('members.error.householdNotFound'));
      else if (result.kind === 'rejected' || result.kind === 'forbidden') setNotice(t('members.error.requestRefused'));
      else setNotice(t('members.error.action'));
    }
  }
  async function pageInvitations(cursor: string | undefined, stack: string[], activated: HTMLElement) {
    if (invitationBusy) return;
    const generation = ++invitationGeneration.current;
    invitationPageFocus.current = activated;
    setInvitationBusy(true);
    const loaded = await loadInvitations(cursor ?? '', generation, stack);
    if (mounted.current && invitationGeneration.current === generation) {
      setInvitationBusy(false);
      if (!loaded) invitationPageFocus.current = null;
    }
  }
  function nextMembers(event: React.MouseEvent<HTMLButtonElement>) { if (memberCursor) void pageMembers(memberCursor, [...memberStack, currentMemberCursor ?? ''], event.currentTarget); }
  function previousMembers(event: React.MouseEvent<HTMLButtonElement>) { const stack = [...memberStack]; const cursor = stack.pop() ?? ''; void pageMembers(cursor || undefined, stack, event.currentTarget); }
  function nextInvitations(event: React.MouseEvent<HTMLButtonElement>) { if (invitationCursor) void pageInvitations(invitationCursor, [...invitationStack, currentInvitationCursor ?? ''], event.currentTarget); }
  function previousInvitations(event: React.MouseEvent<HTMLButtonElement>) { const stack = [...invitationStack]; const cursor = stack.pop() ?? ''; void pageInvitations(cursor || undefined, stack, event.currentTarget); }
  async function invite(replay = false) {
    if (busy || owner !== true || !listReady) return;
    setBusy(true); setNotice(null); setLink(null); setLinkInvitationId(null); setCopied(false);
    const key = replay && unresolvedCreateKey.current ? unresolvedCreateKey.current : newIdempotencyKey();
    if (!replay) { unresolvedCreateKey.current = null; setCanCheckAgain(false); }
    const memberRequestGeneration = memberGeneration.current;
    const result = await createInvitation(householdId, key);
    if (!mounted.current || memberGeneration.current !== memberRequestGeneration) return;
    setBusy(false);
    if (result.kind === 'unauthenticated') { onSignedOut(); return; }
    if (result.kind === 'forbidden') { setOwner(false); setInvitations([]); setNotice(t('members.invite.ownerRequired')); unresolvedCreateKey.current = null; return; }
    if (result.kind === 'rejected' || result.kind === 'notFound') { setNotice(t(result.kind === 'rejected' ? 'members.error.requestRefused' : 'members.error.householdNotFound')); unresolvedCreateKey.current = null; setCanCheckAgain(false); return; }
    if (result.kind === 'created') {
      setRecoveryInvitationId(null);
      unresolvedCreateKey.current = null;
      setCanCheckAgain(false);
      setLink(`${window.location.origin}/invite#${result.invitation.token}`);
      setLinkInvitationId(result.invitation.id);
      pendingLinkFocus.current = true;
      setNotice(t('members.invite.created'));
      refreshInvitations();
      return;
    }
    if (result.kind === 'existing') {
      setRecoveryInvitationId(result.invitation.id);
      unresolvedCreateKey.current = null;
      setCanCheckAgain(false);
      setNotice(t('members.invite.existingNoLink'));
      refreshInvitations();
      return;
    }
    unresolvedCreateKey.current = key;
    setCanCheckAgain(true);
    setNotice(t('members.invite.unavailable'));
    refreshInvitations();
  }
  async function revoke(id: string) {
    if (busy) return;
    setBusy(true);
    const generation = memberGeneration.current;
    const result = await revokeInvitation(householdId, id);
    if (!mounted.current || memberGeneration.current !== generation) return;
    setBusy(false); setRevokeId(null);
    if (result === 'unauthenticated') { onSignedOut(); return; }
    if (result === 'forbidden') { setOwner(false); setInvitations([]); setNotice(t('members.invite.ownerRequired')); unresolvedCreateKey.current = null; setCanCheckAgain(false); return; }
    if (result === 'rejected') { setNotice(t('members.error.requestRefused')); return; }
    if (result === 'notFound') { setNotice(t('members.error.householdNotFound')); return; }
    if (result === 'ok') {
      if (linkInvitationId === id) { setLink(null); setLinkInvitationId(null); setCopied(false); }
      if (recoveryInvitationId === id) setRecoveryInvitationId(null);
      setNotice(t('members.invite.revoked'));
      restoreFocusId.current = null;
      noticeRef.current?.focus();
      refreshInvitations();
    } else setNotice(t('members.error.action'));
  }
  async function copyLink() {
    if (!link) return;
    try { await navigator.clipboard.writeText(link); setCopied(true); setNotice(t('members.invite.copied')); }
    catch { setNotice(t('members.invite.manualCopy')); window.setTimeout(() => { linkRef.current?.focus(); linkRef.current?.select(); }, 0); }
  }
  const formatDate = (date: string) => new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }).format(new Date(date));
  const listReady = state === 'ready' && (owner !== true || invitationsLoaded);
  return <Layout actions={<AccountActions login={login} onSignedOut={onSignedOut} />}><section className="settle">
    <Heading className="font-display text-h1 leading-tight">{t('members.title')}</Heading>
    <div ref={noticeRef} tabIndex={-1} role="status" className="mt-5 outline-none empty:mt-0">{notice && state !== 'error' ? <p className="rounded-xl bg-sand px-4 py-3">{notice}</p> : null}</div>
    {state === 'loading' ? <p role="status" className="mt-8 text-muted">{t('members.loading')}</p> : null}
    {state === 'error' ? <div className="mt-8 space-y-3"><p className="text-muted">{notice && notice !== t('members.error.householdNotFound') && notice !== t('members.error.requestRefused') ? notice : notice ?? t('members.error.load')}</p><Button onClick={() => { setNotice(null); setAttempt((n) => n + 1); }}>{t('app.error.retry')}</Button></div> : null}
    {state === 'ready' ? <div className="mt-8 space-y-8">
      <section><h2 ref={memberHeading} tabIndex={-1} className="font-display text-2xl outline-none">{t('members.list.title')}</h2><ul className="mt-3 divide-y divide-line rounded-2xl border border-line bg-white/70">{members.map((member) => <li key={member.userId} className="flex flex-wrap items-center justify-between gap-2 px-4 py-3"><span className="min-w-0 break-all">{member.login}{member.userId === userId ? ` (${t('members.you')})` : ''}</span><span className="text-sm text-muted">{t(member.role === 'owner' ? 'members.role.owner' : 'members.role.member')}</span></li>)}</ul>
      {memberStack.length > 0 || memberCursor ? <div className="mt-3 flex flex-wrap items-center justify-between gap-2">{memberStack.length ? <Button data-member-page variant="quiet" disabled={memberBusy} onClick={previousMembers}>{memberStack.length > 1 ? t('members.page.previous') : t('members.page.first')}</Button> : <span />}{memberCursor ? <Button data-member-page variant="quiet" disabled={memberBusy} onClick={nextMembers}>{t('members.page.next')}</Button> : null}<p aria-live="polite" className="w-full text-center text-sm text-muted">{t('members.page.number', { n: String(memberStack.length + 1) })}</p></div> : null}
      </section>
      {owner === true ? <section className="border-t border-line pt-6"><h2 className="font-display text-2xl">{t('members.invite.title')}</h2><p className="mt-2 text-muted">{t('members.invite.intro')}</p><Button className="mt-4 w-full whitespace-normal text-center sm:w-auto" disabled={busy || !listReady} onClick={() => void invite()}>{busy ? t('members.invite.creating') : t('members.invite.action')}</Button>
        {recoveryInvitationId ? <Button variant="quiet" className="mt-3" disabled={busy} onClick={() => void revoke(recoveryInvitationId)}>{t('members.invite.revokeRecovered')}</Button> : null}
        {canCheckAgain ? <Button variant="quiet" className="mt-3" disabled={busy || !listReady} onClick={() => void invite(true)}>{t('members.invite.checkAgain')}</Button> : null}
        {link ? <div className="mt-4 rounded-2xl border border-line bg-white/70 p-4"><p className="text-muted">{t('members.invite.share')}</p><div className="mt-3 flex flex-wrap gap-2"><input ref={linkRef} readOnly value={link} aria-label={t('members.invite.link')} className="min-h-11 min-w-0 flex-1 basis-48 rounded-xl border border-field bg-white px-3 text-sm break-all focus-visible:outline-2 focus-visible:outline-accent" /><Button variant="quiet" onClick={copyLink}>{copied ? t('members.invite.copiedButton') : t('members.invite.copy')}</Button></div><p className="mt-3 text-sm text-muted">{t('members.invite.caveat')}</p></div> : null}
        <h3 ref={invitationHeading} tabIndex={-1} className="mt-8 font-display text-xl outline-none">{t('members.invitations.title')}</h3>
        {!listReady ? <p role="status" className="mt-2 text-muted">{t('members.loading')}</p> : invitationsError ? <div className="mt-2"><p className="text-muted">{notice === t('members.error.requestRefused') ? notice : t('members.invitations.error')}</p><Button variant="quiet" className="mt-2" onClick={refreshInvitations}>{t('app.error.retry')}</Button></div> : invitations.length ? <ul className="mt-3 divide-y divide-line rounded-2xl border border-line bg-white/70">{invitations.map((inv) => <li key={inv.id} className="flex flex-wrap items-start justify-between gap-3 p-4"><div className="min-w-0"><p>{t(`members.invitation.status.${inv.status}`)}</p><p className="mt-1 text-sm text-muted">{t('members.invitation.created', { date: formatDate(inv.createdAt) })} · {t('members.invitation.expires', { date: formatDate(inv.expiresAt) })}</p></div>{inv.status === 'pending' ? revokeId === inv.id ? <div className="flex min-h-11 items-center gap-2"><span>{t('members.invitation.revokeConfirm')}</span><Button ref={(element) => { if (element) yesRefs.current.set(inv.id, element); else yesRefs.current.delete(inv.id); }} variant="quiet" size="small" disabled={busy} onClick={() => void revoke(inv.id)}>{t('members.invitation.yes')}</Button><Button variant="quiet" size="small" onClick={() => { restoreFocusId.current = inv.id; setRevokeId(null); }}>{t('members.invitation.cancel')}</Button></div> : <Button ref={(element) => { if (element) revokeRefs.current.set(inv.id, element); else revokeRefs.current.delete(inv.id); }} variant="quiet" size="small" disabled={busy} onClick={() => { setRevokeId(inv.id); window.setTimeout(() => yesRefs.current.get(inv.id)?.focus(), 0); }}>{t('members.invitation.revoke')}</Button> : null}</li>)}</ul> : <p className="mt-2 text-muted">{t('members.invitations.empty')}</p>}
        {listReady && (invitationStack.length > 0 || invitationCursor) ? <div className="mt-3 flex flex-wrap items-center justify-between gap-2">{invitationStack.length ? <Button data-invitation-page variant="quiet" disabled={invitationBusy} onClick={previousInvitations}>{invitationStack.length > 1 ? t('members.page.previous') : t('members.page.first')}</Button> : <span />}{invitationCursor ? <Button data-invitation-page variant="quiet" disabled={invitationBusy} onClick={nextInvitations}>{t('members.page.next')}</Button> : null}<p aria-live="polite" className="w-full text-center text-sm text-muted">{t('members.page.number', { n: String(invitationStack.length + 1) })}</p></div> : null}
      </section> : owner === false ? <p className="border-t border-line pt-5 text-muted">{t('members.ownerOnly')}</p>
      : invitationsError ? <div className="border-t border-line pt-5"><p className="text-muted">{notice === t('members.error.requestRefused') ? notice : t('members.invitations.error')}</p><Button variant="quiet" className="mt-2" onClick={refreshInvitations}>{t('app.error.retry')}</Button></div>
      : <p role="status" className="border-t border-line pt-5 text-muted">{t('members.loading')}</p>}
    </div> : null}
  </section></Layout>;
}
