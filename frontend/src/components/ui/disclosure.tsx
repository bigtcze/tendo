// Adapted from shadcn/ui (MIT) — https://ui.shadcn.com
import type { ReactNode } from 'react';
import { useId, useState } from 'react';
import { Button } from './button';

export function Disclosure({ title, children, defaultOpen = false }: { title: string; children: ReactNode; defaultOpen?: boolean }) {
  const [open, setOpen] = useState(defaultOpen);
  const id = useId();
  return (
    <section>
      <Button variant="quiet" aria-expanded={open} aria-controls={id} onClick={() => setOpen(!open)}>{title}</Button>
      <div id={id} hidden={!open}>{children}</div>
    </section>
  );
}
