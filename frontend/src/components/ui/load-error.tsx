import type { ButtonHTMLAttributes, ReactNode } from 'react';
import { cn } from '../../lib/cn';
import { Button } from './button';
import { Notice } from './notice';

type RetryProps = Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'onClick'> & Partial<Record<`data-${string}`, string | boolean>>;

type LoadErrorProps = {
  message: ReactNode;
  retryLabel: string;
  onRetry: () => void;
  /**
   * Who owns the live region on this screen: the notice itself by default, or `null` when the screen has its own
   * persistent host (for example a polite region beside a presentational notice).
   */
  role?: 'status' | 'alert' | null;
  className?: string;
  retryProps?: RetryProps;
};

/**
 * One presentation for a list that failed to load: the message, then Retry under it in the same error notice.
 * Screens keep their own wording and live-region ownership; only the shape is shared.
 */
export function LoadError({ message, retryLabel, onRetry, role = 'alert', className, retryProps }: LoadErrorProps) {
  return <Notice tone="error" role={role} aria-atomic="true" className={cn('space-y-3', className)}>
    <p>{message}</p>
    <Button variant="secondary" onClick={onRetry} {...retryProps}>{retryLabel}</Button>
  </Notice>;
}
