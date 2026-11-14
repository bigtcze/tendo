// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import type { ReactNode } from 'react';

type EmptyStateProps = { title: string; children?: ReactNode; action?: ReactNode };

/**
 * A list with nothing in it: a plain section under the screen title, like every other flat section, rather than a
 * decorative card. The title uses the section heading scale and the explanation stays quiet text.
 */
export function EmptyState({ title, children, action }: EmptyStateProps) {
  return (
    <section>
      <h2 className="text-h2 text-ink">{title}</h2>
      {children ? <div className="mt-1 max-w-prose text-body text-muted">{children}</div> : null}
      {action ? <div className="mt-4">{action}</div> : null}
    </section>
  );
}
