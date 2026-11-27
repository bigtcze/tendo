// Adapted from shadcn/ui (MIT) — native dialog semantics, focus return owned here.
import type { ReactNode, RefObject } from 'react';
import { useEffect, useId, useRef } from 'react';
import { useI18n } from '../../i18n';
import { cn } from '../../lib/cn';
import { Button } from './button';
import { Glyph } from './glyph';

type DialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  /** Overrides the default `dialog.close` name; only fixtures that need a distinct name pass one. */
  closeLabel?: string;
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

/**
 * Spans the dialog's padding edge to edge so the pinned band covers the content scrolling beneath it. Sticky insets
 * are measured from the scrollport's content edge, so each band is offset by the dialog padding to reach its border.
 * A short viewport (landscape phone, 400% zoom) unpins both: two bands would cover nearly all of its height.
 */
const BLEED = '-mx-4 bg-raised px-4 sm:-mx-6 sm:px-6';
const HEADER = `sticky short:static -top-4 sm:-top-6 z-20 -mt-4 pt-4 pb-3 sm:-mt-6 sm:pt-6 ${BLEED}`;
const ACTIONS = `sticky short:static -bottom-4 sm:-bottom-6 z-20 -mb-4 flex flex-wrap items-center gap-2 border-t border-line pt-4 pb-4 sm:-mb-6 sm:pb-6 ${BLEED}`;

export function Dialog({
  open, onOpenChange, title, closeLabel, children, busy = false,
  triggerRef, returnFocusRef, initialFocusRef, sheet = false,
}: DialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const fallbackRef = useRef<HTMLHeadingElement>(null);
  const openRef = useRef(open);
  const busyRef = useRef(busy);
  const titleId = useId();
  const close = () => onOpenChange(false);

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
      // Centred by layout (inset 0, auto margins, fit-content height), never `translate(-50%)`: layout offsets are
      // pixel-snapped at paint, a fractional transform is not, and it left the pinned header's edge between device rows.
      'md:inset-0 md:m-auto',
      'md:h-fit md:max-h-[90dvh] md:w-[calc(100%-2rem)] md:max-w-xl md:rounded-2xl',
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
        // Keyboard focus scrolled into view lands between the pinned header and a pinned DialogActions bar.
        'scroll-pt-24 scroll-pb-28 short:scroll-p-0',
        sheetClasses,
      )}
    >
      {/* The header stays pinned while a long form scrolls, so the title and the close control never leave view. */}
      <header className={`${HEADER} flex min-w-0 items-start justify-between gap-4`}>
        <h2 id={titleId} ref={fallbackRef} tabIndex={-1} className="min-w-0 break-words text-h2 font-bold text-ink">{title}</h2>
        {/* One close control for every dialog: a 44px icon named for what it does, so a form's own Cancel is the only Cancel. */}
        {closeLabel
          ? <CloseControl label={closeLabel} busy={busy} onClose={close} />
          : <TranslatedCloseControl busy={busy} onClose={close} />}
      </header>
      <div data-dialog-content className="mt-1">{children}</div>
    </dialog>
  );
}

/**
 * A form's primary action bar, pinned to the bottom of the dialog while the form scrolls, so a long form
 * (Repeat on, a 412 conflict) keeps Save in view; short viewports unpin it (see ACTIONS). It must be the dialog's
 * last content.
 */
export function DialogActions({ children }: { children: ReactNode }) {
  return <div data-dialog-actions className={ACTIONS}>{children}</div>;
}

type CloseControlProps = { busy: boolean; onClose: () => void };

/**
 * The dialog's single close control. It keeps its own boundary because an icon-only control is audited as a
 * non-text control: the border-field token reaches 3:1 against the raised dialog surface in both themes, while a
 * borderless quiet control reads as an invisible 1.00:1 target.
 */
function CloseControl({ label, busy, onClose }: CloseControlProps & { label: string }) {
  return (
    <Button
      type="button"
      variant="quiet"
      disabled={busy}
      aria-label={label}
      onClick={onClose}
      className="size-11 shrink-0 border border-control-border !px-0"
    >
      <Glyph><path d="M6 6l12 12M18 6 6 18" /></Glyph>
    </Button>
  );
}

/**
 * Resolves the translated default name only when a caller supplies none, so provider-free fixtures (the primitives
 * harness, unit tests of dialog mechanics) can still pass an explicit label.
 */
function TranslatedCloseControl(props: CloseControlProps) {
  const { t } = useI18n();
  return <CloseControl label={t('dialog.close')} {...props} />;
}
