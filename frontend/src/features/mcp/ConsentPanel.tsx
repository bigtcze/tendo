import { Button } from '../../components/ui/button';
import { Notice } from '../../components/ui/notice';
import { useI18n, type MessageKey } from '../../i18n';
import type { Session } from '../auth/sessionApi';
import { ConnectDestination } from './ConnectDestination';
import { Fill } from './ConnectFrame';
import { ConnectSignedIn } from './ConnectSignedIn';
import type { ConnectDecision, McpAuthorizationRequest } from './connectApi';

const HINT_ID = 'connect-allow-hint';
const SCOPE_TEXT: Record<McpAuthorizationRequest['scopes'][number], MessageKey> = {
  'tendo:read': 'connect.scope.read',
  'tendo:write': 'connect.scope.write',
};

export type ConsentPanelProps = {
  request: McpAuthorizationRequest;
  session: Session;
  busy: boolean;
  allowEnabled: boolean;
  decisionError: 'rateLimited' | 'unavailable' | null;
  onDecide: (decision: ConnectDecision) => void;
  onSignOut: () => void;
};

/**
 * The decision card: where the browser goes, what the app may do, who is signed in and when the request ends.
 * Allow waits for the hook's delay and is never focused automatically; Deny needs no delay or confirmation.
 */
export function ConsentPanel({ request, session, busy, allowEnabled, decisionError, onDecide, onSignOut }: ConsentPanelProps) {
  const { t, locale } = useI18n();
  const expires = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(request.expiresAt));
  return <div className="mt-8 rounded-2xl border border-line bg-raised p-5 sm:p-7">
    <div className="space-y-6">
      <ConnectDestination redirectUri={request.redirectUri} />
      <div className="space-y-2">
        <p className="text-body text-ink">{t('connect.scopes.intro')}</p>
        <ul className="list-disc space-y-1 pl-5 text-body text-ink">
          {request.scopes.map((scope) => <li key={scope}>{t(SCOPE_TEXT[scope])}</li>)}
        </ul>
        <p className="text-small text-muted">{t('connect.scopes.coverage')}</p>
      </div>
      <ConnectSignedIn login={session.login} onSignOut={onSignOut} />
      <p className="text-small text-muted"><Fill id="connect.expires" values={{ time: expires }} /></p>
    </div>
    {decisionError ? <Notice tone="error" className="mt-6">
      {t(decisionError === 'rateLimited' ? 'connect.rateLimited' : 'connect.unavailable')}
    </Notice> : null}
    <div className="mt-8 grid gap-3 sm:grid-cols-2">
      <Button type="button" variant="secondary" disabled={busy} onClick={() => onDecide('deny')} className="w-full">
        {t('connect.deny')}
      </Button>
      <Button
        type="button"
        disabled={busy || !allowEnabled}
        aria-describedby={allowEnabled ? undefined : HINT_ID}
        onClick={() => onDecide('approve')}
        className="w-full"
      >
        {t('connect.allow')}
      </Button>
    </div>
    {allowEnabled ? null : <p id={HINT_ID} className="mt-3 text-small text-muted">{t('connect.allowHint')}</p>}
  </div>;
}
