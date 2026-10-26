import { useEffect, type MutableRefObject } from 'react';
import type { ListState } from './subjectsModel';

export function useSubjectsFocus(
  notice: unknown,
  noticeRef: MutableRefObject<HTMLDivElement | null>,
  loadingMore: boolean,
  pagingFocus: MutableRefObject<HTMLElement | null>,
  list: ListState,
  focusKey: MutableRefObject<string | null>,
) {
  const pagingFocusRef = pagingFocus;
  const focusKeyRef = focusKey;
  const noticeRefValue = noticeRef;
  useEffect(() => {
    if (notice) noticeRefValue.current?.focus();
  }, [notice, noticeRefValue]);
  useEffect(() => {
    if (!loadingMore && pagingFocusRef.current) {
      const activated = pagingFocusRef.current;
      const active = document.activeElement;
      if (active === activated || active === document.body || active === null) {
        const first = list.kind === 'ready' ? list.items[0] : undefined;
        if (first) document.querySelector<HTMLElement>(`[data-focus="row:${first.id}"]`)?.focus();
      }
      pagingFocusRef.current = null;
    }
    const key = focusKeyRef.current;
    if (!key) return;
    const el = document.querySelector<HTMLElement>(`[data-focus="${key}"]`);
    if (el) {
      el.focus();
      focusKeyRef.current = null;
    }
  });
}

export function useUnmountGeneration(generation: MutableRefObject<number>) {
  const generationRef = generation;
  useEffect(() => () => { generationRef.current += 1; }, [generationRef]);
}
