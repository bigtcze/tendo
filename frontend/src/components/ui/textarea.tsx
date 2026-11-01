// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import type { TextareaHTMLAttributes, Ref } from 'react';
import { cn } from '../../lib/cn';

type TextareaProps = TextareaHTMLAttributes<HTMLTextAreaElement> & { ref?: Ref<HTMLTextAreaElement> };

export function Textarea({ className, ...props }: TextareaProps) {
  const classes = [
    'min-h-24 w-full rounded-xl border border-field bg-raised px-4 py-3 text-base text-ink placeholder:text-muted',
    'focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-focus',
    'aria-[invalid=true]:border-danger disabled:opacity-60',
  ].join(' ');
  return <textarea className={cn(classes, className)} {...props} />;
}
