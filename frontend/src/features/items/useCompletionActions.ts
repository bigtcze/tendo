import { useI18n } from '../../i18n';
import { getPendingCompletion, setPendingCompletion } from './pendingCompletions';
import { completeItem, getItem, trackItemWrite, undoCompletion, type ItemGroup, type ItemRead } from './itemsApi';
import { idempotencyKey } from '../../lib/idempotencyKey';
import { groups, type GroupData } from './homeGroups';
import { handleCompletionResult, handleUndoResult, type CompletionTools } from './completionOutcomes';
import { useCompletionFocus } from './useCompletionFocus';
import type { Notice } from './HomeGroupSection';

export function useCompletionActions({
  userId, householdId, lock, refresh, groupData, onSignedOut, createNotice, sectionHeadingRefs, noticeRef, addRef, notice, setNotice,
}: {
  userId: string; householdId: string; lock: { busy: boolean; begin: () => boolean; end: () => void }; refresh: () => Promise<void>;
  groupData: Record<ItemGroup, GroupData>; onSignedOut: () => void; createNotice: 'uncertain' | 'reused' | null;
  sectionHeadingRefs: React.RefObject<Map<ItemGroup, HTMLHeadingElement> | null>; noticeRef: React.RefObject<HTMLDivElement | null>;
  addRef: React.RefObject<HTMLButtonElement | null>; notice: Notice | null; setNotice: (notice: Notice | null) => void;
}) {
  const { t, locale } = useI18n();
  const refs = useCompletionFocus({ notice, createNotice, busy: lock.busy, sectionHeadingRefs, noticeRef, addRef });
  const { focusedRow: focusedRowRef, retryHadFocus: retryHadFocusRef, focusOrigin: focusOriginRef, undoFocus: undoFocusRef } = refs;
  const tools: CompletionTools = {
    userId,
    householdId,
    t,
    locale,
    refresh,
    setNotice,
    onSignedOut,
    focusedRowRef,
    focusedRow: focusedRowRef,
    retryHadFocus: retryHadFocusRef,
    focusOriginRef,
    focusOrigin: focusOriginRef,
    undoFocusRef,
    undoFocus: undoFocusRef,
    noticeRef,
  };

  function itemGroup(itemId: string): ItemGroup | null {
    for (const group of groups) if (groupData[group.id].items.some((item) => item.id === itemId)) return group.id;
    return null;
  }
  function readThenWrite<R>(itemId: string, write: (etag: string) => Promise<R>): Promise<{ current: ItemRead; result?: R }> {
    return trackItemWrite((async () => {
      const current = await getItem(householdId, itemId);
      if (current.kind !== 'ok' || !current.etag) return { current };
      return { current, result: await write(current.etag) };
    })());
  }
  async function finish(itemId: string) {
    const origin = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const focused = origin?.dataset.action === 'done' || origin?.dataset.action === 'completion-trigger';
    if (!lock.begin()) return;
    focusOriginRef.current = origin;
    const key = getPendingCompletion(userId, householdId, itemId) ?? idempotencyKey();
    setPendingCompletion(userId, householdId, itemId, key);
    try {
      const response = await readThenWrite(itemId, (etag) => completeItem(householdId, itemId, etag, key));
      await handleCompletionResult(tools, itemId, key, focused, origin, response.current, response.result ?? { kind: 'readFailure' });
    } finally { lock.end(); }
  }
  async function undo() {
    if (!notice?.completion || !notice.itemId) return;
    const group = itemGroup(notice.itemId);
    if (!lock.begin()) return;
    const previous = notice;
    const itemId = notice.itemId;
    const completion = notice.completion;
    try {
      const response = await readThenWrite(itemId, (etag) => undoCompletion(householdId, itemId, completion.id, etag));
      await handleUndoResult(tools, previous, itemId, group, response.current, response.result ?? { kind: 'readFailure' });
    } finally { lock.end(); }
  }
  async function retryNotice() {
    if (!notice) return;
    if (notice.retryKind === 'complete' && notice.itemId) await finish(notice.itemId);
    else if (notice.retryKind === 'undo') await undo();
  }
  function markRetryFocused() { retryHadFocusRef.current = true; }
  return { finish, undo, retryNotice, markRetryFocused };
}
