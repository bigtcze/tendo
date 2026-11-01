// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import type { HTMLAttributes, ReactNode } from 'react';
import { cn } from '../../lib/cn';

type NoticeProps = HTMLAttributes<HTMLDivElement> & {
  children: ReactNode;
  tone?: 'neutral' | 'success' | 'error';
};

export function Notice({ children, tone = 'neutral', className, ...props }: NoticeProps) {
  const toneClass = tone === 'success'
    ? 'border-success bg-success-bg'
    : tone === 'error' ? 'border-danger bg-danger-bg' : '';
  return (
    <div role={tone === 'error' ? 'alert' : 'status'}
      className={cn('rounded-xl border border-line bg-sand px-4 py-3 text-base text-ink', toneClass, className)} {...props}>
      {children}
    </div>
  );
}
