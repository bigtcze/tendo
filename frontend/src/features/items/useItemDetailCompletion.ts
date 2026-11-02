import { useRef } from 'react';
import { useI18n } from '../../i18n';
import { idempotencyKey } from '../../lib/idempotencyKey';
import { completeItem, getItem, trackItemWrite, undoCompletion } from './itemsApi';
import { getPendingCompletion, setPendingCompletion } from './pendingCompletions';
import { handleCompletionResult, handleUndoResult, type CompletionTools, type ReadThenWrite } from './completionOutcomes';
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
    refresh: async () => refreshDetail(),
    setNotice: (notice) => detail.setCompletionNotice(notice),
    undoneText: t('itemDetail.completionUndone'),
  };

  async function refreshDetail() {
    const token = ++generationRef.current;
    const read = await getItem(householdId, itemId);
    if (!current(token)) return;
    if (read.kind === 'unauthenticated') { onSignedOut(); return; }
    if (read.kind === 'notFound') { detail.setState('gone'); return; }
    if (read.kind === 'ok') {
      detail.setItem(read.item);
      // The latest completion (or its undo) belongs at the top of History: return to the newest page.
      history.reset();
      void history.load(token);
    } else detail.setCompletionNotice({ text: t('items.error.action') });
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
    if (detail.completionNotice?.retryKind === 'undo') await undo();
    else await finish();
  }

  return {
    finish, undo, retryNotice,
    focusAfterUndo: undoFocusRef,
    focusAfterCompletion: { focusedRowRef, retryHadFocusRef, focusOriginRef },
  };
}
