import { LoginScreen } from '../features/auth/LoginScreen';
import { OidcContinueScreen } from '../features/auth/OidcContinueScreen';
import { OnboardingScreen } from '../features/auth/OnboardingScreen';
import { useI18n, type MessageKey, type Translate } from '../i18n';
import { ErrorScreen, LoadingScreen, NotFoundScreen } from './StatusScreens';
import { Layout } from './Layout';
import { readRoute } from './routes';
import type { BootState } from './useAppSession';
import type { OidcOutcome } from '../features/auth/oidcApi';
import type { Session } from '../features/auth/sessionApi';

export type Props = {
  state: BootState;
  retry: () => void;
  signedIn: (session: Session, notice?: MessageKey | null) => void;
  signedOut: () => void;
  accountReadyNotice: boolean;
  oidcOutcome: OidcOutcome | null;
  sessionEndedNotice: boolean;
  passwordResetNotice: boolean;
  loginPrefill: string | null;
  oidcContinue: boolean;
  oidcContinueExpired: boolean;
  continueCancelled: () => void;
  continueExpired: () => void;
};

export function SignedOutApp(props: Props) {
  const { t } = useI18n();
  const notice = getLoginNotice(props, t);
  const route = readRoute();
  return <Layout>
    {props.state.kind === 'loading' ? <LoadingScreen message={t('app.loading')} /> : null}
    {props.state.kind === 'error' ? <ErrorScreen onRetry={props.retry} /> : null}
    {props.state.kind === 'setupRequired' ? <OnboardingScreen onSignedIn={props.signedIn} onSetupComplete={props.signedOut} /> : null}
    {props.state.kind === 'signedOut' && route.kind === 'notFound' ? <NotFoundScreen signedOut /> : null}
    {props.state.kind === 'signedOut' && route.kind !== 'notFound' ? (
      props.oidcContinue
        ? <OidcContinueScreen
          onSignedIn={props.signedIn}
          onCancelled={props.continueCancelled}
          onExpired={props.continueExpired}
        />
        : <LoginScreen onSignedIn={props.signedIn} notice={notice} initialLogin={props.loginPrefill ?? undefined} />
    ) : null}
  </Layout>;
}

export function getLoginNotice(props: Props, t: Translate): string | undefined {
  if (props.passwordResetNotice) return t('reset.notice.success');
  if (props.accountReadyNotice) return t('invite.error.accountReady');
  if (props.oidcOutcome?.kind === 'error') return t(`oidc.error.${props.oidcOutcome.code}`);
  if (props.oidcContinueExpired) return t('oidc.continue.expired');
  if (props.sessionEndedNotice) return t('login.sessionEnded');
  return undefined;
}
