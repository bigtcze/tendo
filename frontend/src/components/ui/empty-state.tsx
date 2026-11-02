// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import type { ReactNode } from 'react';

type EmptyStateProps = { title: string; children?: ReactNode; action?: ReactNode; titleClassName?: string; titleAs?: 'h2' | 'p' };

export function EmptyState({ title, children, action, titleClassName, titleAs = 'h2' }: EmptyStateProps) {
  return (
    <section className="rounded-2xl border border-line bg-raised p-6 text-center">
      {titleAs === 'p' ? <p className={`font-display text-h2 text-ink ${titleClassName ?? ''}`}>{title}</p>
        : <h2 className={`font-display text-h2 text-ink ${titleClassName ?? ''}`}>{title}</h2>}
      {children ? <div className="mt-2 text-base text-muted">{children}</div> : null}
      {action ? <div className="mt-5">{action}</div> : null}
    </section>
  );
}
