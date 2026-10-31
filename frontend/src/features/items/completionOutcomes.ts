import { clearPendingCompletion } from './pendingCompletions';
import { formatDate } from './formatDate';
import { focusUnclaimed } from './itemsScreenHelpers';
import type { CompleteResult, Completion, ItemGroup, ItemRead, UndoResult } from './itemsApi';
import type { Notice } from './HomeOverview';

export function completionInvalidNoticeKey(code: string) {
  return code === 'idempotency_key_reused'
    ? 'items.notice.keyUsed'
    : code === 'future_date'
      ? 'items.notice.futureDate'
      : code === 'date_overflow'
        ? 'items.notice.dateOverflow'
        : 'items.notice.validation';
}
export function completionConflictNoticeKey(code: string) {
  return code === 'item_archived' ? 'items.notice.archived' : code === 'item_done' ? 'items.notice.alreadyDone' : 'items.notice.conflict';
}
export type CompletionTools = {
  userId: string; householdId: string; t: ReturnType<typeof import('../../i18n').useI18n>['t']; locale: string;
  refresh: () => Promise<void>; setNotice: (notice: Notice | null) => void; onSignedOut: () => void;
  /** Set when a completion outcome should move focus to the notice once it renders. */
  focusedRowRef: { current: boolean };
  /** Restored after the mutation lock clears, once a successful Undo re-renders the row. */
  undoFocusRef: { current: { itemId: string; group: ItemGroup | null } | null };
  noticeRef: { current: HTMLDivElement | null };
};
/** The prerequisite item read and, only when it returned an ETag, the guarded write. */
export type ReadThenWrite<R> = { current: ItemRead; result?: R };

async function completionSuccess(
  tools: CompletionTools,
  itemId: string,
  focused: boolean,
  focusedControl: HTMLElement | null,
  current: Extract<ItemRead, { kind: 'ok' }>,
  completion: Completion,
  attemptKey: string,
  originGroup: ItemGroup | null,
) {
  const { userId, householdId, t, locale, refresh, setNotice, focusedRowRef, noticeRef } = tools;
  if (completion.undoneAt) {
    clearPendingCompletion(userId, householdId, itemId, attemptKey);
    await refresh();
    setNotice({ text: t('items.notice.alreadyUndone') });
    focusedRowRef.current = focused;
    requestAnimationFrame(() => { if (focusUnclaimed(focusedControl)) noticeRef.current?.focus(); });
    return;
  }
  clearPendingCompletion(userId, householdId, itemId, attemptKey);
  focusedRowRef.current = focused;
  await refresh();
  const date = completion.nextAttentionOn;
  setNotice({
    text: current.item.recurrence && date
      ? t('items.notice.doneNext', {
          title: current.item.title,
          date: formatDate(date, locale),
        })
      : t('items.notice.done', { title: current.item.title }),
    completion,
    itemId,
    originGroup: originGroup ?? undefined,
    undoAvailable: true,
  });
}

export async function handleCompletionResult(
  tools: CompletionTools,
  itemId: string,
  attemptKey: string,
  focused: boolean,
  focusedControl: HTMLElement | null,
  originGroup: ItemGroup | null,
  { current, result }: ReadThenWrite<CompleteResult>,
) {
  const { userId, householdId, t, refresh, setNotice, onSignedOut } = tools;
  const retry = { itemId, retryKey: attemptKey, retryKind: 'complete' as const, originGroup: originGroup ?? undefined };
  if (current.kind === 'unauthenticated') { onSignedOut(); return; }
  if (current.kind === 'notFound') {
    clearPendingCompletion(userId, householdId, itemId, attemptKey);
    await refresh();
    setNotice({ text: t('items.notice.gone') });
    return;
  }
  if (current.kind !== 'ok' || !result) {
    await refresh();
    setNotice({ text: t(current.kind === 'changed' ? 'items.notice.changed' : 'items.error.action'), ...retry });
    return;
  }
  if (result.kind === 'unauthenticated') { onSignedOut(); return; }
  if (result.kind === 'ok') return completionSuccess(tools, itemId, focused, focusedControl, current, result.completion, attemptKey, originGroup);
  if (result.kind === 'notFound') {
    clearPendingCompletion(userId, householdId, itemId, attemptKey);
    await refresh();
    setNotice({ text: t('items.notice.gone') });
  } else if (result.kind === 'conflict') {
    clearPendingCompletion(userId, householdId, itemId, attemptKey);
    await refresh();
    setNotice({ text: t(completionConflictNoticeKey(result.code)) });
  } else if (result.kind === 'changed') { await refresh(); setNotice({ text: t('items.notice.changed') }); }
  else if (result.kind === 'invalid') {
    clearPendingCompletion(userId, householdId, itemId, attemptKey);
    setNotice({ text: t(completionInvalidNoticeKey(result.code)) });
  }
  else setNotice({ text: t('items.notice.retry'), ...retry });
}

export async function handleUndoResult(
  tools: CompletionTools,
  previous: Notice,
  itemId: string,
  focusedGroup: ItemGroup | null,
  { current, result }: ReadThenWrite<UndoResult>,
) {
  const { t, refresh, setNotice, onSignedOut, undoFocusRef } = tools;
  if (current.kind === 'unauthenticated') { onSignedOut(); return; }
  // A deleted item cannot be undone: refresh and drop Undo instead of offering it again.
  if (current.kind === 'notFound') { await refresh(); setNotice({ text: t('items.notice.gone') }); return; }
  if (current.kind !== 'ok' || !result) {
    await refresh();
    setNotice({ ...previous, text: t('items.notice.changed'), undoAvailable: true });
    return;
  }
  if (result.kind === 'unauthenticated') { onSignedOut(); return; }
  if (result.kind === 'ok') {
    setNotice(null);
    await refresh();
    // Restored after busy clears (useCompletionFocus): the row's Done is disabled until then and cannot take focus.
    undoFocusRef.current = { itemId, group: focusedGroup };
  } else if (result.kind === 'notFound') { await refresh(); setNotice({ text: t('items.notice.gone') }); }
  else if (result.kind === 'conflict') {
    await refresh();
    setNotice({ text: t(result.code === 'completion_not_latest' ? 'items.notice.notLatest' : 'items.notice.archived') });
  } else if (result.kind === 'changed') {
    await refresh();
    setNotice({ ...previous, text: t('items.notice.changed'), retryKind: 'undo', undoAvailable: true });
  } else setNotice({ ...previous, text: t('items.notice.retry'), retryKind: 'undo', undoAvailable: true });
}
