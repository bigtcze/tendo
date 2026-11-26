// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import type { SelectHTMLAttributes, Ref } from 'react';
import { cn } from '../../lib/cn';

type SelectProps = SelectHTMLAttributes<HTMLSelectElement> & {
  ref?: Ref<HTMLSelectElement>;
  /** Layout classes for the wrapper (margins, flex sizing); `className` styles the select itself. */
  containerClassName?: string;
};

/**
 * The one select in the product. The browser's own arrow is replaced by the same stroke chevron the household and
 * account menus use, so a select reads as part of the field family in every theme instead of a platform widget next
 * to custom controls. The element stays a native `<select>`: keyboard, screen-reader and option-list behaviour are
 * the platform's. The chevron is decoration only and never takes a pointer event.
 */
export function Select({ className, containerClassName, ...props }: SelectProps) {
  const classes = [
    'peer block min-h-11 w-full min-w-0 appearance-none rounded-xl border border-field bg-raised py-0 pl-4 pr-10',
    'text-control font-normal text-ink transition-colors',
    'hover:border-muted focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-focus',
    'aria-[invalid=true]:border-danger aria-[invalid=true]:hover:border-danger disabled:cursor-not-allowed disabled:opacity-60',
  ].join(' ');
  return <span data-select className={cn('relative block min-w-0', containerClassName)}>
    <select className={cn(classes, className)} {...props} />
    <svg aria-hidden="true" focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" data-select-chevron
      className="pointer-events-none absolute right-3.5 top-1/2 size-4 -translate-y-1/2 text-muted peer-disabled:opacity-60">
      <path d="m6 9 6 6 6-6" />
    </svg>
  </span>;
}
