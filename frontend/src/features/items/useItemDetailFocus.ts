import { useEffect } from 'react';
import type { ItemDetailBusy } from './useItemDetailBusy';
import type { ItemDetailState } from './useItemDetail';

export function useItemDetailFocus({ detail, focusAfterMutationRef, busy, actions, refs: focusRefs }: {
  detail: Pick<ItemDetailState, 'notice' | 'completionNotice' | 'editing' | 'editLoading'>;
  focusAfterMutationRef: { current: boolean };
  busy: ItemDetailBusy;
  actions: {
    focusAfterCompletion: {
      focusedRowRef: { current: boolean };
      retryHadFocusRef: { current: boolean };
      focusOriginRef: { current: HTMLElement | null };
    };
  };
  refs: {
    returnEditFocusRef: { current: boolean };
    editButtonRef: { current: HTMLButtonElement | null };
    noticeRef: { current: HTMLParagraphElement | null };
    completionNoticeRef: { current: HTMLDivElement | null };
  };
}) {
  const { returnEditFocusRef, editButtonRef, noticeRef, completionNoticeRef } = focusRefs;
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
      const { focusedRowRef, retryHadFocusRef, focusOriginRef } = actions.focusAfterCompletion;
      const active = document.activeElement;
      if ((focusedRowRef.current || retryHadFocusRef.current) &&
        (!active || active === document.body || active === focusOriginRef.current)) {
        completionNoticeRef.current?.focus();
      }
      focusedRowRef.current = false;
      retryHadFocusRef.current = false;
    }
  }, [detail.completionNotice, actions.focusAfterCompletion, completionNoticeRef]);
  useEffect(() => {
    if (!detail.editing && !detail.editLoading && !busy.busy && returnEditFocusRef.current) {
      editButtonRef.current?.focus();
      returnEditFocusRef.current = false;
    }
  }, [detail.editing, detail.editLoading, busy.busy, editButtonRef, returnEditFocusRef]);
}
