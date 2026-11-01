import { createContext, useContext, useLayoutEffect, useRef, type ReactNode, type RefObject } from 'react';

/** True once the user has moved between screens; the first screen on page load keeps natural focus. */
export const ScreenTransitionContext = createContext<RefObject<boolean> | null>(null);

/** True for a screen rendered beside the current one (desktop list next to the item pane): it yields the h1, focus and title. */
export const SecondaryScreenContext = createContext(false);

export function useScreenTransitions() {
  return useContext(ScreenTransitionContext);
}

/** Screen heading (h1). After a screen change it receives focus so assistive tech lands on the new screen. */
export function Heading({ className, children }: { className?: string; children: ReactNode }) {
  const transitioned = useScreenTransitions();
  const secondary = useContext(SecondaryScreenContext);
  const ref = useRef<HTMLHeadingElement>(null);
  // Layout effect: focus moves in the same commit that inserts the heading, so it can never be
  // observed in the DOM unfocused when React yields before passive effects.
  useLayoutEffect(() => {
    if (!secondary && transitioned?.current) ref.current?.focus();
  }, [transitioned, secondary]);
  // WCAG 2.4.2: the document title names the current screen; the h1 is its single source.
  useLayoutEffect(() => {
    if (secondary) return;
    const title = ref.current?.textContent?.trim();
    document.title = title ? `${title} — Tendo` : 'Tendo';
  });
  useLayoutEffect(() => () => { if (!secondary) document.title = 'Tendo'; }, [secondary]);
  const Tag = secondary ? 'h2' : 'h1';
  return (
    <Tag ref={ref} tabIndex={-1} className={`${className ?? ''} outline-none`}>
      {children}
    </Tag>
  );
}
