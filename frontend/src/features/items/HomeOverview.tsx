import type { RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import type { Completion, ItemGroup } from './itemsApi';
import type { CreateNotice } from './useAddItem';

/** Completion/undo outcome shown in the home notice area; Undo and retry controls depend on its fields. */
export type Notice = {
  text: string;
  completion?: Completion;
  itemId?: string;
  retryKey?: string;
  retryKind?: 'complete' | 'undo';
  originGroup?: ItemGroup;
  undoAvailable?: boolean;
};
function NoticeView({ notice, busy, onUndo, onRetry, onRetryFocus }: {
  notice: Notice | null;
  busy: boolean;
  onUndo: () => void;
  onRetry: () => void;
  onRetryFocus: () => void;
}) {
  const { t } = useI18n();
  if (!notice) return null;
  return (
    <div role='status' aria-live='polite' className='flex flex-wrap items-center justify-between gap-2 rounded-xl bg-sand px-4 py-2'>
      <p>{notice.text}</p>
      {notice.undoAvailable && notice.completion?.id ? (
        <Button type='button' variant='quiet' disabled={busy} onClick={onUndo}>
          {t('items.undo')}
        </Button>
      ) : null}
      {notice.retryKey && notice.retryKind ? (
        <Button type='button' data-action='completion-trigger' variant='quiet' disabled={busy} onFocus={onRetryFocus} onClick={onRetry}>
          {t('items.retry')}
        </Button>
      ) : null}
    </div>
  );
}

type NoticeAreaProps = {
  noticeRef: RefObject<HTMLDivElement | null>;
  createNotice: CreateNotice;
  notice: Notice | null;
  busy: boolean;
  onUndo: () => void;
  onRetry: () => void;
  onRetryFocus: () => void;
};

/** Focusable live region for create and completion outcomes. */
export function ItemsNoticeArea({ noticeRef, createNotice, notice, busy, onUndo, onRetry, onRetryFocus }: NoticeAreaProps) {
  const { t } = useI18n();
  return <div ref={noticeRef} data-items-notice tabIndex={-1} className="outline-none">
    {createNotice === 'reused'
      ? <div role="status" aria-live="polite" className="flex flex-wrap items-center justify-between gap-2 rounded-xl bg-sand px-4 py-2">
        <p>{t('items.create.reused')}</p>
      </div>
      : notice
        ? <NoticeView notice={notice} busy={busy} onUndo={onUndo} onRetry={onRetry} onRetryFocus={onRetryFocus} />
        : null}
  </div>;
}
type LabelRetryProps = {
  hasFailures: boolean;
  pending: boolean;
  retryRef: RefObject<HTMLButtonElement | null>;
  onRetry: () => void;
};

/** Subject-label failure notice; its retry stays mounted while pending (outcome focus: useLabelRetryFocus). */
export function SubjectLabelRetry({ hasFailures, pending, retryRef, onRetry }: LabelRetryProps) {
  const { t } = useI18n();
  if (!hasFailures && !pending) return null;
  return <div className="flex items-center gap-2">
    <p className="text-sm text-muted">{t('items.subjectLabels.error')}</p>
    <Button
      ref={retryRef}
      type="button"
      variant="quiet"
      data-action="subject-label-retry"
      aria-disabled={pending}
      onClick={onRetry}
    >{t('items.subjectLabels.retry')}</Button>
  </div>;
}
