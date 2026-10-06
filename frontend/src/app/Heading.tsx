import { createContext, useContext, useEffect, useRef, type ReactNode, type RefObject } from 'react';

/** True once the user has moved between screens; the first screen on page load keeps natural focus. */
export const ScreenTransitionContext = createContext<RefObject<boolean> | null>(null);

export function useScreenTransitions() {
  return useContext(ScreenTransitionContext);
}

/** Screen heading (h1). After a screen change it receives focus so assistive tech lands on the new screen. */
export function Heading({ className, children }: { className?: string; children: ReactNode }) {
  const transitioned = useScreenTransitions();
  const ref = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    if (transitioned?.current) ref.current?.focus();
  }, [transitioned]);
  return (
    <h1 ref={ref} tabIndex={-1} className={`${className ?? ''} outline-none`}>
      {children}
    </h1>
  );
}
