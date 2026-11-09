// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import type { ReactNode } from 'react';

type GroupHeaderProps = { children: ReactNode; count?: number };

export function GroupHeader({ children, count }: GroupHeaderProps) {
  return (
    <h2 className="flex items-baseline justify-between gap-3 text-h2 font-bold text-ink">
      <span className="min-w-0">{children}</span>
      {count === undefined ? null : <span aria-hidden="true" className="rounded-full bg-sand px-2.5 py-0.5 text-meta font-semibold text-muted">{count}</span>}
    </h2>
  );
}
