import { useEffect } from 'react';
import type { ItemDetailBusy } from './useItemDetailBusy';
import type { ItemDetailState } from './useItemDetail';
import { focusUnclaimed } from './itemsScreenHelpers';

export function useItemDetailFocus({ detail, focusAfterMutationRef, busy, actions, refs: focusRefs }: {
  detail: Pick<ItemDetailState, 'notice' | 'completionNotice' | 'editing' | 'editLoading' | 'stale'>;
  focusAfterMutationRef: { current: boolean };
  busy: ItemDetailBusy;
  actions: {
    focusAfterCompletion: {
      focusedRowRef: { current: boolean };
      retryHadFocusRef: { current: boolean };
      focusOriginRef: { current: HTMLElement | null };
    };
    undoFocusRef: { current: { itemId: string; group: import('./itemsApi').ItemGroup | null } | null };
  };
  refs: {
    returnEditFocusRef: { current: boolean };
    editButtonRef: { current: HTMLButtonElement | null };
    noticeRef: { current: HTMLParagraphElement | null };
    completionNoticeRef: { current: HTMLDivElement | null };
  };
}) {
  const { returnEditFocusRef, editButtonRef, noticeRef, completionNoticeRef } = focusRefs;
  const { undoFocusRef } = actions;
  const { focusedRowRef, retryHadFocusRef, focusOriginRef } = actions.focusAfterCompletion;
  useEffect(() => {
    if (focusAfterMutationRef.current && detail.notice) {
      noticeRef.current?.focus();
      focusAfterMutationRef.current = false;
    }
  }, [detail.notice, focusAfterMutationRef, noticeRef]);
  useEffect(() => {
    if (detail.notice === 'itemDetail.updated') {
      noticeRef.current?.focus();
      returnEditFocusRef.current = false;
    }
  }, [detail.notice, noticeRef, returnEditFocusRef]);
  useEffect(() => {
    if (detail.completionNotice) {
      const active = document.activeElement;
      if ((focusedRowRef.current || retryHadFocusRef.current || detail.stale) &&
        (!active || active === document.body || active === focusOriginRef.current)) {
        completionNoticeRef.current?.focus();
      }
      focusedRowRef.current = false;
      retryHadFocusRef.current = false;
    }
  }, [detail.completionNotice, detail.stale, focusedRowRef, retryHadFocusRef, focusOriginRef, completionNoticeRef]);
  useEffect(() => {
    const pending = undoFocusRef.current;
    if (busy.busy || !pending) return;
    undoFocusRef.current = null;
    if (!focusUnclaimed(null)) return;
    const done = document.querySelector<HTMLButtonElement>('[data-action="detail-done"]');
    if (done) done.focus();
    else completionNoticeRef.current?.focus();
  }, [busy.busy, undoFocusRef, completionNoticeRef]);
  useEffect(() => {
    if (!detail.editing && !detail.editLoading && !busy.busy && returnEditFocusRef.current) {
      editButtonRef.current?.focus();
      returnEditFocusRef.current = false;
    }
  }, [detail.editing, detail.editLoading, busy.busy, editButtonRef, returnEditFocusRef]);
}
