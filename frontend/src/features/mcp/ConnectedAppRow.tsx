import { useMemo, type MouseEvent } from 'react';
import { Button } from '../../components/ui/button';
import { useI18n, type MessageKey } from '../../i18n';
import type { McpGrant } from './mcpGrantsApi';

/** One formatter per locale, shared by every row: rows re-render on each list change, the locale rarely does. */
export function useDayFormatter(locale: string) {
  return useMemo(() => new Intl.DateTimeFormat(locale, { dateStyle: 'medium' }), [locale]);
}

/** Access in human words: a read-only app never reads as one that can also change items. */
export function accessKey(scopes: McpGrant['scopes']): MessageKey {
  return scopes.includes('tendo:write') ? 'connectedApps.access.write' : 'connectedApps.access.read';
}

/**
 * One connected app: the name the app gave itself (plain text, marked unverified like the consent screen), when it
 * was connected, what it may do and when its access ends at the latest. Disconnect is named for the app it affects.
 */
export function ConnectedAppRow({ grant, busy, onDisconnect }: {
  grant: McpGrant;
  busy: boolean;
  onDisconnect: (grant: McpGrant, trigger: HTMLButtonElement) => void;
}) {
  const { t, locale } = useI18n();
  const day = useDayFormatter(locale);
  return <li data-grant-id={grant.id} className="flex flex-wrap items-start justify-between gap-x-4 gap-y-3 py-4">
    <div className="min-w-0 flex-1 basis-56 space-y-1">
      <p dir="auto" className="text-row-title font-semibold text-ink [overflow-wrap:anywhere]">{grant.clientName}</p>
      <p className="text-small text-muted">{t('connectedApps.unverified')}</p>
      <p className="text-meta text-muted">{t('connectedApps.connected', { date: day.format(new Date(grant.createdAt)) })}</p>
      <p className="text-meta text-ink">{t(accessKey(grant.scopes))}</p>
      <p className="text-meta text-muted">{t('connectedApps.expires', { date: day.format(new Date(grant.expiresAt)) })}</p>
    </div>
    <Button
      variant="secondary"
      disabled={busy}
      aria-label={t('connectedApps.disconnectLabel', { client: grant.clientName })}
      onClick={(event: MouseEvent<HTMLButtonElement>) => onDisconnect(grant, event.currentTarget)}
    >
      {t('connectedApps.disconnect')}
    </Button>
  </li>;
}
