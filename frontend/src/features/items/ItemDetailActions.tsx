import type { Ref } from 'react';
import { Button } from '../../components/ui/button';
import { Notice } from '../../components/ui/notice';
import type { Translate } from '../../i18n';
import { CheckIcon } from './HomeIcons';
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
    ? <Button type="button" variant="secondary" size="small" disabled={busy} onClick={() => onWorkflow('in_progress')}>
      {t(item.workflowState === 'waiting' ? 'itemDetail.resumeWork' : 'itemDetail.start')}
    </Button>
    : null;
  // On phones the workflow options take their own line below Edit and Done, so the band reads as one decision
  // plus its quieter follow-ups instead of a ragged five-button wrap. DOM order is untouched.
  const group = 'flex w-full flex-wrap items-center gap-2 sm:w-auto';
  if (item.workflowState === 'paused') {
    return <div className={group}>
      {start}
      <Button type="button" variant="secondary" size="small" disabled={busy} onClick={() => onWorkflow('open')}>{t('itemDetail.resume')}</Button>
    </div>;
  }
  return <div className={group}>
    {start}
    <Button type="button" variant="secondary" size="small" disabled={busy} onClick={() => onWorkflow('waiting')}>{t('itemDetail.waitingAction')}</Button>
    <Button type="button" variant="secondary" size="small" disabled={busy} onClick={() => onWorkflow('paused')}>{t('itemDetail.pause')}</Button>
  </div>;
}

function StaleItemNotice({ busy, onReload, t }: Pick<Props, 'busy' | 'onReload' | 't'>) {
  return <div className="space-y-2">
    <p role="alert" className="text-small text-muted">{t('itemDetail.refreshFailed')}</p>
    <Button type="button" variant="secondary" size="small" disabled={busy} onClick={onReload}>{t('itemDetail.reload')}</Button>
  </div>;
}

/**
 * One action hierarchy: the obvious Done is the only primary and the only filled control, Edit is the same
 * outlined secondary, and the workflow options step down to the small secondary size so the band reads as one
 * decision instead of five equal buttons. Edit stays first in the tab order.
 */
function LiveRow(props: Props) {
  const { item, busy, editLoading, editRef, t, onEdit, onDone, stale } = props;
  return <div className="flex flex-wrap items-center gap-2">
    <Button ref={editRef} type="button" variant="secondary" disabled={busy || editLoading || stale} onClick={onEdit}>
      {t('itemDetail.edit.button')}
    </Button>
    <Button
      type="button" data-action="detail-done" className="flex-1 sm:flex-none"
      aria-label={t('items.done.label', { title: item.title })} disabled={busy || stale} onClick={onDone}
    >
      <CheckIcon className="size-4" />
      {t('items.done')}
    </Button>
    <WorkflowButtons {...props} busy={busy || stale} />
  </div>;
}

function CompletionNotice(props: Props) {
  const { busy, t, completionNotice, undoAvailable, retryAvailable, stale, onReload, completionNoticeRef, onUndo, onRetry } = props;
  return <div className="space-y-2">
    <Notice role="status" ref={completionNoticeRef} tabIndex={-1}>{completionNotice}</Notice>
    {undoAvailable ? <Button type="button" variant="secondary" size="small" disabled={busy} onClick={onUndo}>{t('items.undo')}</Button> : null}
    {retryAvailable ? <Button type="button" variant="secondary" size="small" data-action="detail-retry" disabled={busy || stale} onClick={onRetry}>{t('items.retry')}</Button> : null}
    {stale ? <StaleItemNotice busy={busy} onReload={onReload} t={t} /> : null}
  </div>;
}

function ArchiveControl({ item, busy, stale, t, onArchive, onRestore }: Pick<Props, 'item' | 'busy' | 'stale' | 't' | 'onArchive' | 'onRestore'>) {
  return item.archived
    ? <Button type="button" variant="secondary" size="small" disabled={busy || stale} onClick={onRestore}>{t('subjects.restore')}</Button>
    : <Button type="button" variant="secondary" size="small" disabled={busy || stale} onClick={onArchive}>{t('subjects.archive')}</Button>;
}

/** One action band: Edit, the obvious Done and the workflow options, with Archive/Restore kept apart below. */
export function ItemDetailActions(props: Props) {
  const { item, busy, t } = props;
  const live = !item.done && !item.archived;
  const staleNotice = props.stale && !props.completionNotice
    ? <StaleItemNotice busy={busy} onReload={props.onReload} t={t} /> : null;
  const archive = <ArchiveControl item={item} busy={busy} stale={props.stale} t={t} onArchive={props.onArchive} onRestore={props.onRestore} />;
  if (!live && !props.completionNotice) {
    // Nothing to do on a finished or archived item beyond its facts: a slim hairline band, not an empty card.
    return <div data-item-actions className="mt-4 space-y-3 border-t border-line pt-3">
      {staleNotice}
      <div className="flex flex-wrap items-center gap-2">{archive}</div>
    </div>;
  }
  return <section data-item-actions className="mt-4 space-y-3 rounded-xl border border-line bg-raised p-4">
    {staleNotice}
    {props.editLoading ? <p role="status" className="text-small text-muted">{t('itemDetail.edit.loading')}</p> : null}
    {live ? <LiveRow {...props} /> : null}
    {props.completionNotice ? <CompletionNotice {...props} /> : null}
    <div className="flex flex-wrap items-center gap-2 border-t border-line pt-3">{archive}</div>
  </section>;
}
