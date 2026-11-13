import { useEffect, type RefObject } from 'react';

/**
 * Closes a small popover on Escape, on a pointer press outside it, or when focus moves out of it. Escape returns
 * focus to the trigger so keyboard users are never dropped back at the top of the document; tabbing past the last
 * control closes the popover without stealing the focus the user is moving. A press on the popover's own text or
 * padding keeps it open — that press is inside it, even though it hands focus back to the document.
 */
export function useDismiss(open: boolean, close: () => void, root: RefObject<HTMLElement | null>, trigger: RefObject<HTMLElement | null>) {
  useEffect(() => {
    if (!open) return;
    // Set by a pointer press inside the popover. Such a press moves focus off the popover's own control onto the
    // document body, which on its own looks exactly like focus leaving; one press explains one focusout.
    let pressedInside = false;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') {
        if (event.key === 'Tab') pressedInside = false;
        return;
      }
      close();
      trigger.current?.focus();
    };
    const onPointerDown = (event: PointerEvent) => {
      pressedInside = Boolean(root.current?.contains(event.target as Node));
      if (!pressedInside) close();
    };
    // Tab or Shift+Tab leaving the popover closes it; focus continues to wherever the user sent it. Only a focusout
    // that names a destination outside the popover, or one with no destination after no press of its own, closes it:
    // pressing the panel's text or padding keeps it open, while tabbing off the end of the page still closes it.
    const onFocusOut = (event: FocusEvent) => {
      const next = event.relatedTarget as Node | null;
      if (next) {
        if (!root.current?.contains(next)) close();
        return;
      }
      // A control the page itself removed from a popover (the last "more" row, once its page arrived) sends focus
      // nowhere. The page is about to hand focus to the reader's next place, so this is not the reader leaving.
      if (!(event.target as Node | null)?.isConnected) return;
      if (pressedInside) pressedInside = false;
      else close();
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
