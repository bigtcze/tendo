// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import type { HTMLAttributes, ReactNode } from 'react';
import { cn } from '../../lib/cn';

type Tone = 'neutral' | 'success' | 'error';
type NoticeProps = Omit<HTMLAttributes<HTMLDivElement>, 'role'> & { children: ReactNode; tone?: Tone; live?: boolean; role?: 'status' | 'alert' | null };
type NoticeRegionProps = { message?: string; tone?: Tone; className?: string };

const toneClass = (tone: Tone) => {
  if (tone === 'success') return 'border-success bg-success-bg';
  if (tone === 'error') return 'border-danger bg-danger-bg';
  return '';
};
const noticeClass = 'rounded-xl border border-line bg-sand px-4 py-3 text-base text-ink';

export function Notice({ children, tone = 'neutral', live = true, role: roleOverride, className, ...props }: NoticeProps) {
  const role = roleOverride === null ? undefined : roleOverride ?? (live ? (tone === 'error' ? 'alert' : 'status') : undefined);
  return (
    <div role={role} className={cn(noticeClass, toneClass(tone), className)} {...props}>
      {children}
    </div>
  );
}

/**
 * Persistent live host: it is the only live-region owner, so keep it mounted and update
 * `message` rather than mounting an already-populated region. Announcement timing and
 * verbosity remain up to the user's assistive technology.
 */
export function NoticeRegion({ message = '', tone = 'neutral', className }: NoticeRegionProps) {
  return (
    <div role={tone === 'error' ? 'alert' : 'status'} aria-atomic="true" className={cn('min-h-0', className)}>
      {message ? <div className={cn(noticeClass, toneClass(tone))}>{message}</div> : null}
    </div>
  );
}
