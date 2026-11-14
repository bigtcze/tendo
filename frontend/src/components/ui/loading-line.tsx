import type { ReactNode } from 'react';

/**
 * A screen whose content is only on its way: one muted line of text in one status region. Lists that can hold
 * their shape use the skeleton instead, so this is only for a whole screen or a list without rows yet.
 *
 * The classes are joined rather than merged: tailwind-merge reads `text-body` as a text colour and would drop it
 * next to `text-muted`.
 */
export function LoadingLine({ children, className }: { children: ReactNode; className?: string }) {
  return <p role="status" className={`${className ? `${className} ` : ''}text-body text-muted`}>{children}</p>;
}
