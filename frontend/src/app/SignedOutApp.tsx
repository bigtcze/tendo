import { LoginScreen } from '../features/auth/LoginScreen';
import { OidcContinueScreen } from '../features/auth/OidcContinueScreen';
import { OnboardingScreen } from '../features/auth/OnboardingScreen';
import { useI18n, type MessageKey, type Translate } from '../i18n';
import { ErrorScreen, LoadingScreen, NotFoundScreen } from './StatusScreens';
import { Layout } from './Layout';
import { readRoute, type Route } from './routes';
import type { BootState } from './useAppSession';
import type { OidcErrorCode } from '../features/auth/oidcApi';
import type { Session } from '../features/auth/sessionApi';
// A sign-in begun anywhere but the consent page abandons any pending consent, so it can never return there.
import { clearConnectEntry, markConnectReturn } from '../features/mcp/connectEntry';

export type Props = {
  state: BootState;
  retry: () => void;
  signedIn: (session: Session, notice?: MessageKey | null) => void;
  signedOut: () => void;
  accountReadyNotice: boolean;
  oidcOutcome: OidcErrorCode | null;
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
  const route = readRoute();
  const notice = getLoginNotice(props, t, route);
  return <Layout>
    {props.state.kind === 'loading' ? <LoadingScreen message={t('app.loading')} signedOut /> : null}
    {props.state.kind === 'error' ? <ErrorScreen onRetry={props.retry} signedOut /> : null}
    {props.state.kind === 'setupRequired' ? <OnboardingScreen onSignedIn={props.signedIn} onSetupComplete={props.signedOut} /> : null}
    {props.state.kind === 'signedOut' && route.kind === 'notFound' ? <NotFoundScreen signedOut /> : null}
    {props.state.kind === 'signedOut' && route.kind !== 'notFound' ? (
      props.oidcContinue
        ? <OidcContinueScreen
          onSignedIn={props.signedIn}
          onCancelled={props.continueCancelled}
          onExpired={props.continueExpired}
        />
        : <LoginScreen
          onSignedIn={props.signedIn}
          notice={notice}
          initialLogin={props.loginPrefill ?? undefined}
          onSignInStart={route.kind === 'connect' ? markConnectReturn : clearConnectEntry}
        />
    ) : null}
  </Layout>;
}

/** The consent page explains why sign-in is needed, unless an outcome of the attempt already has its own notice. */
export function getLoginNotice(props: Props, t: Translate, route: Route = { kind: 'login' }): string | undefined {
  if (props.passwordResetNotice) return t('reset.notice.success');
  if (props.accountReadyNotice) return t('invite.error.accountReady');
  if (props.oidcOutcome) return t(`oidc.error.${props.oidcOutcome}`);
  if (props.oidcContinueExpired) return t('oidc.continue.expired');
  if (props.sessionEndedNotice) return t('login.sessionEnded');
  if (route.kind === 'connect') return t('connect.signIn.notice');
  return undefined;
}
