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
    <Button type="button" variant="quiet" disabled={busy} onClick={() => onWorkflow('waiting')}>{t('itemDetail.waitingAction')}</Button>
    <Button type="button" variant="quiet" disabled={busy} onClick={() => onWorkflow('paused')}>{t('itemDetail.pause')}</Button>
  </>;
}

function LiveActions({ busy, editLoading, editRef, t, onEdit, stale }: Props) {
  return <>
    {editLoading ? <p role="status" className="mt-3 text-sm text-muted">{t('itemDetail.edit.loading')}</p> : null}
    <Button ref={editRef} type="button" variant="quiet" className="mt-5" disabled={busy || editLoading || stale} onClick={onEdit}>
      {t('itemDetail.edit.button')}
    </Button>
  </>;
}

function StaleItemNotice({ busy, onReload, t }: Pick<Props, 'busy' | 'onReload' | 't'>) {
  return <>
    <p role="alert" className="mt-4 text-sm text-muted">{t('itemDetail.refreshFailed')}</p>
    <Button type="button" variant="quiet" className="mt-2" disabled={busy} onClick={onReload}>{t('itemDetail.reload')}</Button>
  </>;
}

/** Read-mode actions: Edit and workflow while the item is live, then archive or restore. */
export function ItemDetailActions(props: Props) {
  const { item, busy, t, onArchive, onRestore, onDone } = props;
  const { completionNotice, undoAvailable, retryAvailable, stale, onReload, completionNoticeRef, onUndo, onRetry } = props;
  const live = !item.done && !item.archived;
  const staleNotice = stale ? <StaleItemNotice busy={busy} onReload={onReload} t={t} /> : null;
  return <>
    {completionNotice ? null : staleNotice}
    {live ? <LiveActions {...props} /> : null}
    {live ? <div className="mt-5 flex flex-wrap gap-2">
      <Button
        type="button" data-action="detail-done"
        aria-label={t('items.done.label', { title: item.title })} disabled={busy || stale} onClick={onDone}
      >
        {t('items.done')}
      </Button>
      <WorkflowButtons {...props} busy={busy || stale} />
    </div> : null}
    {completionNotice ? <div className="mt-5 space-y-2">
      <Notice role="status" ref={completionNoticeRef} tabIndex={-1}>{completionNotice}</Notice>
      {undoAvailable
        ? <Button type="button" variant="quiet" disabled={busy} onClick={onUndo}>{t('items.undo')}</Button>
        : null}
      {retryAvailable
        ? <Button type="button" variant="quiet" data-action="detail-retry" disabled={busy || stale} onClick={onRetry}>{t('items.retry')}</Button>
        : null}
      {staleNotice}
    </div> : null}
    {!item.archived
      ? <div className="mt-5 border-t border-line pt-4">
        <Button type="button" variant="quiet" disabled={busy || stale} onClick={onArchive}>{t('subjects.archive')}</Button>
      </div>
      : <Button type="button" variant="quiet" className="mt-5" disabled={busy || stale} onClick={onRestore}>{t('subjects.restore')}</Button>}
  </>;
}
