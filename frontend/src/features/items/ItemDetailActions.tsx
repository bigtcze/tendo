import type { Ref } from 'react';
import { Button } from '../../components/ui/button';
import { DetailCompletionActions } from './DetailCompletionActions';
import type { Translate } from '../../i18n';
import type { Item } from './itemsApi';
import type { WorkflowState } from './useItemDetailActions';

type Props = {
  item: Item; busy: boolean; editLoading: boolean; editRef: Ref<HTMLButtonElement>; t: Translate;
  onEdit: () => void; onWorkflow: (state: WorkflowState) => void; onArchive: () => void; onRestore: () => void;
  onDone: () => void; onUndo: () => void; completionNotice: string | null;
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
  const { item, busy, editLoading, editRef, t, onEdit, onArchive, onRestore, onDone, onUndo, completionNotice } = props;
  const live = !item.done && !item.archived;
  return <>
    <div className="item-detail-primary"><DetailCompletionActions className="item-detail-actions" done={item.done} busy={busy} notice={completionNotice} completion={Boolean(completionNotice)} t={t} onDone={onDone} onUndo={onUndo} /></div>
    {live && editLoading ? <p role="status" className="mt-3 text-sm text-muted">{t('itemDetail.edit.loading')}</p> : null}
    <div className="item-secondary-actions flex flex-wrap items-center gap-2">
      {live ? <WorkflowButtons {...props} /> : null}
      {live ? <Button ref={editRef} data-item-detail-focus-return="true" type="button" variant="quiet" disabled={busy || editLoading} onClick={onEdit}>{t('itemDetail.edit.button')}</Button> : null}
      {item.archived ? <Button type="button" variant="quiet" disabled={busy} onClick={onRestore}>{t('subjects.restore')}</Button> : null}
      {!item.archived ? <Button type="button" variant="quiet" disabled={busy} onClick={onArchive}>{t('subjects.archive')}</Button> : null}
    </div>
  </>;
}
