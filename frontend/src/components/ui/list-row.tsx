// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import type { ReactNode } from 'react';
import { Button } from './button';

export function DoneControl({ label, done, onToggle }: { label: string; done: boolean; onToggle: () => void }) {
  return (
    <Button type="button" variant="quiet" aria-label={label} aria-pressed={done} onClick={onToggle}
      className="h-11 w-11 shrink-0 rounded-full border border-control-border !p-0">
      {done ? <span aria-hidden="true">✓</span> : null}
    </Button>
  );
}

export function ListRow({ title, metadata, children }: { title: string; metadata?: ReactNode; children?: ReactNode }) {
  return (
    <article className="flex min-h-16 items-center gap-3 border-b border-line py-3">
      <div className="min-w-0 flex-1">
        <h3 className="text-row-title font-medium text-ink">{title}</h3>
        {metadata ? <div className="mt-1 text-meta text-muted">{metadata}</div> : null}
      </div>
      {children}
    </article>
  );
}
