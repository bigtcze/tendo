import type { ReactNode } from 'react';
import { cn } from '../../lib/cn';

/**
 * A screen whose content is only on its way: one muted line of text in one status region. Lists that can hold
 * their shape use the skeleton instead, so this is only for a whole screen or a list without rows yet.
 */
export function LoadingLine({ children, className }: { children: ReactNode; className?: string }) {
  return <p role="status" className={cn(className, 'text-body text-muted')}>{children}</p>;
}
