import { useLayoutEffect, type RefObject } from 'react';

/** Clearance reserved on top of the card's own height so a focused control never touches its edge. */
const GAP = 12;
/** Floor for an unmeasured card (first paint, or a test environment without layout). */
const FLOOR = 72;

/**
 * Reserves the floating notice's height in the page's scroll clearance while it is shown.
 *
 * The card covers the bottom of the viewport, so the last rows need extra room below them: main's padding keeps
 * the list clear of the card when scrolled to the end, and the frame's scroll margin keeps a keyboard-focused row
 * control out from under it (WCAG 2.4.11). Both read `--notice-clearance`, which returns to zero when the notice
 * goes away so the page never keeps the extra space.
 */
export function useNoticeClearance(shown: boolean, cardRef: RefObject<HTMLElement | null>) {
  useLayoutEffect(() => {
    const root = document.documentElement;
    const clear = () => root.style.removeProperty('--notice-clearance');
    if (!shown) {
      clear();
      return clear;
    }
    const card = cardRef.current;
    const apply = () => {
      const measured = card?.getBoundingClientRect().height ?? 0;
      root.style.setProperty('--notice-clearance', `${Math.ceil(Math.max(measured, FLOOR)) + GAP}px`);
    };
    apply();
    if (!card || typeof ResizeObserver === 'undefined') return clear;
    const observer = new ResizeObserver(apply);
    observer.observe(card);
    return () => {
      observer.disconnect();
      clear();
    };
  }, [shown, cardRef]);
}
