// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import type { LabelHTMLAttributes } from 'react';
import { cn } from '../../lib/cn';

export function Label({ className, ...props }: LabelHTMLAttributes<HTMLLabelElement>) {
  return <label className={cn('block text-meta font-medium text-muted', className)} {...props} />;
}
