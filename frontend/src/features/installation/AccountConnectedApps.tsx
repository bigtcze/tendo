import { useMemo, useRef, type MouseEvent } from 'react';
import { Button } from '../../components/ui/button';
import { Dialog } from '../../components/ui/dialog';
import { LoadingLine } from '../../components/ui/loading-line';
import { Notice, NoticeRegion } from '../../components/ui/notice';
import { useI18n } from '../../i18n';
import type { ConnectedAppsVoice } from '../mcp/ConnectedAppRow';
import { ConnectedAppsList, DIALOG_ERRORS, DisconnectDialog, useFocusAfterRemoval } from '../mcp/ConnectedAppsSection';
import { accountGrants, revokeAllAccountMcpGrants } from '../mcp/mcpGrantsApi';
import { useAppDisconnect } from '../mcp/useAppDisconnect';
import { useConnectedApps } from '../mcp/useConnectedApps';
import { useDisconnectAll, type DisconnectAll } from '../mcp/useDisconnectAll';

const HEADING_ID = 'account-ai-apps-connected';

/** An administrator reads about someone else's apps, so nothing says “your” or “you”. */
const ACCOUNT_VOICE: ConnectedAppsVoice = {
  read: 'accountApps.access.read', write: 'connectedApps.access.write', dialogBody: 'accountApps.dialog.body',
};

/**
 * One account's connected apps, for its installation administrator: the same rows and Disconnect as on the person's
 * own Account, plus Disconnect all. Unlike Account the section always shows, empty or not, because an administrator
 * came here to check. Nothing leaves the list before the server's 204. A lost installation grant or a gone account
 * is handed to the screen, which says so in place of the whole page.
 */
export function AccountConnectedApps({ userId, onSignedOut, onForbidden, onGone }: {
  userId: string;
  onSignedOut: () => void;
  onForbidden: (duringAction: boolean) => void;
  onGone: (duringAction: boolean) => void;
}) {
  const { t } = useI18n();
  const callbacks = { onSignedOut, onForbidden, onGone };
  // The screen mounts anew for another account, so the source is fixed for the section's life.
  const source = useMemo(() => accountGrants(userId), [userId]);
  const apps = useConnectedApps(source, callbacks);
  const disconnect = useAppDisconnect(apps);
  const headingRef = useRef<HTMLHeadingElement>(null);
  const listRef = useRef<HTMLUListElement>(null);
  const all = useDisconnectAll({
    revokeAll: () => revokeAllAccountMcpGrants(userId), apps, disconnect, callbacks, clearedFocusRef: headingRef,
  });
  useFocusAfterRemoval(apps.grants, disconnect, listRef, [headingRef]);
  const empty = apps.state === 'ready' && apps.grants.length === 0 && !apps.refreshFailed;

  return <section aria-labelledby={HEADING_ID} className="mt-6 grid max-w-2xl gap-1 border-t border-line pt-6">
    <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-3">
      <div className="min-w-0 flex-1 basis-64">
        <h2 id={HEADING_ID} ref={headingRef} tabIndex={-1} className="text-h2 text-ink outline-none">{t('connectedApps.title')}</h2>
        <p className="mt-1 max-w-prose text-body text-muted">{t('accountApps.body')}</p>
      </div>
      {apps.state === 'ready' && apps.grants.length > 0 ? <Button variant="secondary" disabled={all.busy || disconnect.busy}
        onClick={(event: MouseEvent<HTMLButtonElement>) => all.request(event.currentTarget)}>
        {t('accountApps.all.open')}
      </Button> : null}
    </div>
    <NoticeRegion className="mt-4 empty:mt-0 [overflow-wrap:anywhere]"
      message={disconnect.notice ? t(disconnect.notice.key, { client: disconnect.notice.client ?? '' }) : ''} />
    {apps.state === 'loading' ? <LoadingLine className="mt-4">{t('accountApps.loading')}</LoadingLine> : null}
    {empty ? <p className="mt-4 max-w-prose text-body text-ink">{t('accountApps.empty')}</p> : null}
    {apps.state === 'loading' || empty
      ? null
      : <ConnectedAppsList apps={apps} listRef={listRef} disconnect={disconnect} voice={ACCOUNT_VOICE} />}
    <DisconnectDialog disconnect={disconnect} voice={ACCOUNT_VOICE} />
    <DisconnectAllDialog all={all} />
  </section>;
}

/** Asks once before every app of the account loses access, and says how to keep them from coming back. */
function DisconnectAllDialog({ all }: { all: DisconnectAll }) {
  const { t } = useI18n();
  return <Dialog
    open={all.open}
    onOpenChange={(open) => { if (!open) all.cancel(); }}
    title={t('accountApps.all.title')}
    busy={all.busy}
    triggerRef={all.triggerRef}
    returnFocusRef={all.returnFocusRef}
  >
    <div className="space-y-4">
      <p className="max-w-prose text-body text-ink">{t('accountApps.all.body')}</p>
      <p className="max-w-prose text-body text-muted">{t('accountApps.all.again')}</p>
      {all.error ? <Notice tone="error">{t(DIALOG_ERRORS[all.error])}</Notice> : null}
      <div className="flex flex-wrap justify-end gap-2">
        <Button variant="quiet" disabled={all.busy} onClick={all.cancel}>{t('admin.action.cancel')}</Button>
        <Button disabled={all.busy} onClick={all.confirm}>
          {all.busy ? t('connectedApps.dialog.working') : t('accountApps.all.confirm')}
        </Button>
      </div>
    </div>
  </Dialog>;
}
