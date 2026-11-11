import type { ReactNode } from 'react';
import { cn } from '../../lib/cn';

/**
 * The one uppercase section micro-label. It is 12 px (0.75rem) so no visible text in the product
 * falls below the floor in `docs/design/v4-design-system.md`; tone only swaps between the two
 * existing label colours, and layout differences (centring, wrapping, hiding) come from `className`.
 */
export function Eyebrow({ children, tone = 'muted', className }: {
  children: ReactNode;
  tone?: 'muted' | 'danger';
  className?: string;
}) {
  return <p className={cn('text-[0.75rem] font-semibold uppercase tracking-[0.12em]', tone === 'danger' ? 'text-danger' : 'text-muted', className)}>{children}</p>;
}
