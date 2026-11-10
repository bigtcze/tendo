import { useEffect, type RefObject } from 'react';

/**
 * After a pager click the list heading receives focus once the new page settles, unless the user already
 * moved focus somewhere else (for example to the pager itself, which is replaced by the new page).
 */
export function usePagedHeadingFocus(pageFocusRef: RefObject<HTMLElement | null>, headingRef: RefObject<HTMLElement | null>, items: unknown, busy: boolean) {
  useEffect(() => {
    if (!pageFocusRef.current || busy) return;
    const active = document.activeElement;
    if (active === pageFocusRef.current || active === document.body || active === null) headingRef.current?.focus();
    pageFocusRef.current = null;
  }, [items, busy, pageFocusRef, headingRef]);
}
