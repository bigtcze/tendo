import { useEffect, useRef, type RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { Dialog } from '../../components/ui/dialog';
import { LoadError } from '../../components/ui/load-error';
import { Notice, NoticeRegion } from '../../components/ui/notice';
import { useI18n } from '../../i18n';
import { cn } from '../../lib/cn';
import { ConnectedAppRow } from './ConnectedAppRow';
import type { McpGrant } from './mcpGrantsApi';
import { useAppDisconnect, type AppDisconnect, type FocusPlan } from './useAppDisconnect';
import { useConnectedApps, type ConnectedApps } from './useConnectedApps';

const HEADING_ID = 'account-connected-apps-heading';

/**
 * Moves focus after a removal: the row that took the removed row's place, else the row above it, else the section
 * heading when it is still shown, else the Account page heading. The plan's index was taken before the removal.
 */
function focusAfterRemoval(list: HTMLElement | null, plan: FocusPlan, fallbacks: (HTMLElement | null)[]) {
  if (list?.querySelector(`[data-grant-id="${plan.id}"]`)) return;
  const buttons = [...(list?.querySelectorAll<HTMLButtonElement>('li[data-grant-id] button') ?? [])];
  const target = buttons[plan.index] ?? buttons[plan.index - 1] ?? fallbacks.find((element) => element?.isConnected);
  target?.focus();
}

/**
 * The connected apps on Account, after the household section. Nothing shows while the first page loads or when the
 * account has no connected apps, so instances that never used MCP see no change. The live notice and the disconnect
 * dialog stay mounted in every state, so disconnecting the last app still announces and keeps focus somewhere real.
 */
export function ConnectedAppsSection({ onSignedOut, pageHeadingRef }: {
  onSignedOut: () => void;
  pageHeadingRef: RefObject<HTMLHeadingElement | null>;
}) {
  const { t } = useI18n();
  const apps = useConnectedApps(onSignedOut);
  const disconnect = useAppDisconnect(apps);
  const headingRef = useRef<HTMLHeadingElement>(null);
  const listRef = useRef<HTMLUListElement>(null);
  const visible = apps.state === 'error' || (apps.state === 'ready' && apps.grants.length > 0);
  useFocusAfterRemoval(apps.grants, disconnect, listRef, [headingRef, pageHeadingRef]);

  return <section
    aria-labelledby={visible ? HEADING_ID : undefined}
    className={cn('mt-6 max-w-3xl sm:mt-8', visible && 'border-t border-line pt-6', !visible && !disconnect.notice && 'hidden')}
  >
    {visible ? <>
      <h2 id={HEADING_ID} ref={headingRef} tabIndex={-1} className="text-h2 text-ink outline-none">{t('connectedApps.title')}</h2>
      <p className="mt-1 max-w-prose text-small text-muted">{t('connectedApps.body')}</p>
    </> : null}
    <NoticeRegion className="mt-4 empty:mt-0 [overflow-wrap:anywhere]" message={disconnect.notice ? t(disconnect.notice.key, { client: disconnect.notice.client }) : ''} />
    {visible ? <ConnectedAppsList apps={apps} listRef={listRef} disconnect={disconnect} /> : null}
    <DisconnectDialog disconnect={disconnect} />
  </section>;
}

/** Keeps the focus rule in one place: runs once the dialog has closed and the removed row has left the list. */
function useFocusAfterRemoval(grants: McpGrant[], disconnect: AppDisconnect, listRef: RefObject<HTMLUListElement | null>, fallbacks: RefObject<HTMLElement | null>[]) {
  const { pending, takeFocusPlan } = disconnect;
  const fallbackRefs = useRef(fallbacks);
  useEffect(() => {
    if (pending) return;
    const plan = takeFocusPlan();
    if (!plan) return;
    focusAfterRemoval(listRef.current, plan, fallbackRefs.current.map((ref) => ref.current));
  }, [grants, pending, takeFocusPlan, listRef]);
}

/** The row list and its inline errors. Show more and retries live here; the section's notice does not depend on them. */
function ConnectedAppsList({ apps, listRef, disconnect }: {
  apps: ConnectedApps;
  listRef: RefObject<HTMLUListElement | null>;
  disconnect: AppDisconnect;
}) {
  const { t } = useI18n();
  const retry = () => apps.retry();
  if (apps.state === 'error') {
    return <LoadError className="mt-4" message={t('connectedApps.error.load')} retryLabel={t('connectedApps.retry')} onRetry={retry} />;
  }
  return <div className="mt-4 space-y-4">
    {apps.refreshFailed ? <LoadError message={t('connectedApps.error.load')} retryLabel={t('connectedApps.retry')} onRetry={retry} /> : null}
    <ul ref={listRef} className="divide-y divide-line">
      {apps.grants.map((grant) => <ConnectedAppRow
        key={grant.id}
        grant={grant}
        busy={disconnect.busy && disconnect.pending?.id === grant.id}
        onDisconnect={disconnect.request}
      />)}
    </ul>
    {apps.moreFailed ? <LoadError message={t('connectedApps.error.more')} retryLabel={t('connectedApps.retry')} onRetry={() => void apps.loadMore()} /> : null}
    {apps.nextCursor ? <Button variant="secondary" disabled={apps.loadingMore} onClick={() => void apps.loadMore()}>
      {t('connectedApps.more')}
    </Button> : null}
  </div>;
}

/** Asks before an app loses access. Its refusals stay in the dialog, and the confirm control is disabled only while in flight. */
function DisconnectDialog({ disconnect }: { disconnect: AppDisconnect }) {
  const { t } = useI18n();
  const { pending, busy, dialogError, triggerRef, cancel, confirm } = disconnect;
  const client = pending?.clientName ?? '';
  return <Dialog
    open={pending !== null}
    onOpenChange={(open) => { if (!open) cancel(); }}
    title={pending ? t('connectedApps.dialog.title', { client }) : ''}
    busy={busy}
    triggerRef={triggerRef}
  >
    <div className="space-y-4">
      <p className="max-w-prose text-body text-ink [overflow-wrap:anywhere]">{t('connectedApps.dialog.body', { client })}</p>
      {dialogError ? <Notice tone="error">
        {t(dialogError === 'rateLimited' ? 'connectedApps.error.rateLimited' : 'connectedApps.error.action')}
      </Notice> : null}
      <div className="flex flex-wrap justify-end gap-2">
        <Button variant="quiet" disabled={busy} onClick={cancel}>{t('admin.action.cancel')}</Button>
        <Button disabled={busy} onClick={confirm}>
          {busy ? t('connectedApps.dialog.working') : t('connectedApps.dialog.confirm')}
        </Button>
      </div>
    </div>
  </Dialog>;
}
