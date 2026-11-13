import { useRef } from 'react';
import { useI18n } from '../../i18n';
import { idempotencyKey } from '../../lib/idempotencyKey';
import { completeItem, getItem, trackItemWrite, undoCompletion } from './itemsApi';
import { getPendingCompletion, setPendingCompletion } from './pendingCompletions';
import { handleCompletionResult, handleUndoResult, settleCompletionKey, type CompletionTools, type ReadThenWrite } from './completionOutcomes';
import type { Notice } from './HomeOverview';
import type { ItemDetailBusy } from './useItemDetailBusy';
import type { ItemDetailState } from './useItemDetail';
import type { ItemDetailHistoryState } from './useItemDetailHistory';
import type { LoadGeneration } from './useLoadGeneration';

type Options = {
  userId: string;
  householdId: string;
  itemId: string;
  onSignedOut: () => void;
  detail: ItemDetailState;
  busy: ItemDetailBusy;
  loads: LoadGeneration;
  noticeRef: { current: HTMLDivElement | null };
  history: Pick<ItemDetailHistoryState, 'load' | 'reset'>;
};

export function useItemDetailCompletion({ userId, householdId, itemId, onSignedOut, detail, busy, loads, noticeRef, history }: Options) {
  const { t, locale } = useI18n();
  const { current, generationRef } = loads;
  const focusedRowRef = useRef(false);
  const retryHadFocusRef = useRef(false);
  const focusOriginRef = useRef<HTMLElement | null>(null);
  const owner = useRef<symbol | null>(null);
  const undoOwner = useRef<symbol | null>(null);
  const undoFocusRef = useRef<{ itemId: string; group: import('./itemsApi').ItemGroup | null } | null>(null);
  const tools: CompletionTools = {
    userId, householdId, t, locale, onSignedOut, focusedRowRef, undoFocusRef, noticeRef,
    refresh: async () => { await refreshDetail(); },
    setNotice: (notice) => detail.setCompletionNotice(notice),
    undoneText: t('itemDetail.completionUndone'),
  };

  async function refreshDetail(): Promise<boolean> {
    const token = ++generationRef.current;
    const read = await getItem(householdId, itemId);
    if (!current(token)) return false;
    if (read.kind === 'unauthenticated') { onSignedOut(); return false; }
    if (read.kind === 'notFound') { detail.setState('gone'); return false; }
    if (read.kind === 'ok') {
      detail.setStale(false);
      detail.setItem(read.item);
      // The latest completion (or its undo) belongs at the top of History: return to the newest page.
      history.reset();
      void history.load(token);
      return true;
    } else {
      detail.setStale(true);
      detail.setCompletionNotice((notice) => notice ? { ...notice } : { text: t('items.error.action') });
      return false;
    }
  }

  function readThenWrite<R>(write: (etag: string) => Promise<R>): Promise<ReadThenWrite<R>> {
    return trackItemWrite((async () => {
      const read = await getItem(householdId, itemId);
      return read.kind === 'ok' ? { current: read, result: await write(read.etag) } : { current: read };
    })());
  }

  async function finish() {
    const origin = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const focused = origin?.dataset.action === 'detail-done';
    owner.current = busy.tryAcquire('item-detail-completion');
    if (!owner.current) return;
    focusOriginRef.current = origin;
    focusedRowRef.current = focused;
    const key = getPendingCompletion(userId, householdId, itemId) ?? idempotencyKey();
    setPendingCompletion(userId, householdId, itemId, key);
    const token = generationRef.current;
    try {
      const result = await readThenWrite((etag) => completeItem(householdId, itemId, etag, key));
      // The key's lifetime follows the server outcome, not this screen: settle it even after unmount.
      settleCompletionKey(userId, householdId, itemId, key, result);
      if (current(token)) await handleCompletionResult(tools, itemId, key, focused, origin, null, result);
    } finally { busy.release(owner.current); }
  }

  async function undo() {
    const notice: Notice | null = detail.completionNotice;
    if (!notice?.completion || !notice.itemId) return;
    undoOwner.current = busy.tryAcquire('item-detail-undo');
    if (!undoOwner.current) return;
    const token = generationRef.current;
    try {
      const result = await readThenWrite((etag) => undoCompletion(householdId, itemId, notice.completion!.id, etag));
      if (current(token)) await handleUndoResult(tools, notice, itemId, null, result);
    } finally { busy.release(undoOwner.current); }
  }

  async function retryNotice() {
    // The outcome takes focus only if it is still on this control (or nowhere) when it arrives.
    focusOriginRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    retryHadFocusRef.current = focusOriginRef.current?.dataset.action === 'detail-retry';
    if (detail.completionNotice?.retryKind === 'undo') await undo();
    else await finish();
  }

  return {
    finish, undo, retryNotice, reload: async () => { await refreshDetail(); },
    focusAfterUndo: undoFocusRef,
    focusAfterCompletion: { focusedRowRef, retryHadFocusRef, focusOriginRef },
  };
}
