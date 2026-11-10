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
  /** Focus a state-specific target after showModal, before generic autofocus/default focus. */
  initialFocusRef?: RefObject<HTMLElement | null>;
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

function focusInitial(dialog: HTMLDialogElement, fallback: HTMLHeadingElement | null, initialFocusRef?: RefObject<HTMLElement | null>) {
  const explicitTarget = initialFocusRef?.current;
  const content = dialog.querySelector('[data-dialog-content]');
  const hasInitialFocus = content?.querySelector(`[autofocus], ${focusableSelector}`);
  const target = explicitTarget?.isConnected && dialog.contains(explicitTarget) ? explicitTarget : null;
  const fallbackTarget = !target && !hasInitialFocus ? fallback : null;
  const focusTarget = target ?? fallbackTarget;
  focusTarget?.focus();
}

export function Dialog({
  open, onOpenChange, title, closeLabel, children, busy = false,
  triggerRef, returnFocusRef, initialFocusRef, sheet = false,
}: DialogProps) {
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
      focusInitial(dialog, fallbackRef.current, initialFocusRef);
    } else if (!open && dialog.open) {
      const returnTarget = returnFocusRef?.current ?? triggerRef?.current;
      if (returnTarget?.isConnected) returnTarget.focus();
      dialog.close();
    }
  }, [open, returnFocusRef, triggerRef, initialFocusRef]);

  const sheetClasses = sheet
    ? [
      'fixed inset-x-0 bottom-0 top-0 m-0 h-dvh max-h-dvh w-full max-w-none rounded-none',
      'md:inset-x-0 md:top-1/2 md:bottom-auto md:mx-auto md:my-0 md:-translate-y-1/2',
      'md:h-auto md:max-h-[90dvh] md:w-[calc(100%-2rem)] md:max-w-xl md:rounded-2xl',
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
        // Tailwind preflight zeroes every margin, which also removes the UA `margin: auto` that centres a
        // modal dialog. Non-sheet dialogs therefore centre themselves explicitly; sheet mode below replaces
        // this with `m-0` (and its own md positioning), and twMerge keeps that override.
        'm-auto max-h-[min(90dvh,48rem)] w-[calc(100%-2rem)] max-w-xl overflow-y-auto rounded-2xl border border-line bg-raised p-4 text-ink shadow-[0_24px_60px_-32px_rgb(7_20_30/0.5)] sm:p-6',
        sheetClasses,
      )}
    >
      <header className="flex min-w-0 items-start justify-between gap-4">
        <h2 id={titleId} ref={fallbackRef} tabIndex={-1} className="min-w-0 break-words font-display text-h2 font-bold">{title}</h2>
        <Button
          type="button"
          variant="quiet"
          disabled={busy}
          aria-label={closeLabel}
          onClick={() => onOpenChange(false)}
          className="min-h-11 shrink-0 !px-2"
        >
          {closeLabel}
        </Button>
      </header>
      <div data-dialog-content className="mt-4">{children}</div>
    </dialog>
  );
}
