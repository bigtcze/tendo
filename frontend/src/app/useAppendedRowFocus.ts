import { useCallback, useEffect, useRef, type RefObject } from 'react';

/**
 * Keeps focus on the list after Show more: the button stays focusable while its page loads (`aria-disabled`), and
 * when the last page takes the button away, focus moves to the first row that page added, or its first control.
 * Call `mark` with the row count just before Show more.
 */
export function useAppendedRowFocus(listRef: RefObject<HTMLUListElement | null>, count: number, loadingMore: boolean) {
  const marked = useRef<number | null>(null);
  useEffect(() => {
    if (marked.current === null || loadingMore) return;
    const first = marked.current;
    marked.current = null;
    const lost = !document.activeElement || document.activeElement === document.body;
    if (!lost || count <= first) return;
    const row = listRef.current?.children[first] as HTMLElement | undefined;
    (row?.querySelector<HTMLElement>('button, a[href]') ?? row)?.focus();
  }, [count, loadingMore, listRef]);
  return useCallback((rows: number) => { marked.current = rows; }, []);
}
