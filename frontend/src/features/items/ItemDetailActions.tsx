import type { Ref } from 'react';
import { Button } from '../../components/ui/button';
import { Notice } from '../../components/ui/notice';
import type { Translate } from '../../i18n';
import type { Item } from './itemsApi';
import type { WorkflowState } from './useItemDetailActions';

type Props = {
  item: Item; busy: boolean; editLoading: boolean; editRef: Ref<HTMLButtonElement>; t: Translate;
  onEdit: () => void; onWorkflow: (state: WorkflowState) => void; onArchive: () => void; onRestore: () => void;
  onDone: () => void;
  stale: boolean;
  onReload: () => void;
  completionNotice: string | null;
  undoAvailable: boolean;
  retryAvailable: boolean;
  completionNoticeRef: Ref<HTMLDivElement>;
  onUndo: () => void;
  onRetry: () => void;
};

function WorkflowButtons({ item, busy, t, onWorkflow }: Props) {
  const startable = item.workflowState === 'open' || item.workflowState === 'waiting';
  const start = startable
    ? <Button type="button" variant="secondary" disabled={busy} onClick={() => onWorkflow('in_progress')}>
      {t(item.workflowState === 'waiting' ? 'itemDetail.resumeWork' : 'itemDetail.start')}
    </Button>
    : null;
  if (item.workflowState === 'paused') {
    return <>{start}<Button type="button" variant="secondary" disabled={busy} onClick={() => onWorkflow('open')}>{t('itemDetail.resume')}</Button></>;
  }
  return <>
    {start}
    <Button type="button" variant="secondary" disabled={busy} onClick={() => onWorkflow('waiting')}>{t('itemDetail.waitingAction')}</Button>
    <Button type="button" variant="secondary" disabled={busy} onClick={() => onWorkflow('paused')}>{t('itemDetail.pause')}</Button>
  </>;
}

function StaleItemNotice({ busy, onReload, t }: Pick<Props, 'busy' | 'onReload' | 't'>) {
  return <div className="space-y-2">
    <p role="alert" className="text-small text-muted">{t('itemDetail.refreshFailed')}</p>
    <Button type="button" variant="secondary" disabled={busy} onClick={onReload}>{t('itemDetail.reload')}</Button>
  </div>;
}

/**
 * One action hierarchy: the obvious Done is the only primary, set apart from the status changes it is not
 * part of, and every other action (Edit, the workflow options) is the same outlined secondary. Edit stays
 * first in the tab order.
 */
function LiveRow(props: Props) {
  const { item, busy, editLoading, editRef, t, onEdit, onDone, stale } = props;
  return <>
    <Button ref={editRef} type="button" variant="secondary" disabled={busy || editLoading || stale} onClick={onEdit}>
      {t('itemDetail.edit.button')}
    </Button>
    <Button
      type="button" data-action="detail-done" className="flex-1 sm:flex-none"
      aria-label={t('items.done.label', { title: item.title })} disabled={busy || stale} onClick={onDone}
    >
      {t('items.done')}
    </Button>
    <div className="flex flex-wrap items-center gap-2 sm:border-l sm:border-line sm:pl-4">
      <WorkflowButtons {...props} busy={busy || stale} />
    </div>
  </>;
}

function CompletionNotice(props: Props) {
  const { busy, t, completionNotice, undoAvailable, retryAvailable, stale, onReload, completionNoticeRef, onUndo, onRetry } = props;
  return <div className="space-y-2">
    <Notice role="status" ref={completionNoticeRef} tabIndex={-1}>{completionNotice}</Notice>
    {undoAvailable ? <Button type="button" variant="secondary" disabled={busy} onClick={onUndo}>{t('items.undo')}</Button> : null}
    {retryAvailable ? <Button type="button" variant="secondary" data-action="detail-retry" disabled={busy || stale} onClick={onRetry}>{t('items.retry')}</Button> : null}
    {stale ? <StaleItemNotice busy={busy} onReload={onReload} t={t} /> : null}
  </div>;
}

function ArchiveControl({ item, busy, stale, t, onArchive, onRestore }: Pick<Props, 'item' | 'busy' | 'stale' | 't' | 'onArchive' | 'onRestore'>) {
  return item.archived
    ? <Button type="button" variant="secondary" size="small" disabled={busy || stale} onClick={onRestore}>{t('subjects.restore')}</Button>
    : <Button type="button" variant="secondary" size="small" disabled={busy || stale} onClick={onArchive}>{t('subjects.archive')}</Button>;
}

/**
 * One flat action band on the page: Edit, the obvious Done and the workflow options, with the small quiet
 * Archive/Restore at its end instead of a card of its own. The outcome of a completion sits under the band.
 */
export function ItemDetailActions(props: Props) {
  const { item, busy, t } = props;
  const live = !item.done && !item.archived;
  const staleNotice = props.stale && !props.completionNotice
    ? <StaleItemNotice busy={busy} onReload={props.onReload} t={t} /> : null;
  const archive = <ArchiveControl item={item} busy={busy} stale={props.stale} t={t} onArchive={props.onArchive} onRestore={props.onRestore} />;
  return <div data-item-actions className="mt-6 border-t border-line pt-4">
    {staleNotice ? <div className="mb-3">{staleNotice}</div> : null}
    {props.editLoading ? <p role="status" className="mb-3 text-small text-muted">{t('itemDetail.edit.loading')}</p> : null}
    <div className="flex flex-wrap items-center gap-2">
      {live ? <LiveRow {...props} /> : null}
      <div className="ml-auto flex flex-wrap items-center gap-2">{archive}</div>
    </div>
    {props.completionNotice ? <div className="mt-3"><CompletionNotice {...props} /></div> : null}
  </div>;
}
