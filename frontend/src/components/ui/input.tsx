// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import type { InputHTMLAttributes, Ref } from 'react';
import { cn } from '../../lib/cn';

export function Input({ className, ...props }: InputHTMLAttributes<HTMLInputElement> & { ref?: Ref<HTMLInputElement> }) {
  return (
    <input
      className={cn(
        'min-h-11 w-full rounded-xl border border-line bg-white px-4 text-base text-ink placeholder:text-muted focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent aria-[invalid=true]:border-danger disabled:opacity-60',
        className,
      )}
      {...props}
    />
  );
}
