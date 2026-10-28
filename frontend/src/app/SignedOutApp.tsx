import { LoginScreen } from '../features/auth/LoginScreen';
import { OnboardingScreen } from '../features/auth/OnboardingScreen';
import { useI18n, type Translate } from '../i18n';
import { ErrorScreen, LoadingScreen } from './StatusScreens';
import { Layout } from './Layout';
import type { BootState } from './useAppSession';
import type { OidcOutcome } from '../features/auth/oidcApi';
import type { Session } from '../features/auth/sessionApi';

export type Props = {
  state: BootState;
  retry: () => void;
  signedIn: (session: Session) => void;
  signedOut: () => void;
  accountReadyNotice: boolean;
  oidcOutcome: OidcOutcome | null;
  sessionEndedNotice: boolean;
};

export function SignedOutApp(props: Props) {
  const { t } = useI18n();
  const notice = getLoginNotice(props, t);
  return <Layout>
    {props.state.kind === 'loading' ? <LoadingScreen message={t('app.loading')} /> : null}
    {props.state.kind === 'error' ? <ErrorScreen onRetry={props.retry} /> : null}
    {props.state.kind === 'setupRequired' ? <OnboardingScreen onSignedIn={props.signedIn} onSetupComplete={props.signedOut} /> : null}
    {props.state.kind === 'signedOut' ? <LoginScreen onSignedIn={props.signedIn} notice={notice} /> : null}
  </Layout>;
}

export function getLoginNotice(props: Props, t: Translate): string | undefined {
  if (props.accountReadyNotice) return t('invite.error.accountReady');
  if (props.oidcOutcome?.kind === 'error') return t(`oidc.error.${props.oidcOutcome.code}`);
  if (props.sessionEndedNotice) return t('login.sessionEnded');
  return undefined;
}
