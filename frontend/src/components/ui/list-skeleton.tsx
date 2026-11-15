import { Skeleton } from './skeleton';

/**
 * Flat placeholder rows in the list's own rhythm hold its place while data loads, so the screen never jumps empty.
 * `actions` also reserves the width a row's buttons take; a read-only list leaves it out.
 */
export function ListSkeleton({ label, actions = false }: { label: string; actions?: boolean }) {
  return <div role="status" className="mt-6 border-t border-line sm:mt-8">
    <span className="sr-only">{label}</span>
    {[0, 1, 2].map((row) => (
      <div key={row} className="flex min-h-16 items-center gap-3 border-b border-line py-3 last:border-b-0">
        <div className="flex-1 space-y-2">
          <Skeleton className="h-3.5 w-32 max-w-[45%]" />
          <Skeleton className="h-3 w-24 max-w-[35%]" />
        </div>
        {actions ? <Skeleton className="h-11 w-28 max-w-[30%]" /> : null}
      </div>
    ))}
  </div>;
}
