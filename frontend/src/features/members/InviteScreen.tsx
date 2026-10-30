import type { FormEvent, RefObject } from 'react';
import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { Button } from '../../components/ui/button';
import { Input } from '../../components/ui/input';
import { Label } from '../../components/ui/label';
import { useI18n, type Translate } from '../../i18n';
import { inviteErrorMessageKey, isFieldError, isLoginError, isPasswordError, type ErrorState } from './inviteAcceptModel';
import { useInviteAccept } from './useInviteAccept';
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

function InviteNotice({ error, accepted, noticeRef, t }: {
  error: ErrorState | null; accepted: boolean; noticeRef: RefObject<HTMLDivElement | null>; t: Translate;
}) {
  let message: string;
  if (accepted) message = t('invite.error.accepted');
  else if (error && !isFieldError(error)) message = t(inviteErrorMessageKey(error));
  else return null;
  return <div ref={noticeRef} tabIndex={-1} role="status" className="mt-5 rounded-xl bg-sand px-4 py-3 outline-none">{message}</div>;
}

function NewAccountForm(props: FormProps & { onSubmit: (event: FormEvent) => void }) {
  const { t, pending, loginName, setLoginName, password, setPassword, error, loginRef, passwordRef, onSubmit } = props;
  const loginInvalid = isLoginError(error);
  const passwordInvalid = isPasswordError(error);
  const message = error && isFieldError(error) ? t(inviteErrorMessageKey(error)) : '';
  return <form onSubmit={onSubmit} className="mt-8 space-y-5" noValidate>
    <div className="space-y-2"><Label htmlFor="invite-login">{t('onboarding.login')}</Label>
      <Input ref={loginRef} id="invite-login" autoComplete="username" autoCapitalize="none" spellCheck={false} required value={loginName}
        onChange={(event) => setLoginName(event.target.value)} aria-invalid={loginInvalid}
        aria-describedby={`invite-login-hint${loginInvalid ? ' invite-error' : ''}`} />
      <p id="invite-login-hint" className="text-sm text-muted">{t('onboarding.login.hint')}</p>
    </div>
    <div className="space-y-2"><Label htmlFor="invite-password">{t('onboarding.password')}</Label>
      <Input ref={passwordRef} id="invite-password" type="password" autoComplete="new-password" required value={password}
        onChange={(event) => setPassword(event.target.value)} aria-invalid={passwordInvalid}
        aria-describedby={`invite-password-hint${passwordInvalid ? ' invite-error' : ''}`} />
      <p id="invite-password-hint" className="text-sm text-muted">{t('onboarding.password.hint')}</p>
    </div>
    {isFieldError(error) ? <div id="invite-error" role="alert" className="text-base text-danger">{message}</div> : null}
    <Button type="submit" disabled={pending} className="w-full sm:w-auto">{pending ? t('invite.creating') : t('invite.create')}</Button>
  </form>;
}

function SignedInForm({ pending, error, onSubmit, onSignOut, t }: {
  pending: boolean; error: ErrorState | null; onSubmit: (event: FormEvent) => void;
  onSignOut: () => void; t: Translate;
}) {
  return <form onSubmit={onSubmit} className="mt-6">
    <Button type="submit" disabled={pending} className="w-full whitespace-normal text-center sm:w-auto">
      {pending ? t('invite.joining') : t('invite.join')}
    </Button>
    {error === 'householdConflict'
      ? <Button type="button" variant="quiet" className="ml-2" disabled={pending} onClick={onSignOut}>
        {t('invite.signOutContinue')}
      </Button>
      : null}
  </form>;
}

function IncompleteInvite({ t }: { t: Translate }) {
  return <><Heading className="font-display text-3xl leading-tight sm:text-4xl">{t('invite.incomplete')}</Heading>
    <p className="mt-3 text-muted">{t('invite.incomplete.body')}</p>
    <a href="/" className="mt-4 inline-flex min-h-11 items-center rounded-xl px-3 text-muted hover:bg-sand">{t('invite.home')}</a></>;
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

  return <Layout><section className="settle">
    {flow.valid ? <>
      <Heading className={`font-display text-3xl leading-tight sm:text-4xl ${flow.currentSession ? '[overflow-wrap:anywhere]' : ''}`}>{title}</Heading>
      <InviteNotice error={flow.error} accepted={flow.accepted} noticeRef={flow.noticeRef} t={t} />
      {flow.error === 'alreadyMember' || flow.error === 'invalid'
        ? <a href="/" className="mt-4 inline-flex min-h-11 items-center rounded-xl px-3 text-muted hover:bg-sand">
          {t('invite.home')}
        </a>
        : null}
      {flow.accepted
        ? <Button className="mt-6" disabled={flow.pending} onClick={() => void flow.retrySession()}>
          {t('invite.continue')}
        </Button>
        : flow.currentSession
          ? !flow.invalidLink
            ? <SignedInForm
              pending={flow.pending}
              error={flow.error}
              onSubmit={(event) => void flow.submit(event)}
              onSignOut={() => void flow.signOutAndContinue()}
              t={t}
            />
            : null
          : !flow.invalidLink
            ? <NewAccountForm {...formProps} onSubmit={(event) => void flow.submit(event)} />
            : null}
    </> : <IncompleteInvite t={t} />}
  </section></Layout>;
}
