import type { FormEvent, RefObject } from 'react';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { Button } from '../../components/ui/button';
import { Eyebrow } from '../../components/ui/eyebrow';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { useI18n, type Translate } from '../../i18n';
import { inviteErrorMessageKey, isFieldError, isLoginError, isPasswordError, type ErrorState } from './inviteAcceptModel';
import { useInviteAccept } from './useInviteAccept';
import { InviteProviderSignIn } from './InviteProviderSignIn';
import type { Session } from '../auth/sessionApi';

type ScreenProps = {
  token: string | null;
  session?: Session;
  onSignedIn: (session: Session) => void;
  onSignedOut: () => void;
  onAccountReady: () => void;
  onJoinedSignedOut: () => void;
  focusLogin?: boolean;
};

type FormProps = {
  t: Translate;
  pending: boolean;
  loginName: string;
  setLoginName: (value: string) => void;
  password: string;
  setPassword: (value: string) => void;
  error: ErrorState | null;
  loginRef: RefObject<HTMLInputElement | null>;
  passwordRef: RefObject<HTMLInputElement | null>;
};

function InviteEyebrow({ t }: { t: Translate }) {
  return <Eyebrow>{t('invite.eyebrow')}</Eyebrow>;
}

function InviteNotice({ error, accepted, noticeRef, t }: {
  error: ErrorState | null; accepted: boolean; noticeRef: RefObject<HTMLDivElement | null>; t: Translate;
}) {
  let message: string;
  if (accepted) message = t('invite.error.accepted');
  else if (error && !isFieldError(error)) message = t(inviteErrorMessageKey(error));
  else return null;
  const tone = accepted ? 'border-line bg-sand text-ink' : 'border-danger bg-danger-bg text-danger';
  return <div ref={noticeRef} tabIndex={-1} role="status" className={`mt-5 rounded-xl border px-4 py-3 text-small outline-none ${tone}`}>{message}</div>;
}

function NewAccountForm(props: FormProps & { onSubmit: (event: FormEvent) => void }) {
  const { t, pending, loginName, setLoginName, password, setPassword, error, loginRef, passwordRef, onSubmit } = props;
  const loginInvalid = isLoginError(error);
  const passwordInvalid = isPasswordError(error);
  const message = error && isFieldError(error) ? t(inviteErrorMessageKey(error)) : '';
  return <form onSubmit={onSubmit} className="space-y-5" noValidate>
    <div className="space-y-2"><Label htmlFor="invite-login">{t('invite.login')}</Label>
      <Input ref={loginRef} id="invite-login" autoComplete="username" autoCapitalize="none" spellCheck={false} required value={loginName}
        onChange={(event) => setLoginName(event.target.value)} aria-invalid={loginInvalid}
        aria-describedby={`invite-login-hint${loginInvalid ? ' invite-error' : ''}`} />
      <p id="invite-login-hint" className="text-small text-muted">{t('onboarding.login.hint')}</p>
    </div>
    <div className="space-y-2"><Label htmlFor="invite-password">{t('onboarding.password')}</Label>
      <Input ref={passwordRef} id="invite-password" type="password" autoComplete="new-password" required value={password}
        onChange={(event) => setPassword(event.target.value)} aria-invalid={passwordInvalid}
        aria-describedby={`invite-password-hint${passwordInvalid ? ' invite-error' : ''}`} />
      <p id="invite-password-hint" className="text-small text-muted">{t('onboarding.password.hint')}</p>
    </div>
    {isFieldError(error) ? <div id="invite-error" role="alert" className="rounded-xl border border-danger bg-danger-bg px-4 py-3 text-small text-danger">{message}</div> : null}
    <Button type="submit" disabled={pending} className="w-full sm:w-auto">{pending ? t('invite.creating') : t('invite.create')}</Button>
  </form>;
}

function SignedInForm({ pending, onSubmit, t }: {
  pending: boolean; onSubmit: (event: FormEvent) => void; t: Translate;
}) {
  return <form onSubmit={onSubmit}>
    <Button type="submit" disabled={pending} className="w-full whitespace-normal text-center sm:w-auto">
      {pending ? t('invite.joining') : t('invite.join')}
    </Button>
  </form>;
}

function HomeLink({ t }: { t: Translate }) {
  return <a href="/" className="mt-4 inline-flex min-h-11 items-center rounded-xl px-3 text-small font-semibold text-muted hover:bg-sand hover:text-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus">{t('invite.home')}</a>;
}

function IncompleteInvite({ t }: { t: Translate }) {
  return <div className="rounded-2xl border border-line bg-raised p-5 sm:p-7">
    <Heading className="text-[2.125rem] font-bold leading-[1.08] text-ink">{t('invite.incomplete')}</Heading>
    <p className="mt-3 text-base text-muted">{t('invite.incomplete.body')}</p>
    <HomeLink t={t} />
  </div>;
}

export function InviteScreen({ token, session, onSignedIn, onSignedOut, onAccountReady, onJoinedSignedOut, focusLogin = false }: ScreenProps) {
  const { t } = useI18n();
  const flow = useInviteAccept({ token, session, focusLogin, onSignedIn, onSignedOut, onAccountReady, onJoinedSignedOut });
  const title = flow.currentSession ? t('invite.signedIn.title', { login: flow.currentSession.login }) : t('invite.title');
  const formProps: FormProps = {
    t,
    pending: flow.pending,
    loginName: flow.loginName,
    setLoginName: flow.setLoginName,
    password: flow.password,
    setPassword: flow.setPassword,
    error: flow.error,
    loginRef: flow.loginRef,
    passwordRef: flow.passwordRef,
  };

  return <Layout><section className="settle mx-auto w-full max-w-xl">
    {flow.valid ? <>
      <InviteEyebrow t={t} />
      <Heading className={`mt-2 text-[2.125rem] font-bold leading-[1.08] text-ink ${flow.currentSession ? '[overflow-wrap:anywhere]' : ''}`}>{title}</Heading>
      <p className="mt-3 max-w-prose text-base text-muted">{t(flow.currentSession ? 'invite.signedIn.intro' : 'invite.intro')}</p>
      <InviteNotice error={flow.error} accepted={flow.accepted} noticeRef={flow.noticeRef} t={t} />
      {flow.error === 'alreadyMember' || flow.error === 'invalid' ? <div><HomeLink t={t} /></div> : null}
      {flow.accepted || !flow.invalidLink ? (
        <div data-invite-panel className="mt-5 rounded-2xl border border-line bg-raised p-5 sm:p-6">
          {flow.accepted
            ? <Button disabled={flow.pending} onClick={() => void flow.retrySession()}>
              {t('invite.continue')}
            </Button>
            : flow.currentSession
              ? <SignedInForm
                pending={flow.pending}
                onSubmit={(event) => void flow.submit(event)}
                t={t}
              />
              : <>
                {token ? <InviteProviderSignIn token={token} disabled={flow.pending} /> : null}
                <NewAccountForm {...formProps} onSubmit={(event) => void flow.submit(event)} />
              </>}
        </div>
      ) : null}
    </> : <>
      <InviteEyebrow t={t} />
      <div className="mt-2"><IncompleteInvite t={t} /></div>
    </>}
  </section></Layout>;
}
