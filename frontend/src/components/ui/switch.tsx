// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import type { ButtonHTMLAttributes } from 'react';
import { cn } from '../../lib/cn';

type SwitchProps = Omit<ButtonHTMLAttributes<HTMLButtonElement>, 'role' | 'onChange'> & {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  name?: string;
};

export function Switch({ checked, onCheckedChange, className, name, ...props }: SwitchProps) {
  return (
    <button type="button" role="switch" aria-checked={checked} data-state={checked ? 'checked' : 'unchecked'}
      className={cn(
        'relative inline-flex min-h-11 min-w-11 items-center rounded-full p-1',
        'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus disabled:opacity-60',
        checked ? 'bg-accent' : 'bg-line', className,
      )}
      onClick={() => onCheckedChange(!checked)} {...props}>
      <span aria-hidden="true" className={cn('h-7 w-7 rounded-full bg-raised transition-transform', checked && 'translate-x-3')} />
      {name ? <input type="checkbox" name={name} checked={checked} readOnly hidden /> : null}
    </button>
  );
}
