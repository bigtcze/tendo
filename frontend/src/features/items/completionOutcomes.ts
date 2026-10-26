import { clearPendingCompletion } from './pendingCompletions';
import { formatDate } from './formatDate';
import { focusUnclaimed } from './itemsScreenHelpers';
import type { Completion, ItemRead, ItemGroup } from './itemsApi';
import type { Notice } from './HomeGroupSection';

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
  focusedRowRef: { current: boolean };
  focusedRow: { current: boolean };
  retryHadFocus: { current: boolean };
  focusOriginRef: { current: HTMLElement | null };
  focusOrigin: { current: HTMLElement | null };
  undoFocusRef: { current: { itemId: string; group: ItemGroup | null } | null };
  undoFocus: { current: { itemId: string; group: ItemGroup | null } | null };
  noticeRef: { current: HTMLDivElement | null };
};

async function completionSuccess(
  tools: CompletionTools,
  itemId: string,
  focused: boolean,
  focusedControl: HTMLElement | null,
  current: Extract<ItemRead, { kind: 'ok' }>,
  completion: Completion,
) {
  const { userId, householdId, t, locale, refresh, setNotice, focusedRowRef, noticeRef } = tools;
  if (completion.undoneAt) {
    clearPendingCompletion(userId, householdId, itemId);
    await refresh();
    setNotice({ text: t('items.notice.alreadyUndone') });
    focusedRowRef.current = focused;
    requestAnimationFrame(() => { if (focusUnclaimed(focusedControl)) noticeRef.current?.focus(); });
    return;
  }
  clearPendingCompletion(userId, householdId, itemId);
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
    undoAvailable: true,
  });
}

export async function handleCompletionResult(
  tools: CompletionTools,
  itemId: string,
  attemptKey: string,
  focused: boolean,
  focusedControl: HTMLElement | null,
  current: ItemRead,
  result: { kind: string; completion?: Completion; code?: string },
) {
  const { userId, householdId, t, refresh, setNotice, onSignedOut } = tools;
  if (current.kind === 'unauthenticated' || result.kind === 'unauthenticated') { onSignedOut(); return; }
  if (current.kind === 'notFound' || result.kind === 'notFound') {
    clearPendingCompletion(userId, householdId, itemId);
    await refresh();
    setNotice({ text: t('items.notice.gone') });
    return;
  }
  if (current.kind !== 'ok' || result.kind === 'readFailure') {
    await refresh();
    setNotice({ text: t(current.kind === 'changed' ? 'items.notice.changed' : 'items.error.action'), itemId, retryKey: attemptKey, retryKind: 'complete' });
    return;
  }
  if (result.kind === 'ok' && result.completion) return completionSuccess(tools, itemId, focused, focusedControl, current, result.completion);
  if (result.kind === 'conflict') {
    clearPendingCompletion(userId, householdId, itemId);
    await refresh();
    setNotice({ text: t(completionConflictNoticeKey(result.code ?? 'unknown')) });
  } else if (result.kind === 'changed') { await refresh(); setNotice({ text: t('items.notice.changed') }); }
  else if (result.kind === 'invalid') {
    clearPendingCompletion(userId, householdId, itemId);
    setNotice({ text: t(completionInvalidNoticeKey(result.code ?? 'unknown')) });
  }
  else setNotice({ text: t('items.notice.retry'), itemId, retryKey: attemptKey, retryKind: 'complete' });
}

export async function handleUndoResult(
  tools: CompletionTools,
  previous: Notice,
  itemId: string,
  focusedGroup: ItemGroup | null,
  current: ItemRead,
  result: { kind: string; code?: string },
) {
  const { t, refresh, setNotice, onSignedOut, undoFocusRef } = tools;
  if (current.kind === 'unauthenticated' || result.kind === 'unauthenticated') { onSignedOut(); return; }
  if (current.kind !== 'ok' || result.kind === 'readFailure') {
    await refresh();
    setNotice({ ...previous, text: t('items.notice.changed'), undoAvailable: true });
  }
  else if (result.kind === 'ok') { setNotice(null); await refresh(); undoFocusRef.current = { itemId, group: focusedGroup }; }
  else if (result.kind === 'notFound') { await refresh(); setNotice({ text: t('items.notice.gone') }); }
  else if (result.kind === 'conflict') {
    await refresh();
    setNotice({
      text: t(
        result.code === 'completion_not_latest'
          ? 'items.notice.notLatest'
          : 'items.notice.archived',
      ),
    });
  }
  else if (result.kind === 'changed') { await refresh(); setNotice({ ...previous, text: t('items.notice.changed'), retryKind: 'undo', undoAvailable: true }); }
  else setNotice({ ...previous, text: t('items.notice.retry'), retryKind: 'undo', undoAvailable: true });
}
