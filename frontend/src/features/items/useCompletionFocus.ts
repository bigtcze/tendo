import { useEffect, useRef, type RefObject } from 'react';
import type { ItemGroup } from './itemsApi';
import { groups } from './homeGroups';
import { focusUnclaimed } from './itemsScreenHelpers';
import type { Notice } from './HomeOverview';

export type CompletionFocusRefs = ReturnType<typeof useCompletionFocusRefs>;

/** Focus intents recorded by completion and undo actions; the effects below apply them after render. */
export function useCompletionFocusRefs() {
  const focusedRowRef = useRef(false);
  const retryHadFocusRef = useRef(false);
  const focusOriginRef = useRef<HTMLElement | null>(null);
  const undoFocusRef = useRef<{ itemId: string; group: ItemGroup | null } | null>(null);
  return { focusedRowRef, retryHadFocusRef, focusOriginRef, undoFocusRef };
}

/** Moves focus to a new completion or reused-create notice unless the user moved it meanwhile. */
export function useCompletionNoticeFocus(refs: CompletionFocusRefs, { notice, createNotice, noticeRef }: {
  notice: Notice | null; createNotice: 'uncertain' | 'reused' | null; noticeRef: RefObject<HTMLDivElement | null>;
}) {
  const { focusedRowRef, retryHadFocusRef, focusOriginRef } = refs;
  useEffect(() => {
    if (createNotice === 'reused' || (notice && (focusedRowRef.current || retryHadFocusRef.current))) {
      // A completion outcome may arrive after the user has moved on (for example into the add form); keep their focus.
      if (createNotice === 'reused' || focusUnclaimed(focusOriginRef.current)) noticeRef.current?.focus();
      focusedRowRef.current = false;
      retryHadFocusRef.current = false;
    }
  }, [createNotice, notice, noticeRef, focusedRowRef, retryHadFocusRef, focusOriginRef]);
}

/** After a successful Undo, returns focus to the row's Done once the mutation lock clears. */
export function useUndoFocus(refs: CompletionFocusRefs, { busy, sectionHeadingRefs, noticeRef, addRef }: {
  busy: boolean;
  sectionHeadingRefs: RefObject<Map<ItemGroup, HTMLHeadingElement> | null>;
  noticeRef: RefObject<HTMLDivElement | null>;
  addRef: RefObject<HTMLButtonElement | null>;
}) {
  const { undoFocusRef } = refs;
  useEffect(() => {
    const pending = undoFocusRef.current;
    if (busy || !pending) return;
    undoFocusRef.current = null;
    // The Undo control unmounted with its notice; restore focus only if the user has not moved it meanwhile.
    if (!focusUnclaimed(null)) return;
    const done = document.querySelector<HTMLButtonElement>(`[data-item-id="${pending.itemId}"] [data-action="done"]`);
    const headings = sectionHeadingRefs.current;
    const firstHeading = headings ? groups.map((group) => headings.get(group.id)).find(Boolean) : undefined;
    if (done) done.focus();
    else if (pending.group && headings?.has(pending.group)) headings.get(pending.group)?.focus();
    else if (firstHeading) firstHeading.focus();
    else if (noticeRef.current) noticeRef.current.focus();
    else addRef.current?.focus();
  }, [busy, sectionHeadingRefs, noticeRef, addRef, undoFocusRef]);
}
