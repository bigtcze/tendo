import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import type { Session } from '../auth/sessionApi';
import { ConnectDestination } from './ConnectDestination';
import { ConnectSignedIn } from './ConnectSignedIn';
import { grantedScopes, type McpAuthorizationRequest } from './connectApi';
import { DecisionError, ScopeList } from './ConsentPanel';

/**
 * Nothing the app asked for is allowed for this account, so Allow could only answer a refusal and is never offered.
 * The card says so, lists what the app asked for, keeps "Not you?" (another account may be allowed) and offers one
 * way back to the app, which tells it the connection was refused.
 */
export function ConsentBlocked({ request, session, busy, decisionError, onBack, onSignOut }: {
  request: McpAuthorizationRequest;
  session: Session;
  busy: boolean;
  decisionError: 'rateLimited' | 'unavailable' | null;
  onBack: () => void;
  onSignOut: () => void;
}) {
  const { t } = useI18n();
  return <div className="mt-8 rounded-2xl border border-line bg-raised p-5 sm:p-7">
    <div className="space-y-6">
      <div className="space-y-2">
        <p className="text-body text-ink">{t('connect.blocked.body')}</p>
        <p className="text-small text-muted">{t('connect.blocked.hint')}</p>
      </div>
      <ScopeList id="connect-scopes-asked" intro="connect.blocked.asked" scopes={grantedScopes(request).withheldScopes} muted />
      <ConnectDestination redirectUri={request.redirectUri} allowNote={false} />
      <ConnectSignedIn login={session.login} busy={busy} onSignOut={onSignOut} />
    </div>
    {decisionError ? <DecisionError kind={decisionError} /> : null}
    <div className="mt-8">
      <Button type="button" variant="secondary" disabled={busy} onClick={onBack} className="w-full">
        {t('connect.back')}
      </Button>
    </div>
  </div>;
}
