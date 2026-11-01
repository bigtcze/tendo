// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import type { InputHTMLAttributes } from 'react';
import { cn } from '../../lib/cn';

type SwitchProps = Omit<InputHTMLAttributes<HTMLInputElement>, 'type' | 'role' | 'checked' | 'onChange'> & {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
};

export function Switch({ checked, onCheckedChange, className, ...props }: SwitchProps) {
  return (
    <span data-switch-root className="relative inline-flex min-h-11 w-14 items-center">
      <input
        type="checkbox"
        role="switch"
        aria-checked={checked}
        checked={checked}
        onChange={(event) => onCheckedChange(event.currentTarget.checked)}
        className={cn(
          // Covers the whole visible track width and stays at least 44px tall.
          'peer absolute inset-0 z-10 m-0 h-full w-full cursor-pointer appearance-none rounded-full opacity-0',
          'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:cursor-not-allowed',
          className,
        )}
        {...props}
      />
      <span
        aria-hidden="true"
        className={cn(
          'pointer-events-none relative inline-flex h-8 w-14 items-center rounded-full p-1 transition-colors',
          'bg-field peer-checked:bg-accent peer-checked:[&>span]:translate-x-6',
          'peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-focus',
          'peer-disabled:opacity-60 motion-reduce:transition-none',
        )}
      >
        <span className="h-6 w-6 rounded-full bg-raised transition-transform motion-reduce:transition-none" />
      </span>
    </span>
  );
}
