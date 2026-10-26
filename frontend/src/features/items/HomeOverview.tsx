import type { RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import type { Completion, ItemGroup, ItemSummary } from './itemsApi';
import { countPluralKey } from './itemsScreenHelpers';
import { groups, type GroupData } from './homeGroups';
import type { CreateNotice } from './useAddItem';

const overviewLinkClass = 'inline-flex min-h-6 items-center rounded-sm underline decoration-line underline-offset-4 '
  + 'hover:decoration-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent';

/** Completion/undo outcome shown in the home notice area; Undo and retry controls depend on its fields. */
export type Notice = {
  text: string;
  completion?: Completion;
  itemId?: string;
  retryKey?: string;
  retryKind?: 'complete' | 'undo';
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

/** Empty-household welcome shown when every group is empty. */
export function EmptyHome() {
  const { t } = useI18n();
  return <div className="rounded-3xl border border-line bg-white/70 p-6 sm:p-10">
    <h2 className="font-display text-2xl">{t('home.empty.title')}</h2>
    <p className="mt-3 text-muted">{t('home.empty.body')} {t('home.empty.addHint')}</p>
  </div>;
}

type OverviewNavProps = {
  summary: ItemSummary;
  groupData: Record<ItemGroup, GroupData>;
  sectionHeadingRefs: RefObject<Map<ItemGroup, HTMLHeadingElement>>;
};

/** In-page links to rendered groups; hidden unless at least two groups have rows. */
export function HomeOverviewNav({ summary, groupData, sectionHeadingRefs }: OverviewNavProps) {
  const { t, locale } = useI18n();
  // Overview links only target sections that actually render (summary count and loaded rows), so every link has a heading to focus.
  const overviewGroups = groups.filter((group) => summary[group.count] > 0 && groupData[group.id].items.length > 0);
  if (overviewGroups.length < 2) return null;
  return <nav aria-label={t('items.group.overview')} className="flex flex-wrap gap-x-4 gap-y-2 text-sm">
    {overviewGroups.map((group) => <a
      key={group.id}
      href={`#group-${group.id}`}
      aria-label={`${t(group.key)} ${t(countPluralKey(summary[group.count], locale), { count: String(summary[group.count]) })}`}
      onClick={(event) => {
        if (event.defaultPrevented || event.button !== 0) return;
        if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
        event.preventDefault();
        sectionHeadingRefs.current.get(group.id)?.focus();
      }}
      className={overviewLinkClass}
    >{t(group.key)} {summary[group.count]}</a>)}
  </nav>;
}
