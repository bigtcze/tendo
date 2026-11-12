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
    ? <Button type="button" variant="quiet" disabled={busy} onClick={() => onWorkflow('in_progress')}>
      {t(item.workflowState === 'waiting' ? 'itemDetail.resumeWork' : 'itemDetail.start')}
    </Button>
    : null;
  if (item.workflowState === 'paused') {
    return <>{start}<Button type="button" variant="quiet" disabled={busy} onClick={() => onWorkflow('open')}>{t('itemDetail.resume')}</Button></>;
  }
  return <>
    {start}
    <Button type="button" variant="quiet" disabled={busy} onClick={() => onWorkflow('waiting')}>{t('itemDetail.waitingAction')}</Button>
    <Button type="button" variant="quiet" disabled={busy} onClick={() => onWorkflow('paused')}>{t('itemDetail.pause')}</Button>
  </>;
}

function StaleItemNotice({ busy, onReload, t }: Pick<Props, 'busy' | 'onReload' | 't'>) {
  return <div className="space-y-2">
    <p role="alert" className="text-small text-muted">{t('itemDetail.refreshFailed')}</p>
    <Button type="button" variant="secondary" disabled={busy} onClick={onReload}>{t('itemDetail.reload')}</Button>
  </div>;
}

/**
 * One action band under the facts: one filled Done, then quiet secondary work (Edit, the workflow
 * options) and Archive kept at the end, away from the primary action.
 */
function LiveRow(props: Props) {
  const { item, busy, editLoading, editRef, t, onEdit, onDone, stale } = props;
  return <>
    <Button
      type="button" data-action="detail-done" className="w-full sm:mr-1.5 sm:w-auto sm:px-5"
      aria-label={t('items.done.label', { title: item.title })} disabled={busy || stale} onClick={onDone}
    >
      {t('items.done')}
    </Button>
    <div className="flex flex-wrap items-center gap-x-1 gap-y-1">
      <Button ref={editRef} type="button" variant="quiet" disabled={busy || editLoading || stale} onClick={onEdit}>
        {t('itemDetail.edit.button')}
      </Button>
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

/** Archive stays a quiet text action: it is rare, and it must never compete with Done. */
function ArchiveControl({ item, busy, stale, t, onArchive, onRestore }: Pick<Props, 'item' | 'busy' | 'stale' | 't' | 'onArchive' | 'onRestore'>) {
  return item.archived
    ? <Button type="button" variant="quiet" size="small" disabled={busy || stale} onClick={onRestore}>{t('subjects.restore')}</Button>
    : <Button type="button" variant="quiet" size="small" disabled={busy || stale} onClick={onArchive}>{t('subjects.archive')}</Button>;
}

export function ItemDetailActions(props: Props) {
  const { item, busy, t } = props;
  const live = !item.done && !item.archived;
  const staleNotice = props.stale && !props.completionNotice
    ? <StaleItemNotice busy={busy} onReload={props.onReload} t={t} />
    : null;
  const archive = <ArchiveControl item={item} busy={busy} stale={props.stale} t={t} onArchive={props.onArchive} onRestore={props.onRestore} />;
  if (!live && !props.completionNotice) {
    return <div data-item-actions className="mt-5 flex flex-wrap items-center justify-between gap-3 border-t border-line pt-4">
      {staleNotice ?? <span />}
      {archive}
    </div>;
  }
  return <section data-item-actions className="mt-5 space-y-3">
    {staleNotice}
    {props.editLoading ? <p role="status" className="text-small text-muted">{t('itemDetail.edit.loading')}</p> : null}
    {live ? <div className="flex flex-col items-start gap-3 border-t border-line pt-5 sm:flex-row sm:flex-wrap sm:items-center sm:justify-between">
      <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center">
        <LiveRow {...props} />
      </div>
      {archive}
    </div> : null}
    {props.completionNotice ? <CompletionNotice {...props} /> : null}
    {!live ? <div className="flex flex-wrap items-center justify-end gap-2 border-t border-line pt-4">{archive}</div> : null}
  </section>;
}
