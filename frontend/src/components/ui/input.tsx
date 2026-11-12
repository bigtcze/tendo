// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import type { InputHTMLAttributes, Ref } from 'react';
import { cn } from '../../lib/cn';

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement> & { ref?: Ref<HTMLInputElement> }) {
  return (
    <input
      className={cn(
        'min-h-11 w-full rounded-lg border border-field bg-raised px-3.5 text-control text-ink transition-colors placeholder:text-muted placeholder:opacity-100 hover:border-muted focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-focus aria-[invalid=true]:border-danger aria-[invalid=true]:hover:border-danger disabled:cursor-not-allowed disabled:opacity-60',
        className,
      )}
      {...props}
    />
  );
}
