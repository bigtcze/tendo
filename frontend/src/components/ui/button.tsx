// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import { cva, type VariantProps } from 'class-variance-authority';
import type { ButtonHTMLAttributes, Ref } from 'react';
import { cn } from '../../lib/cn';

const buttonVariants = cva(
  'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl px-5 text-control font-semibold transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus active:translate-y-px disabled:cursor-not-allowed disabled:opacity-60 disabled:active:translate-y-0 motion-reduce:active:translate-y-0',
  {
    variants: {
      variant: {
        primary: 'bg-accent text-on-accent hover:bg-accent-strong active:bg-accent-strong',
        secondary: 'border border-control-border bg-raised text-ink hover:bg-sand active:bg-line',
        quiet: 'text-muted hover:bg-sand hover:text-ink active:bg-line',
      },
      size: {
        default: '',
        small: 'px-3 text-sm',
      },
    },
    defaultVariants: { variant: 'primary', size: 'default' },
  },
);

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
  ref?: Ref<HTMLButtonElement>;
}

type ButtonLook = VariantProps<typeof buttonVariants> & { className?: string };

/**
 * The button look as props for any element: the variant classes plus `data-variant`, which the design metrics and
 * tests read to find a screen's primary action. A link that is drawn as a button (an `<a>` or `NavLink`) spreads
 * these instead of calling `buttonVariants` itself, so it can never carry the look without the marker.
 */
export function buttonLook({ variant, size, className }: ButtonLook = {}) {
  return { 'data-variant': variant ?? 'primary', className: cn(buttonVariants({ variant, size }), className) };
}

export function Button({ className, variant, size, type = 'button', ...props }: ButtonProps) {
  return <button type={type} {...buttonLook({ variant, size, className })} {...props} />;
}
