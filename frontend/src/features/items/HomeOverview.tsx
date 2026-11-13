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
function NoticeView({ notice, busy, onUndo, onRetry, onRetryFocus, onDismiss }: {
  notice: Notice | null;
  busy: boolean;
  onUndo: () => void;
  onRetry: () => void;
  onRetryFocus: () => void;
  onDismiss: () => void;
}) {
  const { t } = useI18n();
  if (!notice) return null;
  return (
    <>
      <p className="min-w-0 flex-1">{notice.text}</p>
      <div className="pointer-events-auto flex shrink-0 items-center gap-1">
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
        <Button type='button' data-action='notice-dismiss' variant='quiet' onClick={onDismiss}>
          {t('notice.dismiss')}
        </Button>
      </div>
    </>
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
  onDismiss: () => void;
};

/**
 * Create and completion outcomes. The region floats above the content instead of taking a row inside it, so a
 * Done or Undo never pushes the list down. It is a polite live region that can take focus for screen readers,
 * and its Dismiss control retires it without touching the list.
 */
export function ItemsNoticeArea({ noticeRef, createNotice, notice, busy, onUndo, onRetry, onRetryFocus, onDismiss }: NoticeAreaProps) {
  const { t } = useI18n();
  const shown = createNotice === 'reused' || Boolean(notice);
  return <div ref={noticeRef} data-items-notice tabIndex={-1} className="outline-none">
    {shown ? (
      <div className="pointer-events-none notice-in fixed inset-x-4 bottom-24 z-30 flex justify-center sm:inset-x-auto sm:right-6 sm:bottom-6">
        <div role="status" aria-live="polite" className="pointer-events-none flex w-full max-w-md flex-wrap items-center justify-between gap-x-3 gap-y-2 rounded-2xl border border-line bg-raised px-4 py-2 shadow-[0_12px_32px_rgb(20_18_14_/_18%)]">
          {createNotice === 'reused'
            ? <>
              <p className="min-w-0 flex-1">{t('items.create.reused')}</p>
              <Button type='button' data-action='notice-dismiss' variant='quiet' className='pointer-events-auto' onClick={onDismiss}>{t('notice.dismiss')}</Button>
            </>
            : <NoticeView notice={notice} busy={busy} onUndo={onUndo} onRetry={onRetry} onRetryFocus={onRetryFocus} onDismiss={onDismiss} />}
        </div>
      </div>
    ) : null}
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
