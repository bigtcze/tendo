// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import { cva, type VariantProps } from 'class-variance-authority';
import type { ButtonHTMLAttributes, Ref } from 'react';
import { cn } from '../../lib/cn';

export const buttonVariants = cva(
  'inline-flex min-h-11 items-center justify-center gap-2 rounded-xl px-5 text-base font-medium transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:cursor-not-allowed disabled:opacity-60',
  {
    variants: {
      variant: {
        primary: 'bg-accent text-on-accent hover:bg-accent-strong',
        secondary: 'border border-muted bg-raised text-ink hover:bg-sand',
        quiet: 'text-muted hover:bg-sand hover:text-ink',
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

export function Button({ className, variant, size, type = 'button', ...props }: ButtonProps) {
  return <button type={type} data-variant={variant ?? 'primary'} className={cn(buttonVariants({ variant, size }), className)} {...props} />;
}
