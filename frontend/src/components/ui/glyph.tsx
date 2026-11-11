import type { ReactNode } from 'react';

/** Shared 24px stroke-icon frame: one stroke width and cap style across the product, hidden from assistive tech. */
export function Glyph({ children, className = 'size-5' }: { children: ReactNode; className?: string }) {
  return (
    <svg aria-hidden="true" focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth="1.75" strokeLinecap="round" strokeLinejoin="round" className={className}>
      {children}
    </svg>
  );
}
