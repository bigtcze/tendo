// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import type { ReactNode } from 'react';

type GroupHeaderProps = { children: ReactNode; count?: number };

export function GroupHeader({ children, count }: GroupHeaderProps) {
  return (
    <h2 className="flex items-baseline justify-between border-b border-line pb-2 text-h2 font-medium text-ink">
      <span>{children}</span>
      {count === undefined ? null : <span aria-hidden="true" className="text-meta text-muted">{count}</span>}
    </h2>
  );
}
