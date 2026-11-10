// Shared segmented choice used by Account preferences (appearance and language).
import type { ButtonHTMLAttributes, ReactNode, Ref } from 'react';
import { cn } from '../../lib/cn';

/** The selected option is marked by a check glyph and weight/border, not by colour alone. */
function CheckGlyph({ selected }: { selected: boolean }) {
  return (
    <span
      aria-hidden="true"
      className={cn('grid size-4 shrink-0 place-items-center', selected ? 'opacity-100' : 'opacity-0')}
    >
      <svg focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.25"
        strokeLinecap="round" strokeLinejoin="round" className="size-4">
        <path d="m5 12.5 4.5 4.5L19 7" />
      </svg>
    </span>
  );
}

/**
 * `stacked` makes every option a full-width row on narrow screens and a horizontal row from `sm`, where the
 * labels fit. The footer language choice keeps the inline form because English/Čeština always fits.
 */
export function SegmentedGroup({ label, children, className, stacked = false }: {
  label: string;
  children: ReactNode;
  className?: string;
  stacked?: boolean;
}) {
  return (
    <div
      role="group"
      aria-label={label}
      className={cn(
        'gap-1 rounded-xl border border-line bg-paper p-1',
        stacked
          ? 'flex w-full flex-col items-stretch sm:inline-flex sm:w-auto sm:flex-row sm:flex-wrap sm:items-center'
          : 'inline-flex flex-wrap items-center',
        className,
      )}
    >
      {children}
    </div>
  );
}

export function SegmentedOption({ selected, stacked = false, children, className, ...props }: ButtonHTMLAttributes<HTMLButtonElement> & {
  selected: boolean;
  stacked?: boolean;
  ref?: Ref<HTMLButtonElement>;
}) {
  return (
    <button
      type="button"
      aria-pressed={selected}
      className={cn(
        'inline-flex min-h-11 items-center gap-1.5 rounded-lg border px-3 text-small font-semibold transition-colors',
        'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus',
        selected
          ? 'border-line bg-raised text-ink'
          : 'border-transparent text-muted hover:bg-sand hover:text-ink',
        stacked ? 'w-full justify-start sm:w-auto' : '',
        className,
      )}
      {...props}
    >
      <CheckGlyph selected={selected} />
      {children}
    </button>
  );
}
