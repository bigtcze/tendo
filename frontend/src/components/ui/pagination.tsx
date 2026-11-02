// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import { Button } from './button';

type PaginationProps = {
  canPrevious: boolean;
  canNext: boolean;
  hideNext?: boolean;
  range: string;
  label: string;
  previousLabel: string;
  nextLabel: string;
  onPrevious: () => void;
  onNext: () => void;
};

export function Pagination({ canPrevious, canNext, hideNext = false, range, label, previousLabel, nextLabel, onPrevious, onNext }: PaginationProps) {
  return (
    <nav aria-label={label} className="flex flex-wrap items-center justify-between gap-2">
      <Button type="button" variant="quiet" disabled={!canPrevious} onClick={onPrevious}>{previousLabel}</Button>
      <p aria-live="polite" aria-atomic="true" className="text-meta text-muted">{range}</p>
      {hideNext ? null : <Button type="button" variant="quiet" disabled={!canNext} onClick={onNext}>{nextLabel}</Button>}
    </nav>
  );
}
