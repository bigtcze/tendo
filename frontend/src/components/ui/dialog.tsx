// Adapted from shadcn/ui (MIT) — native dialog semantics, focus return owned here.
import type { ReactNode } from 'react';
import { useEffect, useId, useRef } from 'react';
import { cn } from '../../lib/cn';
import { Button } from './button';

type DialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  closeLabel: string;
  children: ReactNode;
  busy?: boolean;
  triggerRef?: { current: HTMLElement | null };
  sheet?: boolean;
};

export function Dialog({ open, onOpenChange, title, closeLabel, children, busy = false, triggerRef, sheet = false }: DialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const fallbackRef = useRef<HTMLHeadingElement>(null);
  const wasOpen = useRef(false);
  const titleId = useId();

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (open) wasOpen.current = true;
    else if (wasOpen.current) {
      wasOpen.current = false;
      if (triggerRef?.current) {
        dialog.close();
        triggerRef.current.focus();
      } else {
        fallbackRef.current?.focus();
        dialog.close();
      }
    }
  }, [open, triggerRef]);

  const sheetClasses = sheet
    ? [
      'fixed inset-x-0 bottom-0 top-0 m-0 h-dvh max-h-dvh w-full max-w-none rounded-none',
      'sm:inset-auto sm:m-auto sm:h-auto sm:max-h-[90dvh] sm:w-[calc(100%-2rem)] sm:max-w-xl sm:rounded-2xl',
    ].join(' ')
    : '';
  return (
    <dialog
      ref={dialogRef}
      aria-labelledby={titleId}
      onCancel={(event) => {
        event.preventDefault();
        if (!busy) onOpenChange(false);
      }}
      onClose={() => {
        if (busy) dialogRef.current?.showModal();
        else if (open) onOpenChange(false);
      }}
      className={cn('max-h-[min(90dvh,48rem)] w-[calc(100%-2rem)] max-w-xl rounded-2xl border border-line bg-paper p-6 text-ink', sheetClasses)}
    >
      <header className="flex items-start justify-between gap-4">
        <h2 id={titleId} ref={fallbackRef} tabIndex={-1} className="font-display text-h2">{title}</h2>
        <Button
          type="button"
          variant="quiet"
          disabled={busy}
          aria-label={closeLabel}
          onClick={() => onOpenChange(false)}
          className="shrink-0 !p-2"
        >
          {closeLabel}
        </Button>
      </header>
      <div className="mt-4">{children}</div>
    </dialog>
  );
}
