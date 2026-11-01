// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import type { ReactNode } from 'react';
import { Label } from './label';

type FieldControlProps = { id: string; 'aria-invalid'?: boolean; 'aria-describedby'?: string };
type FieldProps = {
  id: string;
  label: string;
  hint?: string;
  error?: string;
  children: (props: FieldControlProps) => ReactNode;
};

export function Field({ id, label, hint, error, children }: FieldProps) {
  const describedBy = [hint ? `${id}-hint` : '', error ? `${id}-error` : ''].filter(Boolean).join(' ') || undefined;
  return (
    <div className="space-y-2">
      <Label htmlFor={id}>{label}</Label>
      {children({ id, 'aria-invalid': Boolean(error), 'aria-describedby': describedBy })}
      {hint ? <p id={`${id}-hint`} className="text-meta text-muted">{hint}</p> : null}
      {error ? <p id={`${id}-error`} role="alert" className="text-meta text-danger">{error}</p> : null}
    </div>
  );
}
