// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import type { SelectHTMLAttributes, Ref } from 'react';
import { cn } from '../../lib/cn';

type SelectProps = SelectHTMLAttributes<HTMLSelectElement> & { ref?: Ref<HTMLSelectElement> };

export function Select({ className, ...props }: SelectProps) {
  const classes = [
    'min-h-11 w-full rounded-xl border border-field bg-raised px-4 text-base text-ink',
    'focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-focus',
    'aria-[invalid=true]:border-danger disabled:opacity-60',
  ].join(' ');
  return <select className={cn(classes, className)} {...props} />;
}
