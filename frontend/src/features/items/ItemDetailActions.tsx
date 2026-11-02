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
    ? <Button type="button" disabled={busy} onClick={() => onWorkflow('in_progress')}>
      {t(item.workflowState === 'waiting' ? 'itemDetail.resumeWork' : 'itemDetail.start')}
    </Button>
    : null;
  if (item.workflowState === 'paused') {
    return <>{start}<Button type="button" disabled={busy} onClick={() => onWorkflow('open')}>{t('itemDetail.resume')}</Button></>;
  }
  return <>
    {start}
    <Button type="button" variant="quiet" disabled={busy} onClick={() => onWorkflow('waiting')}>{t('itemDetail.waitingAction')}</Button>
    <Button type="button" variant="quiet" disabled={busy} onClick={() => onWorkflow('paused')}>{t('itemDetail.pause')}</Button>
  </>;
}

/** Read-mode actions: Edit and workflow while the item is live, then archive or restore. */
export function ItemDetailActions(props: Props) {
  const { item, busy, editLoading, editRef, t, onEdit, onArchive, onRestore, onDone } = props;
  const { completionNotice, undoAvailable, retryAvailable, completionNoticeRef, onUndo, onRetry } = props;
  const live = !item.done && !item.archived;
  return <>
    {live ? <>
      {editLoading ? <p role="status" className="mt-3 text-sm text-muted">{t('itemDetail.edit.loading')}</p> : null}
      <Button ref={editRef} type="button" variant="quiet" className="mt-5" disabled={busy || editLoading} onClick={onEdit}>
        {t('itemDetail.edit.button')}
      </Button>
    </> : null}
    {live ? <div className="mt-5 flex flex-wrap gap-2">
      <WorkflowButtons {...props} />
      <Button
        type="button" variant="secondary" data-action="detail-done"
        aria-label={t('items.done.label', { title: item.title })} disabled={busy} onClick={onDone}
      >
        {t('items.done')}
      </Button>
    </div> : null}
    {completionNotice ? <div className="mt-5 space-y-2">
      <Notice role="status" ref={completionNoticeRef}>{completionNotice}</Notice>
      {undoAvailable
        ? <Button type="button" variant="quiet" disabled={busy} onClick={onUndo}>{t('items.undo')}</Button>
        : null}
      {retryAvailable
        ? <Button type="button" variant="quiet" disabled={busy} onClick={onRetry}>{t('items.retry')}</Button>
        : null}
    </div> : null}
    {!item.archived
      ? <div className="mt-5 border-t border-line pt-4">
        <Button type="button" variant="quiet" disabled={busy} onClick={onArchive}>{t('subjects.archive')}</Button>
      </div>
      : <Button type="button" variant="quiet" className="mt-5" disabled={busy} onClick={onRestore}>{t('subjects.restore')}</Button>}
  </>;
}
