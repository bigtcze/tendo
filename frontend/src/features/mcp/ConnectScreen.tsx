import { Heading } from '../../app/Heading';
import { Layout } from '../../app/Layout';
import { LoadingScreen } from '../../app/StatusScreens';
import { useI18n } from '../../i18n';
import type { Session } from '../auth/sessionApi';
import type { ConnectOutcome } from '../../app/useConnectVisit';
import { ConnectFrame, Fill, HEADING_CLASS } from './ConnectFrame';
import { ConnectProblem, connectProblemKey } from './ConnectProblem';
import { ConsentBlocked } from './ConsentBlocked';
import { ConsentPanel } from './ConsentPanel';
import { isBlocked } from './connectApi';
import { useConnectConsent, type ConsentState } from './useConnectConsent';

export type ConnectScreenProps = {
  outcome: ConnectOutcome;
  session: Session;
  onSignOut: () => void;
  onSessionEnded: () => void;
  onHome: () => void;
};

type ConsentProps = Omit<ConnectScreenProps, 'outcome'> & { handle: string };

/** The request is answered: the browser is leaving for the app. Final, with no buttons ever again. */
function Returning() {
  const { t } = useI18n();
  return <ConnectFrame heading={<Heading className={HEADING_CLASS}>{t('connect.returning')}</Heading>}>
    <p className="mt-3 text-center text-base text-muted">{t('connect.returningHint')}</p>
  </ConnectFrame>;
}

/**
 * The request as a card: a heading that names the app, the app's unverified name marker, then the decision panel, or,
 * when the account allows nothing the app asked for, the blocked card with only a way back to the app.
 */
function Decision({ state, session, onSignOut, decide }: {
  state: Extract<ConsentState, { kind: 'ready' | 'deciding' }>;
  session: Session;
  onSignOut: () => void;
  decide: Parameters<typeof ConsentPanel>[0]['onDecide'];
}) {
  const { t } = useI18n();
  const { request } = state;
  const blocked = isBlocked(request);
  return <ConnectFrame
    heading={<Heading className={HEADING_CLASS}>
      <Fill id={blocked ? 'connect.blocked.heading' : 'connect.heading'} values={{ client: request.clientName }} />
    </Heading>}
  >
    <p className="mt-3 text-center text-small text-muted">{t('connect.unverified')}</p>
    {blocked ? <ConsentBlocked
      request={request}
      session={session}
      busy={state.kind === 'deciding'}
      decisionError={state.kind === 'ready' ? state.decisionError : null}
      onBack={() => decide('deny')}
      onSignOut={onSignOut}
    /> : <ConsentPanel
      request={request}
      session={session}
      busy={state.kind === 'deciding'}
      allowEnabled={state.allowEnabled}
      decisionError={state.kind === 'ready' ? state.decisionError : null}
      onDecide={decide}
      onSignOut={onSignOut}
    />}
  </ConnectFrame>;
}

/** Owns the consent state machine for one visit to `/connect` with a valid request handle. */
function ConsentRequest({ handle, session, onSignOut, onSessionEnded, onHome }: ConsentProps) {
  const { t } = useI18n();
  const { state, decide, retry } = useConnectConsent(handle, { onSessionEnded });
  switch (state.kind) {
    case 'loading':
      return <Layout><LoadingScreen message={t('connect.loading')} /></Layout>;
    case 'ready':
    case 'deciding':
      return <Decision state={state} session={session} onSignOut={onSignOut} decide={decide} />;
    case 'done':
      return <Returning />;
    case 'notFound':
      return <ConnectProblem messageKey="connect.gone" onHome={onHome} />;
    case 'rateLimited':
      return <ConnectProblem messageKey="connect.rateLimited" onHome={onHome} onRetry={retry} />;
    case 'unavailable':
      return <ConnectProblem messageKey="connect.unavailable" onHome={onHome} onRetry={retry} />;
    case 'invalidResponse':
      return <ConnectProblem messageKey="connect.problem.generic" onHome={onHome} />;
  }
}

/** The consent page: a request to decide, or the calm reason it cannot continue. Sign-in is handled by the route. */
export function ConnectScreen({ outcome, session, onSignOut, onSessionEnded, onHome }: ConnectScreenProps) {
  switch (outcome.kind) {
    case 'request':
      return <ConsentRequest
        handle={outcome.handle}
        session={session}
        onSignOut={onSignOut}
        onSessionEnded={onSessionEnded}
        onHome={onHome}
      />;
    case 'error':
      return <ConnectProblem messageKey={connectProblemKey(outcome.code)} onHome={onHome} />;
    case 'none':
      // Nothing is pending any more: a reload or Back after a decision lands here.
      return <ConnectProblem messageKey="connect.gone" onHome={onHome} />;
    case 'invalid':
      return <ConnectProblem messageKey="connect.problem.request" onHome={onHome} />;
  }
}
