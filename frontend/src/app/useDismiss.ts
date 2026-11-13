import { useEffect, type RefObject } from 'react';

/**
 * Closes a small popover on Escape, on a pointer press outside it, or when focus moves out of it. Escape returns
 * focus to the trigger so keyboard users are never dropped back at the top of the document; tabbing past the last
 * control closes the popover without stealing the focus the user is moving.
 */
export function useDismiss(open: boolean, close: () => void, root: RefObject<HTMLElement | null>, trigger: RefObject<HTMLElement | null>) {
  useEffect(() => {
    if (!open) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return;
      close();
      trigger.current?.focus();
    };
    const onPointerDown = (event: PointerEvent) => {
      if (root.current?.contains(event.target as Node)) return;
      close();
    };
    // Tab or Shift+Tab leaving the popover closes it; focus continues to wherever the user sent it. Browsers report
    // where focus went as the related target; when they do not, the element that actually holds focus decides.
    const onFocusOut = (event: FocusEvent) => {
      const next = event.relatedTarget as Node | null;
      const inside = next ? root.current?.contains(next) : root.current?.contains(document.activeElement);
      if (!inside) close();
    };
    document.addEventListener('keydown', onKeyDown);
    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('focusout', onFocusOut);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('focusout', onFocusOut);
    };
  }, [open, close, root, trigger]);
}
