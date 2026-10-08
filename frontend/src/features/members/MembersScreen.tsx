import { useCallback, useEffect, useRef, useState } from 'react';
import { AccountActions } from '../../app/AccountActions';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { NavLink } from '../../app/NavLink';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import { createInvitation, listInvitations, listMembers, revokeInvitation, type Invitation, type Member } from './membersApi';

type State = 'loading' | 'ready' | 'error';
function newIdempotencyKey() {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) => byte.toString(16).padStart(2, '0')).join('');
}

export function MembersScreen({ householdId, userId, login, onBack, onSignedOut }: { householdId: string; userId: string; login: string; onBack: () => void; onSignedOut: () => void }) {
  const { t, locale } = useI18n();
  const [members, setMembers] = useState<Member[]>([]);
  const [memberCursor, setMemberCursor] = useState<string | null>(null);
  const [invitations, setInvitations] = useState<Invitation[]>([]);
  const [invitationCursor, setInvitationCursor] = useState<string | null>(null);
  const [state, setState] = useState<State>('loading');
  const [owner, setOwner] = useState<boolean | null>(null);
  const [attempt, setAttempt] = useState(0);
  const onSignedOutRef = useRef(onSignedOut);
  useEffect(() => { onSignedOutRef.current = onSignedOut; }, [onSignedOut]);
  const mounted = useRef(false);
  const memberGeneration = useRef(0);
  const invitationGeneration = useRef(0);
  const unresolvedCreateKey = useRef<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [moreBusy, setMoreBusy] = useState(false);
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
    return () => { mounted.current = false; memberGeneration.current += 1; invitationGeneration.current += 1; };
  }, []);

  const loadInvitations = useCallback(async (cursor: string, generation: number) => {
    const page = await listInvitations(householdId, cursor || undefined);
    if (!mounted.current || invitationGeneration.current !== generation) return;
    if (page.kind === 'unauthenticated') { onSignedOutRef.current(); return; }
    if (page.kind === 'forbidden') { setOwner(false); setInvitations([]); setInvitationCursor(null); setInvitationsError(false); setInvitationsLoaded(true); return; }
    if (page.kind === 'rejected') { setInvitationsError(true); setInvitationsLoaded(true); setNotice(tRef.current('members.error.requestRefused')); return; }
    if (page.kind === 'notFound') { setInvitationsError(true); setInvitationsLoaded(true); setNotice(tRef.current('members.error.householdNotFound')); return; }
    if (page.kind === 'ok') {
      setInvitations((current) => cursor ? [...current, ...page.items.filter((item) => !current.some((old) => old.id === item.id))] : page.items);
      setInvitationCursor(page.nextCursor);
      setInvitationsError(false);
      setInvitationsLoaded(true);
      return;
    }
    setInvitationsError(true);
    setInvitationsLoaded(true);
  }, [householdId]);

  useEffect(() => {
    const generation = ++memberGeneration.current;
    let cancelled = false;
    (async () => {
      setState('loading');
      setOwner(null);
      let cursor: string | undefined;
      let all: Member[] = [];
      let next: string | null;
      do {
        const page = await listMembers(householdId, cursor);
        if (cancelled || memberGeneration.current !== generation) return;
        if (page.kind === 'unauthenticated') { if (mounted.current) onSignedOutRef.current(); return; }
        if (page.kind === 'notFound') { setState('error'); setNotice(tRef.current('members.error.householdNotFound')); return; }
        if (page.kind === 'rejected') { setState('error'); setNotice(tRef.current('members.error.requestRefused')); return; }
        if (page.kind !== 'ok') { setState('error'); setNotice(null); return; }
        all = [...all, ...page.items];
        next = page.nextCursor;
        if (all.some((m) => m.userId === userId) || !next) break;
        cursor = next;
      } while (!cancelled);
      if (cancelled || memberGeneration.current !== generation) return;
      setMembers(all);
      setMemberCursor(next);
      const viewer = all.find((m) => m.userId === userId);
      setOwner(viewer?.role === 'owner');
      setState('ready');
      setInvitations([]);
      setInvitationCursor(null);
      setInvitationsError(false);
      setInvitationsLoaded(viewer?.role !== 'owner');
      const invitationRequest = ++invitationGeneration.current;
      if (viewer?.role === 'owner') await loadInvitations('', invitationRequest);
      else setInvitationsLoaded(true);
    })();
    return () => { cancelled = true; };
  }, [householdId, userId, attempt, onSignedOut, loadInvitations]);

  function refreshInvitations() {
    const generation = ++invitationGeneration.current;
    setMoreBusy(false);
    setInvitationsLoaded(false);
    setInvitationCursor(null);
    void loadInvitations('', generation);
  }

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

  async function moreMembers() {
    if (!memberCursor || moreBusy) return;
    const generation = memberGeneration.current;
    setMoreBusy(true);
    const result = await listMembers(householdId, memberCursor);
    if (!mounted.current || memberGeneration.current !== generation) return;
    setMoreBusy(false);
    if (result.kind === 'unauthenticated') onSignedOut();
    else if (result.kind === 'ok') { setMembers((current) => [...current, ...result.items.filter((m) => !current.some((x) => x.userId === m.userId))]); setMemberCursor(result.nextCursor); }
    else if (result.kind === 'notFound') setNotice(t('members.error.householdNotFound'));
    else if (result.kind === 'rejected') setNotice(t('members.error.requestRefused'));
    else setNotice(t('members.error.action'));
  }
  async function moreInvitations() {
    if (!invitationCursor || moreBusy) return;
    const cursor = invitationCursor;
    const generation = invitationGeneration.current;
    setMoreBusy(true);
    await loadInvitations(cursor, generation);
    if (mounted.current && invitationGeneration.current === generation) setMoreBusy(false);
  }
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
    <NavLink href="/" onNavigate={onBack} className="-ml-3 inline-flex min-h-11 items-center rounded-xl px-3 text-muted hover:bg-sand hover:text-ink focus-visible:outline-2 focus-visible:outline-accent">{t('members.backHome')}</NavLink>
    <Heading className="mt-2 font-display text-3xl leading-tight sm:text-4xl">{t('members.title')}</Heading>
    <div ref={noticeRef} tabIndex={-1} role="status" className="mt-5 outline-none empty:mt-0">{notice && state !== 'error' ? <p className="rounded-xl bg-sand px-4 py-3">{notice}</p> : null}</div>
    {state === 'loading' ? <p role="status" className="mt-8 text-muted">{t('members.loading')}</p> : null}
    {state === 'error' ? <div className="mt-8 space-y-3"><p className="text-muted">{notice && notice !== t('members.error.householdNotFound') && notice !== t('members.error.requestRefused') ? notice : notice ?? t('members.error.load')}</p><Button onClick={() => { setNotice(null); setAttempt((n) => n + 1); }}>{t('app.error.retry')}</Button></div> : null}
    {state === 'ready' ? <div className="mt-8 space-y-8">
      <section><h2 className="font-display text-2xl">{t('members.list.title')}</h2><ul className="mt-3 divide-y divide-line rounded-2xl border border-line bg-white/70">{members.map((member) => <li key={member.userId} className="flex flex-wrap items-center justify-between gap-2 px-4 py-3"><span className="min-w-0 break-all">{member.login}{member.userId === userId ? ` (${t('members.you')})` : ''}</span><span className="text-sm text-muted">{t(member.role === 'owner' ? 'members.role.owner' : 'members.role.member')}</span></li>)}</ul>
      {memberCursor ? <Button variant="quiet" className="mt-3" disabled={moreBusy} onClick={moreMembers}>{moreBusy ? t('members.moreLoading') : t('members.more')}</Button> : null}
      </section>
      {owner === true ? <section className="border-t border-line pt-6"><h2 className="font-display text-2xl">{t('members.invite.title')}</h2><p className="mt-2 text-muted">{t('members.invite.intro')}</p><Button className="mt-4 w-full sm:w-auto" disabled={busy || !listReady} onClick={() => void invite()}>{busy ? t('members.invite.creating') : t('members.invite.action')}</Button>
        {recoveryInvitationId ? <Button variant="quiet" className="mt-3" disabled={busy} onClick={() => void revoke(recoveryInvitationId)}>{t('members.invite.revokeRecovered')}</Button> : null}
        {canCheckAgain ? <Button variant="quiet" className="mt-3" disabled={busy || !listReady} onClick={() => void invite(true)}>{t('members.invite.checkAgain')}</Button> : null}
        {link ? <div className="mt-4 rounded-2xl border border-line bg-white/70 p-4"><p className="text-muted">{t('members.invite.share')}</p><div className="mt-3 flex flex-wrap gap-2"><input ref={linkRef} readOnly value={link} aria-label={t('members.invite.link')} className="min-h-11 min-w-0 flex-1 basis-48 rounded-xl border border-line bg-white px-3 text-sm break-all focus-visible:outline-2 focus-visible:outline-accent" /><Button variant="quiet" onClick={copyLink}>{copied ? t('members.invite.copiedButton') : t('members.invite.copy')}</Button></div><p className="mt-3 text-sm text-muted">{t('members.invite.caveat')}</p></div> : null}
        <h3 className="mt-8 font-display text-xl">{t('members.invitations.title')}</h3>
        {!listReady ? <p role="status" className="mt-2 text-muted">{t('members.loading')}</p> : invitationsError ? <div className="mt-2"><p className="text-muted">{notice === t('members.error.requestRefused') ? notice : t('members.invitations.error')}</p><Button variant="quiet" className="mt-2" onClick={refreshInvitations}>{t('app.error.retry')}</Button></div> : invitations.length ? <ul className="mt-3 divide-y divide-line rounded-2xl border border-line bg-white/70">{invitations.map((inv) => <li key={inv.id} className="flex flex-wrap items-start justify-between gap-3 p-4"><div className="min-w-0"><p>{t(`members.invitation.status.${inv.status}`)}</p><p className="mt-1 text-sm text-muted">{t('members.invitation.created', { date: formatDate(inv.createdAt) })} · {t('members.invitation.expires', { date: formatDate(inv.expiresAt) })}</p></div>{inv.status === 'pending' ? revokeId === inv.id ? <div className="flex min-h-11 items-center gap-2"><span>{t('members.invitation.revokeConfirm')}</span><Button ref={(element) => { if (element) yesRefs.current.set(inv.id, element); else yesRefs.current.delete(inv.id); }} variant="quiet" size="small" disabled={busy} onClick={() => void revoke(inv.id)}>{t('members.invitation.yes')}</Button><Button variant="quiet" size="small" onClick={() => { restoreFocusId.current = inv.id; setRevokeId(null); }}>{t('members.invitation.cancel')}</Button></div> : <Button ref={(element) => { if (element) revokeRefs.current.set(inv.id, element); else revokeRefs.current.delete(inv.id); }} variant="quiet" size="small" disabled={busy} onClick={() => { setRevokeId(inv.id); window.setTimeout(() => yesRefs.current.get(inv.id)?.focus(), 0); }}>{t('members.invitation.revoke')}</Button> : null}</li>)}</ul> : <p className="mt-2 text-muted">{t('members.invitations.empty')}</p>}
        {listReady && invitationCursor ? <Button variant="quiet" className="mt-3" disabled={moreBusy} onClick={moreInvitations}>{moreBusy ? t('members.moreLoading') : t('members.more')}</Button> : null}
      </section> : owner === false ? <p className="border-t border-line pt-5 text-muted">{t('members.ownerOnly')}</p> : null}
    </div> : null}
  </section></Layout>;
}
