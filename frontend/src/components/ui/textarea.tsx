// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import type { TextareaHTMLAttributes, Ref } from 'react';
import { cn } from '../../lib/cn';

type TextareaProps = TextareaHTMLAttributes<HTMLTextAreaElement> & { ref?: Ref<HTMLTextAreaElement> };

export function Textarea({ className, ...props }: TextareaProps) {
  const classes = [
    'min-h-24 w-full rounded-lg border border-field bg-raised px-3.5 py-2.5 text-control text-ink transition-colors placeholder:text-muted',
    'hover:border-muted focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-focus',
    'aria-[invalid=true]:border-danger aria-[invalid=true]:hover:border-danger disabled:cursor-not-allowed disabled:opacity-60',
  ].join(' ');
  return <textarea className={cn(classes, className)} {...props} />;
}
