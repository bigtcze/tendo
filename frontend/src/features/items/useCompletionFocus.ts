import { useEffect, useRef, type RefObject } from 'react';
import type { ItemGroup } from './itemsApi';
import { groups } from './homeGroups';
import { focusUnclaimed } from './itemsScreenHelpers';
import type { Notice } from './HomeGroupSection';

export function useCompletionFocus({ notice, createNotice, busy, sectionHeadingRefs, noticeRef, addRef }: {
  notice: Notice | null; createNotice: 'uncertain' | 'reused' | null; busy: boolean;
  sectionHeadingRefs: RefObject<Map<ItemGroup, HTMLHeadingElement> | null>;
  noticeRef: RefObject<HTMLDivElement | null>;
  addRef: RefObject<HTMLButtonElement | null>;
}) {
  const focusedRow = useRef(false);
  const retryHadFocus = useRef(false);
  const focusOrigin = useRef<HTMLElement | null>(null);
  const undoFocus = useRef<{ itemId: string; group: ItemGroup | null } | null>(null);
  useEffect(() => {
    if (createNotice === 'reused' || (notice && (focusedRow.current || retryHadFocus.current))) {
      if (createNotice === 'reused' || focusUnclaimed(focusOrigin.current)) noticeRef.current?.focus();
      focusedRow.current = false;
      retryHadFocus.current = false;
    }
  }, [createNotice, notice, noticeRef]);
  useEffect(() => {
    const pending = undoFocus.current;
    if (busy || !pending) return;
    undoFocus.current = null;
    if (!focusUnclaimed(null)) return;
    const done = document.querySelector<HTMLButtonElement>(`[data-item-id="${pending.itemId}"] [data-action="done"]`);
    const headings = sectionHeadingRefs.current;
    const firstHeading = headings ? groups.map((group) => headings.get(group.id)).find(Boolean) : undefined;
    if (done) done.focus();
    else if (pending.group && headings?.has(pending.group)) headings.get(pending.group)?.focus();
    else if (firstHeading) firstHeading.focus();
    else if (noticeRef.current) noticeRef.current.focus();
    else addRef.current?.focus();
  }, [busy, sectionHeadingRefs, noticeRef, addRef]);
  return { focusedRow, retryHadFocus, focusOrigin, undoFocus };
}
