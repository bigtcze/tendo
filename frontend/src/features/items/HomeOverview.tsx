import { useEffect, useRef, useState, type RefObject } from 'react';
import { Button } from '../../components/ui/button';
import { useI18n } from '../../i18n';
import type { ItemGroup, ItemSummary } from './itemsApi';
import { countPluralKey } from './itemsScreenHelpers';
import { groups, type GroupData } from './homeGroups';
import { NoticeView, type Notice } from './HomeGroupSection';
import type { CreateNotice } from './useAddItem';

const overviewLinkClass = 'inline-flex min-h-6 items-center rounded-sm underline decoration-line underline-offset-4 '
  + 'hover:decoration-accent focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent';

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
  labels: { loading: boolean; hasFailures: boolean; retry: () => void };
  groupData: Record<ItemGroup, GroupData>;
  sectionHeadingRefs: RefObject<Map<ItemGroup, HTMLHeadingElement>>;
};

/** Subject-label failure notice with a retry that keeps focus on its outcome. */
export function SubjectLabelRetry({ labels, groupData, sectionHeadingRefs }: LabelRetryProps) {
  const { t } = useI18n();
  const labelRetryRef = useRef<HTMLButtonElement>(null);
  const [labelRetryPending, setLabelRetryPending] = useState(false);
  useEffect(() => {
    if (!labelRetryPending || labels.loading) return;
    // The retry control stays mounted while its batch runs; afterwards focus follows the outcome unless the user moved it.
    if (document.activeElement === labelRetryRef.current || document.activeElement === document.body) {
      if (labels.hasFailures) labelRetryRef.current?.focus();
      else sectionHeadingRefs.current.get(groups.find((group) => groupData[group.id].items.length)?.id ?? 'needs')?.focus();
    }
    // eslint-disable-next-line react-hooks/set-state-in-effect -- settle the retry once its batch completes.
    setLabelRetryPending(false);
  }, [labelRetryPending, labels.loading, labels.hasFailures, groupData, sectionHeadingRefs]);
  if (!labels.hasFailures && !labelRetryPending) return null;
  return <div className="flex items-center gap-2">
    <p className="text-sm text-muted">{t('items.subjectLabels.error')}</p>
    <Button
      ref={labelRetryRef}
      type="button"
      variant="quiet"
      data-action="subject-label-retry"
      aria-disabled={labelRetryPending}
      onClick={() => { if (labelRetryPending) return; setLabelRetryPending(true); labels.retry(); }}
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
