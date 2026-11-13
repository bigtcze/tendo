import type { RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import type { Completion, ItemGroup } from './itemsApi';
import type { CreateNotice } from './useAddItem';
import { useNoticeClearance } from './useNoticeClearance';

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
      <div className="flex shrink-0 items-center gap-1">
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
 * Done or Undo never pushes the list down.
 *
 * Until `lg` the bottom bar is fixed, so the card sits above it rather than in the viewport's corner (a corner
 * card would paint over tabs the user cannot see). Short viewports turn that bar static, but it still ends the page,
 * so the card keeps the same 5.5rem lift there and clears the bar at maximum scroll too. The card itself takes pointer events —
 * text must never leak clicks onto the rows it covers — while its positioning wrapper does not, so the card's own
 * controls stay the only things it can activate. The wrapper is never in flow, and `useNoticeClearance` reserves the
 * card's height as scroll clearance, so a focused row control is never hidden behind it (WCAG 2.4.11).
 */
export function ItemsNoticeArea({ noticeRef, createNotice, notice, busy, onUndo, onRetry, onRetryFocus, onDismiss }: NoticeAreaProps) {
  const { t } = useI18n();
  const shown = createNotice === 'reused' || Boolean(notice);
  useNoticeClearance(shown, noticeRef);
  if (!shown) return null;
  return (
    <div className="pointer-events-none notice-in fixed inset-x-4 bottom-[calc(5.5rem+env(safe-area-inset-bottom))] z-30 flex justify-center lg:inset-x-auto lg:bottom-6 lg:right-6">
      <div
        ref={noticeRef}
        data-items-notice
        tabIndex={-1}
        role="status"
        aria-live="polite"
        className="pointer-events-auto flex w-full max-w-md flex-wrap items-center justify-between gap-x-3 gap-y-2 rounded-2xl border border-line bg-raised px-4 py-2 shadow-[0_12px_32px_rgb(20_18_14_/_18%)] outline-none"
      >
        {createNotice === 'reused'
          ? <>
            <p className="min-w-0 flex-1">{t('items.create.reused')}</p>
            <Button type='button' data-action='notice-dismiss' variant='quiet' onClick={onDismiss}>{t('notice.dismiss')}</Button>
          </>
          : <NoticeView notice={notice} busy={busy} onUndo={onUndo} onRetry={onRetry} onRetryFocus={onRetryFocus} onDismiss={onDismiss} />}
      </div>
    </div>
  );
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
