import { createContext, useContext, useLayoutEffect, useRef, type ReactNode, type RefObject } from 'react';

/** True once the user has moved between screens; the first screen on page load keeps natural focus. */
export const ScreenTransitionContext = createContext<RefObject<boolean> | null>(null);

export function useScreenTransitions() {
  return useContext(ScreenTransitionContext);
}

/** Screen heading (h1). After a screen change it receives focus so assistive tech lands on the new screen. */
export function Heading({ className, children, headingRef }: {
  className?: string;
  children: ReactNode;
  /**
   * The heading element itself, for a screen that has its own reason to move focus here (for example a notice handed
   * back after a remount). A supplied ref replaces the internal one, so both never point at different elements.
   */
  headingRef?: RefObject<HTMLHeadingElement | null>;
}) {
  const transitioned = useScreenTransitions();
  const ownRef = useRef<HTMLHeadingElement>(null);
  const ref = headingRef ?? ownRef;
  // Layout effect: focus moves in the same commit that inserts the heading, so it can never be
  // observed in the DOM unfocused when React yields before passive effects.
  useLayoutEffect(() => {
    if (transitioned?.current) ref.current?.focus();
  }, [transitioned, ref]);
  // WCAG 2.4.2: the document title names the current screen; the h1 is its single source.
  useLayoutEffect(() => {
    const title = ref.current?.textContent?.trim();
    document.title = title ? `${title} — Tendo` : 'Tendo';
  });
  useLayoutEffect(() => () => { document.title = 'Tendo'; }, []);
  return (
    <h1 ref={ref} tabIndex={-1} className={`${className ?? ''} outline-none`}>
      {children}
    </h1>
  );
}
