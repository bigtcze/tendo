import { useI18n, type MessageKey } from '../../i18n';
import { Fill } from './ConnectFrame';
import { describeRedirect } from './connectApi';

/**
 * Where the browser will go after the decision. The host is shown exactly as the app registered it (punycode, never
 * decoded), because a lookalike host is only recognisable in that form. The private-scheme note is about Allow, so a
 * blocked request, which never offers Allow, leaves it out.
 */
export function ConnectDestination({ redirectUri, allowNote = true }: { redirectUri: string; allowNote?: boolean }) {
  const { t } = useI18n();
  const destination = describeRedirect(redirectUri);
  const text: Record<typeof destination.kind, MessageKey> = {
    web: 'connect.destination.web',
    loopback: 'connect.destination.loopback',
    scheme: 'connect.destination.scheme',
    unknown: 'connect.destination.unknown',
  };
  const values: Record<string, string> = destination.kind === 'web' || destination.kind === 'loopback'
    ? { host: destination.host }
    : destination.kind === 'scheme' ? { scheme: destination.scheme } : {};
  return <div className="space-y-2">
    <p className="text-body text-ink [overflow-wrap:anywhere]"><Fill id={text[destination.kind]} values={values} /></p>
    {destination.kind === 'scheme' && allowNote ? <p className="text-small text-muted">{t('connect.destination.schemeNote')}</p> : null}
  </div>;
}
