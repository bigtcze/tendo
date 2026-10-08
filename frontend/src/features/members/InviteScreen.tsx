import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { Button } from '../../components/ui/button';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { useI18n } from '../../i18n';
import { fetchSession, login, logout, type Session } from '../auth/sessionApi';
import { acceptInvitationAsCurrentUser, acceptNewAccount } from './membersApi';

type ErrorState = 'invalid' | 'loginTaken' | 'login' | 'loginInvalid' | 'password' | 'passwordInvalid' | 'rateLimited' | 'unavailable' | 'alreadyMember' | 'householdConflict';
type AcceptState = 'idle' | 'accepted';

export function InviteScreen({ token, session, onSignedIn, onSignedOut, onAccountReady, onJoinedSignedOut, focusLogin = false }:  { token: string | null; session?: Session; onSignedIn: (session: Session) => void; onSignedOut: () => void; onAccountReady: () => void; onJoinedSignedOut: () => void; focusLogin?: boolean }) {
  const { t } = useI18n();
  const [loginName, setLoginName] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<ErrorState | null>(null);
  const [pending, setPending] = useState(false);
  const [signedOutMode, setSignedOutMode] = useState(!session);
  const [invalidLink, setInvalidLink] = useState(false);
  const [acceptState, setAcceptState] = useState<AcceptState>('idle');
  const mounted = useRef(false);
  const loginRef = useRef<HTMLInputElement>(null);
  const passwordRef = useRef<HTMLInputElement>(null);
  const noticeRef = useRef<HTMLDivElement>(null);
  const valid = token !== null && /^[A-Za-z0-9_-]{43}$/.test(token);
  const currentSession = session && !signedOutMode ? session : undefined;
  const fieldError = error === 'login' || error === 'loginTaken' || error === 'loginInvalid' || error === 'password' || error === 'passwordInvalid';
  const key = error === 'loginInvalid' ? 'loginValidation' : error === 'passwordInvalid' ? 'passwordValidation' : error;
  const message = error ? t(`invite.error.${key}` as never) : '';
  useEffect(() => {
    mounted.current = true;
    if (focusLogin) loginRef.current?.focus();
    return () => { mounted.current = false; };
  }, [focusLogin]);
  useEffect(() => {
    if ((error && !fieldError) || acceptState === 'accepted') noticeRef.current?.focus();
  }, [error, fieldError, acceptState]);
  function fail(next: ErrorState) {
    setError(next); setPending(false);
    if (next === 'login' || next === 'loginTaken' || next === 'loginInvalid') loginRef.current?.focus();
    if (next === 'password' || next === 'passwordInvalid') passwordRef.current?.focus();
    if (next === 'invalid') setInvalidLink(true);
  }
  async function retrySession() {
    if (pending || acceptState !== 'accepted' || !mounted.current) return;
    setPending(true);
    const refreshed = await fetchSession();
    if (!mounted.current) return;
    setPending(false);
    if (refreshed.kind === 'signedIn') { onSignedIn(refreshed.session); return; }
    if (refreshed.kind === 'signedOut') { onJoinedSignedOut(); return; }
    noticeRef.current?.focus();
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (pending || !valid || !token) return;
    setPending(true); setError(null);
    if (!currentSession) {
      if (!loginName) { fail('login'); return; }
      if (!password) { fail('password'); return; }
      const result = await acceptNewAccount(token, loginName, password);
      if (!mounted.current) return;
      if (result.kind === 'ok') {
        const signedIn = await login(loginName, password);
        if (!mounted.current) return;
        if (signedIn.kind === 'ok') { onSignedIn(signedIn.session); return; }
        onAccountReady(); return;
      }
      if (result.kind === 'invalid') fail('invalid');
      else if (result.kind === 'loginTaken') fail('loginTaken');
      else if (result.kind === 'invalidField') fail(result.field === 'password' ? 'passwordInvalid' : 'loginInvalid');
      else if (result.kind === 'rateLimited') fail('rateLimited');
      else fail('unavailable');
      return;
    }
    const result = await acceptInvitationAsCurrentUser(token);
    if (!mounted.current) return;
    if (result === 'ok') {
      const refreshed = await fetchSession();
      if (!mounted.current) return;
      if (refreshed.kind === 'signedIn') { onSignedIn(refreshed.session); return; }
      if (refreshed.kind === 'signedOut') { onJoinedSignedOut(); return; }
      setAcceptState('accepted'); setPending(false); setError(null); return;
    }
    if (result === 'unauthenticated') { setSignedOutMode(true); setPending(false); onSignedOut(); return; }
    if (result === 'invalid') fail('invalid');
    else if (result === 'alreadyMember') fail('alreadyMember');
    else if (result === 'householdConflict') { setError('householdConflict'); setPending(false); }
    else if (result === 'rateLimited') fail('rateLimited');
    else fail('unavailable');
  }
  async function signOutAndContinue() {
    if (pending) return;
    setPending(true);
    const result = await logout();
    if (!mounted.current) return;
    if (result === 'ok') { setSignedOutMode(true); setError(null); setPending(false); onSignedOut(); }
    else fail('unavailable');
  }
  return <Layout><section className="settle">
    {valid ? <>
      <Heading className={`font-display text-3xl leading-tight sm:text-4xl ${currentSession ? '[overflow-wrap:anywhere]' : ''}`}>{currentSession ? t('invite.signedIn.title', { login: currentSession.login }) : t('invite.title')}</Heading>
      {acceptState === 'accepted' ? <div ref={noticeRef} tabIndex={-1} role="status" className="mt-5 rounded-xl bg-sand px-4 py-3 outline-none">{t('invite.error.accepted')}</div> : error && !fieldError ? <div ref={noticeRef} tabIndex={-1} role="status" className="mt-5 rounded-xl bg-sand px-4 py-3 outline-none">{message}</div> : null}
      {error === 'alreadyMember' || error === 'invalid' ? <a href="/" className="mt-4 inline-flex min-h-11 items-center rounded-xl px-3 text-muted hover:bg-sand">{t('invite.home')}</a> : null}
      {acceptState === 'accepted' ? <Button className="mt-6" disabled={pending} onClick={() => void retrySession()}>{t('invite.continue')}</Button> : currentSession ? !invalidLink ? <form onSubmit={submit} className="mt-6"><Button type="submit" disabled={pending}>{pending ? t('invite.joining') : t('invite.join')}</Button>{error === 'householdConflict' ? <Button type="button" variant="quiet" className="ml-2" disabled={pending} onClick={() => void signOutAndContinue()}>{t('invite.signOutContinue')}</Button> : null}</form> : null : !invalidLink ? <form onSubmit={submit} className="mt-8 space-y-5" noValidate>
        <div className="space-y-2"><Label htmlFor="invite-login">{t('onboarding.login')}</Label><Input ref={loginRef} id="invite-login" autoComplete="username" autoCapitalize="none" spellCheck={false} required value={loginName} onChange={(e) => setLoginName(e.target.value)} aria-invalid={error === 'login' || error === 'loginTaken' || error === 'loginInvalid'} aria-describedby={`invite-login-hint${error === 'login' || error === 'loginTaken' || error === 'loginInvalid' ? ' invite-error' : ''}`}/><p id="invite-login-hint" className="text-sm text-muted">{t('onboarding.login.hint')}</p></div>
        <div className="space-y-2"><Label htmlFor="invite-password">{t('onboarding.password')}</Label><Input ref={passwordRef} id="invite-password" type="password" autoComplete="new-password" required value={password} onChange={(e) => setPassword(e.target.value)} aria-invalid={error === 'password' || error === 'passwordInvalid'} aria-describedby={`invite-password-hint${error === 'password' || error === 'passwordInvalid' ? ' invite-error' : ''}`}/><p id="invite-password-hint" className="text-sm text-muted">{t('onboarding.password.hint')}</p></div>
        {fieldError ? <div id="invite-error" role="alert" className="text-base text-danger">{message}</div> : null}
        <Button type="submit" disabled={pending} className="w-full sm:w-auto">{pending ? t('invite.creating') : t('invite.create')}</Button>
      </form> : null}
    </> : <><Heading className="font-display text-3xl leading-tight sm:text-4xl">{t('invite.incomplete')}</Heading><p className="mt-3 text-muted">{t('invite.incomplete.body')}</p><a href="/" className="mt-4 inline-flex min-h-11 items-center rounded-xl px-3 text-muted hover:bg-sand">{t('invite.home')}</a></>}
  </section></Layout>;
}
