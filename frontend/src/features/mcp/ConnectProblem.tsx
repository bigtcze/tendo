import { Heading } from '../../app/Heading';
import { Button } from '../../components/ui/button';
import { useI18n, type MessageKey } from '../../i18n';
import { ConnectFrame, ConnectHomeLink, HEADING_CLASS } from './ConnectFrame';
import type { ConnectErrorCode } from './connectEntry';

const PROBLEM_KEY: Record<ConnectErrorCode, MessageKey> = {
  invalid_client: 'connect.problem.invalid_client',
  invalid_redirect_uri: 'connect.problem.invalid_client',
  temporarily_unavailable: 'connect.problem.temporarily_unavailable',
  server_error: 'connect.problem.server_error',
  invalid_request: 'connect.problem.request',
  unsupported_response_type: 'connect.problem.request',
  invalid_scope: 'connect.problem.request',
  invalid_target: 'connect.problem.request',
};

/** The message for an error the app's link carried. The code itself never reaches the screen. */
export function connectProblemKey(code: ConnectErrorCode): MessageKey {
  return PROBLEM_KEY[code];
}

/**
 * A request that cannot continue: a calm sentence and a way out. No decision buttons; `onRetry` is offered only where a
 * second try can help (a failed load), never for a request that is already gone.
 */
export function ConnectProblem({ messageKey, onHome, onRetry }: {
  messageKey: MessageKey;
  onHome: () => void;
  onRetry?: () => void;
}) {
  const { t } = useI18n();
  return <ConnectFrame heading={<Heading className={HEADING_CLASS}>{t('connect.problem.title')}</Heading>}>
    <p className="mt-3 text-center text-base text-muted">{t(messageKey)}</p>
    <div className="mt-8 flex flex-col items-center gap-3 rounded-2xl border border-line bg-raised p-5 sm:p-7">
      {onRetry ? <Button type="button" onClick={onRetry}>{t('app.error.retry')}</Button> : null}
      <ConnectHomeLink onHome={onHome} />
    </div>
  </ConnectFrame>;
}
