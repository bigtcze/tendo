// Adapted from shadcn/ui (MIT) — native dialog semantics, focus return owned here.
import type { ReactNode, RefObject } from 'react';
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
  triggerRef?: RefObject<HTMLElement | null>;
  returnFocusRef?: RefObject<HTMLElement | null>;
  sheet?: boolean;
};

const focusableSelector = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(', ');

export function Dialog({ open, onOpenChange, title, closeLabel, children, busy = false, triggerRef, returnFocusRef, sheet = false }: DialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const fallbackRef = useRef<HTMLHeadingElement>(null);
  const openRef = useRef(open);
  const busyRef = useRef(busy);
  const titleId = useId();

  useEffect(() => {
    openRef.current = open;
    busyRef.current = busy;
  }, [open, busy]);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) {
      dialog.showModal();
      const content = dialog.querySelector('[data-dialog-content]');
      const hasInitialFocus = content?.querySelector(`[autofocus], ${focusableSelector}`);
      if (!hasInitialFocus) fallbackRef.current?.focus();
    } else if (!open && dialog.open) {
      const returnTarget = returnFocusRef?.current ?? triggerRef?.current;
      if (returnTarget?.isConnected) returnTarget.focus();
      dialog.close();
    }
  }, [open, returnFocusRef, triggerRef]);

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
        if (!busyRef.current && openRef.current) onOpenChange(false);
      }}
      onClose={() => {
        const dialog = dialogRef.current;
        if (!dialog?.isConnected) return;
        if (openRef.current) {
          // Stale close from an earlier dismissal: the dialog has already been reopened.
          if (dialog.open) return;
          // A native close (e.g. form method="dialog") while saving is refused.
          if (busyRef.current) {
            dialog.showModal();
            return;
          }
          // Genuine native close while idle: sync the controlled state with the platform.
          onOpenChange(false);
        }
        const target = returnFocusRef?.current ?? triggerRef?.current;
        if (target?.isConnected) target.focus();
      }}
      className={cn(
        'max-h-[min(90dvh,48rem)] w-[calc(100%-2rem)] max-w-xl rounded-2xl border border-line bg-paper p-6 text-ink',
        sheetClasses,
      )}
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
      <div data-dialog-content className="mt-4">{children}</div>
    </dialog>
  );
}
