import { useLayoutEffect, useRef } from 'react';
import { Button } from '../../components/ui/button';
import { Notice } from '../../components/ui/notice';
import { useI18n, type MessageKey } from '../../i18n';
import type { Session } from '../auth/sessionApi';
import { ConnectDestination } from './ConnectDestination';
import { Fill } from './ConnectFrame';
import { ConnectSignedIn } from './ConnectSignedIn';
import { grantedScopes, type ConnectDecision, type McpAuthorizationRequest } from './connectApi';

const HINT_ID = 'connect-allow-hint';
const GRANTED_ID = 'connect-scopes-granted';
const WITHHELD_ID = 'connect-scopes-withheld';
type Scope = McpAuthorizationRequest['scopes'][number];
const SCOPE_TEXT: Record<Scope, MessageKey> = {
  'tendo:read': 'connect.scope.read',
  'tendo:write': 'connect.scope.write',
};

/** A sentence and the scopes it introduces, named by that sentence for assistive tech. */
export function ScopeList({ id, intro, scopes, muted = false }: { id: string; intro: MessageKey; scopes: Scope[]; muted?: boolean }) {
  const { t } = useI18n();
  const tone = muted ? 'text-muted' : 'text-ink';
  return <div className="space-y-2">
    <p id={id} className={`text-body ${tone}`}>{t(intro)}</p>
    <ul aria-labelledby={id} className={`list-disc space-y-1 pl-5 text-body ${tone}`}>
      {scopes.map((scope) => <li key={scope}>{t(SCOPE_TEXT[scope])}</li>)}
    </ul>
  </div>;
}

/**
 * The decision failed and the buttons are back. Focus moves to this message, so a keyboard user does not lose their
 * place to the page body.
 */
export function DecisionError({ kind }: { kind: 'rateLimited' | 'unavailable' }) {
  const { t } = useI18n();
  const ref = useRef<HTMLDivElement>(null);
  // Layout effect, like Heading: the heading also focuses in a layout effect, and this message must win that race.
  useLayoutEffect(() => { ref.current?.focus(); }, []);
  return <Notice ref={ref} tabIndex={-1} tone="error" className="mt-6 outline-none">
    {t(kind === 'rateLimited' ? 'connect.rateLimited' : 'connect.unavailable')}
  </Notice>;
}

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
 * The decision card: where the browser goes, what the app may do, what it asked for but will not get, who is signed in
 * and when the request ends. Allow waits for the hook's delay and is never focused automatically; Deny needs no delay
 * or confirmation.
 */
export function ConsentPanel({ request, session, busy, allowEnabled, decisionError, onDecide, onSignOut }: ConsentPanelProps) {
  const { t, locale } = useI18n();
  const expires = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(request.expiresAt));
  const { allowedScopes, withheldScopes } = grantedScopes(request);
  return <div className="mt-8 rounded-2xl border border-line bg-raised p-5 sm:p-7">
    <div className="space-y-6">
      <ConnectDestination redirectUri={request.redirectUri} />
      <div className="space-y-2">
        <ScopeList id={GRANTED_ID} intro="connect.scopes.intro" scopes={allowedScopes} />
        <p className="text-small text-muted">{t('connect.scopes.coverage')}</p>
      </div>
      {withheldScopes.length > 0 ? <ScopeList id={WITHHELD_ID} intro="connect.scopes.withheld" scopes={withheldScopes} muted /> : null}
      <ConnectSignedIn login={session.login} busy={busy} onSignOut={onSignOut} />
      <p className="text-small text-muted"><Fill id="connect.expires" values={{ time: expires }} /></p>
    </div>
    {decisionError ? <DecisionError kind={decisionError} /> : null}
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
